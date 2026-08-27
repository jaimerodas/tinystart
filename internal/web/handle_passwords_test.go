package web

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// test/controllers/passwords_controller_test.rb, ported, plus the token rules
// that in Rails belonged to generates_token_for.

func TestPasswordNewRenders(t *testing.T) {
	ts := newTestServer(t)

	ts.get("/passwords/new").
		assertStatus(http.StatusOK).
		assertContains("<h1>Forgot your password?</h1>").
		assertContains("<title>Forgot your password? - TinyStart</title>")
}

func TestPasswordCreateWithAnExistingEmailSendsMail(t *testing.T) {
	ts := newTestServer(t)
	user := ts.createUser("one@example.com")

	ts.post("/passwords", url.Values{"email": {"one@example.com"}}).
		assertRedirect("/sign_in")

	sent := ts.mail.messages()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	if sent[0].To != user.Email {
		t.Errorf("To = %q, want %q", sent[0].To, user.Email)
	}
	if sent[0].Subject != "Reset your password" {
		t.Errorf("Subject = %q, want %q", sent[0].Subject, "Reset your password")
	}
	if !strings.Contains(sent[0].TextBody, "You can reset your password within the next 15 minutes") {
		t.Errorf("TextBody = %q, want the fifteen-minute sentence", sent[0].TextBody)
	}
	// The link is absolute and built from the configured host, because a
	// relative one in mail points at the mail client.
	if !strings.Contains(sent[0].HTMLBody, `<a href="https://start.example.com/passwords/`) {
		t.Errorf("HTMLBody = %q, want an absolute reset link", sent[0].HTMLBody)
	}
	// The link is the last thing in the plain text, and then a blank line.
	// Rails' mailer.text.erb layout is `<%= yield %>` and contributes the
	// newline after it. The parity harness compares the message as sent, so
	// the mail's whitespace is as much a part of it as a page's.
	if !strings.HasSuffix(sent[0].TextBody, "/edit\n\n") {
		t.Errorf("TextBody = %q, want it to end with the link and a blank line", sent[0].TextBody)
	}

	ts.get("/sign_in").assertContains(resetSentNotice)
}

// The answer is the same whether the address is one we know or not. Anything
// else turns this form into a way of asking who has an account here.
func TestPasswordCreateWithAnUnknownEmailSendsNothingAndSaysTheSame(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")

	ts.post("/passwords", url.Values{"email": {"nonexistent@example.com"}}).
		assertRedirect("/sign_in")

	if sent := ts.mail.messages(); len(sent) != 0 {
		t.Errorf("sent %d messages for an unknown address, want 0", len(sent))
	}
	ts.get("/sign_in").assertContains(resetSentNotice)
}

// A mailer that is down must not change the answer either — a 500 here says
// "that address exists" as loudly as a different notice does.
func TestPasswordCreateSaysTheSameWhenTheMailerFails(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")
	ts.mail.failWith = errMailerDown

	ts.post("/passwords", url.Values{"email": {"one@example.com"}}).
		assertRedirect("/sign_in")
	ts.get("/sign_in").assertContains(resetSentNotice)
}

func TestPasswordEditWithAValidToken(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")
	token := ts.resetToken("one@example.com")

	ts.get("/passwords/" + token + "/edit").
		assertStatus(http.StatusOK).
		assertContains("<h1>Update your password</h2>").
		assertContains(`<form action="/passwords/` + token + `"`).
		assertContains(`<input type="hidden" name="_method" value="put" />`)
}

func TestPasswordEditWithAnInvalidToken(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")

	ts.get("/passwords/invalid-token/edit").assertRedirect("/passwords/new")
	ts.get("/passwords/new").assertContains(resetTokenInvalid)
}

func TestPasswordUpdateWithAValidPassword(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")
	token := ts.resetToken("one@example.com")

	ts.post("/passwords/"+token, url.Values{
		"_method":               {"put"},
		"password":              {"newpassword123"},
		"password_confirmation": {"newpassword123"},
	}).assertRedirect("/sign_in")

	ts.get("/sign_in").assertContains(resetDoneNotice)

	if _, err := ts.db.Authenticate(t.Context(), "one@example.com", "newpassword123"); err != nil {
		t.Errorf("the new password does not work: %v", err)
	}
	if _, err := ts.db.Authenticate(t.Context(), "one@example.com", testPassword); err == nil {
		t.Error("the old password still works")
	}
}

