package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Group is a named box of tiles at a column and a position on someone's start
// page. Column is 1-based, because that is how the grid is talked about.
// Position is 0-based and always compacted, so a position is an index.
type Group struct {
	ID        int64
	UserID    int64
	Name      string
	Column    int
	Position  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// "column" is quoted everywhere it appears: it is a reserved word in SQL, and
// SQLite will accept it bare in some places and not others. Quoting it once
// here and in every query is cheaper than finding out which.
const groupColumns = `id, user_id, name, "column", position, created_at, updated_at`

// CreateGroup adds a group at the bottom of a column. The form that calls it
// sits at the bottom of that column and says nothing about position — where
// the group lands follows from the column alone.
func (db *DB) CreateGroup(ctx context.Context, userID int64, name string, column int) (*Group, error) {
	var group *Group
	err := db.tx(ctx, func(tx *sql.Tx) error {
		fields, err := groupErrors(ctx, tx, userID, 0, name, column)
		if err != nil {
			return err
		}
		if err := invalid(fields...); err != nil {
			return err
		}

		position, err := nextPositionInColumn(ctx, tx, userID, column)
		if err != nil {
			return err
		}

		now := utcNow()
		result, err := tx.ExecContext(ctx,
			`INSERT INTO start_page_groups (user_id, name, "column", position, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			userID, name, column, position, railsTime(now), railsTime(now))
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}

		group = &Group{ID: id, UserID: userID, Name: name, Column: column,
			Position: position, CreatedAt: now, UpdatedAt: now}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return group, nil
}

// UpdateGroup renames a group. Only the name: the edit form does not offer the
// column, because someone moves a group by dragging it or by walking it there
// with the keyboard, and both of those go through MoveGroup.
func (db *DB) UpdateGroup(ctx context.Context, userID, groupID int64, name string) (*Group, error) {
	var group *Group
	err := db.tx(ctx, func(tx *sql.Tx) error {
		existing, err := scanGroup(tx.QueryRowContext(ctx,
			`SELECT `+groupColumns+` FROM start_page_groups WHERE id = ? AND user_id = ?`,
			groupID, userID))
		if err != nil {
			return err
		}

		fields, err := groupErrors(ctx, tx, userID, groupID, name, existing.Column)
		if err != nil {
			return err
		}
		if err := invalid(fields...); err != nil {
			return err
		}

		now := utcNow()
		if _, err := tx.ExecContext(ctx,
			`UPDATE start_page_groups SET name = ?, updated_at = ? WHERE id = ?`,
			name, railsTime(now), groupID); err != nil {
			return err
		}

		existing.Name = name
		existing.UpdatedAt = now
		group = existing
		return nil
	})
	if err != nil {
		return nil, err
	}
	return group, nil
}

// GroupByID is always scoped to a user. Every controller looks a group up
// through current_user for the same reason: an id from a URL is a number
// anyone can type.
func (db *DB) GroupByID(ctx context.Context, userID, groupID int64) (*Group, error) {
	return scanGroup(db.sql.QueryRowContext(ctx,
		`SELECT `+groupColumns+` FROM start_page_groups WHERE id = ? AND user_id = ?`,
		groupID, userID))
}

// StartPageCounts is how many groups a page has and how many tiles are in
// them — the two numbers Settings puts above everything else, links first.
//
// Two subqueries rather than a join with two COUNT(DISTINCT …): a group with
// no tiles has to count as a group. The join that gets that right is harder
// to read than the two questions asked separately.
func (db *DB) StartPageCounts(ctx context.Context, userID int64) (groups, items int, err error) {
	err = db.sql.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(*) FROM start_page_groups WHERE user_id = ?),
		        (SELECT COUNT(*) FROM start_page_items
		         WHERE start_page_group_id IN
		               (SELECT id FROM start_page_groups WHERE user_id = ?))`,
		userID, userID).Scan(&groups, &items)
	return groups, items, err
}

// GroupsByColumn is the whole grid in one query, bucketed the way the page
// draws it. Columns with nothing in them are simply absent from the map. That
// is what ranging over the user's column count and looking each one up
// expects.
func (db *DB) GroupsByColumn(ctx context.Context, userID int64) (map[int][]Group, error) {
	groups, err := db.queryGroups(ctx,
		`SELECT `+groupColumns+` FROM start_page_groups WHERE user_id = ?
		 ORDER BY "column", position`, userID)
	if err != nil {
		return nil, err
	}

	byColumn := make(map[int][]Group)
	for _, group := range groups {
		byColumn[group.Column] = append(byColumn[group.Column], group)
	}
	return byColumn, nil
}

// GroupsInColumn is one column, in order — what a Turbo Stream redraws after a
// move.
func (db *DB) GroupsInColumn(ctx context.Context, userID int64, column int) ([]Group, error) {
	return db.queryGroups(ctx,
		`SELECT `+groupColumns+` FROM start_page_groups WHERE user_id = ? AND "column" = ?
		 ORDER BY position`, userID, column)
}

// MoveGroup puts a group at a position in a column, renumbering the groups it
// lands between and closing the gap it leaves behind.
//
// Writing an absolute position without shifting anyone leaves two groups
// sharing it, and SQLite then decides the order however it likes. The problem
// stayed invisible while the only way to move something was to swap it with a
// neighbour. It becomes obvious the moment a drag can drop something anywhere.
//
// A position past the end of the column appends. A negative one goes to the
// top. Clamping rather than refusing is deliberate: the client sends the index
// the node already occupies in its own DOM, and it is allowed to be optimistic.
func (db *DB) MoveGroup(ctx context.Context, userID, groupID int64, column, position int) error {
	return db.tx(ctx, func(tx *sql.Tx) error {
		group, err := scanGroup(tx.QueryRowContext(ctx,
			`SELECT `+groupColumns+` FROM start_page_groups WHERE id = ? AND user_id = ?`,
			groupID, userID))
		if err != nil {
			return err
		}

		// The renumbering below writes positions straight out, skipping the
		// validations an ordinary save runs. So this code makes sure that the
		// bounds hold here, and nowhere else. A group parked outside the grid
		// renders nowhere and has no controls left to bring it back.
		if column < 1 {
			return invalid(FieldError{"column", "must be greater than 0"})
		}
		limit, err := userColumnCount(ctx, tx, userID)
		if err != nil {
			return err
		}
		if column > limit {
			return invalid(FieldError{"column", columnLimitMessage(limit)})
		}

		neighbours, err := queryGroups(ctx, tx,
			`SELECT `+groupColumns+` FROM start_page_groups
			 WHERE user_id = ? AND "column" = ? AND id != ? ORDER BY position`,
			userID, column, groupID)
		if err != nil {
			return err
		}

		source := group.Column
		if source != column {
			if _, err := tx.ExecContext(ctx,
				`UPDATE start_page_groups SET "column" = ? WHERE id = ?`, column, groupID); err != nil {
				return err
			}
		}

		ordered := insertAt(neighbours, *group, clamp(position, 0, len(neighbours)))
		for i, neighbour := range ordered {
			if err := writeGroupPosition(ctx, tx, neighbour, i); err != nil {
				return err
			}
		}

		if source != column {
			return reorderGroupsInColumn(ctx, tx, userID, source)
		}
		return nil
	})
}

// ReorderGroupsInColumn numbers a column's groups 0, 1, 2…, closing any gaps.
// All of the positions land or none of them do.
func (db *DB) ReorderGroupsInColumn(ctx context.Context, userID int64, column int) error {
	return db.tx(ctx, func(tx *sql.Tx) error {
		return reorderGroupsInColumn(ctx, tx, userID, column)
	})
}

// DeleteGroup removes a group, its tiles, and the gap it leaves in its column,
// as one unit. The tiles go first because the foreign key is on, which is the
// same order `dependent: :destroy` produced.
func (db *DB) DeleteGroup(ctx context.Context, userID, groupID int64) error {
	return db.tx(ctx, func(tx *sql.Tx) error {
		group, err := scanGroup(tx.QueryRowContext(ctx,
			`SELECT `+groupColumns+` FROM start_page_groups WHERE id = ? AND user_id = ?`,
			groupID, userID))
		if err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx,
			`DELETE FROM start_page_items WHERE start_page_group_id = ?`, groupID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM start_page_groups WHERE id = ?`, groupID); err != nil {
			return err
		}
		return reorderGroupsInColumn(ctx, tx, userID, group.Column)
	})
}

// groupErrors runs the model's validations in the order they are declared, so
// that the messages come out in the order the editor prints them. excludeID is
// the group being renamed, or 0 when there is not one yet.
func groupErrors(ctx context.Context, tx *sql.Tx, userID, excludeID int64, name string, column int) ([]FieldError, error) {
	var fields []FieldError

	if name == "" {
		fields = append(fields, FieldError{"name", msgBlank})
	} else {
		taken, err := exists(ctx, tx,
			`SELECT 1 FROM start_page_groups WHERE user_id = ? AND name = ? AND id != ?`,
			userID, name, excludeID)
		if err != nil {
			return nil, err
		}
		if taken {
			fields = append(fields, FieldError{"name", msgTaken})
		}
	}

	// A column of zero or less fails the numericality check and never reaches
	// the limit check, which is why this is an else and not a second if —
	// Rails reports one message here, not two.
	if column < 1 {
		fields = append(fields, FieldError{"column", "must be greater than 0"})
	} else {
		limit, err := userColumnCount(ctx, tx, userID)
		if err != nil {
			return nil, err
		}
		if column > limit {
			fields = append(fields, FieldError{"column", columnLimitMessage(limit)})
		}
	}

	return fields, nil
}

func columnLimitMessage(limit int) string {
	return fmt.Sprintf("cannot exceed start page column limit of %d", limit)
}

// userColumnCount is how wide this user's grid is. The call happens inside
// the transaction that needs it, so a column count changing underneath a move
// cannot let a group past the edge.
func userColumnCount(ctx context.Context, tx *sql.Tx, userID int64) (int, error) {
	var columns int
	err := tx.QueryRowContext(ctx, `SELECT "columns" FROM users WHERE id = ?`, userID).Scan(&columns)
	return columns, notFound(err)
}

// nextPositionInColumn asks where the last group is rather than counting
// them. Positions can carry a gap for as long as a request is in flight.
// Counting puts the new group on top of an existing one.
func nextPositionInColumn(ctx context.Context, tx *sql.Tx, userID int64, column int) (int, error) {
	var highest sql.NullInt64
	err := tx.QueryRowContext(ctx,
		`SELECT MAX(position) FROM start_page_groups WHERE user_id = ? AND "column" = ?`,
		userID, column).Scan(&highest)
	if err != nil {
		return 0, err
	}
	if !highest.Valid {
		return 0, nil
	}
	return int(highest.Int64) + 1, nil
}

func reorderGroupsInColumn(ctx context.Context, tx *sql.Tx, userID int64, column int) error {
	groups, err := queryGroups(ctx, tx,
		`SELECT `+groupColumns+` FROM start_page_groups WHERE user_id = ? AND "column" = ?
		 ORDER BY position`, userID, column)
	if err != nil {
		return err
	}
	for i, group := range groups {
		if err := writeGroupPosition(ctx, tx, group, i); err != nil {
			return err
		}
	}
	return nil
}

// writeGroupPosition writes a position, and only when it is not already the
// one on disk. Reordering a column that is already in order must cost no
// writes at all.
//
// updated_at stays untouched on purpose. Rails renumbered with update_column,
// which skips callbacks, so a group's timestamp records when it was last
// renamed, not when someone dragged a neighbour past it.
func writeGroupPosition(ctx context.Context, tx *sql.Tx, group Group, position int) error {
	if group.Position == position {
		return nil
	}
	_, err := tx.ExecContext(ctx,
		`UPDATE start_page_groups SET position = ? WHERE id = ?`, position, group.ID)
	return err
}

func (db *DB) queryGroups(ctx context.Context, query string, args ...any) ([]Group, error) {
	rows, err := db.sql.QueryContext(ctx, query, args...)
	return collectGroups(rows, err)
}

func queryGroups(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]Group, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	return collectGroups(rows, err)
}

func collectGroups(rows *sql.Rows, err error) ([]Group, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []Group
	for rows.Next() {
		group, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, *group)
	}
	return groups, rows.Err()
}

func scanGroup(row scanner) (*Group, error) {
	var group Group
	err := row.Scan(&group.ID, &group.UserID, &group.Name, &group.Column, &group.Position,
		(*railsTime)(&group.CreatedAt), (*railsTime)(&group.UpdatedAt))
	if err != nil {
		return nil, notFound(err)
	}
	return &group, nil
}

// clamp and insertAt are the two moves both MoveGroup and MoveItemToGroup
// make, kept here so the two read the same way.
func clamp(value, low, high int) int {
	return min(max(value, low), high)
}

func insertAt[T any](items []T, item T, index int) []T {
	ordered := make([]T, 0, len(items)+1)
	ordered = append(ordered, items[:index]...)
	ordered = append(ordered, item)
	return append(ordered, items[index:]...)
}
