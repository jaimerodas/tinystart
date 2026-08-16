//go:build browser

// test/system/start_page_keyboard_test.rb, ported.
//
// The keyboard is the one reorder path a test can drive without synthesising
// anything: send_keys is send_keys. Everything the grid answers to is here,
// checked against both the DOM and the database — the database because a page
// that shows a move the server refused is exactly the bug this suite exists
// for.
package web

import (
	"slices"
	"testing"

	"github.com/chromedp/chromedp/kb"
)

// === NAVIGATION ===

func TestBrowserGridIsOneTabStop(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tiles(user)

	p.visit("/start/edit")
	p.enterGrid()

	p.assertFocusedRow("Work")

	// Out the other side in one press, however many tiles are on the page.
	p.sendKeys(kb.Tab)
	if p.focusInsideGrid() {
		t.Errorf("Tab left focus inside the grid")
	}
}

func TestBrowserArrowKeysWalkTheRowsAndCrossColumns(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tiles(user)

	p.visit("/start/edit")
	p.enterGrid()

	p.assertFocusedRow("Work")
	p.sendKeys(kb.ArrowDown)
	p.assertFocusedRow("Gmail")
	p.sendKeys(kb.ArrowDown)
	p.assertFocusedRow("Calendar")
	p.sendKeys(kb.ArrowUp)
	p.assertFocusedRow("Gmail")

	p.sendKeys(kb.ArrowRight)
	if got := p.focusedColumn(); got != 2 {
		t.Errorf("→ landed in column %d, want 2", got)
	}
}

func TestBrowserArrowingPastTheLastTileReachesTheAddTrigger(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tiles(user)

	p.visit("/start/edit")
	p.enterGrid()

	p.sendKeys(kb.ArrowDown, kb.ArrowDown, kb.ArrowDown)
	p.assertFocusedRow("Add link")
}

func TestBrowserTheHighlightStopsAtTheEnds(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tiles(user)

	p.visit("/start/edit")
	p.enterGrid()

	p.sendKeys(kb.ArrowUp, kb.ArrowUp, kb.ArrowUp)
	p.assertFocusedRow("Work")

	p.sendKeys(kb.End)
	last := p.focusedText()
	p.sendKeys(kb.ArrowDown, kb.ArrowDown)
	if got := p.focusedText(); got != last {
		t.Errorf("the highlight wrapped past the end: %q, want %q", got, last)
	}

	p.sendKeys(kb.Home)
	p.assertFocusedRow("Work")
}

// === KEYBOARD MODE ===

func TestBrowserTheLegendOffersTheWayInThenTheKeys(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tiles(user)

	p.visit("/start/edit")
	p.assertText(".keyboard-legend-enter", "to enter keyboard mode")
	p.assertNoSelector(".keyboard-legend-keys")

	p.enterGrid()

	p.assertText(".keyboard-legend-keys", "for all the shortcuts")
	p.assertNoSelector(".keyboard-legend-enter")
}

// Two ways to move the same tile at the same time is one too many — and a
// handle is unreachable by keyboard anyway, so in keyboard mode it is clutter.
func TestBrowserDragHandlesWithdrawInKeyboardMode(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tiles(user)

	p.visit("/start/edit")
	p.assertSelector("#column_1 .drag-handle")

	p.enterGrid()
	p.assertNoSelector("#column_1 .drag-handle")

	p.sendKeys(kb.Tab)
	if p.focusInsideGrid() {
		t.Errorf("Tab left focus inside the grid")
	}
	p.assertSelector("#column_1 .drag-handle")
}

// Clicking a row focuses it, but a pointer user has not asked for keyboard
// mode and must not lose the handles for reaching one.
func TestBrowserClickingARowKeepsTheDragHandles(t *testing.T) {
	p, user := startPageBrowser(t)
	_, gmail, _ := p.tiles(user)

	p.visit("/start/edit")
	p.click(itemSel(gmail) + " .item-title")

	p.assertSelector("#column_1 .drag-handle")
	p.assertNoSelector(".keyboard-legend-keys")
}

// === ACTIONS ===