func TestPasswordUpdateWithMismatchedPasswords(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")
	token := ts.resetToken("one@example.com")

	ts.post("/passwords/"+token, url.Values{
		"_method":               {"put"},
		"password":              {"newpassword123"},
		"password_confirmation": {"differentpassword"},
	}).assertRedirect("/passwords/" + token + "/edit")

	ts.get("/passwords/" + token + "/edit").assertContains(resetMismatch)

	if _, err := ts.db.Authenticate(t.Context(), "one@example.com", testPassword); err != nil {
		t.Errorf("the original password stopped working: %v", err)
	}
}

// A password ActiveRecord refuses came back as "Passwords did not
// match." too, because PasswordsController#update was one call to update. The
// message is misleading and it is the one the deployed app shows.
func TestPasswordUpdateWithABlankPassword(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")
	token := ts.resetToken("one@example.com")

	ts.post("/passwords/"+token, url.Values{
		"_method":               {"put"},
		"password":              {""},
		"password_confirmation": {""},
	}).assertRedirect("/passwords/" + token + "/edit")

	ts.get("/passwords/" + token + "/edit").assertContains(resetMismatch)
}

func TestPasswordUpdateWithAnInvalidToken(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")

	ts.post("/passwords/invalid-token", url.Values{
		"_method":               {"put"},
		"password":              {"newpassword123"},
		"password_confirmation": {"newpassword123"},
	}).assertRedirect("/passwords/new")
}

// The token stops working after fifteen minutes.
func TestAResetTokenExpires(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")
	token := ts.resetToken("one@example.com")

	ts.clock.advance(passwordResetLifetime + time.Second)

	ts.get("/passwords/" + token + "/edit").assertRedirect("/passwords/new")
}

// And after the password it was issued against changes, which is what makes
// it single-use without a table to write it down in.
func TestAResetTokenDiesWhenThePasswordChanges(t *testing.T) {
	ts := newTestServer(t)
	user := ts.createUser("one@example.com")
	token := ts.resetToken("one@example.com")

	if err := ts.db.ResetPassword(t.Context(), user.ID, "somethingelse"); err != nil {
		t.Fatalf("changing the password: %v", err)
	}

	ts.get("/passwords/" + token + "/edit").assertRedirect("/passwords/new")
}

// A token signed with a different key is no token at all.
func TestAResetTokenWithATamperedSignatureIsRefused(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")
	token := ts.resetToken("one@example.com")

	tampered := tamperSignature(token)

	ts.get("/passwords/" + tampered + "/edit").assertRedirect("/passwords/new")
}

// The whole flow, end to end, the way somebody actually walks it.
func TestPasswordResetFlow(t *testing.T) {
	ts := newTestServer(t)
	ts.createApprovedUser("one@example.com")

	ts.post("/passwords", url.Values{"email": {"one@example.com"}}).
		assertRedirect("/sign_in")

	sent := ts.mail.messages()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	token := tokenFromMail(t, sent[0].TextBody)

	ts.get("/passwords/" + token + "/edit").assertStatus(http.StatusOK)

	ts.post("/passwords/"+token, url.Values{
		"_method":               {"put"},
		"password":              {"mynewpassword123"},
		"password_confirmation": {"mynewpassword123"},
	}).assertRedirect("/sign_in")

	// And the new password gets them in.
	ts.post("/sign_in", url.Values{
		"email":    {"one@example.com"},
		"password": {"mynewpassword123"},
	}).assertRedirect("/")
}

// resetToken mints a token the way the mailer does, for the tests that are
// about what happens after the mail rather than about the mail.
func (ts *testServer) resetToken(email string) string {
	ts.t.Helper()
	user, err := ts.db.UserByEmail(ts.t.Context(), email)
	if err != nil {
		ts.t.Fatalf("looking up %s: %v", email, err)
	}
	return ts.app.passwordResetToken(user)
}

// tokenFromMail pulls the token back out of the message, so the flow test
// walks the same link a person clicks.
func tokenFromMail(t *testing.T, body string) string {
	t.Helper()
	match := regexp.MustCompile(`/passwords/([^/\s]+)/edit`).FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("no reset link in %q", body)
	}
	return match[1]
}

// tamperSignature changes the first character after the dot — the first six
// bits of the signature, every one of them significant. Not the last
// character. A 32-byte HMAC in raw base64url is 43 characters. The last one
// carries only two bits of data above four bits of padding that the decoder
// ignores. Flipping it between A and B changed nothing the verifier could
// see. That made this test pass or fail based on how the token happened to
// end.
func tamperSignature(token string) string {
	dot := strings.LastIndexByte(token, '.')
	first := token[dot+1]
	replacement := byte('A')
	if first == 'A' {
		replacement = 'B'
	}
	return token[:dot+1] + string(replacement) + token[dot+2:]
}

// errMailerDown stands in for Postmark being unreachable.
var errMailerDown = errors.New("postmark is unreachable")
