package web

import (
	"net/http"
	"slices"
	"strconv"
	"testing"
)

func TestGroupCreate(t *testing.T) {
	ts, user := startPageServer(t)

	ts.post("/start/groups", form("start_page_group[name]", "Work Links", "start_page_group[column]", "1")).
		assertRedirect("/start/edit")
	ts.assertFlash(flashNotice, groupCreated)

	groups := ts.groupNames(user.ID, 1)
	if !slices.Equal(groups, []string{"Work Links"}) {
		t.Fatalf("column 1 = %v", groups)
	}
}

// The add-group form sits at the bottom of a column and sends no position, so
// a new group lands after the ones already there. A position sent by hand
// has to be ignored, because the column alone decides.
func TestGroupCreateAppendsToTheEndOfItsColumn(t *testing.T) {
	ts, user := startPageServer(t)
	ts.newGroup(user.ID, "First", 2)
	ts.newGroup(user.ID, "Second", 2)

	ts.post("/start/groups", form(
		"start_page_group[name]", "Third",
		"start_page_group[column]", "2",
		"start_page_group[position]", "0"))

	if got := ts.groupNames(user.ID, 2); !slices.Equal(got, []string{"First", "Second", "Third"}) {
		t.Errorf("column 2 = %v", got)
	}
}

func TestGroupCreateRefusesAnInvalidGroup(t *testing.T) {
	ts, user := startPageServer(t)

	ts.post("/start/groups", form("start_page_group[name]", "", "start_page_group[column]", "5")).
		assertRedirect("/start/edit")
	ts.assertFlash(flashAlert, "Failed to create group: Name can't be blank, Column cannot exceed start page column limit of 3")

	if got := ts.groupNames(user.ID, 1); len(got) != 0 {
		t.Errorf("column 1 = %v, want nothing", got)
	}
}

func TestGroupUpdate(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Original Name", 1)

	ts.send(http.MethodPatch, "/start/groups/"+id(group.ID), form("start_page_group[name]", "Updated Name")).
		assertRedirect("/start/edit")
	ts.assertFlash(flashNotice, groupUpdated)

	if got := ts.group(user.ID, group.ID).Name; got != "Updated Name" {
		t.Errorf("name = %q", got)
	}
}

func TestGroupUpdateRefusesInvalidData(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Valid Name", 1)

	ts.send(http.MethodPatch, "/start/groups/"+id(group.ID), form("start_page_group[name]", "")).
		assertRedirect("/start/edit")
	ts.assertFlash(flashAlert, "Failed to update group: Name can't be blank")

	if got := ts.group(user.ID, group.ID).Name; got != "Valid Name" {
		t.Errorf("name = %q, want it unchanged", got)
	}
}

func TestGroupDestroy(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)

	ts.send(http.MethodDelete, "/start/groups/"+id(group.ID), nil).
		assertRedirect("/start/edit")
	ts.assertFlash(flashNotice, groupDeleted)

	if got := ts.groupNames(user.ID, 1); len(got) != 0 {
		t.Errorf("column 1 = %v, want nothing", got)
	}
}

func TestGroupDestroyCompactsTheColumn(t *testing.T) {
	ts, user := startPageServer(t)
	ts.newGroup(user.ID, "First", 1)
	middle := ts.newGroup(user.ID, "Middle", 1)
	last := ts.newGroup(user.ID, "Last", 1)

	ts.send(http.MethodDelete, "/start/groups/"+id(middle.ID), nil)

	if got := ts.group(user.ID, last.ID).Position; got != 1 {
		t.Errorf("the last group's position = %d, want 1", got)
	}
}

// --- turbo stream responses ---
//
// Every write is scoped to the smallest node that changed. So the
// rest of the page — including any other form someone has open — stays put.

func TestGroupCreateReplacesOnlyTheColumn(t *testing.T) {
	ts, _ := startPageServer(t)

	resp := ts.turbo(http.MethodPost, "/start/groups",
		form("start_page_group[name]", "Work", "start_page_group[column]", "2"))

	resp.assertStatus(http.StatusOK).assertStreams("replace:column_2")
	resp.assertNotContains(`target="start_page_grid"`)
}