func TestBrowserEnterOpensATileFormAndEscapeHandsFocusBack(t *testing.T) {
	p, user := startPageBrowser(t)
	_, gmail, _ := p.tiles(user)

	p.visit("/start/edit")
	p.enterGrid()
	p.sendKeys(kb.ArrowDown)

	p.sendKeys(kb.Enter)
	p.assertSelector(itemSel(gmail) + ` input[aria-label="Title"]`)
	if got := p.evalString(`document.querySelector('` + itemSel(gmail) + ` input[aria-label="Title"]').value`); got != "Gmail" {
		t.Errorf("the form opened holding %q, want %q", got, "Gmail")
	}
	if got := p.focusedTag(); got != "input" {
		t.Errorf("focus is on a %s, want the input", got)
	}

	p.sendKeys(kb.Escape)
	p.assertFocusedRow("Gmail")
}

func TestBrowserEnterOnTheAddTriggerOpensTheNewLinkForm(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tiles(user)

	p.visit("/start/edit")
	p.enterGrid()

	p.sendKeys(kb.ArrowDown, kb.ArrowDown, kb.ArrowDown)
	p.assertFocusedRow("Add link")

	p.sendKeys(kb.Enter)
	if got := p.focusedTag(); got != "input" {
		t.Errorf("focus is on a %s, want the input", got)
	}
	if got := p.focusedLabel(); got != "Title" {
		t.Errorf("focus is on the %q field, want Title", got)
	}
}

func TestBrowserDeleteRemovesATileAndLeavesTheHighlightWhereItWas(t *testing.T) {
	p, user := startPageBrowser(t)
	group, gmail, _ := p.tiles(user)

	p.visit("/start/edit")
	p.enterGrid()
	p.sendKeys(kb.ArrowDown)
	p.assertFocusedRow("Gmail")

	p.onConfirm(true)
	p.sendKeys(kb.Delete)

	p.assertNoSelector(itemSel(gmail))
	p.waitForDB("the tile to be deleted", func() bool {
		return slices.Equal(p.ts.itemTitles(group.ID), []string{"Calendar"})
	})
	// The row that took its place, not the top of the document.
	p.assertFocusedRow("Calendar")
}

// === REORDERING ===

// The move the JSON-body bug broke: it passed 639 unit tests and every
// captured page, and failed the moment a browser sent it.
func TestBrowserSpacePicksUpAnArrowMovesAndSpaceSaves(t *testing.T) {
	p, user := startPageBrowser(t)
	group, gmail, _ := p.tiles(user)

	p.visit("/start/edit")
	p.enterGrid()
	p.sendKeys(kb.ArrowDown)

	p.sendKeys(" ")
	p.assertSelector(itemSel(gmail) + ".grabbed")

	p.sendKeys(kb.ArrowDown)
	p.sendKeys(" ")

	p.assertNoSelector(".grabbed")
	p.assertText(groupSel(group)+" .start-page-item:first-of-type", "Calendar")
	p.waitForDB("the move to be stored", func() bool {
		return slices.Equal(p.ts.itemTitles(group.ID), []string{"Calendar", "Gmail"})
	})
	if got := p.positions(group.ID); !slices.Equal(got, []int{0, 1}) {
		t.Errorf("positions = %v, want [0 1]", got)
	}
	// The highlight followed the tile through the re-render.
	p.assertFocusedRow("Gmail")
}

// Letting go commits, the same rule Tab follows.
func TestBrowserClickingAwayWhileCarryingCommitsTheMove(t *testing.T) {
	p, user := startPageBrowser(t)
	group, _, _ := p.tiles(user)

	p.visit("/start/edit")
	p.enterGrid()
	p.sendKeys(kb.ArrowDown)

	p.sendKeys(" ")
	p.sendKeys(kb.ArrowDown)

	// The legend is outside the grid and focuses nothing, so this is a bare
	// departure rather than a move to another row.
	p.click(".keyboard-legend")

	p.assertNoSelector(".grabbed")
	p.assertText(groupSel(group)+" .start-page-item:first-of-type", "Calendar")
	p.waitForDB("the move to be stored", func() bool {
		return slices.Equal(p.ts.itemTitles(group.ID), []string{"Calendar", "Gmail"})
	})
}

// A move destroys the focused row and focus sits on <body> until the render
// brings it back. Reading that as leaving flickers the handles back and swaps
// the legend twice on every single move.
func TestBrowserKeyboardModeSurvivesAMove(t *testing.T) {
	p, user := startPageBrowser(t)
	group, _, _ := p.tiles(user)

	p.visit("/start/edit")
	p.enterGrid()
	p.sendKeys(kb.ArrowDown)

	p.sendKeys(" ")
	p.sendKeys(kb.ArrowDown)
	p.sendKeys(" ")

	p.assertText(groupSel(group)+" .start-page-item:first-of-type", "Calendar")
	p.waitForDB("the move to be stored", func() bool {
		return slices.Equal(p.ts.itemTitles(group.ID), []string{"Calendar", "Gmail"})
	})

	p.assertSelector(".start-page-grid.keyboard-mode")
	p.assertNoSelector("#column_1 .drag-handle")
	p.assertSelector(".keyboard-legend-keys")
}

