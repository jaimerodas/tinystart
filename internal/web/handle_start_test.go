package web

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jaimerodas/tinystart/internal/store"
)

// --- the fixture the three start page suites share ---

// startPageServer is users(:one), signed in, on a grid three columns wide.
// Three because half of these tests are about what happens at the edge of the
// grid, and one column has no edge to speak of.
func startPageServer(t *testing.T) (*testServer, *store.User) {
	t.Helper()
	ts := newTestServer(t)
	user := ts.createApprovedUser("one@example.com")
	ts.setColumns(user, 3)
	ts.signIn(user.Email)
	return ts, user
}

func (ts *testServer) setColumns(user *store.User, columns int) {
	ts.t.Helper()
	if err := ts.db.UpdateColumns(ts.t.Context(), user.ID, columns); err != nil {
		ts.t.Fatalf("setting %s to %d columns: %v", user.Email, columns, err)
	}
	user.Columns = columns
}

func (ts *testServer) newGroup(userID int64, name string, column int) *store.Group {
	ts.t.Helper()
	group, err := ts.db.CreateGroup(ts.t.Context(), userID, name, column)
	if err != nil {
		ts.t.Fatalf("creating group %q: %v", name, err)
	}
	return group
}

func (ts *testServer) newItem(userID, groupID int64, title, itemURL string) *store.Item {
	ts.t.Helper()
	item, err := ts.db.CreateItem(ts.t.Context(), userID, groupID, title, itemURL)
	if err != nil {
		ts.t.Fatalf("creating tile %q: %v", title, err)
	}
	return item
}

func (ts *testServer) group(userID, groupID int64) *store.Group {
	ts.t.Helper()
	group, err := ts.db.GroupByID(ts.t.Context(), userID, groupID)
	if err != nil {
		ts.t.Fatalf("reading group %d: %v", groupID, err)
	}
	return group
}

func (ts *testServer) item(userID, itemID int64) *store.Item {
	ts.t.Helper()
	item, err := ts.db.ItemByID(ts.t.Context(), userID, itemID)
	if err != nil {
		ts.t.Fatalf("reading tile %d: %v", itemID, err)
	}
	return item
}

// groupNames is one column, in order — the assertion a reorder test wants to
// make and the one a position-by-position check makes badly.
func (ts *testServer) groupNames(userID int64, column int) []string {
	ts.t.Helper()
	groups, err := ts.db.GroupsInColumn(ts.t.Context(), userID, column)
	if err != nil {
		ts.t.Fatalf("reading column %d: %v", column, err)
	}
	names := make([]string, len(groups))
	for i, group := range groups {
		names[i] = group.Name
	}
	return names
}

func (ts *testServer) itemTitles(groupID int64) []string {
	ts.t.Helper()
	items, err := ts.db.ItemsInGroup(ts.t.Context(), groupID)
	if err != nil {
		ts.t.Fatalf("reading group %d: %v", groupID, err)
	}
	titles := make([]string, len(items))
	for i, item := range items {
		titles[i] = item.Title
	}
	return titles
}

// flash is the message waiting for the next page, read out of the cookie
// rather than by following the redirect. That is what flash[:notice] was in
// the controller tests this suite is ported from.
//
// It does not clear the cookie the client holds, so a test that writes twice
// and reads once can read the older message. Every test here makes one
// write.
func (ts *testServer) flash() flashMessage {
	ts.t.Helper()
	req := ts.request(http.MethodGet, "/", nil)
	base, err := url.Parse(ts.http.URL)
	if err != nil {
		ts.t.Fatalf("parsing the server URL: %v", err)
	}
	for _, cookie := range ts.client.Jar.Cookies(base) {
		req.AddCookie(cookie)
	}
	messages := ts.app.takeFlash(discardHeader{}, req)
	if len(messages) == 0 {
		return flashMessage{}
	}
	return messages[0]
}

// discardHeader is a ResponseWriter for a response nobody will send.
// takeFlash wants somewhere to put the cookie that clears the flash, and
// here there is nowhere for it to go.
type discardHeader struct{}

