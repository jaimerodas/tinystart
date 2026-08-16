package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jaimerodas/tinystart/internal/store"
)

// fakeApp stands in for the connected app: a real server on a real port, so
// that the client under test does real HTTP and nothing is mocked. Its fields
// are what a test wants to vary — what the other app answers, and what it was
// asked.
type fakeApp struct {
	*httptest.Server

	mu sync.Mutex
	// status and body are what /api/v1/search answers.
	status int
	body   string
	// visitStatus is what a visit answers, and visited is every link id one
	// was recorded for.
	visitStatus int
	visited     []string
	// queries is every q the search was asked for.
	queries []string
}

func newFakeApp(t *testing.T) *fakeApp {
	t.Helper()
	app := &fakeApp{
		status:      http.StatusOK,
		body:        `{"links":[{"id":1,"title":"Alpha","url":"https://a.example"}]}`,
		visitStatus: http.StatusOK,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/search", func(w http.ResponseWriter, r *http.Request) {
		app.mu.Lock()
		defer app.mu.Unlock()
		app.queries = append(app.queries, r.URL.Query().Get("q"))
		w.WriteHeader(app.status)
		w.Write([]byte(app.body)) //nolint:errcheck // a test server writing to a test client
	})
	mux.HandleFunc("POST /api/v1/links/{id}/visit", func(w http.ResponseWriter, r *http.Request) {
		app.mu.Lock()
		defer app.mu.Unlock()
		app.visited = append(app.visited, r.PathValue("id"))
		w.WriteHeader(app.visitStatus)
	})

	app.Server = httptest.NewServer(mux)
	t.Cleanup(app.Close)
	return app
}

func (a *fakeApp) answer(status int, body string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.status, a.body = status, body
}

func (a *fakeApp) answerVisit(status int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.visitStatus = status
}

func (a *fakeApp) recordedVisits() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.visited...)
}

func (a *fakeApp) askedFor() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.queries...)
}

// connect gives a user a connection to an address, which is what approving a
// device grant leaves behind.
func (ts *testServer) connect(user *store.User, baseURL string) *store.Connection {
	ts.t.Helper()
	connection, err := ts.db.ReplaceConnection(context.Background(), user.ID,
		baseURL, "a-token", "search,visit", time.Time{})
	if err != nil {
		ts.t.Fatalf("connecting %s to %s: %v", user.Email, baseURL, err)
	}
	return connection
}

// lastError is what the connection has recorded, which is what the start page
// reads to decide whether to offer a reconnect.
func (ts *testServer) lastError(userID int64) string {
	ts.t.Helper()
	connection, err := ts.db.ConnectionForUser(context.Background(), userID)
	if err != nil {
		ts.t.Fatalf("reading the connection: %v", err)
	}
	return connection.LastError
}

func TestSearchRequiresAuthentication(t *testing.T) {
	ts := newTestServer(t)
	ts.get("/search.json?q=alpha").assertRedirect("/session/new")
}

func TestSearchReturnsTheResultsAsABareArray(t *testing.T) {
	ts := newTestServer(t)
	app := newFakeApp(t)
	user := ts.createUser("one@example.com")
	ts.connect(user, app.URL)
	ts.signIn(user.Email)

	resp := ts.get("/search.json?q=alpha")

	resp.assertStatus(http.StatusOK)
	if got := resp.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want JSON", got)
	}
	if resp.body != `[{"id":1,"title":"Alpha","url":"https://a.example"}]` {
		t.Errorf("body = %s", resp.body)
	}
	if got := app.askedFor(); len(got) != 1 || got[0] != "alpha" {
		t.Errorf("the other app was asked for %v, want [alpha]", got)
	}
}