func TestBrowserEscapeDuringAMovePutsTheTileBack(t *testing.T) {
	p, user := startPageBrowser(t)
	group, _, _ := p.tiles(user)

	p.visit("/start/edit")
	p.enterGrid()
	p.sendKeys(kb.ArrowDown)

	p.sendKeys(" ")
	p.sendKeys(kb.ArrowDown)
	// Already rearranged on screen, but nothing has been sent yet.
	p.assertText(groupSel(group)+" .start-page-item:first-of-type", "Calendar")

	p.sendKeys(kb.Escape)

	p.assertNoSelector(".grabbed")
	p.assertText(groupSel(group)+" .start-page-item:first-of-type", "Gmail")
	if got := p.ts.itemTitles(group.ID); !slices.Equal(got, []string{"Gmail", "Calendar"}) {
		t.Errorf("stored order = %v, want it untouched", got)
	}
	p.assertFocusedRow("Gmail")
}

func TestBrowserATileCarriedPastTheEndSpillsIntoTheNextGroup(t *testing.T) {
	p, user := startPageBrowser(t)
	group, _, calendar := p.tiles(user)
	other := p.ts.newGroup(user.ID, "Reading", 1)
	p.ts.newItem(user.ID, other.ID, "RSS", "https://example.com/rss")

	p.visit("/start/edit")
	p.enterGrid()
	p.sendKeys(kb.ArrowDown, kb.ArrowDown)
	p.assertFocusedRow("Calendar")

	p.sendKeys(" ")
	p.sendKeys(kb.ArrowDown)
	p.sendKeys(" ")

	p.assertSelector(groupSel(other) + " " + itemSel(calendar))
	p.waitForDB("the tile to change groups", func() bool {
		return p.reloadItem(user.ID, calendar.ID).GroupID == other.ID
	})
	if got := p.ts.itemTitles(other.ID); !slices.Equal(got, []string{"Calendar", "RSS"}) {
		t.Errorf("Reading = %v, want [Calendar RSS]", got)
	}
	if got := p.ts.itemTitles(group.ID); !slices.Equal(got, []string{"Gmail"}) {
		t.Errorf("Work = %v, want [Gmail]", got)
	}
	if got := p.positions(group.ID); !slices.Equal(got, []int{0}) {
		t.Errorf("Work positions = %v, want [0]", got)
	}
}

func TestBrowserAGroupCanBeCarriedIntoTheNextColumn(t *testing.T) {
	p, user := startPageBrowser(t)
	group, _, _ := p.tiles(user)

	p.visit("/start/edit")
	p.enterGrid()
	p.assertFocusedRow("Work")

	p.sendKeys(" ")
	p.sendKeys(kb.ArrowRight)
	p.sendKeys(" ")

	p.assertSelector("#column_2 " + groupSel(group))
	p.waitForDB("the group to change columns", func() bool {
		return p.reloadGroup(user.ID, group.ID).Column == 2
	})
	if got := p.reloadGroup(user.ID, group.ID).Position; got != 0 {
		t.Errorf("position = %d, want 0", got)
	}
}

func TestBrowserAGroupReordersWithinItsColumn(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tiles(user)
	p.ts.newGroup(user.ID, "Reading", 1)

	p.visit("/start/edit")
	p.enterGrid()

	p.sendKeys(" ")
	p.sendKeys(kb.ArrowDown)
	p.sendKeys(" ")

	p.assertText("#column_1 .start-page-group:first-of-type .group-name", "Reading")
	p.waitForDB("the groups to be reordered", func() bool {
		return slices.Equal(p.ts.groupNames(user.ID, 1), []string{"Reading", "Work"})
	})
	if got := p.groupPositions(user.ID, 1); !slices.Equal(got, []int{0, 1}) {
		t.Errorf("positions = %v, want [0 1]", got)
	}
}

