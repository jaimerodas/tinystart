package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"github.com/jaimerodas/tinystart/internal/startpage"
)

// The two ends of Settings → Import & Export: a user's whole start page out as
// a startpage.Layout, and a startpage.Layout back in over the page they have.
//
// This file is the only place the two packages meet, and the dependency points
// this way on purpose. startpage is the format — parsing, validating, writing
// YAML — and knows nothing about a database. store knows what a layout means
// in SQL. The other direction creates a cycle, and it also means you cannot
// reason about the format without a database in the room.

// StartPageLayout reads a user's page as the exporter wants it: columns in
// order, groups in position order inside them, tiles in position order inside
// those. Width is the user's own column count, which can be wider than the
// widest column that holds anything — that difference is what the export
// header warns about.
//
// One query for the whole grid rather than one per group: the page is a few
// dozen rows, and the tiles have to be sorted by group anyway.
func (db *DB) StartPageLayout(ctx context.Context, userID int64) (startpage.Layout, error) {
	var layout startpage.Layout
	if err := db.sql.QueryRowContext(ctx,
		`SELECT "columns" FROM users WHERE id = ?`, userID).Scan(&layout.Width); err != nil {
		return startpage.Layout{}, notFound(err)
	}

	rows, err := db.sql.QueryContext(ctx,
		`SELECT g."column", g.id, g.name, i.title, i.url
		 FROM start_page_groups g
		 LEFT JOIN start_page_items i ON i.start_page_group_id = g.id
		 WHERE g.user_id = ?
		 ORDER BY g."column", g.position, i.position`, userID)
	if err != nil {
		return startpage.Layout{}, err
	}
	defer rows.Close()

	// The join repeats a group once per tile, so this code answers both "is
	// this a new column" and "is this a new group" by looking at the row
	// before.
	var groupID int64
	for rows.Next() {
		var (
			column     int
			id         int64
			name       string
			title, url sql.NullString
		)
		if err := rows.Scan(&column, &id, &name, &title, &url); err != nil {
			return startpage.Layout{}, err
		}

		if len(layout.Columns) == 0 || layout.Columns[len(layout.Columns)-1].Number != column {
			layout.Columns = append(layout.Columns, startpage.Column{Number: column})
		}
		last := &layout.Columns[len(layout.Columns)-1]

		if id != groupID {
			last.Groups = append(last.Groups, startpage.Group{Name: name})
			groupID = id
		}
		// A group with no tiles is one row with nothing on its right-hand side.
		if title.Valid {
			group := &last.Groups[len(last.Groups)-1]
			group.Items = append(group.Items, startpage.Item{Title: title.String, URL: url.String})
		}
	}
	return layout, rows.Err()
}

// ReplaceStartPage rebuilds a user's page from a layout, in one transaction.
//
// It replaces rather than merges — it deletes the groups and builds the page
// again from the file — which is what makes export, hand-edit, import again
// idempotent. Merging has to invent answers for renamed groups and removed
// tiles that nobody needs.
//
// Because it replaces, a refusal has to change nothing: every validation runs
// inside the same transaction as the delete, so a file that fails on its last
// tile leaves the page exactly as it was.
//
// A rejected record comes back as a *RejectedError, whose message names the
// group or the tile and repeats what the model said about it. Anything else
// means the database refused the write.
func (db *DB) ReplaceStartPage(ctx context.Context, userID int64, layout startpage.Layout) error {
	if layout.Width < 1 {
		return invalid(FieldError{"columns", "must be greater than 0"})
	}
	if layout.Width > MaxColumns {
		return invalid(FieldError{"columns", fmt.Sprintf("must be less than or equal to %d", MaxColumns)})
	}

	// Ascending, because this code makes sure that a group's column fits the
	// page's width. The first group with a repeated name is also the one the
	// refusal must name. Both facts depend on the order things are created
	// in, and the caller must not control that order.
	columns := slices.Clone(layout.Columns)
	slices.SortFunc(columns, func(a, b startpage.Column) int { return a.Number - b.Number })

	return db.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM start_page_items WHERE start_page_group_id IN
			 (SELECT id FROM start_page_groups WHERE user_id = ?)`, userID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM start_page_groups WHERE user_id = ?`, userID); err != nil {
			return err
		}

		// The width goes in before the first group. users.columns starts at 1,
		// and validation refuses a group whose column is past it. Without
		// that order, a file using column 3 fails on its very first group.
		// Narrowing is safe here for the mirror-image reason: this code
		// already deleted the groups a narrower page cannot show.
		now := utcNow()
		result, err := tx.ExecContext(ctx,
			`UPDATE users SET "columns" = ?, updated_at = ? WHERE id = ?`,
			layout.Width, railsTime(now), userID)
		if err != nil {
			return err
		}
		if err := mustHaveChanged(result); err != nil {
			return err
		}

		for _, column := range columns {
			for position, group := range column.Groups {
				if err := writeGroup(ctx, tx, userID, column.Number, position, group); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// writeGroup creates one group and its tiles. Positions are the indexes
// themselves. Because ReplaceStartPage empties the page first, counting from
// zero and creating in file order reproduces the file's order with no
// arithmetic. That is exactly what the models' place_at_end_of_column and
// place_at_end_of_group did.
func writeGroup(ctx context.Context, tx *sql.Tx, userID int64, column, position int, group startpage.Group) error {
	fields, err := groupErrors(ctx, tx, userID, 0, group.Name, column)
	if err != nil {
		return err
	}
	if len(fields) > 0 {
		return &RejectedError{Group: group.Name, Errors: fields}
	}

	now := utcNow()
	result, err := tx.ExecContext(ctx,
		`INSERT INTO start_page_groups (user_id, name, "column", position, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		userID, group.Name, column, position, railsTime(now), railsTime(now))
	if err != nil {
		return err
	}
	groupID, err := result.LastInsertId()
	if err != nil {
		return err
	}

	for position, item := range group.Items {
		fields, err := itemErrors(ctx, tx, groupID, 0, item.Title, item.URL)
		if err != nil {
			return err
		}
		if len(fields) > 0 {
			return &RejectedError{Group: group.Name, Title: item.Title, URL: item.URL, Errors: fields}
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO start_page_items
				(start_page_group_id, title, url, position, visit_count, created_at, updated_at)
			 VALUES (?, ?, ?, ?, 0, ?, ?)`,
			groupID, item.Title, item.URL, position, railsTime(now), railsTime(now)); err != nil {
			return err
		}
	}
	return nil
}

// RejectedError is one record an import failed to write, worded the way the
// import's own message was: it names the group or the tile, because "Name has
// already been taken" on its own does not say which of forty tiles it is about.
type RejectedError struct {
	// Group is the group's name, or the name of the group a tile belongs to.
	Group string
	// Title and URL are the tile's, and are empty when the group itself, not
	// a tile, was rejected.
	Title string
	URL   string
	// Errors is what the record's validations said, in ActiveRecord's order.
	Errors ValidationError
}

func (e *RejectedError) Error() string {
	messages := toSentence(e.Errors.FullMessages())
	if e.Title == "" && e.URL == "" {
		return fmt.Sprintf(`the group "%s" was rejected: %s`, e.Group, messages)
	}
	return fmt.Sprintf(`the link "%s" (%s) in "%s" was rejected: %s`, e.Title, e.URL, e.Group, messages)
}

// Unwrap lets a caller reach past the sentence to the field errors, so that
// errors.As(err, &store.ValidationError{}) works on this the way it does on
// every other refusal in the package.
func (e *RejectedError) Unwrap() error { return e.Errors }