// The command bar treats an empty list as "no federated results" and carries
// on showing local tiles, so every downstream failure has to land here as [] —
// and as [], not as null, which is what a nil slice would have encoded to.
func TestSearchDegradesToAnEmptyArray(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testServer, *fakeApp, *store.User)
	}{
		{"the app was never connected", func(ts *testServer, _ *fakeApp, _ *store.User) {}},
		{"the app answers with something that isn't JSON", func(ts *testServer, app *fakeApp, user *store.User) {
			ts.connect(user, app.URL)
			app.answer(http.StatusOK, "<html>nope</html>")
		}},
		{"the app is having a bad day", func(ts *testServer, app *fakeApp, user *store.User) {
			ts.connect(user, app.URL)
			app.answer(http.StatusInternalServerError, "")
		}},
		{"the app is not there at all", func(ts *testServer, app *fakeApp, user *store.User) {
			ts.connect(user, app.URL)
			app.Close()
		}},
		{"the token was rejected", func(ts *testServer, app *fakeApp, user *store.User) {
			ts.connect(user, app.URL)
			app.answer(http.StatusUnauthorized, "")
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ts := newTestServer(t)
			app := newFakeApp(t)
			user := ts.createUser("one@example.com")
			test.prepare(ts, app, user)
			ts.signIn(user.Email)

			resp := ts.get("/search.json?q=alpha").assertStatus(http.StatusOK)
			if resp.body != "[]" {
				t.Errorf("body = %s, want []", resp.body)
			}
		})
	}
}

// A token grants access to exactly one account on the other app. Before
// connections were scoped to a user, any authenticated search reached whichever
// connection happened to exist — one person's archive in another's command bar.
func TestSearchNeverUsesAnotherUsersConnection(t *testing.T) {
	ts := newTestServer(t)
	app := newFakeApp(t)
	first := ts.createUser("one@example.com")
	second := ts.createApprovedUser("two@example.com")
	ts.connect(second, app.URL)

	ts.signIn(first.Email)
	resp := ts.get("/search.json?q=anything").assertStatus(http.StatusOK)

	if resp.body != "[]" {
		t.Errorf("body = %s, want [] — the other user's connection was used", resp.body)
	}
	if got := app.askedFor(); len(got) != 0 {
		t.Errorf("the other app was asked %v, and should not have been asked at all", got)
	}
}

func TestSearchUsesYourOwnConnection(t *testing.T) {
	ts := newTestServer(t)
	mine := newFakeApp(t)
	theirs := newFakeApp(t)
	theirs.answer(http.StatusOK, `{"links":[{"id":9,"title":"Theirs","url":"https://t.example"}]}`)

	first := ts.createUser("one@example.com")
	second := ts.createApprovedUser("two@example.com")
	ts.connect(first, mine.URL)
	ts.connect(second, theirs.URL)

	ts.signIn(first.Email)
	resp := ts.get("/search.json?q=anything")

	if !strings.Contains(resp.body, `"Alpha"`) || strings.Contains(resp.body, "Theirs") {
		t.Errorf("body = %s, want only this user's results", resp.body)
	}
}

// The recording contract from the tinylinks package doc: a rejected token is
// worth telling the user about, because a lapsed credential and an empty
// archive look identical from the command bar.
func TestSearchRecordsARejectedToken(t *testing.T) {
	ts := newTestServer(t)
	app := newFakeApp(t)
	user := ts.createUser("one@example.com")
	ts.connect(user, app.URL)
	ts.signIn(user.Email)

	app.answer(http.StatusUnauthorized, "")
	ts.get("/search.json?q=alpha")

	if got := ts.lastError(user.ID); !strings.Contains(got, "rejected the token") {
		t.Errorf("last_error = %q, want the rejection recorded", got)
	}
}

func TestSearchClearsARecordedFailureOnSuccess(t *testing.T) {
	ts := newTestServer(t)
	app := newFakeApp(t)
	user := ts.createUser("one@example.com")
	connection := ts.connect(user, app.URL)
	ts.signIn(user.Email)

	if err := ts.db.RecordConnectionFailure(context.Background(), connection.ID, "went wrong once"); err != nil {
		t.Fatalf("recording a failure: %v", err)
	}

	ts.get("/search.json?q=alpha")

	if got := ts.lastError(user.ID); got != "" {
		t.Errorf("last_error = %q, want it cleared by a working search", got)
	}
}

// The other app's problem is not the user's problem: a 500 there is logged and
// nothing is asked of anybody.
func TestSearchDoesNotAskForAReconnectOverTheOtherAppsOwnFailure(t *testing.T) {
	ts := newTestServer(t)
	app := newFakeApp(t)
	user := ts.createUser("one@example.com")
	ts.connect(user, app.URL)
	ts.signIn(user.Email)

	app.answer(http.StatusBadGateway, "")
	ts.get("/search.json?q=alpha")

	if got := ts.lastError(user.ID); got != "" {
		t.Errorf("last_error = %q, want nothing recorded for the other app's own failure", got)
	}
}

