//go:build browser

// test/system/start_page_integration_test.rb, ported — the editor half: the
// column count, the inline forms, and what a refusal leaves on screen.
//
// Plus the two things the Ruby suite could not do. The Ruby suite left
// dragging to be checked by hand there. Here it is driven by dispatching the
// DragEvents the page listens for, and checked against the database like
// every other move. And this test watches a Turbo Stream apply, rather than
// assuming from the fact that the node changed.
package web

import (
	"fmt"
	"slices"
	"testing"
)

// There is nothing to create any more — the grid is there from signup, and the
// only thing to configure is how wide it is. That happens in the editor's
// toolbar, with no submit button: picking the value is the whole interaction.
func TestBrowserColumnCountIsChangedFromTheEditor(t *testing.T) {
	p, _ := startPageBrowser(t)

	p.visit("/start/edit")
	p.assertSelector(`.start-page-grid[data-columns="3"]`)

	p.selectOption("#column_count select", "5")

	p.assertSelector(`.start-page-grid[data-columns="5"]`)
	p.assertPresent(`#column_count select option[selected][value="5"]`)

	p.visit("/")
	p.assertSelector(`.start-page-grid[data-columns="5"]`)
}

// The whole reason the control moved: a shrink can be refused, and the group
// the refusal names is only on screen here.
func TestBrowserAShrinkThatWouldStrandAGroupIsRefused(t *testing.T) {
	p, user := startPageBrowser(t)
	p.ts.newGroup(user.ID, "Reading", 3)

	p.visit("/start/edit")
	p.selectOption("#column_count select", "1")

	p.assertText("#start_page_notice", `that would hide "Reading"`)
	p.assertSelector(`.start-page-grid[data-columns="3"]`)
	// A refusal redraws rather than only reporting, so the select goes back
	// too.
	p.assertPresent(`#column_count select option[selected][value="3"]`)
	if got := p.ts.reloadUser(user).Columns; got != 3 {
		t.Errorf("columns = %d, want it unchanged at 3", got)
	}
}

// The whole editing loop in one pass, each step done where the thing lives.
// A group starts from the foot of its column, tiles from the foot of the
// group. Every write swaps a node in place, so these wait on rendered state
// and the database rather than on a flash.
func TestBrowserAddAGroupAddTilesEditThemAndDelete(t *testing.T) {
	p, user := startPageBrowser(t)

	p.visit("/start/edit")

	p.clickOn(newGroupSel(1), "Add group")
	p.fillInLabelled(newGroupSel(1), "Group name", "Daily")
	p.clickOn(newGroupSel(1), "Add")

	p.assertText("#column_1 .group-name", "Daily")
	p.waitForDB("the group to be stored", func() bool {
		return slices.Equal(p.ts.groupNames(user.ID, 1), []string{"Daily"})
	})
	group := p.groupNamed(user.ID, 1, "Daily")

	p.clickOn(newItemSel(group.ID), "Add link")
	p.fillInLabelled(newItemSel(group.ID), "Title", "GitHub")
	p.fillInLabelled(newItemSel(group.ID), "URL", "https://github.com")
	p.clickOn(newItemSel(group.ID), "Add")

	p.assertText(groupSel(group)+" .item-title", "GitHub")

	// The add form comes back open, so a second link needs no second click.
	p.assertSelector(newItemSel(group.ID) + " .inline-form")
	p.fillInLabelled(newItemSel(group.ID), "Title", "Apple")
	p.fillInLabelled(newItemSel(group.ID), "URL", "https://apple.com")
	p.clickOn(newItemSel(group.ID), "Add")

	p.assertText(groupSel(group)+" .item-title", "Apple")
	p.waitForDB("both tiles to be stored", func() bool {
		return slices.Equal(p.ts.itemTitles(group.ID), []string{"GitHub", "Apple"})
	})

	// Editing a tile opens the same form that added it, over its own row.
	github := p.itemNamed(group.ID, "GitHub")
	p.clickOn(itemSel(github), "Edit tile")
	p.fillInLabelled(itemSel(github), "Title", "GitHub Home")
	p.clickOn(itemSel(github), "Save")

	p.assertText(itemSel(github)+" .item-title", "GitHub Home")
	p.waitForDB("the rename to be stored", func() bool {
		return p.reloadItem(user.ID, github.ID).Title == "GitHub Home"
	})

	p.clickOn(groupSel(group)+" .group-heading", "Rename group")
	p.fillInLabelled(groupSel(group)+" .group-heading", "Group name", "Every day")
	p.clickOn(groupSel(group)+" .group-heading", "Save")

	p.assertText(groupSel(group)+" .group-name", "Every day")

	// Reordering has files of its own — the keyboard's and, below, the
	// pointer's.
	p.onConfirm(true)
	p.clickOn(itemSel(github), "Remove tile")

	p.assertNoSelector(itemSel(github))
	p.waitForDB("the tile to be deleted", func() bool {
		return slices.Equal(p.ts.itemTitles(group.ID), []string{"Apple"})
	})
	if got := p.positions(group.ID); !slices.Equal(got, []int{0}) {
		t.Errorf("positions = %v, want [0] — the gap was not closed", got)
	}
}

