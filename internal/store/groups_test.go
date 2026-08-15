package store

import "testing"

func TestCreateGroupValidations(t *testing.T) {
	tests := []struct {
		name   string
		group  string
		column int
		want   []string
	}{
		{"no name", "", 1, []string{"Name can't be blank"}},
		{"column zero", "Work", 0, []string{"Column must be greater than 0"}},
		{"a negative column", "Work", -3, []string{"Column must be greater than 0"}},
		{"a column past the grid", "Work", 4,
			[]string{"Column cannot exceed start page column limit of 3"}},
		{"nothing right at all", "", 9,
			[]string{"Name can't be blank", "Column cannot exceed start page column limit of 3"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t)
			user := newUser(t, db, "test@example.com")

			_, err := db.CreateGroup(t.Context(), user.ID, test.group, test.column)
			assertInvalid(t, err, test.want...)
		})
	}
}

func TestCreateGroupNameIsUniquePerUser(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	other := newUser(t, db, "other@example.com")
	newGroup(t, db, user.ID, "Test Group", 1)

	// Same user, another column: still taken.
	_, err := db.CreateGroup(t.Context(), user.ID, "Test Group", 2)
	assertInvalid(t, err, "Name has already been taken")

	// Another user's grid is not this one.
	if _, err := db.CreateGroup(t.Context(), other.ID, "Test Group", 1); err != nil {
		t.Errorf("the same name for another user was refused: %v", err)
	}
}

// The add-group form lives at the bottom of a column and says nothing about
// position, so the store has to work out where a new group lands.
func TestCreateGroupAppendsToItsColumn(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")

	newGroup(t, db, user.ID, "First", 1)
	newGroup(t, db, user.ID, "Second", 1)
	// Another column starts its own numbering.
	elsewhere := newGroup(t, db, user.ID, "Elsewhere", 2)
	third := newGroup(t, db, user.ID, "Third", 1)

	if third.Position != 2 {
		t.Errorf("appended at %d, want 2", third.Position)
	}
	if elsewhere.Position != 0 {
		t.Errorf("first group in column 2 landed at %d, want 0", elsewhere.Position)
	}
}

// Positions can carry a gap while a request is in flight, so the next position
// is one past the last, not the number of groups.
func TestCreateGroupLandsAfterAGap(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	first := newGroup(t, db, user.ID, "First", 1)
	second := newGroup(t, db, user.ID, "Second", 1)

	if err := db.DeleteGroup(t.Context(), user.ID, first.ID); err != nil {
		t.Fatalf("DeleteGroup: %v", err)
	}
	// Put the gap back by hand: deleting closes it, and the point here is what
	// happens when one is open.
	if _, err := db.sql.ExecContext(t.Context(),
		`UPDATE start_page_groups SET position = 5 WHERE id = ?`, second.ID); err != nil {
		t.Fatalf("opening a gap: %v", err)
	}

	appended := newGroup(t, db, user.ID, "Third", 1)
	if appended.Position != 6 {
		t.Errorf("appended at %d, want 6", appended.Position)
	}
}

func TestUpdateGroup(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	group := newGroup(t, db, user.ID, "Test Group", 1)
	newGroup(t, db, user.ID, "Taken", 1)

	renamed, err := db.UpdateGroup(t.Context(), user.ID, group.ID, "Renamed")
	if err != nil {
		t.Fatalf("UpdateGroup: %v", err)
	}
	if renamed.Name != "Renamed" {
		t.Errorf("name = %q, want Renamed", renamed.Name)
	}

	// Its own name is not "already taken".
	if _, err := db.UpdateGroup(t.Context(), user.ID, group.ID, "Renamed"); err != nil {
		t.Errorf("renaming a group to what it is already called: %v", err)
	}

	_, err = db.UpdateGroup(t.Context(), user.ID, group.ID, "Taken")
	assertInvalid(t, err, "Name has already been taken")

	_, err = db.UpdateGroup(t.Context(), user.ID, group.ID, "")
	assertInvalid(t, err, "Name can't be blank")
}

func TestGroupsByColumn(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	newGroup(t, db, user.ID, "Group 1", 1)
	newGroup(t, db, user.ID, "Group 2", 2)
	newGroup(t, db, user.ID, "Group 3", 1)

	byColumn, err := db.GroupsByColumn(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("GroupsByColumn: %v", err)
	}

	if len(byColumn[1]) != 2 || byColumn[1][0].Name != "Group 1" || byColumn[1][1].Name != "Group 3" {
		t.Errorf("column 1 = %v", byColumn[1])
	}
	if len(byColumn[2]) != 1 || byColumn[2][0].Name != "Group 2" {
		t.Errorf("column 2 = %v", byColumn[2])
	}
	// A column with nothing in it is simply absent, which is what the page
	// ranging over the user's columns expects.
	if _, ok := byColumn[3]; ok {
		t.Errorf("an empty column showed up in the map")
	}
}

