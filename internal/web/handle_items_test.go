package web

import (
	"net/http"
	"slices"
	"testing"
)

// itemFixture is the group every tile test hangs off, plus the url the first
// tile in it gets.
const itemURL = "https://example.com/one"

func TestItemCreate(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)

	ts.post("/start/items", form(
		"start_page_item[url]", itemURL,
		"start_page_item[title]", "One",
		"group_id", id(group.ID))).
		assertRedirect("/start/edit")
	ts.assertFlash(flashNotice, itemCreated)

	if got := ts.itemTitles(group.ID); !slices.Equal(got, []string{"One"}) {
		t.Errorf("tiles = %v", got)
	}
}

func TestItemCreateRefusesADuplicateInTheSameGroup(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)
	ts.newItem(user.ID, group.ID, "One", itemURL)

	ts.post("/start/items", form(
		"start_page_item[url]", itemURL,
		"start_page_item[title]", "One",
		"group_id", id(group.ID))).
		assertRedirect("/start/edit")
	ts.assertFlash(flashAlert, "Failed to add tile: Url has already been taken")

	if got := ts.itemTitles(group.ID); len(got) != 1 {
		t.Errorf("tiles = %v, want just the one", got)
	}
}

// --- update ---
//
// A tile owns its own title and url and there is no metadata to re-fetch, so a
// typo has to be fixable from the edit page.

func TestItemUpdate(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)
	item := ts.newItem(user.ID, group.ID, "One", itemURL)

	ts.send(http.MethodPatch, "/start/items/"+id(item.ID), form(
		"start_page_item[url]", "https://example.com/uno",
		"start_page_item[title]", "Uno")).
		assertRedirect("/start/edit")
	ts.assertFlash(flashNotice, itemUpdated)

	updated := ts.item(user.ID, item.ID)
	if updated.Title != "Uno" || updated.URL != "https://example.com/uno" {
		t.Errorf("tile = %q %q", updated.Title, updated.URL)
	}
}

func TestItemUpdateRefusals(t *testing.T) {
	cases := []struct {
		name  string
		url   string
		alert string
	}{
		{"a malformed url", "not a url", "Failed to update tile: Url must be a valid URL"},
		{"a url another tile in the group already has", "https://example.com/two",
			"Failed to update tile: Url has already been taken"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ts, user := startPageServer(t)
			group := ts.newGroup(user.ID, "Test Group", 1)
			ts.newItem(user.ID, group.ID, "Two", "https://example.com/two")
			item := ts.newItem(user.ID, group.ID, "One", itemURL)

			ts.send(http.MethodPatch, "/start/items/"+id(item.ID), form(
				"start_page_item[url]", c.url,
				"start_page_item[title]", "One")).
				assertRedirect("/start/edit")
			ts.assertFlash(flashAlert, c.alert)

			if got := ts.item(user.ID, item.ID).URL; got != itemURL {
				t.Errorf("url = %q, want it unchanged", got)
			}
		})
	}
}

// --- destroy ---

func TestItemDestroy(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)
	item := ts.newItem(user.ID, group.ID, "One", itemURL)

	ts.send(http.MethodDelete, "/start/items/"+id(item.ID), nil).
		assertRedirect("/start/edit")
	ts.assertFlash(flashNotice, itemDeleted)

	if got := ts.itemTitles(group.ID); len(got) != 0 {
		t.Errorf("tiles = %v, want none", got)
	}
}

func TestItemDestroyCompactsTheRemainingPositions(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)
	ts.newItem(user.ID, group.ID, "One", "https://example.com/one")
	middle := ts.newItem(user.ID, group.ID, "Two", "https://example.com/two")
	last := ts.newItem(user.ID, group.ID, "Three", "https://example.com/three")

	ts.send(http.MethodDelete, "/start/items/"+id(middle.ID), nil)

	if got := ts.item(user.ID, last.ID).Position; got != 1 {
		t.Errorf("the last tile's position = %d, want 1", got)
	}
}

// --- visit ---

// Fire and forget from the grid: bump the counter and say nothing back.
func TestItemVisit(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)
	item := ts.newItem(user.ID, group.ID, "One", itemURL)

	ts.post("/start/items/"+id(item.ID)+"/visit", nil).
		assertStatus(http.StatusNoContent)

	if got := ts.item(user.ID, item.ID).VisitCount; got != 1 {
		t.Errorf("visit count = %d, want 1", got)
	}
}

// --- turbo stream responses ---

func TestItemCreateReplacesTheGroupAndLeavesTheAddFormOpen(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)

	resp := ts.turbo(http.MethodPost, "/start/items", form(
		"start_page_item[url]", itemURL,
		"start_page_item[title]", "One",
		"group_id", id(group.ID)))

	resp.assertStatus(http.StatusOK).assertStreams("replace:group_" + id(group.ID))
	// So a second link can be typed straight away.
	resp.assertContains(`id="new_item_group_` + id(group.ID) + `"`)
	resp.assertContains(`data-inline-form-open-value="true"`)
}

