package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jaimerodas/tinystart/internal/store"
)

// fakeFlow is the other app's half of the device flow: a real server.
// Everything under test — the form post, the poller, the token that comes
// back — is real HTTP.
//
// answer is what the token endpoint says next, which is how a test walks a
// grant from pending to approved without waiting for anything.
type fakeFlow struct {
	*httptest.Server

	mu      sync.Mutex
	answer  string
	asked   []string
	clients []string
}

func newFakeFlow(t *testing.T) *fakeFlow {
	t.Helper()
	flow := &fakeFlow{answer: `{"error":"authorization_pending"}`}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/device_authorizations", func(w http.ResponseWriter, r *http.Request) {
		flow.mu.Lock()
		defer flow.mu.Unlock()
		r.ParseForm() //nolint:errcheck // a test server reading a test client
		flow.clients = append(flow.clients, r.PostFormValue("client_name"))
		w.Write([]byte(`{"device_code":"abc","verification_url":"` + flow.URL + //nolint:errcheck
			`/device/new?code=abc","expires_in":600,"interval":5}`))
	})
	mux.HandleFunc("POST /api/v1/device_authorizations/token", func(w http.ResponseWriter, r *http.Request) {
		flow.mu.Lock()
		defer flow.mu.Unlock()
		r.ParseForm() //nolint:errcheck // as above
		flow.asked = append(flow.asked, r.PostFormValue("device_code"))
		w.Write([]byte(flow.answer)) //nolint:errcheck // as above
	})

	flow.Server = httptest.NewServer(mux)
	t.Cleanup(flow.Close)
	return flow
}

func (f *fakeFlow) answers(body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answer = body
}

func (f *fakeFlow) clientNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.clients...)
}

// connectionOf is the user's connection, or nil when there is none.
func (ts *testServer) connectionOf(userID int64) *store.Connection {
	ts.t.Helper()
	connection, err := ts.db.ConnectionForUser(ts.t.Context(), userID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		ts.t.Fatalf("reading the connection: %v", err)
	}
	return connection
}

// pollStatus is the one field the poller's JSON carries.
func (ts *testServer) pollStatus() string {
	ts.t.Helper()
	body := ts.get("/settings/connections/poll").assertStatus(http.StatusOK).body
	prefix, suffix := `{"status":"`, `"}`
	if !strings.HasPrefix(body, prefix) || !strings.HasSuffix(body, suffix) {
		ts.t.Fatalf("poll answered %s, want a bare status object", body)
	}
	return strings.TrimSuffix(strings.TrimPrefix(body, prefix), suffix)
}

func TestConnectionsRequireAuthentication(t *testing.T) {
	ts := newTestServer(t)
	ts.get("/settings/connections").assertRedirect("/session/new")
	ts.get("/settings/connections/poll").assertRedirect("/session/new")
	ts.post("/settings/connections", form("base_url", "https://links.example.com")).
		assertRedirect("/session/new")
}

// Connecting your own account on the other app needs no privilege — the
// scoping is what keeps users apart, not a role check.
func TestConnectionsAreOpenToAnyoneSignedIn(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("admin@example.com")
	plain := ts.createApprovedUser("two@example.com")
	ts.signIn(plain.Email)

	ts.get("/settings/connections").
		assertStatus(http.StatusOK).
		assertContains(`<form class="connect-form"`)
}

func TestConnectionsOffersTheFormWhenNeverConnected(t *testing.T) {
	ts, _ := settingsServer(t)

	ts.get("/settings/connections").
		assertContains(`<form class="connect-form"`).
		assertContains(`value="` + defaultBaseURL + `"`).
		assertNotContains("connection-status connected")
}

// The three lines are three tiers: the state, the token's facts, a footnote.
func TestConnectionsShowsAHealthyConnection(t *testing.T) {
	ts, user := settingsServer(t)
	if _, err := ts.db.ReplaceConnection(ts.t.Context(), user.ID,
		"https://links.example.com", "a-token", "search,visit",
		ts.clock.Now().Add(25*24*time.Hour)); err != nil {
		t.Fatalf("connecting: %v", err)
	}

	ts.get("/settings/connections").
		assertContains(`<div class="connection-status connected">`).
		assertContains(`<p class="connection-state"><strong>Connected</strong> to https://links.example.com</p>`).
		assertContains("search, visit").
		assertContains("token expires 25 days from now").
		assertContains(`<button class="action-button danger" data-turbo-confirm="Disconnect links.example.com?`).
		assertNotContains(`<form class="connect-form"`)
}

// A token with no expiry says nothing about one rather than saying "expires
// less than a minute from now".
func TestConnectionsSaysNothingAboutATokenWithNoExpiry(t *testing.T) {
	ts, user := settingsServer(t)
	ts.connect(user, "https://links.example.com")

	ts.get("/settings/connections").
		assertContains("connection-status connected").
		assertNotContains("token expires")
}