func (discardHeader) Header() http.Header         { return http.Header{} }
func (discardHeader) Write(b []byte) (int, error) { return len(b), nil }
func (discardHeader) WriteHeader(int)             {}

func (ts *testServer) assertFlash(kind, message string) {
	ts.t.Helper()
	got := ts.flash()
	if got.Type != kind || got.Message != message {
		ts.t.Errorf("flash = %s %q, want %s %q", got.Type, got.Message, kind, message)
	}
}

// --- the start page ---

// There is no start page to create any more: every user has one from signup.
func TestStartPageShowsWithoutAnySetup(t *testing.T) {
	ts, _ := startPageServer(t)

	ts.get("/").
		assertStatus(http.StatusOK).
		assertContains(`class="start-page-grid" data-columns="3"`)
}

// The page has one URL. /start is still the PATCH target and the prefix every
// group and item route hangs off, but it is not somewhere you can go.
func TestStartPageIsNotServedAtStart(t *testing.T) {
	ts, _ := startPageServer(t)

	ts.get("/start").assertStatus(http.StatusNotFound)
}

// A blank grid is indistinguishable from a broken one, so say which it is.
func TestStartPageTellsAnEmptyGridHowToFillItself(t *testing.T) {
	ts, user := startPageServer(t)

	t.Run("with nothing at all", func(t *testing.T) {
		ts.get("/").
			assertContains(`class="start-page-empty"`).
			assertContains("No links added yet").
			assertContains(`<a href="/start/edit">edit the page</a>`)
	})

	// A group with no tiles still leaves the page blank, so the notice stays.
	group := ts.newGroup(user.ID, "Work", 1)
	t.Run("with a group but no tiles", func(t *testing.T) {
		ts.get("/").assertContains("No links added yet")
	})

	ts.newItem(user.ID, group.ID, "Example", "https://example.com")
	t.Run("once a tile exists", func(t *testing.T) {
		ts.get("/").assertNotContains("start-page-empty")
	})
}

func TestStartPageLaysOutTheUsersColumnCount(t *testing.T) {
	ts, user := startPageServer(t)
	ts.setColumns(user, 5)

	resp := ts.get("/").assertStatus(http.StatusOK)
	resp.assertContains(`data-columns="5"`)
	if got := strings.Count(resp.body, `<div class="start-page-column">`); got != 5 {
		t.Errorf("columns drawn = %d, want 5", got)
	}
}

// The bar filters tiles without a round trip, so the page has to hand it the
// whole list up front.
func TestStartPageEmbedsTheCommandBarsLinks(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Test Group", 1)
	ts.newItem(user.ID, group.ID, "Amazon", "https://amazon.com")
	ts.newItem(user.ID, group.ID, "GitHub", "https://github.com")

	resp := ts.get("/").assertStatus(http.StatusOK)
	resp.assertContains(`class="command-bar"`)
	resp.assertContains(`data-command-bar-target="input"`)
	resp.assertContains(`data-command-bar-target="suggestions"`)
	resp.assertContains(`data-controller="command-bar start-shortcuts"`)

	links := ts.commandBarLinks(resp)
	if len(links) != 2 {
		t.Fatalf("links = %d, want 2", len(links))
	}
	if links[0].Title != "Amazon" || links[0].URL != "https://amazon.com" {
		t.Errorf("first link = %+v", links[0])
	}
}

// commandBarLinks reads the JSON back out of the attribute. That is the only
// way to make sure that what the bar receives is what the database holds.
func (ts *testServer) commandBarLinks(resp *response) []store.Link {
	ts.t.Helper()
	const attribute = `data-command-bar-links-value="`
	start := strings.Index(resp.body, attribute)
	if start < 0 {
		ts.t.Fatal("no data-command-bar-links-value on the page")
	}
	start += len(attribute)
	raw := resp.body[start : start+strings.Index(resp.body[start:], `"`)]
	raw = strings.ReplaceAll(raw, "&#34;", `"`)
	raw = strings.ReplaceAll(raw, "&amp;", "&")

	var links []store.Link
	if err := json.Unmarshal([]byte(raw), &links); err != nil {
		ts.t.Fatalf("parsing %q: %v", raw, err)
	}
	return links
}

