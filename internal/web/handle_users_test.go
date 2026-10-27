package web

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/jaimerodas/tinystart/internal/store"
)

// test/controllers/users_controller_test.rb, ported. Sign-up only exists on
// an empty install, where it makes the admin. Everyone after that is invited.

func TestSignUpPageRenders(t *testing.T) {
	ts := newTestServer(t)

	ts.get("/sign_up").
		assertStatus(http.StatusOK).
		assertContains(`<form action="/sign_up"`).
		assertContains(`name="user[email]"`).
		assertContains(`name="user[password]"`).
		assertNotContains("Already registered?")
}

func TestSignUpCreatesTheAdminAndSignsThemIn(t *testing.T) {
	ts := newTestServer(t)

	ts.post("/sign_up", url.Values{
		"user[email]":    {"first@example.com"},
		"user[password]": {"password"},
	}).assertRedirect("/")

	first := mustUser(t, ts, "first@example.com")
	if !first.Admin || !first.Approved {
		t.Errorf("first user: admin=%v approved=%v, want both true", first.Admin, first.Approved)
	}
	ts.get("/").assertContains("User was successfully created.")
	ts.get(ts.protectedPath()).assertStatus(http.StatusOK)
}

// Once anyone exists, the page and the form are gone: a 404 like any other
// URL the app does not have, signed in or not, and nothing is created.
func TestSignUpIsGoneOnceAUserExists(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("first@example.com")

	signUp := url.Values{
		"user[email]":    {"second@example.com"},
		"user[password]": {"password"},
	}
	ts.get("/sign_up").assertStatus(http.StatusNotFound)
	ts.post("/sign_up", signUp).assertStatus(http.StatusNotFound)
	ts.signIn("first@example.com")
	ts.get("/sign_up").assertStatus(http.StatusNotFound)
	ts.post("/sign_up", signUp).assertStatus(http.StatusNotFound)

	if _, err := ts.db.UserByEmail(t.Context(), "second@example.com"); err == nil {
		t.Error("a sign-up after the first user created an account")
	}
}

// Two rejected attributes make two list items and the plural, which is
// ActionView's pluralize doing the one job this app gives it.
func TestSignUpListsEveryError(t *testing.T) {
	ts := newTestServer(t)

	resp := ts.post("/sign_up", url.Values{
		"user[email]":    {""},
		"user[password]": {""},
	})

	resp.assertStatus(http.StatusUnprocessableEntity).
		assertContains("2 errors prohibited this user from being saved:").
		assertContains("<li>Email can&#39;t be blank</li>").
		assertContains("<li>Password can&#39;t be blank</li>").
		// The field is marked, so the reader sees which box is wrong.
		assertContains(`<div class="field_with_errors"><input type="text" name="user[email]" id="user_email" /></div>`)
}

// params.expect(user: […]) refuses a body with no user key at all rather than
// treating it as an empty one.
func TestSignUpWithNoUserParametersIsABadRequest(t *testing.T) {
	ts := newTestServer(t)

	ts.post("/sign_up", url.Values{"email": {"wrong@example.com"}}).
		assertStatus(http.StatusBadRequest)
}

// The admin is approved on the way in, so there is no wait to mail them
// about.
func TestSignUpSendsNoMail(t *testing.T) {
	ts := newTestServer(t)

	ts.post("/sign_up", url.Values{
		"user[email]":    {"first@example.com"},
		"user[password]": {"password"},
	}).assertRedirect("/")

	if sent := ts.mail.messages(); len(sent) != 0 {
		t.Errorf("sent %d messages for the bootstrap admin, want 0", len(sent))
	}
}

func mustUser(t *testing.T, ts *testServer, email string) *store.User {
	t.Helper()
	user, err := ts.db.UserByEmail(t.Context(), email)
	if err != nil {
		t.Fatalf("looking up %s: %v", email, err)
	}
	return user
}
