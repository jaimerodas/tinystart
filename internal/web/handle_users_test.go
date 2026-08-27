package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jaimerodas/tinystart/internal/store"
)

// test/controllers/users_controller_test.rb, ported, plus the two copy rules
// the sessions test held. The sign-up page offers a way back to sign-in only
// once there is somebody to sign in as.

func TestSignUpPageRenders(t *testing.T) {
	ts := newTestServer(t)

	ts.get("/sign_up").
		assertStatus(http.StatusOK).
		assertContains(`<form action="/sign_up"`).
		assertContains(`name="user[email]"`).
		assertContains(`name="user[password]"`)
}

func TestSignUpPageOffersNoSignInLinkWhenThereAreNoUsers(t *testing.T) {
	ts := newTestServer(t)

	ts.get("/sign_up").assertNotContains("Already registered?")
}

func TestSignUpPageOffersASignInLinkOnceAUserExists(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")

	ts.get("/sign_up").
		assertContains("Already registered?").
		assertContains(`<a href="/session/new">Log in</a>`)
}

func TestSignUpCreatesAUser(t *testing.T) {
	ts := newTestServer(t)

	ts.post("/sign_up", url.Values{
		"user[email]":    {"useremail@email.com"},
		"user[password]": {"password"},
	}).assertRedirect("/")

	if _, err := ts.db.UserByEmail(t.Context(), "useremail@email.com"); err != nil {
		t.Errorf("the user was not created: %v", err)
	}
	ts.get("/sign_up").assertContains("User was successfully created.")
}

// A new account is not signed in. Everyone after the first waits for an admin,
// so there is nothing to sign in to.
func TestSignUpDoesNotSignTheNewUserIn(t *testing.T) {
	ts := newTestServer(t)

	ts.post("/sign_up", url.Values{
		"user[email]":    {"useremail@email.com"},
		"user[password]": {"password"},
	})

	if ts.sessionCookie() != "" {
		t.Error("signing up left a session cookie")
	}
}

func TestSignUpWithADuplicateEmailIsRefused(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")

	resp := ts.post("/sign_up", url.Values{
		"user[email]":    {"one@example.com"},
		"user[password]": {"password"},
	})

	resp.assertStatus(http.StatusUnprocessableEntity).
		assertContains("1 error prohibited this user from being saved:").
		assertContains("<li>Email has already been taken</li>").
		// The address is put back in the field, and the field is marked.
		assertContains(`<div class="field_with_errors"><input type="text" value="one@example.com" name="user[email]" id="user_email" /></div>`)
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
		assertContains("<li>Password can&#39;t be blank</li>")
}

func TestSignUpRedirectsASignedInVisitorAway(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")
	ts.signIn("one@example.com")

	ts.get("/sign_up").assertRedirect("/")
}

func TestSignUpRefusesASignedInVisitorWithoutCreatingAUser(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")
	ts.signIn("one@example.com")

	ts.post("/sign_up", url.Values{
		"user[email]":    {"another@email.com"},
		"user[password]": {"password"},
	}).assertRedirect("/")

	if _, err := ts.db.UserByEmail(t.Context(), "another@email.com"); err == nil {
		t.Error("a signed-in visitor created a second account")
	}
}

// params.expect(user: […]) refuses a body with no user key at all rather than
// treating it as an empty one.
func TestSignUpWithNoUserParametersIsABadRequest(t *testing.T) {
	ts := newTestServer(t)

	ts.post("/sign_up", url.Values{"email": {"wrong@example.com"}}).
		assertStatus(http.StatusBadRequest)
}

// The first person to sign up runs the installation, so they are approved
// and an admin. Everyone after them waits. The rule lives in the store, and
// this pins that the handler does not undo it.
func TestTheFirstUserToSignUpIsAnApprovedAdmin(t *testing.T) {
	ts := newTestServer(t)

	ts.post("/sign_up", url.Values{
		"user[email]":    {"first@example.com"},
		"user[password]": {"password"},
	})
	ts.post("/sign_up", url.Values{
		"user[email]":    {"second@example.com"},
		"user[password]": {"password"},
	})

	first := mustUser(t, ts, "first@example.com")
	second := mustUser(t, ts, "second@example.com")

	if !first.Admin || !first.Approved {
		t.Errorf("first user: admin=%v approved=%v, want both true", first.Admin, first.Approved)
	}
	if second.Admin || second.Approved {
		t.Errorf("second user: admin=%v approved=%v, want both false", second.Admin, second.Approved)
	}
}

// Everyone after the first waits for an admin, and the mail is how they find
// out there is a wait at all.
func TestSignUpSendsAwaitingApprovalMail(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("first@example.com") // the bootstrap admin

	ts.post("/sign_up", url.Values{
		"user[email]":    {"second@example.com"},
		"user[password]": {"password"},
	}).assertRedirect("/")

	sent := ts.mail.messages()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	if sent[0].To != "second@example.com" {
		t.Errorf("To = %q, want %q", sent[0].To, "second@example.com")
	}
	if sent[0].From != ts.app.cfg.MailFrom {
		t.Errorf("From = %q, want %q", sent[0].From, ts.app.cfg.MailFrom)
	}
	if sent[0].Subject != "Your account waits for approval" {
		t.Errorf("Subject = %q, want %q", sent[0].Subject, "Your account waits for approval")
	}
	if sent[0].TextBody == "" {
		t.Error("TextBody is empty")
	}
	if sent[0].HTMLBody == "" {
		t.Error("HTMLBody is empty")
	}
	if !strings.Contains(sent[0].TextBody, "An admin must approve it first.") {
		t.Errorf("TextBody = %q, want the approval sentence", sent[0].TextBody)
	}
}

// The first signup bootstraps the install as an approved admin. Nobody has
// to approve them, so nothing gets mailed.
func TestTheFirstUserToSignUpSendsNoMail(t *testing.T) {
	ts := newTestServer(t)

	ts.post("/sign_up", url.Values{
		"user[email]":    {"first@example.com"},
		"user[password]": {"password"},
	}).assertRedirect("/")

	if sent := ts.mail.messages(); len(sent) != 0 {
		t.Errorf("sent %d messages for the bootstrap admin, want 0", len(sent))
	}
}

// A signup that fails validation never reaches the database, so there is
// nobody to mail.
func TestSignUpWithInvalidDataSendsNoMail(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("first@example.com")

	ts.post("/sign_up", url.Values{
		"user[email]":    {""},
		"user[password]": {""},
	}).assertStatus(http.StatusUnprocessableEntity)

	if sent := ts.mail.messages(); len(sent) != 0 {
		t.Errorf("sent %d messages for an invalid signup, want 0", len(sent))
	}
}

// A mailer that is down does not stop the account from being created — the
// person still signed up, and an admin still needs to see them waiting.
func TestSignUpRedirectsEvenWhenTheMailerFails(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("first@example.com")
	ts.mail.failWith = errMailerDown

	ts.post("/sign_up", url.Values{
		"user[email]":    {"second@example.com"},
		"user[password]": {"password"},
	}).assertRedirect("/")

	ts.get("/sign_up").assertContains("User was successfully created.")
	if _, err := ts.db.UserByEmail(t.Context(), "second@example.com"); err != nil {
		t.Errorf("the user was not created: %v", err)
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
