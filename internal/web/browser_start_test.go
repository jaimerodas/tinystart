//go:build browser

// The rest of test/system/start_page_integration_test.rb — the command bar and
// the visit counter — plus test/system/import_export_test.rb. Plus the two
// journeys nothing else in this package drives with a browser: signing in
// through the form, and the theme picker writing on <html>.
package web

import (
	"slices"
	"strings"
	"testing"

	"github.com/chromedp/chromedp/kb"
	"github.com/jaimerodas/tinystart/internal/store"
)

// === SIGNING IN ===

// Everything else here starts signed in. This is the one that says the form
// itself works in a browser. The cookie is set, the redirect is followed, and
// the page that comes back is the start page.
func TestBrowserSignInThroughTheForm(t *testing.T) {
	p := newBrowserPage(t)
	user := p.ts.createApprovedUser("one@example.com")

	p.visit("/sign_in")
	p.fillIn("#email", user.Email)
	p.fillIn("#password", "the wrong one")
	p.click(`input[value="Sign in"]`)

	p.assertText(".flash-card", "Try another email address or password")
	if got := p.currentPath(); got != "/sign_in" {
		t.Errorf("path = %q, want to still be on the form", got)
	}
	// The flash is a full-screen overlay while it is up, so a second attempt
	// starts by getting it out of the way. That is the whole of what the
	// flash controller does.
	p.dismissFlash()

	p.fillIn("#email", user.Email)
	p.fillIn("#password", testPassword)
	p.click(`input[value="Sign in"]`)

	p.assertSelector("main.start-page")
	if got := p.currentPath(); got != "/" {
		t.Errorf("path = %q, want /", got)
	}
}

// Every page, opened in a browser, with nothing thrown. The harness fails a
// test on any uncaught exception, so this is a real assertion and not a tour.
// The importmap eager-loads every Stimulus controller by name. A module that
// 404s, or a controller that throws on connect, only says so here.
func TestBrowserEveryPageLoadsWithoutAScriptError(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tiles(user)

	pages := []struct{ path, marker string }{
		{"/", ".command-bar"},
		{"/start/edit", ".editor-toolbar"},
		{"/settings", "#user-display"},
		{"/settings/password/edit", "form"},
		{"/settings/import_export", "#import-export"},
		{"/settings/connections", "#connection-settings"},
		{"/settings/admin/users", "#users-list"},
	}
	for _, each := range pages {
		p.visit(each.path)
		p.assertSelector(each.marker)
		// Stimulus is what everything on these pages hangs off, and a page
		// whose application.js never ran looks exactly like one whose
		// controllers are all idle.
		if !p.evalBool(`window.Stimulus !== undefined || document.querySelector("[data-controller]") !== null`) {
			t.Errorf("%s has no Stimulus controller on it", each.path)
		}
	}

}

// The same for the pages on the other side of the wall, which need a tab with
// no session in it.
func TestBrowserThePagesForVisitorsLoadWithoutAScriptError(t *testing.T) {
	p := newBrowserPage(t)
	p.ts.createApprovedUser("one@example.com")

	for _, each := range []struct{ path, marker string }{
		{"/sign_in", "#login form"},
		{"/sign_up", "form"},
		{"/passwords/new", "form"},
		{"/", ".demo-cta"},
	} {
		p.visit(each.path)
		p.assertSelector(each.marker)
	}
}

// === THE COMMAND BAR ===

// The tiles the filtering tests search through.
func (p *browserPage) tilesForFiltering(user *store.User) *store.Group {
	p.t.Helper()
	group := p.ts.newGroup(user.ID, "Shopping", 1)
	p.ts.newItem(user.ID, group.ID, "Amazon Shopping", "https://amazon.com")
	p.ts.newItem(user.ID, group.ID, "Apple", "https://apple.com")
	p.ts.newItem(user.ID, group.ID, "GitHub", "https://github.com")
	return group
}

