package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jaimerodas/tinystart/internal/postmark"
	"github.com/jaimerodas/tinystart/internal/store"
)

// invitationServer is the admin, and somebody the admin invited. Nobody is
// signed in: the invitation link is for someone with no account yet.
func invitationServer(t *testing.T) (*testServer, *store.User, string) {
	t.Helper()
	ts := newTestServer(t)
	ts.createUser("admin@example.com")
	invited := ts.inviteUser("new@example.com")
	return ts, invited, ts.app.invitationToken(invited)
}

func acceptForm(password, confirmation string) url.Values {
	return form("password", password, "password_confirmation", confirmation)
}

func TestInvitationPageWithAValidToken(t *testing.T) {
	ts, _, token := invitationServer(t)

	ts.get("/invitations/" + token).
		assertStatus(http.StatusOK).
		assertContains("<title>Welcome to TinyStart - TinyStart</title>").
		assertContains(`<form action="/invitations/` + token + `" accept-charset="UTF-8" method="post">`).
		// The address in a username field, so a password manager saves the
		// new password against the right account.
		assertContains(`<input readonly="readonly" autocomplete="username" value="new@example.com" type="email" name="email" id="email" />`).
		assertContains(`autocomplete="new-password" placeholder="Choose a password"`).
		assertContains(`name="password_confirmation"`)
}

// A link that is not one — made up, expired, used, taken back — lands where
// a new one can be asked for. The forgot-password form sends a fresh
// invitation to an address that has one open.
func TestInvitationPageWithAnInvalidToken(t *testing.T) {
	ts, _, token := invitationServer(t)

	for _, bad := range []string{"invalid-token", tamperSignature(token)} {
		ts.get("/invitations/" + bad).assertRedirect("/passwords/new")
		ts.get("/passwords/new").assertContains(invitationInvalid)
	}
}

func TestAcceptingAnInvitation(t *testing.T) {
	ts, invited, token := invitationServer(t)

	ts.post("/invitations/"+token, acceptForm("newpassword", "newpassword")).
		assertRedirect("/settings/browsers")

	accepted := ts.reloadUser(invited)
	if !accepted.Approved || accepted.Invited() {
		t.Errorf("after accepting approved=%v invited=%v, want true, false", accepted.Approved, accepted.Invited())
	}
	// Signed in, and on the page that makes TinyStart the browser's home.
	ts.get("/settings/browsers").
		assertStatus(http.StatusOK).
		assertContains("Welcome to TinyStart.")
	if _, err := ts.db.Authenticate(t.Context(), "new@example.com", "newpassword"); err != nil {
		t.Errorf("the new password does not work: %v", err)
	}
}

// The new user gets a welcome, and every admin hears that the invitation was
// accepted. Nobody else gets anything.
func TestAcceptingAnInvitationMailsTheUserAndTheAdmins(t *testing.T) {
	ts, _, token := invitationServer(t)
	ts.createApprovedUser("approved@example.com")
	ts.createUser("blocked@example.com")

	ts.post("/invitations/"+token, acceptForm("newpassword", "newpassword"))

	sent := ts.mail.messages()
	if len(sent) != 2 {
		t.Fatalf("sent %d messages, want 2", len(sent))
	}
	welcome, admin := sent[0], sent[1]
	if welcome.To == "admin@example.com" {
		welcome, admin = admin, welcome
	}

	assertMail(t, welcome, "new@example.com", "Welcome to TinyStart",
		"https://start.example.com/settings/browsers")
	assertMail(t, admin, "admin@example.com", "new@example.com accepted your TinyStart invitation",
		"https://start.example.com/settings/admin/users")
}

// The account is real and signed in by then. A mail that did not go is a
// logged line, not a failed acceptance.
func TestAcceptingAnInvitationWhenTheMailerFails(t *testing.T) {
	ts, invited, token := invitationServer(t)
	ts.mail.failWith = errMailerDown

	ts.post("/invitations/"+token, acceptForm("newpassword", "newpassword")).
		assertRedirect("/settings/browsers")

	if !ts.reloadUser(invited).Approved {
		t.Error("the invitation was not accepted")
	}
	ts.get(ts.protectedPath()).assertStatus(http.StatusOK)
}

// A refusal sends the reader back to the same link, which still works.
func TestAcceptingAnInvitationWithAPasswordItRefuses(t *testing.T) {
	for _, test := range []struct {
		name                   string
		password, confirmation string
		want                   string
	}{
		{"mismatch", "newpassword", "different", resetMismatch},
		{"blank", "", "", "Password can&#39;t be blank"},
		{"too long", strings.Repeat("a", 73), strings.Repeat("a", 73), "Password is too long (maximum is 72 characters)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ts, invited, token := invitationServer(t)

			ts.post("/invitations/"+token, acceptForm(test.password, test.confirmation)).
				assertRedirect("/invitations/" + token)

			ts.get("/invitations/" + token).
				assertStatus(http.StatusOK).
				assertContains(test.want)
			if !ts.reloadUser(invited).Invited() {
				t.Error("the refused password was saved")
			}
			if ts.sessionCookie() != "" {
				t.Error("a refused acceptance signed somebody in")
			}
			if sent := ts.mail.messages(); len(sent) != 0 {
				t.Errorf("sent %d messages for a refusal, want 0", len(sent))
			}
		})
	}
}