// The command bar cannot tell "not connected" from "no matches" on its own, so
// the page has to hand it the state up front.
func TestStartPageTellsTheCommandBarAboutFederation(t *testing.T) {
	ts, user := startPageServer(t)

	t.Run("off without a connection", func(t *testing.T) {
		ts.get("/").
			assertContains(`data-command-bar-federation-value="off"`).
			assertNotContains("search-disconnected")
	})

	connection, err := ts.db.ReplaceConnection(t.Context(), user.ID,
		"https://links.example.com", "mine", "read", ts.clock.Now())
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}

	t.Run("active with a healthy connection", func(t *testing.T) {
		ts.get("/").
			assertContains(`data-command-bar-federation-value="active"`).
			assertContains(`data-command-bar-source-value="links.example.com"`).
			assertNotContains("search-disconnected")
	})

	// A lapsed token has to be visible: silent federated failure looks exactly
	// like an empty archive.
	if err := ts.db.RecordConnectionFailure(t.Context(), connection.ID, "links.example.com rejected the token"); err != nil {
		t.Fatalf("recording the failure: %v", err)
	}

	t.Run("reconnect once the token was rejected", func(t *testing.T) {
		ts.get("/").
			assertContains(`data-command-bar-federation-value="reconnect"`).
			assertContains(`class="search-disconnected"`).
			assertContains("Search of links.example.com is disconnected.").
			assertContains(`<a href="/settings/connections">Reconnect</a>`)
	})
}

// The command bar reads its search engine off the page, not off a default:
// a user who picked Google in Settings gets Google here.
func TestStartPageTellsTheCommandBarTheUsersSearchEngine(t *testing.T) {
	ts, user := startPageServer(t)
	if err := ts.db.UpdatePreferences(t.Context(), user.ID, user.ThemePreference, user.ColorPreference, "google"); err != nil {
		t.Fatalf("setting the search engine: %v", err)
	}

	ts.get("/").assertContains(`data-command-bar-engine-value="google"`)
}

// One person's tiles must never surface in another person's grid or command
// bar.
func TestStartPageShowsOnlyTheSignedInUsersTiles(t *testing.T) {
	ts, user := startPageServer(t)
	mine := ts.newGroup(user.ID, "Mine", 1)
	ts.newItem(user.ID, mine.ID, "Mine", "https://mine.example.com")

	other := ts.createApprovedUser("two@example.com")
	theirs := ts.newGroup(other.ID, "Theirs", 1)
	ts.newItem(other.ID, theirs.ID, "Theirs", "https://theirs.example.com")

	resp := ts.get("/").assertStatus(http.StatusOK)
	resp.assertContains("Mine").assertNotContains("Theirs")

	links := ts.commandBarLinks(resp)
	if len(links) != 1 || links[0].Title != "Mine" {
		t.Errorf("links = %+v, want just Mine", links)
	}
}

// / has a demo for a signed-out visitor now (handle_demo_test.go); only the
// editor still turns them away.
func TestEditorRequiresAuthentication(t *testing.T) {
	ts := newTestServer(t)
	ts.createApprovedUser("one@example.com")

	ts.get("/start/edit").assertRedirect("/sign_in")
}

// --- the editor ---

func TestEditorRenders(t *testing.T) {
	ts, _ := startPageServer(t)

	ts.get("/start/edit").
		assertStatus(http.StatusOK).
		assertContains(`id="start_page_grid"`).
		assertContains(`id="start_page_notice"`).
		assertContains(`id="column_count"`)
}