func TestItemCreateFailureReopensTheAddFormWithItsErrors(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)
	ts.newItem(user.ID, group.ID, "One", itemURL)

	resp := ts.turbo(http.MethodPost, "/start/items", form(
		"start_page_item[url]", itemURL,
		"start_page_item[title]", "Dupe",
		"group_id", id(group.ID)))

	resp.assertStatus(http.StatusUnprocessableEntity).
		assertStreams("replace:new_item_group_" + id(group.ID)).
		assertContains("Url has already been taken").
		assertContains(`<div class="field_with_errors">`)

	if got := ts.itemTitles(group.ID); len(got) != 1 {
		t.Errorf("tiles = %v, want just the one", got)
	}
}

func TestItemUpdateReplacesOnlyTheTile(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)
	item := ts.newItem(user.ID, group.ID, "One", itemURL)

	resp := ts.turbo(http.MethodPatch, "/start/items/"+id(item.ID), form(
		"start_page_item[url]", itemURL,
		"start_page_item[title]", "Uno"))

	resp.assertStatus(http.StatusOK).assertStreams("replace:item_" + id(item.ID))
	if got := ts.item(user.ID, item.ID).Title; got != "Uno" {
		t.Errorf("title = %q", got)
	}
}

// A rejected edit shows the typed value in the form and the saved one in the
// row, for the same reason a rejected rename does.
func TestItemUpdateFailureKeepsTheEditFormOpenWithItsErrors(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)
	item := ts.newItem(user.ID, group.ID, "One", itemURL)

	resp := ts.turbo(http.MethodPatch, "/start/items/"+id(item.ID), form(
		"start_page_item[url]", "not a url",
		"start_page_item[title]", "One"))

	resp.assertStatus(http.StatusUnprocessableEntity).
		assertStreams("replace:item_" + id(item.ID)).
		assertContains("Url must be a valid URL").
		assertContains(`<span class="item-title">One</span>`).
		assertContains(`data-pristine="` + itemURL + `"`).
		assertContains(`value="not a url"`)

	if got := ts.item(user.ID, item.ID).URL; got != itemURL {
		t.Errorf("url = %q, want it unchanged", got)
	}
}

// A group owns its tile rows and their positions, so anything that adds,
// removes or reorders a tile redraws the group rather than the tile.
func TestItemDestroyReplacesTheGroup(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)
	item := ts.newItem(user.ID, group.ID, "One", itemURL)

	ts.turbo(http.MethodDelete, "/start/items/"+id(item.ID), nil).
		assertStatus(http.StatusOK).
		assertStreams("replace:group_" + id(group.ID))
}

// --- moves ---

func TestItemMoveToAnotherGroup(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)
	item := ts.newItem(user.ID, group.ID, "One", itemURL)
	destination := ts.newGroup(user.ID, "New Group", 2)

	ts.post("/start/items/"+id(item.ID)+"/move", form("group_id", id(destination.ID), "position", "1")).
		assertRedirect("/start/edit")
	ts.assertFlash(flashNotice, itemMoved)

	if got := ts.item(user.ID, item.ID).GroupID; got != destination.ID {
		t.Errorf("group = %d, want %d", got, destination.ID)
	}
}

func TestItemMoveWithinItsOwnGroup(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)
	first := ts.newItem(user.ID, group.ID, "One", "https://example.com/one")
	ts.newItem(user.ID, group.ID, "Two", "https://example.com/two")

	ts.post("/start/items/"+id(first.ID)+"/move", form("position", "1")).
		assertRedirect("/start/edit")
	ts.assertFlash(flashNotice, itemMoved)

	if got := ts.itemTitles(group.ID); !slices.Equal(got, []string{"Two", "One"}) {
		t.Errorf("tiles = %v", got)
	}
}

// The one refusal a tile move can meet: the group it is going to already holds
// the link.
func TestItemMoveIntoAGroupThatAlreadyHasTheLink(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)
	item := ts.newItem(user.ID, group.ID, "One", itemURL)
	destination := ts.newGroup(user.ID, "New Group", 2)
	ts.newItem(user.ID, destination.ID, "One", itemURL)

	ts.post("/start/items/"+id(item.ID)+"/move", form("group_id", id(destination.ID), "position", "0")).
		assertRedirect("/start/edit")
	ts.assertFlash(flashAlert, itemNotMoved)

	if got := ts.item(user.ID, item.ID).GroupID; got != group.ID {
		t.Errorf("group = %d, want it unchanged", got)
	}
}