// The command bar fires on every keystroke, backspace included, and an empty
// query is not a call to the other app at all.
func TestSearchForNothingAsksNobody(t *testing.T) {
	ts := newTestServer(t)
	app := newFakeApp(t)
	user := ts.createUser("one@example.com")
	ts.connect(user, app.URL)
	ts.signIn(user.Email)

	resp := ts.get("/search.json?q=").assertStatus(http.StatusOK)

	if resp.body != "[]" {
		t.Errorf("body = %s, want []", resp.body)
	}
	if got := app.askedFor(); len(got) != 0 {
		t.Errorf("the other app was asked %v for an empty query", got)
	}
}

// /search answers the same as /search.json: Rails routed the resource and the
// controller rendered JSON whatever format was asked for.
func TestSearchAnswersBothSpellings(t *testing.T) {
	ts := newTestServer(t)
	app := newFakeApp(t)
	user := ts.createUser("one@example.com")
	ts.connect(user, app.URL)
	ts.signIn(user.Email)

	resp := ts.get("/search?q=alpha").assertStatus(http.StatusOK)
	if !strings.Contains(resp.body, `"Alpha"`) {
		t.Errorf("body = %s", resp.body)
	}
}

func TestVisitRequiresAuthentication(t *testing.T) {
	ts := newTestServer(t)
	ts.post("/visits?link_id=7", nil).assertRedirect("/session/new")
}

func TestVisitForwardsToTheConnectedApp(t *testing.T) {
	ts := newTestServer(t)
	app := newFakeApp(t)
	user := ts.createUser("one@example.com")
	ts.connect(user, app.URL)
	ts.signIn(user.Email)

	ts.post("/visits?link_id=7", nil).assertStatus(http.StatusNoContent)

	if got := app.recordedVisits(); len(got) != 1 || got[0] != "7" {
		t.Errorf("visits recorded = %v, want [7]", got)
	}
}

// Tracking is fire and forget: the browser has already navigated away, so a
// failure upstream must not surface as an error on a click already made.
func TestVisitAnswers204Whatever(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testServer, *fakeApp, *store.User)
	}{
		{"with no connection", func(ts *testServer, _ *fakeApp, _ *store.User) {}},
		{"with an app that refuses", func(ts *testServer, app *fakeApp, user *store.User) {
			ts.connect(user, app.URL)
			app.answerVisit(http.StatusInternalServerError)
		}},
		{"with an app that is not there", func(ts *testServer, app *fakeApp, user *store.User) {
			ts.connect(user, app.URL)
			app.Close()
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ts := newTestServer(t)
			app := newFakeApp(t)
			user := ts.createUser("one@example.com")
			test.prepare(ts, app, user)
			ts.signIn(user.Email)

			ts.post("/visits?link_id=7", nil).assertStatus(http.StatusNoContent)
		})
	}
}

// A visit records a rejected token too — it is the same credential, and the
// command bar is the same place the reconnect will be offered.
func TestVisitRecordsARejectedToken(t *testing.T) {
	ts := newTestServer(t)
	app := newFakeApp(t)
	user := ts.createUser("one@example.com")
	ts.connect(user, app.URL)
	ts.signIn(user.Email)

	app.answerVisit(http.StatusForbidden)
	ts.post("/visits?link_id=7", nil).assertStatus(http.StatusNoContent)

	if got := ts.lastError(user.ID); !strings.Contains(got, "missing a scope") {
		t.Errorf("last_error = %q, want the missing scope recorded", got)
	}
}

// A visit never clears a recorded failure, only a search does. Rails' contract,
// kept: the visit's success says the token still works for visits, and the
// message on the page is about search.
func TestVisitDoesNotClearARecordedFailure(t *testing.T) {
	ts := newTestServer(t)
	app := newFakeApp(t)
	user := ts.createUser("one@example.com")
	connection := ts.connect(user, app.URL)
	ts.signIn(user.Email)

	if err := ts.db.RecordConnectionFailure(context.Background(), connection.ID, "went wrong once"); err != nil {
		t.Fatalf("recording a failure: %v", err)
	}

	ts.post("/visits?link_id=7", nil)

	if got := ts.lastError(user.ID); got != "went wrong once" {
		t.Errorf("last_error = %q, want it left alone by a visit", got)
	}
}