func TestGroupCreateFailureReopensTheFormWithItsErrors(t *testing.T) {
	ts, user := startPageServer(t)
	ts.newGroup(user.ID, "Work", 1)

	resp := ts.turbo(http.MethodPost, "/start/groups",
		form("start_page_group[name]", "Work", "start_page_group[column]", "2"))

	resp.assertStatus(http.StatusUnprocessableEntity).
		assertStreams("replace:new_group_column_2").
		assertContains("Name has already been taken").
		assertContains(`data-inline-form-open-value="true"`)

	if got := ts.groupNames(user.ID, 2); len(got) != 0 {
		t.Errorf("column 2 = %v, want nothing", got)
	}
}

func TestGroupUpdateReplacesOnlyTheGroup(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Original", 1)

	resp := ts.turbo(http.MethodPatch, "/start/groups/"+id(group.ID),
		form("start_page_group[name]", "Renamed"))

	resp.assertStatus(http.StatusOK).assertStreams("replace:group_" + id(group.ID))
	if got := ts.group(user.ID, group.ID).Name; got != "Renamed" {
		t.Errorf("name = %q", got)
	}
}

// A rejected rename shows the typed value in the form and the saved one in
// the header. The header describes stored state, not an edit in flight.
func TestGroupUpdateFailureKeepsTheFormOpenWithItsErrors(t *testing.T) {
	ts, user := startPageServer(t)
	ts.newGroup(user.ID, "Taken", 1)
	group := ts.newGroup(user.ID, "Original", 1)

	resp := ts.turbo(http.MethodPatch, "/start/groups/"+id(group.ID),
		form("start_page_group[name]", "Taken"))

	resp.assertStatus(http.StatusUnprocessableEntity).
		assertStreams("replace:group_" + id(group.ID)).
		assertContains("Name has already been taken").
		assertContains(`<span class="group-name" id="group_name_` + id(group.ID) + `">Original</span>`).
		assertContains(`data-pristine="Original"`).
		assertContains(`value="Taken"`)

	if got := ts.group(user.ID, group.ID).Name; got != "Original" {
		t.Errorf("name = %q, want it unchanged", got)
	}
}

func TestGroupDestroyReplacesTheColumn(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Work", 3)

	ts.turbo(http.MethodDelete, "/start/groups/"+id(group.ID), nil).
		assertStatus(http.StatusOK).
		assertStreams("replace:column_3")
}

// --- moves ---

func TestGroupMoveToAnotherColumn(t *testing.T) {
	ts, user := startPageServer(t)
	ts.newGroup(user.ID, "Already there", 2)
	group := ts.newGroup(user.ID, "Test Group", 1)

	ts.post("/start/groups/"+id(group.ID)+"/move", form("column", "2", "position", "1")).
		assertRedirect("/start/edit")
	ts.assertFlash(flashNotice, groupMoved)

	moved := ts.group(user.ID, group.ID)
	if moved.Column != 2 || moved.Position != 1 {
		t.Errorf("moved to column %d position %d, want 2/1", moved.Column, moved.Position)
	}
}

// A drag within one column is the case the move buttons never produced: the
// target position is always already occupied.
func TestGroupMoveReordersWithinItsOwnColumn(t *testing.T) {
	ts, user := startPageServer(t)
	ts.newGroup(user.ID, "First", 1)
	ts.newGroup(user.ID, "Second", 1)
	third := ts.newGroup(user.ID, "Third", 1)

	ts.post("/start/groups/"+id(third.ID)+"/move", form("column", "1", "position", "0")).
		assertRedirect("/start/edit")

	if got := ts.groupNames(user.ID, 1); !slices.Equal(got, []string{"Third", "First", "Second"}) {
		t.Errorf("column 1 = %v", got)
	}
}

func TestGroupMoveRefusesAColumnOffTheEndOfTheGrid(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)

	ts.post("/start/groups/"+id(group.ID)+"/move", form("column", "5", "position", "0")).
		assertRedirect("/start/edit")
	ts.assertFlash(flashAlert, groupNotMoved)

	if got := ts.group(user.ID, group.ID).Column; got != 1 {
		t.Errorf("column = %d, want it unchanged", got)
	}
}

