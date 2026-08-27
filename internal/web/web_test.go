package web

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jaimerodas/tinystart/internal/postmark"
	"github.com/jaimerodas/tinystart/internal/store"
)

// TestMain turns the password hashing cost down for the whole package. At the
// real cost a single sign-up takes a quarter of a second, and seconds under
// -race. This suite creates users in almost every test.
func TestMain(m *testing.M) {
	restore := store.UseCheapPasswordHashing()
	code := m.Run()
	restore()
	if closeBrowser != nil {
		closeBrowser()
	}
	os.Exit(code)
}

// closeBrowser shuts down the Chrome the browser suite shares, and is nil
// unless that suite is compiled in (-tags browser) and something opened one.
// It lives here because after the last test is the only moment it can run,
// and that moment belongs to TestMain. t.Cleanup ties the browser to
// whichever test happened to start it, not to the last one.
var closeBrowser func()

// testPassword is the one password every test user has. Long enough to pass
// the model's validations and short enough to read.
const testPassword = "password123"

// testProtectedPath and testAdminPath are the two pages the tests put behind
// the authentication middleware. See newTestServer.
const (
	testProtectedPath = "/protected"
	testAdminPath     = "/admin-only"
)

// testServer is one app, on a real port, with a database of its own.
//
// The requests go over TCP through a client with a cookie jar rather than
// straight into the handler. Half of what this package does is set cookies
// and redirect. A test that calls ServeHTTP directly has to copy cookies
// from one response to the next by hand. It then tests that copying rather
// than the app.
type testServer struct {
	t *testing.T
	// app is the Server itself, for the handful of tests that need to mint
	// something the app normally only puts in a mail.
	app    *Server
	db     *store.DB
	mail   *recordingMailer
	clock  *testClock
	http   *httptest.Server
	client *http.Client
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()

	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "test.sqlite3"))
	if err != nil {
		t.Fatalf("opening the test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrating the test database: %v", err)
	}

	mail := &recordingMailer{}
	clock := &testClock{now: time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)}

	s, err := newServer(Config{
		SecretKey: []byte(strings.Repeat("secret-key-", 4)),
		Host:      "https://start.example.com",
	}, db, slog.New(slog.NewTextHandler(io.Discard, nil)), mail, clock.Now)
	if err != nil {
		t.Fatalf("building the server: %v", err)
	}

	mux := http.NewServeMux()
	addRoutes(mux, s)

	// One route the real app does not have: a page behind
	// requireAuthentication. Every page that will be behind it belongs to a
	// later phase, and the middleware is this phase's work, so the tests mount
	// their own. A more specific pattern than the catch-all addRoutes
	// registers, so it wins without any ordering games.
	secret := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("secret")) //nolint:errcheck // a test server writing to a test client
	})
	mux.Handle("GET "+testProtectedPath, s.requireAuthentication(secret))
	mux.Handle("GET "+testAdminPath, s.requireAuthentication(s.adminOnly(secret)))

	server := httptest.NewServer(s.wrap(mux))
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("building a cookie jar: %v", err)
	}

	return &testServer{
		t:     t,
		app:   s,
		db:    db,
		mail:  mail,
		clock: clock,
		http:  server,
		client: &http.Client{
			Jar: jar,
			// Redirects are the answer half these handlers give, so they are
			// what the tests assert on. Following them automatically hides
			// the Location header behind whatever it led to.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// get and post are the two verbs the tests need. Both return a response
// with its body already read and closed. Every assertion here is on the
// status, a header or the whole body, and none of them wants a defer.
func (ts *testServer) get(path string) *response {
	ts.t.Helper()
	return ts.do(ts.request(http.MethodGet, path, nil))
}

func (ts *testServer) post(path string, form url.Values) *response {
	ts.t.Helper()
	return ts.do(ts.request(http.MethodPost, path, form))
}

// send is get and post generalised to the other verbs. The editor's forms
// reach PATCH and DELETE through the hidden _method field. But the Stimulus
// controllers use the real verb, so the tests use the real verb too.
func (ts *testServer) send(method, path string, form url.Values) *response {
	ts.t.Helper()
	return ts.do(ts.request(method, path, form))
}

// turbo is send with the header that decides everything on the editor. With
// it a write answers with the pieces of the page that changed. Without it
// the same write redirects.
func (ts *testServer) turbo(method, path string, form url.Values) *response {
	ts.t.Helper()
	req := ts.request(method, path, form)
	req.Header.Set("Accept", turboStreamMIME+", text/html, application/xhtml+xml")
	return ts.do(req)
}

func (ts *testServer) request(method, path string, form url.Values) *http.Request {
	ts.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ts.t.Context(), method, ts.http.URL+path, body)
	if err != nil {
		ts.t.Fatalf("building a %s %s request: %v", method, path, err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return req
}

func (ts *testServer) do(req *http.Request) *response {
	ts.t.Helper()
	resp, err := ts.client.Do(req)
	if err != nil {
		ts.t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		ts.t.Fatalf("reading the body of %s %s: %v", req.Method, req.URL.Path, err)
	}
	return &response{t: ts.t, Response: resp, body: string(body)}
}

// createUser signs someone up straight through the store, which is what a
// fixture was. The point of most of these tests is what happens after there
// is an account, not the signing up.
func (ts *testServer) createUser(email string) *store.User {
	ts.t.Helper()
	user, err := ts.db.CreateUser(context.Background(), email, testPassword)
	if err != nil {
		ts.t.Fatalf("creating %s: %v", email, err)
	}
	return user
}

// createApprovedUser is createUser plus the admin's approval, for everyone
// after the first. They are approved on the way in, because there is
// nobody to approve them.
func (ts *testServer) createApprovedUser(email string) *store.User {
	ts.t.Helper()
	user := ts.createUser(email)
	if user.Approved {
		return user
	}
	approved, err := ts.db.ToggleApproved(context.Background(), user.ID)
	if err != nil {
		ts.t.Fatalf("approving %s: %v", email, err)
	}
	return approved
}

// protectedPath is the test-only page behind the authentication wall, and
// adminPath the one behind the admin check as well.
func (ts *testServer) protectedPath() string { return testProtectedPath }
func (ts *testServer) adminPath() string     { return testAdminPath }

// signIn goes through the real form, so a client holding a real session
// cookie does everything the tests do afterwards.
func (ts *testServer) signIn(email string) {
	ts.t.Helper()
	resp := ts.post("/sign_in", url.Values{"email": {email}, "password": {testPassword}})
	if resp.StatusCode != http.StatusSeeOther {
		ts.t.Fatalf("signing in as %s: status %d, want %d", email, resp.StatusCode, http.StatusSeeOther)
	}
}

// sessionCookie is the raw value of the session cookie the client holds,
// or "" when it holds none.
func (ts *testServer) sessionCookie() string {
	ts.t.Helper()
	base, err := url.Parse(ts.http.URL)
	if err != nil {
		ts.t.Fatalf("parsing the server URL: %v", err)
	}
	for _, cookie := range ts.client.Jar.Cookies(base) {
		if cookie.Name == sessionCookie {
			return cookie.Value
		}
	}
	return ""
}

// currentSessionID is the row id inside that cookie. Reading it is also a
// check on the cookie's shape: the value is the id and a signature over it,
// both base64url, and nothing else.
func (ts *testServer) currentSessionID() int64 {
	ts.t.Helper()
	value := ts.sessionCookie()
	if value == "" {
		ts.t.Fatal("no session cookie")
	}
	encoded, _, found := strings.Cut(value, ".")
	if !found {
		ts.t.Fatalf("session cookie %q carries no signature", value)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		ts.t.Fatalf("session cookie %q is not base64url: %v", value, err)
	}
	id, err := strconv.ParseInt(string(decoded), 10, 64)
	if err != nil {
		ts.t.Fatalf("session cookie %q does not carry a row id: %v", value, err)
	}
	return id
}

// sessionRowGone reports whether the sessions table forgot a row.
//
// The store has no "count the sessions" call — nothing in the app lists them —
// so the question is asked the only way the public API allows. Deleting a row
// that is not there answers ErrNotFound. It is destructive, which is fine for
// a row a test is about to stop caring about.
func (ts *testServer) sessionRowGone(id int64) bool {
	ts.t.Helper()
	err := ts.db.DeleteSession(context.Background(), id)
	if err == nil {
		return false
	}
	if !errors.Is(err, store.ErrNotFound) {
		ts.t.Fatalf("deleting session %d: %v", id, err)
	}
	return true
}

// response is an http.Response with its body already in hand, plus the few
// assertions every test in this package makes.
type response struct {
	t *testing.T
	*http.Response
	body string
}

func (r *response) assertStatus(want int) *response {
	r.t.Helper()
	if r.StatusCode != want {
		r.t.Errorf("status = %d, want %d", r.StatusCode, want)
	}
	return r
}

func (r *response) assertRedirect(want string) *response {
	r.t.Helper()
	if r.StatusCode < 300 || r.StatusCode > 399 {
		r.t.Errorf("status = %d, want a redirect", r.StatusCode)
		return r
	}
	if got := r.Header.Get("Location"); got != want {
		r.t.Errorf("Location = %q, want %q", got, want)
	}
	return r
}

// streams is every <turbo-stream> in the body, as "action:target". That is
// what the ported controller tests assert on, because the rule they encode
// names which node a write replaces, not its contents.
func (r *response) streams() []string {
	r.t.Helper()
	var found []string
	for _, match := range streamPattern.FindAllStringSubmatch(r.body, -1) {
		found = append(found, match[1]+":"+match[2])
	}
	return found
}

func (r *response) assertStreams(want ...string) *response {
	r.t.Helper()
	if got := r.streams(); !slices.Equal(got, want) {
		r.t.Errorf("streams = %v, want %v", got, want)
	}
	return r
}

var streamPattern = regexp.MustCompile(`<turbo-stream action="([a-z]+)" target="([^"]+)"`)

func (r *response) assertContains(want string) *response {
	r.t.Helper()
	if !strings.Contains(r.body, want) {
		r.t.Errorf("body does not contain %q", want)
	}
	return r
}

func (r *response) assertNotContains(unwanted string) *response {
	r.t.Helper()
	if strings.Contains(r.body, unwanted) {
		r.t.Errorf("body contains %q and should not", unwanted)
	}
	return r
}

// recordingMailer is the Mailer the tests inject: it keeps the messages
// instead of sending them, which is ActionMailer::Base.deliveries with a
// different name.
type recordingMailer struct {
	mu       sync.Mutex
	sent     []postmark.Message
	failWith error
}

func (m *recordingMailer) Send(_ context.Context, message postmark.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failWith != nil {
		return m.failWith
	}
	m.sent = append(m.sent, message)
	return nil
}

func (m *recordingMailer) messages() []postmark.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]postmark.Message(nil), m.sent...)
}

// testClock is the clock the server is built with, so that a test can move
// time forward instead of sleeping through it.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) set(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// form builds a form body from alternating keys and values, which reads better
// than url.Values{"a": {"b"}} at a call site with two fields.
func form(pairs ...string) url.Values {
	values := url.Values{}
	for i := 0; i < len(pairs); i += 2 {
		values.Set(pairs[i], pairs[i+1])
	}
	return values
}

// turboJSON is what lib/start_page_moves.js actually sends: a JSON body with
// the stream Accept header. The forms in the other tests are what Rails' own
// tests sent, and the editor's moves never went through a form. That is how
// a handler that only read form values passed every test and failed the page.
func (ts *testServer) turboJSON(method, path, body string) *response {
	ts.t.Helper()
	req, err := http.NewRequestWithContext(ts.t.Context(), method, ts.http.URL+path, strings.NewReader(body))
	if err != nil {
		ts.t.Fatalf("building a %s %s request: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", turboStreamMIME)
	return ts.do(req)
}
