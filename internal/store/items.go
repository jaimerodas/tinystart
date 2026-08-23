package store

import (
	"context"
	"database/sql"
	"net/url"
	"time"
)

// Item is a tile: a title and a URL at a position inside a group. It owns its
// own title rather than pointing at a shared bookmark record — the same page
// can be called different things in two different groups.
type Item struct {
	ID         int64
	GroupID    int64
	Title      string
	URL        string
	Position   int
	VisitCount int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

const itemColumns = `id, start_page_group_id, title, url, position, visit_count, created_at, updated_at`

// Link is one entry of the JSON the start page embeds so the command bar can
// filter tiles without a round trip. The field order is the order Rails' hash
// literal had, because encoding/json writes fields in declaration order. The
// rewrite compares the two documents byte for byte.
type Link struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	ID    int64  `json:"id"`
}

// CreateItem adds a tile at the bottom of a group. Like CreateGroup, the form
// says nothing about position: it sits at the bottom of the group, and that is
// the answer.
func (db *DB) CreateItem(ctx context.Context, userID, groupID int64, title, itemURL string) (*Item, error) {
	var item *Item
	err := db.tx(ctx, func(tx *sql.Tx) error {
		if err := groupBelongsTo(ctx, tx, userID, groupID); err != nil {
			return err
		}

		fields, err := itemErrors(ctx, tx, groupID, 0, title, itemURL)
		if err != nil {
			return err
		}
		if err := invalid(fields...); err != nil {
			return err
		}

		position, err := nextPositionInGroup(ctx, tx, groupID)
		if err != nil {
			return err
		}

		now := utcNow()
		result, err := tx.ExecContext(ctx,
			`INSERT INTO start_page_items (start_page_group_id, title, url, position, visit_count, created_at, updated_at)
			 VALUES (?, ?, ?, ?, 0, ?, ?)`,
			groupID, title, itemURL, position, railsTime(now), railsTime(now))
		if err != nil {
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}

		item = &Item{ID: id, GroupID: groupID, Title: title, URL: itemURL,
			Position: position, CreatedAt: now, UpdatedAt: now}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return item, nil
}

// UpdateItem fixes a title or a URL. There is no metadata to re-fetch behind a
// tile, so a typo has to be fixable in place.
func (db *DB) UpdateItem(ctx context.Context, userID, itemID int64, title, itemURL string) (*Item, error) {
	var item *Item
	err := db.tx(ctx, func(tx *sql.Tx) error {
		existing, err := scanItem(tx.QueryRowContext(ctx, itemByIDQuery, itemID, userID))
		if err != nil {
			return err
		}

		fields, err := itemErrors(ctx, tx, existing.GroupID, itemID, title, itemURL)
		if err != nil {
			return err
		}
		if err := invalid(fields...); err != nil {
			return err
		}

		now := utcNow()
		if _, err := tx.ExecContext(ctx,
			`UPDATE start_page_items SET title = ?, url = ?, updated_at = ? WHERE id = ?`,
			title, itemURL, railsTime(now), itemID); err != nil {
			return err
		}

		existing.Title = title
		existing.URL = itemURL
		existing.UpdatedAt = now
		item = existing
		return nil
	})
	if err != nil {
		return nil, err
	}
	return item, nil
}

// itemByIDQuery reaches the owner through the group, because items have no
// user_id of their own. Every lookup goes through it, so that an id in a URL
// only ever addresses one person's tile.
const itemByIDQuery = `SELECT ` + itemColumns + ` FROM start_page_items
	WHERE id = ? AND start_page_group_id IN (SELECT id FROM start_page_groups WHERE user_id = ?)`

func (db *DB) ItemByID(ctx context.Context, userID, itemID int64) (*Item, error) {
	return scanItem(db.sql.QueryRowContext(ctx, itemByIDQuery, itemID, userID))
}

// ItemsInGroup is one group's tiles in the order they are drawn.
func (db *DB) ItemsInGroup(ctx context.Context, groupID int64) ([]Item, error) {
	rows, err := db.sql.QueryContext(ctx,
		`SELECT `+itemColumns+` FROM start_page_items WHERE start_page_group_id = ? ORDER BY position`,
		groupID)
	return collectItems(rows, err)
}

// LinksForCommandBar is every tile the user owns, flattened.
//
// Rails asked for these through a has_many :through with no order at all and
// took whatever the join gave it. That was group by group in id order, and
// inside each group rowid by rowid. This says so out loud instead of leaving
// it to a query plan. But it says the same thing, id and id, because the
// order is what somebody sees their suggestions in. A rewrite is not the
// place to change it. It is creation order, not drawing order: a tile dragged
// to the top of its group stays where it is in this list.
//
// The first version ordered the tiles by position, on the assumption that
// drawing order was what the join gave. The parity harness proved otherwise
// against a start page that had been rearranged.
func (db *DB) LinksForCommandBar(ctx context.Context, userID int64) ([]Link, error) {
	rows, err := db.sql.QueryContext(ctx,
		`SELECT i.title, i.url, i.id
		 FROM start_page_items i
		 JOIN start_page_groups g ON g.id = i.start_page_group_id
		 WHERE g.user_id = ?
		 ORDER BY g.id, i.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Not nil: the page serialises this straight to JSON, and a nil slice
	// serialises as the literal null rather than [].
	links := []Link{}
	for rows.Next() {
		var link Link
		if err := rows.Scan(&link.Title, &link.URL, &link.ID); err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, rows.Err()
}

// MoveItem moves a tile within its own group.
//
// Its idea of a position is not MoveItemToGroup's. This code preserves the
// difference instead of smoothing it over. Here the destination index is "the
// first tile whose position is at least the one asked for", computed with the
// tile still in the list. A position past the end appends. Positions always
// stay compacted, so for the editor — which sends the index the row already
// occupies in the DOM — the two agree. On a list with gaps in it, the two
// part company, and that is the behavior this code uses.
func (db *DB) MoveItem(ctx context.Context, userID, itemID int64, position int) error {
	return db.tx(ctx, func(tx *sql.Tx) error {
		item, err := scanItem(tx.QueryRowContext(ctx, itemByIDQuery, itemID, userID))
		if err != nil {
			return err
		}

		items, err := itemsInGroup(ctx, tx, item.GroupID)
		if err != nil {
			return err
		}

		from := -1
		to := len(items)
		for i, candidate := range items {
			if candidate.ID == itemID {
				from = i
			}
			if to == len(items) && candidate.Position >= position {
				to = i
			}
		}
		// The tile was in another group, or another request deleted it between
		// the lookup and here.
		if from == -1 {
			return ErrNotFound
		}

		// The loop above computes the index with the tile still in the list,
		// so the index can be one past the end without it. Clamping is the
		// last step. Rails did not clamp, and a position past the last tile
		// (position=99 on a group of three) padded the array with a nil and
		// then raised NoMethodError on it. The editor never sends one — it
		// sends the index the row already occupies — so this fixes a 500 that
		// only a hand-written request can reach.
		remaining := append(items[:from:from], items[from+1:]...)
		moved := insertAt(remaining, *item, clamp(to, 0, len(remaining)))
		return writeItemPositions(ctx, tx, moved)
	})
}

// MoveItemToGroup moves a tile into a group at a position clamped into range,
// and closes the gap it leaves in the group it came from.
//
// The target group can already hold this URL, which is the one refusal a move
// can meet. The tile stays where it was, and the editor redraws both groups
// from what is actually stored.
func (db *DB) MoveItemToGroup(ctx context.Context, userID, itemID, groupID int64, position int) error {
	return db.tx(ctx, func(tx *sql.Tx) error {
		item, err := scanItem(tx.QueryRowContext(ctx, itemByIDQuery, itemID, userID))
		if err != nil {
			return err
		}
		if err := groupBelongsTo(ctx, tx, userID, groupID); err != nil {
			return err
		}

		source := item.GroupID
		changingGroup := source != groupID

		if changingGroup {
			taken, err := exists(ctx, tx,
				`SELECT 1 FROM start_page_items WHERE start_page_group_id = ? AND url = ?`,
				groupID, item.URL)
			if err != nil {
				return err
			}
			if taken {
				return invalid(FieldError{"url", msgTaken})
			}

			// A real save rather than a bare position write, so updated_at
			// moves: changing which group a tile is in is an edit, while
			// shuffling it up and down inside one is not.
			if _, err := tx.ExecContext(ctx,
				`UPDATE start_page_items SET start_page_group_id = ?, updated_at = ? WHERE id = ?`,
				groupID, railsTime(utcNow()), itemID); err != nil {
				return err
			}
			item.GroupID = groupID
		}

		neighbours, err := itemsInGroupExcept(ctx, tx, groupID, itemID)
		if err != nil {
			return err
		}
		ordered := insertAt(neighbours, *item, clamp(position, 0, len(neighbours)))
		if err := writeItemPositions(ctx, tx, ordered); err != nil {
			return err
		}

		if changingGroup {
			return reorderItemsInGroup(ctx, tx, source)
		}
		return nil
	})
}

// ReorderItemsInGroup numbers a group's tiles 0, 1, 2…, closing any gaps.
func (db *DB) ReorderItemsInGroup(ctx context.Context, groupID int64) error {
	return db.tx(ctx, func(tx *sql.Tx) error {
		return reorderItemsInGroup(ctx, tx, groupID)
	})
}

// DeleteItem removes a tile and closes the gap it leaves, as one unit.
func (db *DB) DeleteItem(ctx context.Context, userID, itemID int64) error {
	return db.tx(ctx, func(tx *sql.Tx) error {
		item, err := scanItem(tx.QueryRowContext(ctx, itemByIDQuery, itemID, userID))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM start_page_items WHERE id = ?`, itemID); err != nil {
			return err
		}
		return reorderItemsInGroup(ctx, tx, item.GroupID)
	})
}