// A rejected save leaves the form where it was, and it must still hold what
// was typed, or the message has nothing to point at.
func TestBrowserARejectedTileKeepsItsFormOpenWithTheTypedValues(t *testing.T) {
	p, user := startPageBrowser(t)
	group := p.ts.newGroup(user.ID, "Tools", 1)
	p.ts.newItem(user.ID, group.ID, "GitHub", "https://github.com")

	p.visit("/start/edit")

	p.clickOn(newItemSel(group.ID), "Add link")
	p.fillInLabelled(newItemSel(group.ID), "Title", "Duplicate")
	p.fillInLabelled(newItemSel(group.ID), "URL", "https://github.com")
	p.clickOn(newItemSel(group.ID), "Add")

	p.assertText(newItemSel(group.ID)+" .form-errors", "Url has already been taken")
	p.assertFieldValue(newItemSel(group.ID), "Title", "Duplicate")
	p.assertSelector(newItemSel(group.ID) + " .inline-form")

	if got := p.ts.itemTitles(group.ID); len(got) != 1 {
		t.Errorf("the group holds %v, want the one tile it started with", got)
	}
}

// The form keeps what was typed so the error has something to point at. The
// row behind it describes what is actually saved. And Cancel has to discard
// the refused values, not adopt them.
func TestBrowserARejectedEditKeepsTheSavedValuesBehindIt(t *testing.T) {
	p, user := startPageBrowser(t)
	group := p.ts.newGroup(user.ID, "Tools", 1)
	item := p.ts.newItem(user.ID, group.ID, "GitHub", "https://github.com")

	p.visit("/start/edit")

	p.clickOn(itemSel(item), "Edit tile")
	p.fillInLabelled(itemSel(item), "Title", "Renamed")
	// Passes the browser's own url validation, fails the model's.
	p.fillInLabelled(itemSel(item), "URL", "ftp://example.com")
	p.clickOn(itemSel(item), "Save")

	p.assertText(itemSel(item)+" .form-errors", "Url must be a valid URL")
	p.assertFieldValue(itemSel(item), "Title", "Renamed")
	// The row behind the open form still reports the saved title.
	if got := p.evalString(fmt.Sprintf(`document.querySelector(%q).textContent.trim()`, itemSel(item)+" .item-title")); got != "GitHub" {
		t.Errorf("the row behind the form reads %q, want the saved GitHub", got)
	}

	p.clickOn(itemSel(item), "Cancel")
	p.assertNoSelector(itemSel(item) + " .form-errors")

	p.clickOn(itemSel(item), "Edit tile")
	p.assertFieldValue(itemSel(item), "Title", "GitHub")
	p.assertFieldValue(itemSel(item), "URL", "https://github.com")
	p.assertNoSelectorNow(itemSel(item) + " .form-errors")

	stored := p.reloadItem(user.ID, item.ID)
	if stored.Title != "GitHub" || stored.URL != "https://github.com" {
		t.Errorf("stored tile = %q %q, want it untouched", stored.Title, stored.URL)
	}
}

// Cancel closes the form and throws the edit away rather than saving it.
func TestBrowserCancellingAnEditLeavesTheTileAlone(t *testing.T) {
	p, user := startPageBrowser(t)
	group := p.ts.newGroup(user.ID, "Tools", 1)
	item := p.ts.newItem(user.ID, group.ID, "GitHub", "https://github.com")

	p.visit("/start/edit")

	p.clickOn(itemSel(item), "Edit tile")
	p.fillInLabelled(itemSel(item), "Title", "Something else")
	p.clickOn(itemSel(item), "Cancel")

	p.assertNoSelector(itemSel(item) + " .inline-form")
	p.assertText(itemSel(item)+" .item-title", "GitHub")

	if got := p.reloadItem(user.ID, item.ID).Title; got != "GitHub" {
		t.Errorf("title = %q, want it untouched", got)
	}
}