func TestBrowserCommandBarFiltersTheTilesOnThePage(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tilesForFiltering(user)

	p.visit("/")
	p.assertSelector(".command-bar input[autofocus]")

	p.fillIn(".command-bar input", "a")

	p.assertText(".command-bar-suggestions", "Amazon Shopping")
	p.assertText(".command-bar-suggestions", "Apple")
	p.assertNoTextNow(".command-bar-suggestions", "GitHub")

	p.fillIn(".command-bar input", "")
	p.assertNoSelector(".command-bar-suggestions")

	// Matching is case-insensitive.
	p.fillIn(".command-bar input", "APPLE")

	p.assertText(".command-bar-suggestions", "Apple")
	p.assertNoTextNow(".command-bar-suggestions", "Amazon")
	p.assertNoTextNow(".command-bar-suggestions", "GitHub")
}

// The signed-out twin of the test above: no account, no tiles from the
// database, just the fixed demo grid — but the same local filtering over it.
func TestBrowserDemoCommandBarFiltersTheDemoTiles(t *testing.T) {
	p := newBrowserPage(t)

	p.visit("/")
	p.assertSelector(".command-bar input[autofocus]")

	p.fillIn(".command-bar input", "gmail")

	p.assertText(".command-bar-suggestions", "Gmail")
	p.assertNoTextNow(".command-bar-suggestions", "GitHub")

	p.fillIn(".command-bar input", "")
	p.assertNoSelector(".command-bar-suggestions")
}

// Nothing to federate to means no "All Links" at all — not a header that
// flashes "Searching..." and then quietly empties itself.
func TestBrowserCommandBarOffersNoAllLinksWithoutAConnection(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tilesForFiltering(user)

	p.visit("/")
	p.fillIn(".command-bar input", "a")

	// The local results and the All Links header used to render in the same
	// tick, so these are checked without waiting. A patient assertion passes
	// either way once /search.json answers with an empty list.
	p.assertText(".command-bar-suggestions", "Amazon Shopping")
	p.assertCountNow(".command-bar-section-header", 1)
	p.assertNoSelectorNow(".command-bar-searching")
}

// A rejected token is worth saying out loud, but retrying it is not.
func TestBrowserCommandBarSaysSoOnceTheTokenWasRejected(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tilesForFiltering(user)
	connection := p.ts.connect(user, "https://links.example.com")
	if err := p.ts.db.RecordConnectionFailure(t.Context(), connection.ID,
		"links.example.com rejected the token"); err != nil {
		t.Fatalf("recording the failure: %v", err)
	}

	p.visit("/")
	p.fillIn(".command-bar input", "a")

	p.assertText(".command-bar-suggestions", "Amazon Shopping")
	p.assertText(".command-bar-notice", "links.example.com search disconnected — reconnect in Settings.")
	p.assertCountNow(".command-bar-section-header", 1)
	p.assertNoSelectorNow(".command-bar-searching")
}

// === VISITS ===

func TestBrowserClickingATileRecordsAVisit(t *testing.T) {
	p, user := startPageBrowser(t)
	group := p.ts.newGroup(user.ID, "Tools", 1)
	// Point at the in-app health route so the same-tab navigation stays
	// same-origin and resolves instantly, with nothing to fetch off the
	// machine.
	item := p.ts.newItem(user.ID, group.ID, "Health Check", p.ts.http.URL+"/up")

	p.visit("/")
	p.click(`a[data-item-id="` + itemDOMID(item.ID)[len("item_"):] + `"]`)

	p.assertVisitRecorded(user.ID, item.ID)
}

func TestBrowserSelectingASuggestionRecordsAVisit(t *testing.T) {
	p, user := startPageBrowser(t)
	group := p.ts.newGroup(user.ID, "Shopping", 1)
	item := p.ts.newItem(user.ID, group.ID, "Apple", p.ts.http.URL+"/up")

	p.visit("/")
	p.fillIn(".command-bar input", "Apple")
	p.assertText(".command-bar-suggestions", "Apple")

	p.sendKeys(kb.Enter)

	p.assertVisitRecorded(user.ID, item.ID)
}