// Setting the password changes the digest the token carries a fingerprint
// of, so the link works once.
func TestAnInvitationWorksOnce(t *testing.T) {
	ts, _, token := invitationServer(t)
	ts.post("/invitations/"+token, acceptForm("newpassword", "newpassword"))

	ts.get("/invitations/" + token).assertRedirect("/passwords/new")
	ts.post("/invitations/"+token, acceptForm("another", "another")).assertRedirect("/passwords/new")

	if _, err := ts.db.Authenticate(t.Context(), "new@example.com", "newpassword"); err != nil {
		t.Errorf("the first password stopped working: %v", err)
	}
}

// A week, which is long enough to get to an email and short enough that an
// old one in an inbox is not a way in forever.
func TestAnInvitationExpires(t *testing.T) {
	ts, _, token := invitationServer(t)

	ts.clock.advance(invitationLifetime - time.Minute)
	ts.get("/invitations/" + token).assertStatus(http.StatusOK)

	ts.clock.advance(2 * time.Minute)
	ts.get("/invitations/" + token).assertRedirect("/passwords/new")
}

// Each token is signed for its purpose. A reset link cannot accept an
// invitation, and an invitation cannot reset a password.
func TestResetAndInvitationTokensDoNotCross(t *testing.T) {
	ts, invited, token := invitationServer(t)
	ts.createUser("blocked@example.com")

	ts.get("/invitations/" + ts.resetToken(invited.Email)).assertRedirect("/passwords/new")
	ts.get("/invitations/" + ts.resetToken("blocked@example.com")).assertRedirect("/passwords/new")
	ts.get("/passwords/" + token + "/edit").assertRedirect("/passwords/new")
}

// A blocked user has a password, so no invitation can approve them, even
// one signed for them by mistake. The store refuses the write.
func TestABlockedUserCannotAcceptAnInvitation(t *testing.T) {
	ts, _, _ := invitationServer(t)
	blocked := ts.createUser("blocked@example.com")

	ts.post("/invitations/"+ts.app.invitationToken(blocked), acceptForm("letmein", "letmein")).
		assertRedirect("/passwords/new")

	if ts.reloadUser(blocked).Approved {
		t.Error("an invitation unblocked a blocked user")
	}
	if _, err := ts.db.Authenticate(t.Context(), blocked.Email, "letmein"); err == nil {
		t.Error("an invitation changed a blocked user's password")
	}
	if ts.sessionCookie() != "" {
		t.Error("an invitation signed a blocked user in")
	}
}

// The whole thing, the way two people walk it: the admin invites, the new
// user follows the link in the mail, and the password works from then on.
func TestTheInvitationFlow(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("admin@example.com")
	ts.signIn("admin@example.com")

	ts.post(invitePath, form("email", "new@example.com")).assertRedirect("/settings/admin/users")
	ts.post("/session", form("_method", "delete")).assertRedirect("/sign_in")

	sent := ts.mail.messages()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	token := invitationTokenFromMail(t, sent[0].TextBody)

	ts.get("/invitations/" + token).assertStatus(http.StatusOK)
	ts.post("/invitations/"+token, acceptForm("mynewpassword", "mynewpassword")).
		assertRedirect("/settings/browsers")
	ts.post("/session", form("_method", "delete")).assertRedirect("/sign_in")

	ts.post("/sign_in", form("email", "new@example.com", "password", "mynewpassword")).
		assertRedirect("/")
}

// assertInvitationMail checks the mail an admin's invitation sends, and that
// its link is one that works.
func assertInvitationMail(t *testing.T, message postmark.Message, to string) {
	t.Helper()
	assertMail(t, message, to, "You're invited to TinyStart", "https://start.example.com/invitations/")
	if !strings.Contains(message.TextBody, "The link works for 7 days.") {
		t.Errorf("TextBody = %q, want how long the link works", message.TextBody)
	}
}

// assertMail checks what every mail this app sends has: the right address and
// subject, both bodies, the outbound stream, and an absolute link. A relative
// link in a mail points at the mail client.
func assertMail(t *testing.T, message postmark.Message, to, subject, link string) {
	t.Helper()
	if message.To != to {
		t.Errorf("To = %q, want %q", message.To, to)
	}
	if message.Subject != subject {
		t.Errorf("Subject = %q, want %q", message.Subject, subject)
	}
	if !strings.Contains(message.TextBody, link) {
		t.Errorf("TextBody = %q, want %s", message.TextBody, link)
	}
	if !strings.Contains(message.HTMLBody, `<a href="`+link) {
		t.Errorf("HTMLBody = %q, want a link to %s", message.HTMLBody, link)
	}
	if message.MessageStream != "outbound" {
		t.Errorf("MessageStream = %q, want outbound", message.MessageStream)
	}
}

// invitationTokenFromMail pulls the token out of the message, so the flow
// test follows the same link a person clicks.
func invitationTokenFromMail(t *testing.T, body string) string {
	t.Helper()
	match := regexp.MustCompile(`/invitations/([^/\s]+)`).FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("no invitation link in %q", body)
	}
	return match[1]
}
