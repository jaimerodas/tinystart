package store

import "testing"

func TestCreateItemValidations(t *testing.T) {
	tests := []struct {
		name  string
		title string
		url   string
		want  []string
	}{
		{"no url", "One", "", []string{"Url can't be blank"}},
		{"no title", "", "https://example.com/one", []string{"Title can't be blank"}},
		{"a url that is not http or https", "One", "ftp://example.com/thing",
			[]string{"Url must be a valid URL"}},
		{"a url with no scheme at all", "One", "example.com",
			[]string{"Url must be a valid URL"}},
		{"something that is not a url", "One", "not a url at all",
			[]string{"Url must be a valid URL"}},
		// The URL check is a separate validation declared after the others, so
		// its message comes last however the form is laid out.
		{"neither", "", "ftp://example.com/thing",
			[]string{"Title can't be blank", "Url must be a valid URL"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t)
			user := newUser(t, db, "test@example.com")
			group := newGroup(t, db, user.ID, "Test Group", 1)

			_, err := db.CreateItem(t.Context(), user.ID, group.ID, test.title, test.url)
			assertInvalid(t, err, test.want...)
		})
	}
}

func TestCreateItemAcceptsHTTPAndHTTPS(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	group := newGroup(t, db, user.ID, "Test Group", 1)

	for _, url := range []string{
		"https://example.com",
		"http://example.com/with/a/path?and=a&query#fragment",
		"http://localhost:3000",
	} {
		if _, err := db.CreateItem(t.Context(), user.ID, group.ID, "Title "+url, url); err != nil {
			t.Errorf("CreateItem(%q): %v", url, err)
		}
	}
}

func TestCreateItemURLIsUniquePerGroup(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	group := newGroup(t, db, user.ID, "Test Group", 1)
	other := newGroup(t, db, user.ID, "Other Group", 2)
	newItem(t, db, user.ID, group.ID, "One", "https://example.com/one")

	_, err := db.CreateItem(t.Context(), user.ID, group.ID,
		"A different title, same destination", "https://example.com/one")
	assertInvalid(t, err, "Url has already been taken")

	// The same page can live in two groups, under two names.
	if _, err := db.CreateItem(t.Context(), user.ID, other.ID, "One", "https://example.com/one"); err != nil {
		t.Errorf("the same url in another group was refused: %v", err)
	}
}

func TestCreateItemAppendsToItsGroup(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	group := newGroup(t, db, user.ID, "Test Group", 1)

	newItem(t, db, user.ID, group.ID, "One", "https://example.com/one")
	second := newItem(t, db, user.ID, group.ID, "Two", "https://example.com/two")
	if second.Position != 1 {
		t.Errorf("appended at %d, want 1", second.Position)
	}

	// Counting the tiles is not the same as asking where the last one is.
	if _, err := db.sql.ExecContext(t.Context(),
		`UPDATE start_page_items SET position = 5 WHERE id = ?`, second.ID); err != nil {
		t.Fatalf("opening a gap: %v", err)
	}
	third := newItem(t, db, user.ID, group.ID, "Three", "https://example.com/three")
	if third.Position != 6 {
		t.Errorf("appended at %d, want 6", third.Position)
	}
}

func TestUpdateItem(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	group := newGroup(t, db, user.ID, "Test Group", 1)
	item := newItem(t, db, user.ID, group.ID, "One", "https://example.com/one")
	newItem(t, db, user.ID, group.ID, "Two", "https://example.com/two")

	updated, err := db.UpdateItem(t.Context(), user.ID, item.ID, "Uno", "https://example.com/uno")
	if err != nil {
		t.Fatalf("UpdateItem: %v", err)
	}
	if updated.Title != "Uno" || updated.URL != "https://example.com/uno" {
		t.Errorf("stored %q / %q", updated.Title, updated.URL)
	}

	// Its own url is not "already taken".
	if _, err := db.UpdateItem(t.Context(), user.ID, item.ID, "Uno", "https://example.com/uno"); err != nil {
		t.Errorf("saving a tile unchanged: %v", err)
	}

	_, err = db.UpdateItem(t.Context(), user.ID, item.ID, "Uno", "https://example.com/two")
	assertInvalid(t, err, "Url has already been taken")

	_, err = db.UpdateItem(t.Context(), user.ID, item.ID, "Uno", "ftp://example.com")
	assertInvalid(t, err, "Url must be a valid URL")
}