// IncrementVisitCount is fire and forget from the grid: bump the counter and
// say nothing back. No validations, no updated_at — following a link is not an
// edit of the tile.
func (db *DB) IncrementVisitCount(ctx context.Context, userID, itemID int64) error {
	return db.update(ctx,
		`UPDATE start_page_items SET visit_count = visit_count + 1
		 WHERE id = ? AND start_page_group_id IN (SELECT id FROM start_page_groups WHERE user_id = ?)`,
		itemID, userID)
}

// itemErrors runs the model's validations in declaration order. The URL check
// is a separate `validate` declared last, so its message comes after the
// title's even though both are about fields above it on the form.
func itemErrors(ctx context.Context, tx *sql.Tx, groupID, excludeID int64, title, itemURL string) ([]FieldError, error) {
	var fields []FieldError

	if itemURL == "" {
		fields = append(fields, FieldError{"url", msgBlank})
	} else {
		taken, err := exists(ctx, tx,
			`SELECT 1 FROM start_page_items WHERE start_page_group_id = ? AND url = ? AND id != ?`,
			groupID, itemURL, excludeID)
		if err != nil {
			return nil, err
		}
		if taken {
			fields = append(fields, FieldError{"url", msgTaken})
		}
	}

	if title == "" {
		fields = append(fields, FieldError{"title", msgBlank})
	}

	if itemURL != "" && !isWebURL(itemURL) {
		fields = append(fields, FieldError{"url", "must be a valid URL"})
	}

	return fields, nil
}

