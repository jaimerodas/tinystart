//go:build browser

// test/system/start_page_shortcuts_test.rb, ported.
//
// The chords that move between the two states of the start page, and the ? that
// lists every key either of them answers to.
//
// Both chords are matched on event.code rather than event.key, because on a Mac
// ⌥E is a dead key and ⌥S is ß. The harness sends Alt held over the physical
// key, with the character that key would type, which is exactly what the
// controller reads and what "swallowed" has to defeat.
package web

import (
	"slices"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// commandBarValue is what is in the bar, which several of these turn on.
func (p *browserPage) commandBarValue() string {
	p.t.Helper()
	return p.evalString(`document.querySelector('[data-command-bar-target="input"]').value`)
}

// === THE CHORDS ===

// The command bar has autofocus, so this is also the case that matters: the
// chord has to work from inside a text field rather than in spite of it.
func TestBrowserAltEOpensTheEditor(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tiles(user)

	p.visit("/")
	if got := p.focusedTag(); got != "input" {
		t.Errorf("focus is on a %s, want the autofocused command bar", got)
	}

	p.altE()

	p.assertSelector(".editor-toolbar")
	if got := p.currentPath(); got != "/start/edit" {
		t.Errorf("path = %q, want /start/edit", got)
	}
}

func TestBrowserAltSGoesBackToTheStartPage(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tiles(user)

	p.visit("/start/edit")
	p.altS()

	p.assertSelector(".command-bar")
	if got := p.currentPath(); got != "/" {
		t.Errorf("path = %q, want /", got)
	}
}

// Each chord is a no-op on the page it would take you to. It is still
// swallowed there — otherwise ⌥S would type ß into the search box.
func TestBrowserAltSOnTheStartPageGoesNowhereAndTypesNothing(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tiles(user)

	p.visit("/")
	p.altS()

	p.assertNoSelectorNow(".editor-toolbar")
	if got := p.commandBarValue(); got != "" {
		t.Errorf("the command bar reads %q, want it empty", got)
	}
}

func TestBrowserAltEInTheEditorGoesNowhere(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tiles(user)

	p.visit("/start/edit")
	p.altE()

	p.assertNoSelectorNow(".command-bar")
}

// Letting go commits, whichever way you let go. Leaving by chord with a tile
// still in hand has to save it, exactly as Tab and clicking away already do —
// otherwise the move is lost on the way out.
func TestBrowserAltSWhileCarryingSavesTheMove(t *testing.T) {
	p, user := startPageBrowser(t)
	group, _, _ := p.tiles(user)

	p.visit("/start/edit")
	p.enterGrid()
	p.sendKeys(kb.ArrowDown)
	p.assertFocusedRow("Gmail")

	p.sendKeys(" ")
	p.sendKeys(kb.ArrowDown)
	p.altS()

	p.assertSelector(".command-bar")
	// Dropped before the visit is asked for, so the page you land on is
	// already showing the new order rather than the one the move left behind.
	if got := p.texts(".start-page-grid li"); !slices.Equal(got, []string{"Calendar", "Gmail"}) {
		t.Errorf("the page shows %v, want [Calendar Gmail]", got)
	}
	if got := p.ts.itemTitles(group.ID); !slices.Equal(got, []string{"Calendar", "Gmail"}) {
		t.Errorf("stored order = %v, want [Calendar Gmail]", got)
	}
}

// Reading the shortcuts is not letting go of what you are carrying — half of
// the list is how to move it, and Esc is in there as the way to change your
// mind. Opening it takes focus the way a click outside the grid does, which is
// what commits a move, so this is the case that has to be exempt.
func TestBrowserAskingForTheShortcutsMidCarryDoesNotCommit(t *testing.T) {
	p, user := startPageBrowser(t)
	group, _, _ := p.tiles(user)

	p.visit("/start/edit")
	p.enterGrid()
	p.sendKeys(kb.ArrowDown)

	p.sendKeys(" ")
	p.sendKeys(kb.ArrowDown)
	p.sendKeys("?")
	p.assertSelector(".shortcuts-dialog[open]")

	p.sendKeys(kb.Escape)
	p.assertNoSelector(".shortcuts-dialog[open]")

	// Still in hand, and nothing was saved on the way in or out.
	p.assertSelector(".start-page-item.grabbed")
	if got := p.ts.itemTitles(group.ID); !slices.Equal(got, []string{"Gmail", "Calendar"}) {
		t.Errorf("stored order = %v, want it untouched", got)
	}
}

// === THE DIALOG ===

// showModal() sets the open attribute, and Turbo photographs the page with it
// still set. Restoring that snapshot brings the panel back rendered inline —
// no backdrop, no top layer — where Esc cannot reach it and ? will not reopen
// something it already believes is open.
func TestBrowserTheShortcutsListDoesNotComeBackOpen(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tiles(user)

	p.visit("/start/edit")
	p.sendKeys("?")
	p.assertSelector(".shortcuts-dialog[open]")

	// Leaving with it still up is the whole point: that is the page Turbo
	// caches.
	p.altS()
	p.assertSelector(".command-bar")

	p.run(chromedp.NavigateBack())

	p.assertSelector(".editor-toolbar")
	p.assertNoSelector(".shortcuts-dialog[open]")
}

func TestBrowserQuestionMarkListsTheShortcuts(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tiles(user)

	p.visit("/start/edit")
	p.sendKeys("?")

	p.assertSelector(".shortcuts-dialog[open]")
	p.assertText(".shortcuts-dialog", "back to the start page")
	p.assertText(".shortcuts-dialog", "pick up / drop")

	p.sendKeys(kb.Escape)
	p.assertNoSelector(".shortcuts-dialog[open]")
}

func TestBrowserTheStartPageListNamesTheEditorChord(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tiles(user)

	p.visit("/")
	p.sendKeys(kb.Escape) // off the command bar, which has autofocus
	p.sendKeys("?")

	p.assertSelector(".shortcuts-dialog[open]")
	p.assertText(".shortcuts-dialog", "edit the start page")
}

// ? is a shortcut only when nothing is being typed into. The command bar is
// autofocused, so without this guard it could never be searched for.
func TestBrowserQuestionMarkInTheCommandBarIsACharacter(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tiles(user)

	p.visit("/")
	p.sendKeys("?")

	p.assertNoSelectorNow(".shortcuts-dialog[open]")
	if got := p.commandBarValue(); got != "?" {
		t.Errorf("the command bar reads %q, want %q", got, "?")
	}
}

// And the guard is only survivable because escape gets you off the bar: it is
// the only way to reach anything else on this page by keyboard.
func TestBrowserEscapeOnAnEmptyCommandBarStepsOutOfIt(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tiles(user)

	p.visit("/")
	if got := p.focusedTag(); got != "input" {
		t.Errorf("focus is on a %s, want the command bar", got)
	}

	p.sendKeys(kb.Escape)
	if got := p.focusedTag(); got == "input" {
		t.Errorf("focus is still on the command bar")
	}
}

func TestBrowserTheFirstEscapeClearsTheBarAndTheSecondLeavesIt(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tiles(user)

	p.visit("/")
	p.sendKeys("gm")
	if got := p.commandBarValue(); got != "gm" {
		t.Errorf("the command bar reads %q, want %q", got, "gm")
	}

	p.sendKeys(kb.Escape)
	if got := p.commandBarValue(); got != "" {
		t.Errorf("the command bar reads %q, want it cleared", got)
	}
	if got := p.focusedTag(); got != "input" {
		t.Errorf("focus is on a %s, want it still on the command bar", got)
	}

	p.sendKeys(kb.Escape)
	if got := p.focusedTag(); got == "input" {
		t.Errorf("focus is still on the command bar")
	}
}