func TestItemScopedToItsOwner(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	other := newUser(t, db, "other@example.com")
	group := newGroup(t, db, user.ID, "Mine", 1)
	item := newItem(t, db, user.ID, group.ID, "One", "https://example.com/one")

	_, err := db.ItemByID(t.Context(), other.ID, item.ID)
	assertNotFound(t, err)
	assertNotFound(t, db.DeleteItem(t.Context(), other.ID, item.ID))
	assertNotFound(t, db.MoveItem(t.Context(), other.ID, item.ID, 0))
	assertNotFound(t, db.IncrementVisitCount(t.Context(), other.ID, item.ID))

	_, err = db.UpdateItem(t.Context(), other.ID, item.ID, "Theirs", "https://example.com/theirs")
	assertNotFound(t, err)
}

// MoveItem's idea of a position is "the first tile whose position is at least
// this one", computed with the tile still in the list. On compacted positions
// — which is all the editor ever sends — that is the index it lands on.
func TestMoveItemWithinItsGroup(t *testing.T) {
	tests := []struct {
		name     string
		move     string
		position int
		want     []string
	}{
		{"to the front", "Three", 0, []string{"Three", "One", "Two"}},
		{"to the back", "One", 2, []string{"Two", "Three", "One"}},
		{"into the middle", "One", 1, []string{"Two", "One", "Three"}},
		{"past the end, which appends", "One", 99, []string{"Two", "Three", "One"}},
		{"to where it already is", "One", 0, []string{"One", "Two", "Three"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t)
			user := newUser(t, db, "test@example.com")
			group := newGroup(t, db, user.ID, "Test Group", 1)
			items := map[string]*Item{}
			for _, title := range []string{"One", "Two", "Three"} {
				items[title] = newItem(t, db, user.ID, group.ID, title,
					"https://example.com/"+title)
			}

			if err := db.MoveItem(t.Context(), user.ID, items[test.move].ID, test.position); err != nil {
				t.Fatalf("MoveItem: %v", err)
			}

			assertEqualStrings(t, itemTitles(t, db, group.ID), test.want)
			assertEqualInts(t, itemPositions(t, db, group.ID), []int{0, 1, 2})
		})
	}
}

// The move buttons are gone, but the request they made is still the one the
// keyboard and the drag handles make, and it can arrive for a tile another
// request has already deleted.
func TestMoveItemThatIsNoLongerThere(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	group := newGroup(t, db, user.ID, "Test Group", 1)
	item := newItem(t, db, user.ID, group.ID, "One", "https://example.com/one")

	if _, err := db.sql.ExecContext(t.Context(),
		`DELETE FROM start_page_items WHERE id = ?`, item.ID); err != nil {
		t.Fatalf("deleting: %v", err)
	}

	assertNotFound(t, db.MoveItem(t.Context(), user.ID, item.ID, 0))
}

// A drag drops a tile at a chosen slot in the target group, so the position it
// lands on is usually already taken.
func TestMoveItemToAnotherGroup(t *testing.T) {
	tests := []struct {
		name     string
		position int
		want     []string
	}{
		{"at the top", 0, []string{"W", "X", "Y", "Z"}},
		{"in the middle", 1, []string{"X", "W", "Y", "Z"}},
		{"past the end, which appends", 99, []string{"X", "Y", "Z", "W"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t)
			user := newUser(t, db, "test@example.com")
			source := newGroup(t, db, user.ID, "Source", 1)
			target := newGroup(t, db, user.ID, "Target", 2)
			for _, title := range []string{"X", "Y", "Z"} {
				newItem(t, db, user.ID, target.ID, title, "https://example.com/"+title)
			}
			travelling := newItem(t, db, user.ID, source.ID, "W", "https://example.com/w")

			if err := db.MoveItemToGroup(t.Context(), user.ID, travelling.ID,
				target.ID, test.position); err != nil {
				t.Fatalf("MoveItemToGroup: %v", err)
			}

			assertEqualStrings(t, itemTitles(t, db, target.ID), test.want)
			assertEqualInts(t, itemPositions(t, db, target.ID), []int{0, 1, 2, 3})
		})
	}
}

// The group a tile leaves has to close up behind it, or the next tile added
// there takes a position that is already in use.
func TestMoveItemClosesTheGapItLeaves(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	source := newGroup(t, db, user.ID, "Source", 1)
	target := newGroup(t, db, user.ID, "Target", 2)
	newItem(t, db, user.ID, source.ID, "A", "https://example.com/a")
	middle := newItem(t, db, user.ID, source.ID, "B", "https://example.com/b")
	newItem(t, db, user.ID, source.ID, "C", "https://example.com/c")

	if err := db.MoveItemToGroup(t.Context(), user.ID, middle.ID, target.ID, 0); err != nil {
		t.Fatalf("MoveItemToGroup: %v", err)
	}

	assertEqualStrings(t, itemTitles(t, db, source.ID), []string{"A", "C"})
	assertEqualInts(t, itemPositions(t, db, source.ID), []int{0, 1})
}