// A token with no scopes at all is described rather than left blank.
func TestConnectionsCallsAScopelessTokenFullAccess(t *testing.T) {
	ts, user := settingsServer(t)
	if _, err := ts.db.ReplaceConnection(ts.t.Context(), user.ID,
		"https://links.example.com", "a-token", "", time.Time{}); err != nil {
		t.Fatalf("connecting: %v", err)
	}

	ts.get("/settings/connections").assertContains("full access")
}

func TestConnectionsShowsTheErrorAndTheFormWhenTheTokenWasRejected(t *testing.T) {
	ts, user := settingsServer(t)
	connection := ts.connect(user, "https://links.example.com")
	if err := ts.db.RecordConnectionFailure(ts.t.Context(), connection.ID, "links.example.com rejected the token"); err != nil {
		t.Fatalf("recording a failure: %v", err)
	}

	ts.get("/settings/connections").
		assertContains(`<div class="connection-status disconnected">`).
		assertContains("links.example.com rejected the token").
		assertContains(`<form class="connect-form"`).
		// …and the form offers the address that was connected, not the default.
		assertContains(`value="https://links.example.com"`)
}

// One user's connection is invisible to another, and disconnecting leaves it
// alone. The token is one account on the other app. Nothing about it can
// leak sideways.
func TestConnectionsAreScopedToTheUser(t *testing.T) {
	ts := newTestServer(t)
	first := ts.createUser("one@example.com")
	second := ts.createApprovedUser("two@example.com")
	ts.connect(second, "https://theirs.example.com")

	ts.signIn(first.Email)
	ts.get("/settings/connections").
		assertNotContains("connection-status connected").
		assertNotContains("theirs.example.com").
		assertContains(`<form class="connect-form"`)

	ts.connect(first, "https://mine.example.com")
	ts.send(http.MethodDelete, "/settings/connections", nil).
		assertRedirect("/settings/connections")

	if ts.connectionOf(first.ID) != nil {
		t.Error("the signed-in user's connection survived a disconnect")
	}
	if ts.connectionOf(second.ID) == nil {
		t.Error("the other user's connection was deleted")
	}
}

func TestConnectionsDisconnect(t *testing.T) {
	ts, user := settingsServer(t)
	ts.connect(user, "https://links.example.com")

	ts.send(http.MethodDelete, "/settings/connections", nil).assertRedirect("/settings/connections")
	ts.get("/settings/connections").assertContains("Disconnected.")

	if ts.connectionOf(user.ID) != nil {
		t.Error("the connection survived")
	}
}

// Opening a grant leaves the waiting state, with the approval page to go to
// and the poller's attributes on it.
func TestConnectionCreateOpensAGrantAndWaits(t *testing.T) {
	ts, _ := settingsServer(t)
	flow := newFakeFlow(t)

	ts.post("/settings/connections", form("base_url", flow.URL)).
		assertRedirect("/settings/connections")

	ts.get("/settings/connections").
		assertContains(`<div class="connection-status pending"`).
		assertContains(`data-device-flow-url-value="/settings/connections/poll"`).
		assertContains(`href="` + flow.URL + `/device/new?code=abc"`)
}

// So the other app's list of tokens says which tinystart asked for each one.
// A laptop and the real thing are two grants under one name otherwise.
func TestConnectionCreateTellsTheOtherAppWhichHostIsAsking(t *testing.T) {
	ts, _ := settingsServer(t)
	flow := newFakeFlow(t)

	ts.post("/settings/connections", form("base_url", flow.URL))

	names := flow.clientNames()
	host := strings.TrimPrefix(ts.http.URL, "http://")
	if len(names) != 1 || names[0] != "tinystart ("+host+")" {
		t.Errorf("client_name = %v, want tinystart (%s)", names, host)
	}
}

func TestConnectionCreateSaysSoWhenTheAppCannotBeReached(t *testing.T) {
	ts, _ := settingsServer(t)
	flow := newFakeFlow(t)
	address := flow.URL
	flow.Close()

	ts.post("/settings/connections", form("base_url", address)).
		assertRedirect("/settings/connections")

	ts.get("/settings/connections").
		assertContains("Could not reach " + address + ". Check the address and try again.").
		assertContains(`<form class="connect-form"`)
}

func TestPollIsIdleWithNoGrantInFlight(t *testing.T) {
	ts, _ := settingsServer(t)

	if got := ts.pollStatus(); got != "idle" {
		t.Errorf("status = %q, want idle", got)
	}
}

func TestPollWaitsWhileTheGrantIsPending(t *testing.T) {
	ts, _ := settingsServer(t)
	flow := newFakeFlow(t)
	ts.post("/settings/connections", form("base_url", flow.URL))

	if got := ts.pollStatus(); got != "pending" {
		t.Errorf("status = %q, want pending", got)
	}
}