func TestGroupScopedToItsOwner(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	other := newUser(t, db, "other@example.com")
	group := newGroup(t, db, user.ID, "Mine", 1)

	if _, err := db.GroupByID(t.Context(), user.ID, group.ID); err != nil {
		t.Fatalf("GroupByID: %v", err)
	}

	// An id in a URL is a number anyone can type.
	_, err := db.GroupByID(t.Context(), other.ID, group.ID)
	assertNotFound(t, err)
	assertNotFound(t, db.DeleteGroup(t.Context(), other.ID, group.ID))
	assertNotFound(t, db.MoveGroup(t.Context(), other.ID, group.ID, 2, 0))

	_, err = db.UpdateGroup(t.Context(), other.ID, group.ID, "Theirs")
	assertNotFound(t, err)
}

func TestMoveGroupRefusesAColumnOffTheGrid(t *testing.T) {
	tests := []struct {
		name   string
		column int
		want   string
	}{
		{"past the last column", 5, "Column cannot exceed start page column limit of 3"},
		{"column zero", 0, "Column must be greater than 0"},
		{"a negative column", -3, "Column must be greater than 0"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t)
			user := newUser(t, db, "test@example.com")
			group := newGroup(t, db, user.ID, "Test Group", 1)

			assertInvalid(t, db.MoveGroup(t.Context(), user.ID, group.ID, test.column, 0), test.want)

			// Refused, and left exactly where it was: a group parked outside
			// the grid renders nowhere and has no controls left to bring it
			// back.
			stored, err := db.GroupByID(t.Context(), user.ID, group.ID)
			if err != nil {
				t.Fatalf("GroupByID: %v", err)
			}
			if stored.Column != 1 {
				t.Errorf("column = %d, want 1", stored.Column)
			}
		})
	}
}

// Dropping a group anywhere in a column means the position it lands on is
// usually already taken. Writing it without shifting the neighbours would
// leave two groups sharing a position.
func TestMoveGroupRenumbersTheColumn(t *testing.T) {
	tests := []struct {
		name     string
		move     string
		column   int
		position int
		want     []string
	}{
		{"up within its own column", "Third", 1, 0, []string{"Third", "First", "Second"}},
		{"down within its own column", "First", 1, 2, []string{"Second", "Third", "First"}},
		{"to the top", "Second", 1, 0, []string{"Second", "First", "Third"}},
		{"past the end, which appends", "First", 1, 99, []string{"Second", "Third", "First"}},
		{"before the start, which goes to the top", "Third", 1, -5, []string{"Third", "First", "Second"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t)
			user := newUser(t, db, "test@example.com")
			groups := map[string]*Group{}
			for _, name := range []string{"First", "Second", "Third"} {
				groups[name] = newGroup(t, db, user.ID, name, 1)
			}

			if err := db.MoveGroup(t.Context(), user.ID, groups[test.move].ID,
				test.column, test.position); err != nil {
				t.Fatalf("MoveGroup: %v", err)
			}

			assertEqualStrings(t, groupNames(t, db, user.ID, 1), test.want)
			assertEqualInts(t, groupPositions(t, db, user.ID, 1), []int{0, 1, 2})
		})
	}
}

func TestMoveGroupToAnotherColumn(t *testing.T) {
	t.Run("appends when the position is past the end", func(t *testing.T) {
		db := newTestDB(t)
		user := newUser(t, db, "test@example.com")
		newGroup(t, db, user.ID, "First", 2)
		newGroup(t, db, user.ID, "Second", 2)
		travelling := newGroup(t, db, user.ID, "Travelling", 1)

		if err := db.MoveGroup(t.Context(), user.ID, travelling.ID, 2, 99); err != nil {
			t.Fatalf("MoveGroup: %v", err)
		}

		assertEqualStrings(t, groupNames(t, db, user.ID, 2), []string{"First", "Second", "Travelling"})
		assertEqualInts(t, groupPositions(t, db, user.ID, 2), []int{0, 1, 2})
	})

	t.Run("inserts between the groups already there", func(t *testing.T) {
		db := newTestDB(t)
		user := newUser(t, db, "test@example.com")
		newGroup(t, db, user.ID, "First", 2)
		newGroup(t, db, user.ID, "Second", 2)
		travelling := newGroup(t, db, user.ID, "Travelling", 1)

		if err := db.MoveGroup(t.Context(), user.ID, travelling.ID, 2, 1); err != nil {
			t.Fatalf("MoveGroup: %v", err)
		}

		assertEqualStrings(t, groupNames(t, db, user.ID, 2), []string{"First", "Travelling", "Second"})
		assertEqualInts(t, groupPositions(t, db, user.ID, 2), []int{0, 1, 2})
	})

	// The column a group leaves has to close up behind it, or the next group
	// added there takes a position that is already in use.
	t.Run("closes the gap it leaves behind", func(t *testing.T) {
		db := newTestDB(t)
		user := newUser(t, db, "test@example.com")
		travelling := newGroup(t, db, user.ID, "Travelling", 1)
		newGroup(t, db, user.ID, "Stays", 1)

		if err := db.MoveGroup(t.Context(), user.ID, travelling.ID, 2, 0); err != nil {
			t.Fatalf("MoveGroup: %v", err)
		}

		assertEqualStrings(t, groupNames(t, db, user.ID, 1), []string{"Stays"})
		assertEqualInts(t, groupPositions(t, db, user.ID, 1), []int{0})
	})
}