// assertVisitRecorded polls the database: the visit is a fire-and-forget POST
// the page does not wait for, and the click that sends it also navigates away.
func (p *browserPage) assertVisitRecorded(userID, itemID int64) {
	p.t.Helper()
	p.waitForDB("the visit to be counted", func() bool {
		return p.reloadItem(userID, itemID).VisitCount >= 1
	})
	if got := p.reloadItem(userID, itemID).VisitCount; got != 1 {
		p.t.Errorf("visit count = %d, want 1", got)
	}
}

// === THEME ===

// A form like any other stores the preferences, but what they change is two
// attributes on <html>. The controller writes them from the submit event
// rather than waiting for a reload, so this is only true in a browser.
func TestBrowserThemePickerWritesOnTheHTMLElement(t *testing.T) {
	p, user := startPageBrowser(t)

	p.visit("/settings")
	if got := p.evalString(`document.documentElement.dataset.theme`); got != "system" {
		t.Errorf("theme = %q, want the default system", got)
	}

	p.click("#theme_dark")
	// The color radios are opacity: 0 behind their swatches. So the swatch —
	// the label — is what there is to click, for a test as much as for anyone
	// else.
	p.click(`label[for="color_purple"]`)
	p.clickOn("#user-display", "Save display preferences")

	p.waitFor(`document.documentElement.dataset.theme === "dark" &&
		document.documentElement.dataset.color === "purple"`,
		"the theme and colour to be written on <html>")

	stored := p.ts.reloadUser(user)
	if stored.ThemePreference != "dark" || stored.ColorPreference != "purple" {
		t.Errorf("stored preferences = %q %q, want dark purple",
			stored.ThemePreference, stored.ColorPreference)
	}
}

// === IMPORT AND EXPORT ===

// The controller test already drives a real multipart POST, so what is left
// here is the half that only exists on the client. That is the confirm that
// stands between a click and the page replacement.
func TestBrowserImportAsksBeforeItReplacesThePage(t *testing.T) {
	p, user := startPageBrowser(t)
	group := p.ts.newGroup(user.ID, "Lo de siempre", 1)
	p.ts.newItem(user.ID, group.ID, "Fastmail", "https://app.fastmail.com")

	p.visit("/settings/import_export")
	p.attachFile("#file", "testdata/start_page.yml")

	p.onConfirm(true)
	p.clickOn("", "Import")

	p.assertText(".flash-card", "Imported 6 links")
	if asked := p.waitForConfirm(1); !strings.Contains(asked[0], "replaces every group and link") {
		t.Errorf("the confirm asked %q", asked[0])
	}
	p.waitForDB("the import to land", func() bool {
		groups, err := p.ts.db.GroupsByColumn(t.Context(), user.ID)
		if err != nil {
			t.Fatalf("reading the start page: %v", err)
		}
		return len(groups[1])+len(groups[2]) == 3
	})
	if got := p.ts.reloadUser(user).Columns; got != 2 {
		t.Errorf("columns = %d, want the 2 the file asks for", got)
	}
}

func TestBrowserDismissingTheImportConfirmLeavesThePageAlone(t *testing.T) {
	p, user := startPageBrowser(t)
	group := p.ts.newGroup(user.ID, "Lo de siempre", 1)
	p.ts.newItem(user.ID, group.ID, "Fastmail", "https://app.fastmail.com")

	p.visit("/settings/import_export")
	p.attachFile("#file", "testdata/start_page.yml")

	p.onConfirm(false)
	p.clickOn("", "Import")
	p.waitForConfirm(1)

	p.assertNoTextNow("body", "Imported")
	if got := p.ts.groupNames(user.ID, 1); !slices.Equal(got, []string{"Lo de siempre"}) {
		t.Errorf("column 1 = %v, want it untouched", got)
	}
}

// Turbo Drive intercepts link clicks and has nothing to do with an attachment
// response, so the export link opts out of it. Asserted here rather than left
// to the handler test, because it is the client half of the download.
func TestBrowserTheExportLinkOptsOutOfTurbo(t *testing.T) {
	p, _ := startPageBrowser(t)

	p.visit("/settings/import_export")
	p.assertSelector(`a[href="/settings/export"][data-turbo="false"]`)
}