func TestMoveItemToAGroupThatAlreadyHasTheURL(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	source := newGroup(t, db, user.ID, "Source", 1)
	target := newGroup(t, db, user.ID, "Target", 2)
	newItem(t, db, user.ID, target.ID, "Mine", "https://example.com/w")
	travelling := newItem(t, db, user.ID, source.ID, "W", "https://example.com/w")

	err := db.MoveItemToGroup(t.Context(), user.ID, travelling.ID, target.ID, 0)
	assertInvalid(t, err, "Url has already been taken")

	// Refused, and nothing moved: the editor redraws both groups from what is
	// actually stored.
	assertEqualStrings(t, itemTitles(t, db, target.ID), []string{"Mine"})
	assertEqualStrings(t, itemTitles(t, db, source.ID), []string{"W"})
}

func TestMoveItemToAGroupThatIsNotYours(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	other := newUser(t, db, "other@example.com")
	mine := newGroup(t, db, user.ID, "Mine", 1)
	theirs := newGroup(t, db, other.ID, "Theirs", 1)
	item := newItem(t, db, user.ID, mine.ID, "One", "https://example.com/one")

	assertNotFound(t, db.MoveItemToGroup(t.Context(), user.ID, item.ID, theirs.ID, 0))
}

// Changing which group a tile is in is an edit; shuffling it up and down
// inside one is not, and Rails' update_column skipped the callbacks that would
// have said otherwise.
func TestWhichMovesTouchUpdatedAt(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	source := newGroup(t, db, user.ID, "Source", 1)
	target := newGroup(t, db, user.ID, "Target", 2)
	first := newItem(t, db, user.ID, source.ID, "One", "https://example.com/one")
	second := newItem(t, db, user.ID, source.ID, "Two", "https://example.com/two")

	if err := db.MoveItem(t.Context(), user.ID, second.ID, 0); err != nil {
		t.Fatalf("MoveItem: %v", err)
	}
	moved, err := db.ItemByID(t.Context(), user.ID, first.ID)
	if err != nil {
		t.Fatalf("ItemByID: %v", err)
	}
	if !moved.UpdatedAt.Equal(first.UpdatedAt) {
		t.Errorf("a reposition moved updated_at to %v, was %v", moved.UpdatedAt, first.UpdatedAt)
	}

	if err := db.MoveItemToGroup(t.Context(), user.ID, second.ID, target.ID, 0); err != nil {
		t.Fatalf("MoveItemToGroup: %v", err)
	}
	travelled, err := db.ItemByID(t.Context(), user.ID, second.ID)
	if err != nil {
		t.Fatalf("ItemByID: %v", err)
	}
	if !travelled.UpdatedAt.After(second.UpdatedAt) {
		t.Errorf("a change of group left updated_at at %v", travelled.UpdatedAt)
	}
}

func TestReorderItemsInGroup(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	group := newGroup(t, db, user.ID, "Test Group", 1)
	newItem(t, db, user.ID, group.ID, "One", "https://example.com/one")
	second := newItem(t, db, user.ID, group.ID, "Two", "https://example.com/two")
	third := newItem(t, db, user.ID, group.ID, "Three", "https://example.com/three")

	for id, position := range map[int64]int{second.ID: 4, third.ID: 9} {
		if _, err := db.sql.ExecContext(t.Context(),
			`UPDATE start_page_items SET position = ? WHERE id = ?`, position, id); err != nil {
			t.Fatalf("opening gaps: %v", err)
		}
	}

	if err := db.ReorderItemsInGroup(t.Context(), group.ID); err != nil {
		t.Fatalf("ReorderItemsInGroup: %v", err)
	}
	assertEqualInts(t, itemPositions(t, db, group.ID), []int{0, 1, 2})
	assertEqualStrings(t, itemTitles(t, db, group.ID), []string{"One", "Two", "Three"})

	// And nothing at all when it is already in order.
	before := totalChanges(t, db)
	if err := db.ReorderItemsInGroup(t.Context(), group.ID); err != nil {
		t.Fatalf("ReorderItemsInGroup: %v", err)
	}
	if after := totalChanges(t, db); after != before {
		t.Errorf("%d rows were written, want none", after-before)
	}
}

