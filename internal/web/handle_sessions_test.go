package web

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

// These are test/controllers/sessions_controller_test.rb, ported. The names
// say the same things the Ruby ones said, because they are the same rules.

// A brand new install has nobody to sign in as, so the form is a dead
// end. The first sign-up bootstraps itself as an approved admin.

func TestSessionNewRedirectsToSignUpWhenThereAreNoUsers(t *testing.T) {
	ts := newTestServer(t)

	ts.get("/session/new").assertRedirect("/sign_up")
}

func TestSessionCreateRedirectsToSignUpWhenThereAreNoUsers(t *testing.T) {
	ts := newTestServer(t)

	ts.post("/session", url.Values{
		"email":    {"nobody@example.com"},
		"password": {testPassword},
	}).assertRedirect("/sign_up")
}

func TestSessionNewShowsTheFormOnceAUserExists(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")

	ts.get("/session/new").
		assertStatus(http.StatusOK).
		assertContains(`<form data-turbo="false" action="/session"`)
}

// Anything behind the sign-in wall funnels to sign-up too, because that path
// goes through the session page.
func TestAProtectedPageSendsABrandNewInstallToSignUp(t *testing.T) {
	ts := newTestServer(t)

	// There is no protected page in this phase, so the middleware is exercised
	// directly — which is also the only thing under test here.
	protected := ts.protectedPath()

	ts.get(protected).assertRedirect("/session/new")
	ts.get("/session/new").assertRedirect("/sign_up")
}

// The login form opts out of Turbo so that the theme and color attributes on
// <html> are rendered fresh after signing in.
func TestSessionNewFormOptsOutOfTurbo(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")

	ts.get("/session/new").assertContains(`data-turbo="false" action="/session"`)
}

func TestSignInWithValidCredentials(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")

	ts.post("/session", url.Values{
		"email":    {"one@example.com"},
		"password": {testPassword},
	}).assertRedirect("/")

	if ts.sessionCookie() == "" {
		t.Error("no session cookie after signing in")
	}
}

// Sign-ups wait for an admin. The gate is in the sign-in handler, so correct
// credentials alone are not enough.
func TestAnUnapprovedUserCannotSignIn(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("first@example.com")
	pending := ts.createUser("pending@example.com")
	if pending.Approved {
		t.Fatal("the second user was approved on creation; the fixture is wrong")
	}

	ts.post("/session", url.Values{
		"email":    {"pending@example.com"},
		"password": {testPassword},
	}).assertRedirect("/session/new")

	if ts.sessionCookie() != "" {
		t.Error("an unapproved user was given a session cookie")
	}
	ts.get("/session/new").assertContains("Try another email address or password.")
}

func TestAnApprovedUserCanSignIn(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("first@example.com")
	ts.createApprovedUser("pending@example.com")

	ts.post("/session", url.Values{
		"email":    {"pending@example.com"},
		"password": {testPassword},
	}).assertRedirect("/")
}

func TestSignInWithInvalidCredentials(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")

	ts.post("/session", url.Values{
		"email":    {"one@example.com"},
		"password": {"wrongpassword"},
	}).assertRedirect("/session/new")

	if ts.sessionCookie() != "" {
		t.Error("a failed sign-in left a session cookie")
	}
	ts.get("/session/new").assertContains("Try another email address or password.")
}

func TestSigningOut(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")
	ts.signIn("one@example.com")

	id := ts.currentSessionID()

	// The log-out button is a form, so the DELETE arrives as a POST carrying
	// _method — which is the method-override middleware's whole job.
	ts.post("/session", url.Values{"_method": {"delete"}}).assertRedirect("/session/new")

	if ts.sessionCookie() != "" {
		t.Error("the session cookie survived signing out")
	}
	if !ts.sessionRowGone(id) {
		t.Error("the session row survived signing out")
	}
}

func TestSigningInReturnsToWhereTheVisitorWasHeaded(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")

	protected := ts.protectedPath()
	ts.get(protected).assertRedirect("/session/new")

	ts.post("/session", url.Values{
		"email":    {"one@example.com"},
		"password": {testPassword},
	}).assertRedirect(protected)
}

func TestSigningInCleansUpExpiredSessions(t *testing.T) {
	ts := newTestServer(t)
	user := ts.createUser("one@example.com")

	expired, err := ts.db.CreateSession(t.Context(), user.ID, "Old Browser", "192.168.1.1",
		ts.clock.Now().Add(-5*24*time.Hour))
	if err != nil {
		t.Fatalf("creating an expired session: %v", err)
	}

	ts.signIn("one@example.com")

	if !ts.sessionRowGone(expired.ID) {
		t.Error("the expired session survived a sign-in")
	}
}

// The request that arrives extends a session that is nearly over, so that
// someone who visits every week is never signed out. Someone who disappears
// for a month is signed out.
func TestASessionCloseToExpiryIsExtended(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")
	ts.signIn("one@example.com")

	id := ts.currentSessionID()
	before, err := ts.db.ActiveSession(t.Context(), id)
	if err != nil {
		t.Fatalf("reading the session: %v", err)
	}

	// Far enough that the session has less than a week left, but not so far
	// that it has run out.
	ts.clock.advance(25 * 24 * time.Hour)
	ts.get("/session/new")

	after, err := ts.db.ActiveSession(t.Context(), id)
	if err != nil {
		t.Fatalf("reading the session again: %v", err)
	}
	if !after.ExpiresAt.After(before.ExpiresAt) {
		t.Errorf("expires_at = %v, want it pushed out from %v", after.ExpiresAt, before.ExpiresAt)
	}
}

func TestASessionWithPlentyOfTimeLeftIsNotExtended(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")
	ts.signIn("one@example.com")

	id := ts.currentSessionID()
	before, err := ts.db.ActiveSession(t.Context(), id)
	if err != nil {
		t.Fatalf("reading the session: %v", err)
	}

	ts.get("/session/new")

	after, err := ts.db.ActiveSession(t.Context(), id)
	if err != nil {
		t.Fatalf("reading the session again: %v", err)
	}
	if !after.ExpiresAt.Equal(before.ExpiresAt) {
		t.Errorf("expires_at = %v, want it left at %v", after.ExpiresAt, before.ExpiresAt)
	}
}

// Signing out is behind the wall. An anonymous DELETE has no session to end
// and is sent to the sign-in page like any other protected request.
func TestSigningOutRequiresASession(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")

	ts.post("/session", url.Values{"_method": {"delete"}}).assertRedirect("/session/new")
}

// The admin section is not an error page for someone signed in without the
// flag: it is the start page. Being told the section exists is itself a leak.
func TestAdminOnlyTurnsAwayANonAdmin(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("admin@example.com")
	ts.createApprovedUser("ordinary@example.com")

	ts.signIn("ordinary@example.com")
	ts.get(ts.adminPath()).assertRedirect("/")

	// And lets the first user — the one the installation bootstrapped — in.
	ts.post("/session", url.Values{"_method": {"delete"}})
	ts.signIn("admin@example.com")
	ts.get(ts.adminPath()).assertStatus(http.StatusOK)
}