// Every write on this page answers with a <turbo-stream> that swaps one node.
// The whole editor depends on that swap instead of a reload. The grid carries
// the drag and keyboard controllers, and a full navigation removes their
// state. A value left on window is the plainest possible proof the document
// survived.
func TestBrowserAWriteSwapsANodeWithoutReloadingThePage(t *testing.T) {
	p, user := startPageBrowser(t)

	p.visit("/start/edit")
	p.eval(`window.__marker = "kept"`, nil)

	p.clickOn(newGroupSel(1), "Add group")
	p.fillInLabelled(newGroupSel(1), "Group name", "Daily")
	p.clickOn(newGroupSel(1), "Add")

	p.assertText("#column_1 .group-name", "Daily")
	p.waitForDB("the group to be stored", func() bool {
		return slices.Equal(p.ts.groupNames(user.ID, 1), []string{"Daily"})
	})

	if got := p.evalString(`window.__marker`); got != "kept" {
		t.Errorf("window.__marker = %q — the page reloaded instead of applying a stream", got)
	}
}

// === DRAGGING ===
//
// The pointer's way to reorder. See dragTo for what is synthesised and what
// is not. Everything from the parting list to the stored position is the
// page's own code.

func TestBrowserDraggingAGroupIntoAnotherColumn(t *testing.T) {
	p, user := startPageBrowser(t)
	group, _, _ := p.tiles(user)

	p.visit("/start/edit")
	p.dragTo(groupSel(group)+" .group-header .drag-handle", "#column_2", true)

	p.assertSelector("#column_2 " + groupSel(group))
	p.waitForDB("the group to change columns", func() bool {
		return p.reloadGroup(user.ID, group.ID).Column == 2
	})
	if got := p.reloadGroup(user.ID, group.ID).Position; got != 0 {
		t.Errorf("position = %d, want 0", got)
	}
	if got := p.ts.groupNames(user.ID, 1); len(got) != 0 {
		t.Errorf("column 1 still holds %v", got)
	}
}

func TestBrowserDraggingATileIntoAnotherGroup(t *testing.T) {
	p, user := startPageBrowser(t)
	group, gmail, _ := p.tiles(user)
	other := p.ts.newGroup(user.ID, "Reading", 2)
	p.ts.newItem(user.ID, other.ID, "RSS", "https://example.com/rss")

	p.visit("/start/edit")
	p.dragTo(itemSel(gmail)+" .drag-handle", groupSel(other)+" .group-items", true)

	p.assertSelector(groupSel(other) + " " + itemSel(gmail))
	p.waitForDB("the tile to change groups", func() bool {
		return p.reloadItem(user.ID, gmail.ID).GroupID == other.ID
	})
	if got := p.ts.itemTitles(other.ID); !slices.Equal(got, []string{"Gmail", "RSS"}) {
		t.Errorf("Reading = %v, want [Gmail RSS] — it was dropped at the top", got)
	}
	if got := p.ts.itemTitles(group.ID); !slices.Equal(got, []string{"Calendar"}) {
		t.Errorf("Work = %v, want [Calendar]", got)
	}
	if got := p.positions(group.ID); !slices.Equal(got, []int{0}) {
		t.Errorf("Work positions = %v, want [0] — the gap was not closed", got)
	}
}

func TestBrowserDraggingATileToTheEndOfItsOwnGroup(t *testing.T) {
	p, user := startPageBrowser(t)
	group, gmail, _ := p.tiles(user)

	p.visit("/start/edit")
	p.dragTo(itemSel(gmail)+" .drag-handle", groupSel(group)+" .group-items", false)

	p.assertText(groupSel(group)+" .start-page-item:first-of-type", "Calendar")
	p.waitForDB("the move to be stored", func() bool {
		return slices.Equal(p.ts.itemTitles(group.ID), []string{"Calendar", "Gmail"})
	})
	if got := p.positions(group.ID); !slices.Equal(got, []int{0, 1}) {
		t.Errorf("positions = %v, want [0 1]", got)
	}
}