func TestDeleteItem(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	group := newGroup(t, db, user.ID, "Test Group", 1)
	first := newItem(t, db, user.ID, group.ID, "One", "https://example.com/one")
	newItem(t, db, user.ID, group.ID, "Two", "https://example.com/two")
	newItem(t, db, user.ID, group.ID, "Three", "https://example.com/three")

	if err := db.DeleteItem(t.Context(), user.ID, first.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}

	assertEqualStrings(t, itemTitles(t, db, group.ID), []string{"Two", "Three"})
	assertEqualInts(t, itemPositions(t, db, group.ID), []int{0, 1})
	assertNotFound(t, db.DeleteItem(t.Context(), user.ID, first.ID))
}

func TestIncrementVisitCount(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	group := newGroup(t, db, user.ID, "Test Group", 1)
	item := newItem(t, db, user.ID, group.ID, "One", "https://example.com/one")

	for range 3 {
		if err := db.IncrementVisitCount(t.Context(), user.ID, item.ID); err != nil {
			t.Fatalf("IncrementVisitCount: %v", err)
		}
	}

	visited, err := db.ItemByID(t.Context(), user.ID, item.ID)
	if err != nil {
		t.Fatalf("ItemByID: %v", err)
	}
	if visited.VisitCount != 3 {
		t.Errorf("visit_count = %d, want 3", visited.VisitCount)
	}
	// Following a link is not an edit of the tile.
	if !visited.UpdatedAt.Equal(item.UpdatedAt) {
		t.Errorf("updated_at moved to %v, was %v", visited.UpdatedAt, item.UpdatedAt)
	}
}

func TestLinksForCommandBar(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	other := newUser(t, db, "other@example.com")

	search := newGroup(t, db, user.ID, "Search", 1)
	development := newGroup(t, db, user.ID, "Development", 2)
	amazon := newItem(t, db, user.ID, search.ID, "Amazon Shopping", "https://amazon.com")
	newItem(t, db, user.ID, development.ID, "GitHub", "https://github.com")
	newItem(t, db, user.ID, development.ID, "Stack Overflow", "https://stackoverflow.com")

	// A connection's token grants one account; the grid has to be just as
	// private.
	theirs := newGroup(t, db, other.ID, "Theirs", 1)
	newItem(t, db, other.ID, theirs.ID, "Theirs", "https://theirs.example.com")

	links, err := db.LinksForCommandBar(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("LinksForCommandBar: %v", err)
	}

	titles := make([]string, len(links))
	for i, link := range links {
		titles[i] = link.Title
	}
	assertEqualStrings(t, titles, []string{"Amazon Shopping", "GitHub", "Stack Overflow"})
	if links[0].URL != "https://amazon.com" || links[0].ID != amazon.ID {
		t.Errorf("first link = %+v", links[0])
	}
}

// The order is the order the tiles were made, not the order they are drawn.
//
// Rails asked for these through a has_many :through with no ORDER BY and took
// what SQLite gave it, which is group by group and rowid by rowid — so a tile
// dragged to the top of its group stays at the bottom of this list. The
// parity harness caught the difference the first time a development database
// had a group whose drawing order and creation order disagreed. It matters
// because this list is what the command bar filters, and its order is the
// order somebody sees the suggestions in.
func TestLinksForCommandBarIsInCreationOrderNotDrawingOrder(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	group := newGroup(t, db, user.ID, "Search", 1)

	newItem(t, db, user.ID, group.ID, "First", "https://first.example.com")
	newItem(t, db, user.ID, group.ID, "Second", "https://second.example.com")
	last := newItem(t, db, user.ID, group.ID, "Last", "https://last.example.com")

	// Drawn first from now on, and still made last.
	if err := db.MoveItem(t.Context(), user.ID, last.ID, 0); err != nil {
		t.Fatalf("MoveItem: %v", err)
	}

	links, err := db.LinksForCommandBar(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("LinksForCommandBar: %v", err)
	}
	titles := make([]string, len(links))
	for i, link := range links {
		titles[i] = link.Title
	}
	assertEqualStrings(t, titles, []string{"First", "Second", "Last"})
}

// The page serialises this straight to JSON, and a nil slice would be the
// literal null rather than an empty array.
func TestLinksForCommandBarWithNoTiles(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")

	links, err := db.LinksForCommandBar(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("LinksForCommandBar: %v", err)
	}
	if links == nil {
		t.Errorf("links = nil, want an empty slice")
	}
	if len(links) != 0 {
		t.Errorf("links = %v, want empty", links)
	}
}