func TestBrowserCarryingATilePastTheTopSavesNothing(t *testing.T) {
	p, user := startPageBrowser(t)
	group, _, _ := p.tiles(user)

	p.visit("/start/edit")
	p.enterGrid()
	p.sendKeys(kb.ArrowDown)

	p.sendKeys(" ")
	p.sendKeys(kb.ArrowUp)
	p.sendKeys(kb.ArrowUp)
	p.sendKeys(" ")

	// It never left position 0, so the order is what it always was.
	p.assertFocusedRow("Gmail")
	if got := p.ts.itemTitles(group.ID); !slices.Equal(got, []string{"Gmail", "Calendar"}) {
		t.Errorf("stored order = %v, want it untouched", got)
	}
}

// A move that cannot land has to say so. It used to answer 200 with a stream
// aimed at an id rendered nowhere, so nothing appeared and the client's
// response.ok check passed.
func TestBrowserARejectedMoveReportsItselfInTheNotice(t *testing.T) {
	p, user := startPageBrowser(t)
	group, gmail, _ := p.tiles(user)
	other := p.ts.newGroup(user.ID, "Reading", 1)
	p.ts.newItem(user.ID, other.ID, "Mail", "https://mail.google.com")

	p.visit("/start/edit")
	p.enterGrid()
	p.sendKeys(kb.ArrowDown)

	p.sendKeys(" ")
	p.sendKeys(kb.ArrowDown)
	p.sendKeys(kb.ArrowDown)
	p.sendKeys(" ")

	p.assertText("#start_page_notice", itemNotMoved)
	if got := p.reloadItem(user.ID, gmail.ID).GroupID; got != group.ID {
		t.Errorf("the tile moved to group %d and should not have", got)
	}

	// The client moved it before it asked, so the refusal has to put it back —
	// otherwise the page shows an order the database refused, and the next
	// move computes its position from that page.
	p.assertSelector(groupSel(group) + " " + itemSel(gmail))
	p.assertNoSelector(groupSel(other) + " " + itemSel(gmail))
}

// A notice that outlives its failure is worse than none: it reports the last
// thing you did as broken when it worked.
func TestBrowserALaterSuccessfulMoveClearsTheNotice(t *testing.T) {
	p, user := startPageBrowser(t)
	group, _, _ := p.tiles(user)
	other := p.ts.newGroup(user.ID, "Reading", 1)
	p.ts.newItem(user.ID, other.ID, "Mail", "https://mail.google.com")

	p.visit("/start/edit")
	p.enterGrid()
	p.sendKeys(kb.ArrowDown)

	p.sendKeys(" ")
	p.sendKeys(kb.ArrowDown, kb.ArrowDown)
	p.sendKeys(" ")
	p.assertText("#start_page_notice", itemNotMoved)

	// Somewhere it is allowed to go.
	p.sendKeys(" ")
	p.sendKeys(kb.ArrowDown)
	p.sendKeys(" ")

	p.assertNoSelector(".start-page-notice-error")
	p.waitForDB("the second move to be stored", func() bool {
		return slices.Equal(p.ts.itemTitles(group.ID), []string{"Calendar", "Gmail"})
	})
}

// Declining the confirm submits nothing, so nothing will ever redeem a
// highlight promised for "after the delete" — and the next unrelated render
// would redeem it by yanking focus out of whatever was open by then.
func TestBrowserDecliningADeleteDoesNotLeaveTheHighlightOwing(t *testing.T) {
	p, user := startPageBrowser(t)
	group, _, _ := p.tiles(user)

	p.visit("/start/edit")
	p.enterGrid()
	p.sendKeys(kb.ArrowDown)

	p.onConfirm(false)
	p.sendKeys(kb.Delete)
	// Nothing is submitted, so there is no render to wait on: the dialog
	// having been asked and answered is the whole of the event.
	p.waitForConfirm(1)
	if got := p.ts.itemTitles(group.ID); !slices.Equal(got, []string{"Gmail", "Calendar"}) {
		t.Errorf("stored order = %v, want it untouched", got)
	}

	// Open the add form, which re-renders the group — the focus promise, if it
	// were still owing, gets redeemed here and steals the field.
	p.sendKeys(kb.ArrowDown, kb.ArrowDown)
	p.assertFocusedRow("Add link")
	p.sendKeys(kb.Enter)
	p.fillInLabelled(newItemSel(group.ID), "Title", "Docs")
	p.fillInLabelled(newItemSel(group.ID), "URL", "https://docs.example.com")
	p.clickOn(newItemSel(group.ID), "Add")

	p.assertText(groupSel(group)+" .item-title", "Docs")
	if got := p.focusedTag(); got != "input" {
		t.Errorf("focus is on a %s, want the input the add form left open", got)
	}
}