// The default is one column. If 1 is not on offer, the browser preselects
// the first option, and a user can never get back to one.
func TestEditorOffersEveryColumnCountWithTheCurrentOneSelected(t *testing.T) {
	ts := newTestServer(t)
	user := ts.createApprovedUser("fresh@example.com")
	ts.signIn(user.Email)

	if user.Columns != 1 {
		t.Fatalf("a new account starts on %d columns, want 1", user.Columns)
	}

	resp := ts.get("/start/edit").assertStatus(http.StatusOK)
	resp.assertContains(`<select name="user[columns]" id="user_columns">`)
	resp.assertContains(`<option selected="selected" value="1">1</option>`)
	if got := strings.Count(resp.body, "<option "); got != store.MaxColumns {
		t.Errorf("options = %d, want %d", got, store.MaxColumns)
	}
}

// --- the column count ---
//
// It used to be a field on the Preferences form. It lives in the editor's
// toolbar now, so a refused shrink can answer on the page that shows the
// group it names.

func TestColumnCountUpdateSendsTheEditorBackForARedraw(t *testing.T) {
	ts, user := startPageServer(t)

	ts.send(http.MethodPatch, "/start", form("user[columns]", "5")).
		assertRedirect("/start/edit")

	if got := ts.reloadUser(user).Columns; got != 5 {
		t.Errorf("columns = %d, want 5", got)
	}
}

// A refusal has to redraw, not just report: the select already shows the
// value the database rejected, so it has to be sent back too.
func TestColumnCountRefusesACountOutsideTheRangeAndResetsTheSelect(t *testing.T) {
	ts, user := startPageServer(t)

	resp := ts.turbo(http.MethodPatch, "/start", form("user[columns]", "9"))

	resp.assertStatus(http.StatusUnprocessableEntity)
	// update, not replace: #start_page_notice is a live region.
	resp.assertStreams("update:start_page_notice", "replace:column_count")
	resp.assertContains("Columns must be less than or equal to 6")
	resp.assertContains(`<option selected="selected" value="3">3</option>`)
	// Rails wrapped the fields of a model carrying errors in a
	// .field_with_errors div, which is a block element and breaks the
	// one-line toolbar apart. The notice speaks the refusal instead.
	resp.assertNotContains("field_with_errors")

	if got := ts.reloadUser(user).Columns; got != 3 {
		t.Errorf("columns = %d, want 3", got)
	}
}

// There is no stream to apply without Turbo, so the refusal has to reach the
// user some other way, not as raw <turbo-stream> markup on screen.
func TestColumnCountRefusesInAFlashWithoutTurbo(t *testing.T) {
	ts, user := startPageServer(t)

	ts.send(http.MethodPatch, "/start", form("user[columns]", "9")).
		assertRedirect("/start/edit")
	ts.assertFlash(flashAlert, "Columns must be less than or equal to 6")

	if got := ts.reloadUser(user).Columns; got != 3 {
		t.Errorf("columns = %d, want 3", got)
	}
}

// Saying only "failed" leaves you to pick the same value again and again.
func TestColumnCountSaysWhichGroupBlocksAShrink(t *testing.T) {
	ts, user := startPageServer(t)
	ts.newGroup(user.ID, "Reading", 3)

	resp := ts.turbo(http.MethodPatch, "/start", form("user[columns]", "1"))

	resp.assertStatus(http.StatusUnprocessableEntity)
	resp.assertContains("that would hide &#34;Reading&#34;")

	if got := ts.reloadUser(user).Columns; got != 3 {
		t.Errorf("columns = %d, want 3", got)
	}
}

func TestColumnCountCannotBeSetForAnotherUser(t *testing.T) {
	ts, user := startPageServer(t)
	other := ts.createApprovedUser("two@example.com")

	ts.send(http.MethodPatch, "/start", form("user[columns]", "6"))

	if got := ts.reloadUser(other).Columns; got != 1 {
		t.Errorf("the other user's columns = %d, want 1", got)
	}
	if got := ts.reloadUser(user).Columns; got != 6 {
		t.Errorf("columns = %d, want 6", got)
	}
}

func (ts *testServer) reloadUser(user *store.User) *store.User {
	ts.t.Helper()
	reloaded, err := ts.db.UserByID(ts.t.Context(), user.ID)
	if err != nil {
		ts.t.Fatalf("reloading %s: %v", user.Email, err)
	}
	return reloaded
}