// isWebURL is Rails' valid_url: parse it, and accept it only if what comes out
// is an http or https URL. Go's parser is far more forgiving than Ruby's — it
// will happily read "not a url at all" as a relative path. So the scheme is
// what does the work here, exactly as the URI::HTTP check did there.
//
// A host is not required, because Ruby did not require one either: "http://"
// is a URI::HTTP, and Ruby accepted it.
func isWebURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

// groupBelongsTo is the scoping check every write through a group makes.
func groupBelongsTo(ctx context.Context, tx *sql.Tx, userID, groupID int64) error {
	found, err := exists(ctx, tx,
		`SELECT 1 FROM start_page_groups WHERE id = ? AND user_id = ?`, groupID, userID)
	if err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	return nil
}

// nextPositionInGroup asks where the last tile is rather than counting them.
// Positions can carry a gap while someone drags a tile away, and counting
// instead drops the new tile on top of one.
func nextPositionInGroup(ctx context.Context, tx *sql.Tx, groupID int64) (int, error) {
	var highest sql.NullInt64
	err := tx.QueryRowContext(ctx,
		`SELECT MAX(position) FROM start_page_items WHERE start_page_group_id = ?`, groupID).Scan(&highest)
	if err != nil {
		return 0, err
	}
	if !highest.Valid {
		return 0, nil
	}
	return int(highest.Int64) + 1, nil
}

func reorderItemsInGroup(ctx context.Context, tx *sql.Tx, groupID int64) error {
	items, err := itemsInGroup(ctx, tx, groupID)
	if err != nil {
		return err
	}
	return writeItemPositions(ctx, tx, items)
}

// writeItemPositions renumbers a list from zero. It skips every tile that is
// already in its correct position. This leaves updated_at alone for the same
// reason it is on groups: a neighbour that moves past a tile is not an edit
// of that tile.
func writeItemPositions(ctx context.Context, tx *sql.Tx, items []Item) error {
	for i, item := range items {
		if item.Position == i {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE start_page_items SET position = ? WHERE id = ?`, i, item.ID); err != nil {
			return err
		}
	}
	return nil
}

func itemsInGroup(ctx context.Context, tx *sql.Tx, groupID int64) ([]Item, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT `+itemColumns+` FROM start_page_items WHERE start_page_group_id = ? ORDER BY position`,
		groupID)
	return collectItems(rows, err)
}

func itemsInGroupExcept(ctx context.Context, tx *sql.Tx, groupID, itemID int64) ([]Item, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT `+itemColumns+` FROM start_page_items
		 WHERE start_page_group_id = ? AND id != ? ORDER BY position`, groupID, itemID)
	return collectItems(rows, err)
}

func collectItems(rows *sql.Rows, err error) ([]Item, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func scanItem(row scanner) (*Item, error) {
	var item Item
	err := row.Scan(&item.ID, &item.GroupID, &item.Title, &item.URL, &item.Position,
		&item.VisitCount, (*railsTime)(&item.CreatedAt), (*railsTime)(&item.UpdatedAt))
	if err != nil {
		return nil, notFound(err)
	}
	return &item, nil
}