// A blip mid-flow must not look like a denial. The grant is still good, and
// the page continues to wait until it runs out on its own.
func TestPollKeepsWaitingWhenTheAppIsBrieflyUnreachable(t *testing.T) {
	ts, _ := settingsServer(t)
	flow := newFakeFlow(t)
	ts.post("/settings/connections", form("base_url", flow.URL))

	flow.answers("<html>nope</html>")
	if got := ts.pollStatus(); got != "pending" {
		t.Errorf("status = %q, want pending", got)
	}
	// …and the grant is still in flight, so the next tick tries again.
	if got := ts.pollStatus(); got != "pending" {
		t.Errorf("status on the second tick = %q, want pending", got)
	}
}

func TestPollStoresTheConnectionAgainstTheUserWhoApprovedIt(t *testing.T) {
	ts, user := settingsServer(t)
	flow := newFakeFlow(t)
	ts.post("/settings/connections", form("base_url", flow.URL))

	flow.answers(`{"token":"a-token","scopes":["search","visit"],"expires_at":"2027-01-01T00:00:00Z"}`)
	if got := ts.pollStatus(); got != "connected" {
		t.Fatalf("status = %q, want connected", got)
	}

	connection := ts.connectionOf(user.ID)
	if connection == nil {
		t.Fatal("no connection was stored")
	}
	if connection.BaseURL != flow.URL || connection.Token != "a-token" || connection.Scopes != "search,visit" {
		t.Errorf("connection = %+v, want the approved grant", connection)
	}
	if want := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC); !connection.TokenExpiresAt.Equal(want) {
		t.Errorf("token_expires_at = %v, want %v", connection.TokenExpiresAt, want)
	}

	// The grant is consumed, so the poller has nothing left to wait on.
	if got := ts.pollStatus(); got != "idle" {
		t.Errorf("status after approval = %q, want idle", got)
	}
}

// Reconnecting replaces the row rather than piling up. There is one
// connection per user, and a new grant is a different token with no history
// worth keeping.
func TestPollReplacesAnExistingConnection(t *testing.T) {
	ts, user := settingsServer(t)
	ts.connect(user, "https://old.example.com")
	flow := newFakeFlow(t)
	ts.post("/settings/connections", form("base_url", flow.URL))

	flow.answers(`{"token":"new","scopes":"search,visit"}`)
	if got := ts.pollStatus(); got != "connected" {
		t.Fatalf("status = %q, want connected", got)
	}

	if got := ts.connectionOf(user.ID); got.Token != "new" || got.BaseURL != flow.URL {
		t.Errorf("connection = %+v, want the new grant", got)
	}
}

// Denial and expiry end the flow: the status is reported once and the grant
// is forgotten, so the poller has nothing left to ask about.
func TestPollReportsTheEndOfAGrantAndForgetsIt(t *testing.T) {
	tests := []struct {
		name   string
		answer string
		want   string
	}{
		{"a denial", `{"error":"access_denied"}`, "denied"},
		{"an expiry", `{"error":"expired_token"}`, "expired"},
		{"something this app has never heard of", `{"error":"slow_down_please"}`, "expired"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ts, _ := settingsServer(t)
			flow := newFakeFlow(t)
			ts.post("/settings/connections", form("base_url", flow.URL))

			flow.answers(test.answer)
			if got := ts.pollStatus(); got != test.want {
				t.Errorf("status = %q, want %q", got, test.want)
			}
			if got := ts.pollStatus(); got != "idle" {
				t.Errorf("status on the next tick = %q, want idle", got)
			}
		})
	}
}

// The page that notices drops a grant that ran out while the tab sat open,
// and never asks the other app about it.
func TestAnExpiredGrantIsForgotten(t *testing.T) {
	ts, _ := settingsServer(t)
	flow := newFakeFlow(t)
	ts.post("/settings/connections", form("base_url", flow.URL))

	// The fake says the grant lasts ten minutes.
	ts.clock.advance(11 * time.Minute)

	if got := ts.pollStatus(); got != "idle" {
		t.Errorf("status = %q, want idle", got)
	}
	ts.get("/settings/connections").
		assertNotContains("connection-status pending").
		assertContains(`<form class="connect-form"`)
}

// Disconnecting drops a grant in flight as well as the row. Leaving one
// means a page that says "not connected" and then connects itself.
func TestDisconnectingDropsAGrantInFlight(t *testing.T) {
	ts, _ := settingsServer(t)
	flow := newFakeFlow(t)
	ts.post("/settings/connections", form("base_url", flow.URL))

	ts.send(http.MethodDelete, "/settings/connections", nil)

	if got := ts.pollStatus(); got != "idle" {
		t.Errorf("status = %q, want idle", got)
	}
}

// With no address in the form, the page reconnects to what it was connected
// to. With nothing connected either, it reconnects to the app this one is
// usually pointed at.
func TestConnectionCreateFallsBackToTheStoredAddress(t *testing.T) {
	ts, user := settingsServer(t)
	flow := newFakeFlow(t)
	ts.connect(user, flow.URL)

	ts.post("/settings/connections", nil).assertRedirect("/settings/connections")

	if got := flow.clientNames(); len(got) != 1 {
		t.Errorf("the stored address was asked %d times, want once", len(got))
	}
}