// A move renumbers the column it left and the column it landed in, and nothing
// else — so those are what get redrawn. Redrawing the whole grid takes
// #start_page_grid with it, and that node carries the drag and keyboard
// controllers. Replacing it drops the keyboard highlight on every move.
func TestGroupMoveStreamsTheTwoColumnsAndNotTheGrid(t *testing.T) {
	ts, user := startPageServer(t)
	ts.newGroup(user.ID, "Already there", 2)
	group := ts.newGroup(user.ID, "Test Group", 1)

	resp := ts.turbo(http.MethodPost, "/start/groups/"+id(group.ID)+"/move",
		form("column", "2", "position", "1"))

	// The destination first, then the source.
	resp.assertStatus(http.StatusOK).assertStreams("replace:column_2", "replace:column_1")
	resp.assertNotContains(`target="start_page_grid"`)
}

func TestGroupMoveWithinOneColumnStreamsThatColumnAlone(t *testing.T) {
	ts, user := startPageServer(t)
	ts.newGroup(user.ID, "First", 1)
	second := ts.newGroup(user.ID, "Second", 1)

	ts.turbo(http.MethodPost, "/start/groups/"+id(second.ID)+"/move", form("column", "1", "position", "0")).
		assertStatus(http.StatusOK).
		assertStreams("replace:column_1")
}

// The failure branch used to answer 200 with a stream aimed at an id that is
// rendered nowhere. So Turbo applied it to nothing, and the client's
// response.ok check passed. A failed move was silent from both ends.
//
// The client also moved the group before it asked. A refusal that only says
// so leaves the page showing a column the database does not have. The next
// move then computes its index from that page. Only the columns that exist
// are worth redrawing. The refused one is off the end of the grid.
func TestGroupMoveRefusalAnswers422AndRedrawsWhatExists(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)

	resp := ts.turbo(http.MethodPost, "/start/groups/"+id(group.ID)+"/move",
		form("column", "5", "position", "0"))

	resp.assertStatus(http.StatusUnprocessableEntity).
		// update, not replace: the region is a live one.
		assertStreams("update:start_page_notice", "replace:column_1").
		assertContains(groupNotMoved).
		assertNotContains(`target="column_5"`)

	if got := ts.group(user.ID, group.ID).Column; got != 1 {
		t.Errorf("column = %d, want it unchanged", got)
	}
}

// --- scoping ---

// An id that belongs to someone else is not "forbidden", it is "not there":
// telling the two apart works as confirmation that the group exists.
func TestGroupWritesAreScopedToTheSignedInUser(t *testing.T) {
	ts, _ := startPageServer(t)
	other := ts.createApprovedUser("two@example.com")
	theirs := ts.newGroup(other.ID, "Theirs", 1)
	path := "/start/groups/" + id(theirs.ID)

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"update", http.MethodPatch, path},
		{"destroy", http.MethodDelete, path},
		{"move", http.MethodPost, path + "/move"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ts.send(c.method, c.path, form("start_page_group[name]", "Mine now", "column", "1", "position", "0")).
				assertStatus(http.StatusNotFound)
		})
	}

	if got := ts.group(other.ID, theirs.ID).Name; got != "Theirs" {
		t.Errorf("their group is now %q", got)
	}
}

func TestGroupCreateRequiresAuthentication(t *testing.T) {
	ts := newTestServer(t)
	ts.createApprovedUser("one@example.com")

	ts.post("/start/groups", form("start_page_group[name]", "Test Group")).
		assertRedirect("/sign_in")
}

// id is the string form of a row id, which every one of these paths needs.
func id(value int64) string { return strconv.FormatInt(value, 10) }

// The drag and keyboard controllers post JSON, not a form — see
// lib/start_page_moves.js — and Rails read either without being asked to.
func TestGroupMoveAcceptsTheJSONTheEditorSends(t *testing.T) {
	ts, user := startPageServer(t)
	ts.newGroup(user.ID, "Already there", 2)
	group := ts.newGroup(user.ID, "Test Group", 1)

	ts.turboJSON(http.MethodPost, "/start/groups/"+id(group.ID)+"/move", `{"column":2,"position":1}`).
		assertStatus(http.StatusOK).
		assertStreams("replace:column_2", "replace:column_1")

	moved := ts.group(user.ID, group.ID)
	if moved.Column != 2 || moved.Position != 1 {
		t.Errorf("moved to column %d position %d, want 2/1", moved.Column, moved.Position)
	}
}