// A move redraws the group the tile left and the group it landed in, and
// nothing else. Redrawing the whole grid would take #start_page_grid with it,
// and that node carries the drag and keyboard controllers.
func TestItemMoveStreamsTheTwoGroupsAndNotTheGrid(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)
	item := ts.newItem(user.ID, group.ID, "One", itemURL)
	destination := ts.newGroup(user.ID, "New Group", 2)

	resp := ts.turbo(http.MethodPost, "/start/items/"+id(item.ID)+"/move",
		form("group_id", id(destination.ID), "position", "0"))

	// The destination first, then the source.
	resp.assertStatus(http.StatusOK).
		assertStreams("replace:group_"+id(destination.ID), "replace:group_"+id(group.ID))
	resp.assertNotContains(`target="start_page_grid"`)
}

func TestItemMoveWithinOneGroupStreamsThatGroupAlone(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)
	first := ts.newItem(user.ID, group.ID, "One", "https://example.com/one")
	ts.newItem(user.ID, group.ID, "Two", "https://example.com/two")

	ts.turbo(http.MethodPost, "/start/items/"+id(first.ID)+"/move", form("position", "1")).
		assertStatus(http.StatusOK).
		assertStreams("replace:group_" + id(group.ID))
}

// The client moved the tile before it asked. A refusal that only says so leaves
// the page showing a position the database does not have — and the next move
// computes its index from that page — so both groups come back as stored.
func TestItemMoveRefusalAnswers422AndRedrawsBothGroups(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)
	item := ts.newItem(user.ID, group.ID, "One", itemURL)
	destination := ts.newGroup(user.ID, "New Group", 2)
	ts.newItem(user.ID, destination.ID, "One", itemURL)

	resp := ts.turbo(http.MethodPost, "/start/items/"+id(item.ID)+"/move",
		form("group_id", id(destination.ID), "position", "0"))

	resp.assertStatus(http.StatusUnprocessableEntity).
		// update, not replace: the region is a live one.
		assertStreams("update:start_page_notice",
			"replace:group_"+id(destination.ID),
			"replace:group_"+id(group.ID)).
		assertContains(itemNotMoved)

	if got := ts.item(user.ID, item.ID).GroupID; got != group.ID {
		t.Errorf("group = %d, want it unchanged", got)
	}
}

// --- scoping ---

func TestItemWritesAreScopedToTheSignedInUser(t *testing.T) {
	ts, user := startPageServer(t)
	mine := ts.newGroup(user.ID, "Mine", 1)

	other := ts.createApprovedUser("two@example.com")
	theirGroup := ts.newGroup(other.ID, "Other Group", 1)
	theirItem := ts.newItem(other.ID, theirGroup.ID, "Two", "https://example.com/two")
	path := "/start/items/" + id(theirItem.ID)

	cases := []struct {
		name   string
		method string
		path   string
		body   map[string]string
	}{
		{"update", http.MethodPatch, path, map[string]string{"start_page_item[title]": "Mine now"}},
		{"destroy", http.MethodDelete, path, nil},
		{"move", http.MethodPost, path + "/move", map[string]string{"position": "0"}},
		{"visit", http.MethodPost, path + "/visit", nil},
		// A tile of mine into a group of theirs is the same refusal from the
		// other end: the group id has to belong to the signed-in user too.
		{"move into another user's group", http.MethodPost,
			"/start/items/" + id(ts.newItem(user.ID, mine.ID, "Mine", "https://mine.example.com").ID) + "/move",
			map[string]string{"group_id": id(theirGroup.ID), "position": "0"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			values := form()
			for key, value := range c.body {
				values.Set(key, value)
			}
			ts.send(c.method, c.path, values).assertStatus(http.StatusNotFound)
		})
	}

	if got := ts.item(other.ID, theirItem.ID).Title; got != "Two" {
		t.Errorf("their tile is now %q", got)
	}
}

// The group a tile is added to has to be the signed-in user's as well.
func TestItemCreateInAnotherUsersGroupIsNotFound(t *testing.T) {
	ts, _ := startPageServer(t)
	other := ts.createApprovedUser("two@example.com")
	theirs := ts.newGroup(other.ID, "Theirs", 1)

	ts.post("/start/items", form(
		"start_page_item[url]", itemURL,
		"start_page_item[title]", "One",
		"group_id", id(theirs.ID))).
		assertStatus(http.StatusNotFound)

	if got := ts.itemTitles(theirs.ID); len(got) != 0 {
		t.Errorf("their group = %v, want nothing", got)
	}
}

func TestItemCreateRequiresAuthentication(t *testing.T) {
	ts := newTestServer(t)
	ts.createApprovedUser("one@example.com")

	ts.post("/start/items", form("start_page_item[url]", itemURL, "start_page_item[title]", "One")).
		assertRedirect("/session/new")
}