// Rails renumbered with update_column, which skips the callbacks: a group's
// timestamp records when it was last renamed, not when a neighbour was dragged
// past it.
func TestMovingAGroupLeavesItsNeighboursTimestampsAlone(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	first := newGroup(t, db, user.ID, "First", 1)
	newGroup(t, db, user.ID, "Second", 1)
	third := newGroup(t, db, user.ID, "Third", 1)

	if err := db.MoveGroup(t.Context(), user.ID, third.ID, 1, 0); err != nil {
		t.Fatalf("MoveGroup: %v", err)
	}

	moved, err := db.GroupByID(t.Context(), user.ID, first.ID)
	if err != nil {
		t.Fatalf("GroupByID: %v", err)
	}
	if !moved.UpdatedAt.Equal(first.UpdatedAt) {
		t.Errorf("updated_at moved to %v, was %v", moved.UpdatedAt, first.UpdatedAt)
	}
}

func TestReorderGroupsInColumn(t *testing.T) {
	t.Run("closes a gap", func(t *testing.T) {
		db := newTestDB(t)
		user := newUser(t, db, "test@example.com")
		first := newGroup(t, db, user.ID, "First", 1)
		newGroup(t, db, user.ID, "Middle", 1)
		last := newGroup(t, db, user.ID, "Last", 1)

		if _, err := db.sql.ExecContext(t.Context(),
			`DELETE FROM start_page_groups WHERE name = 'Middle'`); err != nil {
			t.Fatalf("deleting: %v", err)
		}

		if err := db.ReorderGroupsInColumn(t.Context(), user.ID, 1); err != nil {
			t.Fatalf("ReorderGroupsInColumn: %v", err)
		}

		assertEqualInts(t, groupPositions(t, db, user.ID, 1), []int{0, 1})
		assertEqualStrings(t, groupNames(t, db, user.ID, 1), []string{first.Name, last.Name})
	})

	t.Run("leaves the other columns alone", func(t *testing.T) {
		db := newTestDB(t)
		user := newUser(t, db, "test@example.com")
		elsewhere := newGroup(t, db, user.ID, "Elsewhere", 2)
		if _, err := db.sql.ExecContext(t.Context(),
			`UPDATE start_page_groups SET position = 3 WHERE id = ?`, elsewhere.ID); err != nil {
			t.Fatalf("opening a gap: %v", err)
		}
		newGroup(t, db, user.ID, "Here", 1)

		if err := db.ReorderGroupsInColumn(t.Context(), user.ID, 1); err != nil {
			t.Fatalf("ReorderGroupsInColumn: %v", err)
		}

		assertEqualInts(t, groupPositions(t, db, user.ID, 2), []int{3})
	})

	// The renumbering costs a write per group, so it must not run on positions
	// that are already right.
	t.Run("writes nothing when the column is already in order", func(t *testing.T) {
		db := newTestDB(t)
		user := newUser(t, db, "test@example.com")
		newGroup(t, db, user.ID, "First", 1)
		newGroup(t, db, user.ID, "Second", 1)

		before := totalChanges(t, db)
		if err := db.ReorderGroupsInColumn(t.Context(), user.ID, 1); err != nil {
			t.Fatalf("ReorderGroupsInColumn: %v", err)
		}
		if after := totalChanges(t, db); after != before {
			t.Errorf("%d rows were written, want none", after-before)
		}
	})
}

func TestDeleteGroup(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "test@example.com")
	first := newGroup(t, db, user.ID, "First", 1)
	newGroup(t, db, user.ID, "Second", 1)
	newGroup(t, db, user.ID, "Last", 1)
	newItem(t, db, user.ID, first.ID, "One", "https://example.com/one")

	if err := db.DeleteGroup(t.Context(), user.ID, first.ID); err != nil {
		t.Fatalf("DeleteGroup: %v", err)
	}

	assertEqualStrings(t, groupNames(t, db, user.ID, 1), []string{"Second", "Last"})
	assertEqualInts(t, groupPositions(t, db, user.ID, 1), []int{0, 1})

	// The tiles went with it, which the foreign key would have refused had
	// they not gone first.
	items, err := db.ItemsInGroup(t.Context(), first.ID)
	if err != nil {
		t.Fatalf("ItemsInGroup: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("%d tiles survived the group", len(items))
	}
}
