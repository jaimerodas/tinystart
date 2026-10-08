//go:build browser

// The rest of test/system/start_page_integration_test.rb — the command bar and
// the visit counter — plus test/system/import_export_test.rb. Plus the two
// journeys nothing else in this package drives with a browser: signing in
// through the form, and the theme picker writing on <html>.
package web

import (
	"encoding/json"
	"fmt"
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

	p.assertText(".flash", "Try another email address or password")
	if got := p.currentPath(); got != "/sign_in" {
		t.Errorf("path = %q, want to still be on the form", got)
	}

	// The flash sits in the page, not over it, so the second attempt does not
	// have to get it out of the way first.
	p.fillIn("#email", user.Email)
	p.fillIn("#password", testPassword)
	p.click(`input[value="Sign in"]`)

	p.assertSelector("main.start-page")
	if got := p.currentPath(); got != "/" {
		t.Errorf("path = %q, want /", got)
	}
}

// Nothing takes the flash away on a timer: an error that leaves by itself can
// leave before it is read. Its own button closes it.
func TestBrowserTheFlashClosesWithItsButton(t *testing.T) {
	p := newBrowserPage(t)
	user := p.ts.createApprovedUser("one@example.com")

	p.visit("/sign_in")
	p.fillIn("#email", user.Email)
	p.fillIn("#password", "the wrong one")
	p.click(`input[value="Sign in"]`)
	p.assertSelector(".flash")

	p.clickOn(".flash", "Dismiss")
	p.assertNoSelector(".flash")
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
		{"/settings", "#user-preferences"},
		{"/settings/password/edit", "form"},
		{"/settings/import_export", "#import-export"},
		{"/settings/connections", "#connection-settings"},
		{"/settings/browsers", "#chrome-extension"},
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

// The tile somebody opens most is the one they most likely want again, so it
// goes first, ahead of the alphabet.
func TestBrowserCommandBarPutsTheMostVisitedTileFirst(t *testing.T) {
	p, user := startPageBrowser(t)
	group := p.tilesForFiltering(user)
	apple := p.ts.newItem(user.ID, group.ID, "Apple Music", "https://music.apple.com")
	for range 3 {
		if err := p.ts.db.IncrementVisitCount(t.Context(), user.ID, apple.ID); err != nil {
			t.Fatalf("IncrementVisitCount: %v", err)
		}
	}

	p.visit("/")
	p.fillIn(".command-bar input", "a")
	p.assertText(".command-bar-suggestions", "Apple Music")

	// The last row is not a tile. It searches for what was typed, and it comes
	// after every tile.
	got := p.texts(".command-bar-suggestion .suggestion-title")
	want := []string{"Apple Music", "Amazon Shopping", "Apple", "Search DuckDuckGo for “a”"}
	if !slices.Equal(got, want) {
		t.Errorf("suggestions = %q, want %q", got, want)
	}
}

// A tile is often remembered by its address and not by its title. The scheme
// is not part of what is matched: "https" is in every URL, so one letter of it
// would match every tile.
func TestBrowserCommandBarMatchesTheURL(t *testing.T) {
	p, user := startPageBrowser(t)
	group := p.tilesForFiltering(user)
	p.ts.newItem(user.ID, group.ID, "Mail", "https://app.fastmail.com")

	p.visit("/")
	p.fillIn(".command-bar input", "fastm")

	p.assertText(".command-bar-suggestions", "Mail")

	p.fillIn(".command-bar input", "https")

	// No tile: the only row is the search for what was typed.
	got := p.texts(".command-bar-suggestion .suggestion-title")
	if want := []string{"Search DuckDuckGo for “https”"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

// Matching on the address has a cost: "prusa3d.com" matches a tile for
// connect.prusa3d.com, and Enter opens the tile. The row after the tiles goes
// to what was typed instead. The tile stays the default, so it gets no visit.
func TestBrowserCommandBarCanGoToTheTypedAddressPastAMatchingTile(t *testing.T) {
	p, user := startPageBrowser(t)
	group := p.ts.newGroup(user.ID, "Making", 1)
	item := p.ts.newItem(user.ID, group.ID, "Prusa Connect", "https://connect.prusa3d.com")

	p.visit("/")
	p.fillIn(".command-bar input", "prusa3d.com")

	p.assertText(".command-bar-suggestion.selected", "Prusa Connect")
	p.assertText(".command-bar-suggestion", "Go to prusa3d.com")

	p.sendKeys(kb.ArrowDown, kb.Enter)

	p.assertNavigatedTo("https://prusa3d.com/")
	if got := p.reloadItem(user.ID, item.ID).VisitCount; got != 0 {
		t.Errorf("visit count = %d, want 0: the tile was not opened", got)
	}
}

// With no tile to match, the row is still there, and it is highlighted: the
// bar always shows where Enter goes.
func TestBrowserCommandBarSaysWhereEnterGoesWhenNoTileMatches(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tilesForFiltering(user)

	p.visit("/")
	p.fillIn(".command-bar input", "prusa3d.com")

	got := p.texts(".command-bar-suggestion .suggestion-title")
	if want := []string{"Go to prusa3d.com"}; !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}
	p.assertText(".command-bar-suggestion.selected", "Go to prusa3d.com")

	p.sendKeys(kb.Enter)

	p.assertNavigatedTo("https://prusa3d.com/")
}

// The same row for text that is not an address: a tile called GitHub must not
// make a web search for "github" impossible.
func TestBrowserCommandBarCanSearchForTextThatMatchesATile(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tilesForFiltering(user)

	p.visit("/")
	p.fillIn(".command-bar input", "github")

	p.assertText(".command-bar-suggestion", "Search DuckDuckGo for “github”")

	p.sendKeys(kb.ArrowDown, kb.Enter)

	p.assertNavigatedTo("https://duckduckgo.com/?q=github")
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

// There is no save button: picking is saving. What a pick changes is
// attributes on <html>, which the controller writes as the pick is made, so
// this is only true in a browser. The save answers in the heading row, and no
// flash comes back to cover anything.
func TestBrowserThemePickerSavesAsItChanges(t *testing.T) {
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

	p.waitFor(`document.documentElement.dataset.theme === "dark" &&
		document.documentElement.dataset.color === "purple"`,
		"the theme and colour to be written on <html>")
	p.waitForDB("the theme and colour to be stored", func() bool {
		stored := p.ts.reloadUser(user)
		return stored.ThemePreference == "dark" && stored.ColorPreference == "purple"
	})
	p.assertText("#preferences_status", "Saved")
	p.assertNoSelectorNow(".flash")
}

// The typeface changes without a reload, like the theme. The controller
// writes data-font on <html>, and the page already links every family it
// offers, so the new one is there to switch to. The test cannot see the font
// itself, because the test browser cannot reach Google Fonts, so it reads the
// computed family.
func TestBrowserTypefacePickerSwitchesTheFont(t *testing.T) {
	p, user := startPageBrowser(t)

	p.visit("/settings")
	if got := p.evalString(`document.documentElement.dataset.font`); got != "geist" {
		t.Errorf("font = %q, want the default geist", got)
	}

	p.click("#font_literata")

	p.waitFor(`document.documentElement.dataset.font === "literata"`, "the font to be written on <html>")
	p.assertPresent(`link[href*="family=Literata"]`)
	if got := p.evalString(`getComputedStyle(document.body).fontFamily`); !strings.HasPrefix(got, "Literata") {
		t.Errorf("body font-family = %q, want Literata first", got)
	}

	p.waitForDB("the font to be stored", func() bool {
		return p.ts.reloadUser(user).FontPreference == "literata"
	})
}

// In the picker, each choice is set in the typeface it picks, whatever the
// page itself is in.
func TestBrowserEachTypefaceChoiceIsSetInItsOwnFont(t *testing.T) {
	p, _ := startPageBrowser(t)

	p.visit("/settings")
	for label, want := range map[string]string{"font_geist": "Geist", "font_literata": "Literata"} {
		got := p.evalString(fmt.Sprintf(`getComputedStyle(document.querySelector('label[for=%q]')).fontFamily`, label))
		if !strings.HasPrefix(got, want) {
			t.Errorf("%s is set in %q, want %s first", label, got, want)
		}
	}
	if got := p.evalString(`getComputedStyle(document.body).fontFamily`); !strings.HasPrefix(got, "Geist") {
		t.Errorf("the page is set in %q, want Geist first", got)
	}
}

// === COLOUR ===

// Every accent, in both themes, keeps the 4.5:1 that WCAG AA asks of text: a
// link on the page, and a button's label on the accent that fills it. The
// eight accents go from L 0.55 to L 0.82, so no one fixed offset from the
// accent can do this. The colour has to come from a clamp.
func TestBrowserEveryAccentKeepsTextReadable(t *testing.T) {
	p, user := startPageBrowser(t)
	group := p.ts.newGroup(user.ID, "Daily", 1)
	p.ts.newItem(user.ID, group.ID, "Example", "https://example.com")

	p.visit("/settings")
	failures := p.contrastFailures(`a[href="/settings/password/edit"]`, ".user-section h2")
	// The main page saves as it changes, so it has no button to measure.
	p.visit("/settings/browsers")
	failures = append(failures, p.contrastFailures(".action-button")...)

	// The start page puts the accent on the group names, and tints the
	// background behind the tiles.
	p.visit("/")
	failures = append(failures, p.contrastFailures(".start-page-grid section > h2")...)
	for _, failure := range failures {
		t.Error(failure)
	}
}

// The command bar draws a ring when it has focus. The border changes colour
// too, but that alone is not enough: on yellow the change is 1.76:1. The ring
// was in the stylesheet before and never drew, because rgba() cannot hold an
// oklch() colour, and the browser drops the whole declaration.
func TestBrowserTheCommandBarShowsItsFocus(t *testing.T) {
	p, _ := startPageBrowser(t)

	p.waitFor(`document.activeElement.matches(".command-bar input")`, "the command bar to have focus")
	if got := p.evalString(`getComputedStyle(document.activeElement).boxShadow`); got == "none" {
		t.Error("the focused command bar has no ring")
	}
}

// contrastFailures sets each accent in each theme on <html> and measures each
// selector's text against the first opaque background behind it. It returns
// one line for each pair below 4.5:1.
//
// A canvas converts each computed colour to sRGB, because the computed value
// can be oklch() or display-p3, and the contrast formula is for sRGB. The
// style it adds stops the transitions, so the page shows the new colours at
// once and not part of the way there.
func (p *browserPage) contrastFailures(selectors ...string) []string {
	p.t.Helper()
	args, err := json.Marshal([]any{store.ValidColors, selectors})
	if err != nil {
		p.t.Fatal(err)
	}
	return p.eval[[]string](`((colors, selectors) => {
		const stop = document.createElement("style");
		stop.textContent = "* { transition: none !important }";
		document.head.append(stop);

		const canvas = document.createElement("canvas").getContext("2d", { willReadFrequently: true });
		const rgba = css => {
			canvas.clearRect(0, 0, 1, 1);
			canvas.fillStyle = css;
			canvas.fillRect(0, 0, 1, 1);
			return [...canvas.getImageData(0, 0, 1, 1).data];
		};
		const luminance = rgb => {
			const [r, g, b] = rgb.slice(0, 3).map(v => (v /= 255) <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
			return 0.2126 * r + 0.7152 * g + 0.0722 * b;
		};
		const background = el => {
			for (; el; el = el.parentElement) {
				const color = rgba(getComputedStyle(el).backgroundColor);
				if (color[3] > 0) return color;
			}
			return [255, 255, 255, 255];
		};

		const failures = [];
		for (const theme of ["light", "dark"]) {
			for (const color of colors) {
				document.documentElement.dataset.theme = theme;
				document.documentElement.dataset.color = color;
				for (const selector of selectors) {
					const el = document.querySelector(selector);
					const [hi, lo] = [luminance(rgba(getComputedStyle(el).color)), luminance(background(el))].sort((a, b) => b - a);
					const ratio = (hi + 0.05) / (lo + 0.05);
					if (ratio < 4.5) failures.push(theme + " " + color + " " + selector + ": " + ratio.toFixed(2) + ":1");
				}
			}
		}
		return failures;
	})(...` + string(args) + `)`)
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

	p.assertText(".flash", "Imported 6 links")
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
