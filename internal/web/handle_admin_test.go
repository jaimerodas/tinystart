package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/jaimerodas/tinystart/internal/store"
)

// adminServer is the first user — who is bootstrapped as an approved admin —
// signed in, with somebody blocked, somebody approved, and somebody invited
// who has not accepted yet.
func adminServer(t *testing.T) (*testServer, *storeUsers) {
	t.Helper()
	ts := newTestServer(t)
	users := &storeUsers{
		admin:    ts.createUser("admin@example.com"),
		blocked:  ts.createUser("blocked@example.com"),
		approved: ts.createApprovedUser("approved@example.com"),
		invited:  ts.inviteUser("invited@example.com"),
	}
	ts.signIn(users.admin.Email)
	return ts, users
}

type storeUsers struct {
	admin, blocked, approved, invited *store.User
}

func TestAdminUsersRequiresAuthentication(t *testing.T) {
	ts := newTestServer(t)
	ts.get("/settings/admin/users").assertRedirect("/sign_in")
}

// Not an error page, just the start page: someone who is signed in and not an
// admin has no business being told the section exists.
func TestAdminSectionSendsEverybodyElseHome(t *testing.T) {
	ts, users := adminServer(t)
	ts.signIn(users.approved.Email)

	ts.get("/settings/admin/users").assertRedirect("/")
	ts.post(approvePath(users.blocked.ID), nil).assertRedirect("/")
	ts.post(resetPath(users.blocked.ID), nil).assertRedirect("/")
	ts.post(deletePath(users.blocked.ID), deleteForm()).assertRedirect("/")
	ts.post(invitePath, form("email", "new@example.com")).assertRedirect("/")

	if ts.reloadUser(users.blocked).Approved {
		t.Error("a non-admin's POST approved somebody")
	}
	if _, err := ts.db.UserByEmail(t.Context(), "new@example.com"); err == nil {
		t.Error("a non-admin's POST invited somebody")
	}
	if len(ts.mail.messages()) != 0 {
		t.Error("a non-admin's POST sent mail")
	}
}

// Newest first, and each row says where its account stands.
func TestAdminUsersListsEverybodyNewestFirst(t *testing.T) {
	ts, users := adminServer(t)

	resp := ts.get("/settings/admin/users").assertStatus(http.StatusOK)

	order := []int64{users.invited.ID, users.approved.ID, users.blocked.ID, users.admin.ID}
	position := -1
	for _, id := range order {
		found := strings.Index(resp.body, fmt.Sprintf(`<li id="user_%d"`, id))
		if found < position {
			t.Errorf("user %d is out of order", id)
		}
		position = found
	}

	resp.assertContains(`<span class="user-status blocked">Blocked</span>`).
		assertContains(`<span class="user-status approved">Approved</span>`).
		assertContains(`<span class="user-status invited">Invited</span>`).
		assertContains(`<span class="user-admin">Admin</span>`)
}

// The toggle is hidden for an approved admin. Otherwise, that admin can
// block themselves out of the only account that can unblock anyone. It is
// hidden for an invitation too, which accepting approves. The reset button is
// hidden for your own row. Delete shows only on an account that is not
// approved and not an admin.
func TestAdminUsersHidesTheActionsThatMakeNoSense(t *testing.T) {
	ts, users := adminServer(t)

	resp := ts.get("/settings/admin/users")

	resp.assertNotContains(approvePath(users.admin.ID)).
		assertNotContains(resetPath(users.admin.ID)).
		assertContains(approvePath(users.blocked.ID)).
		assertContains(resetPath(users.blocked.ID)).
		assertContains(deleteAction(users.blocked.ID)).
		assertNotContains(deleteAction(users.approved.ID)).
		assertNotContains(deleteAction(users.admin.ID)).
		assertNotContains(approvePath(users.invited.ID)).
		assertContains(deleteAction(users.invited.ID))
}

// For an invitation, the reset button sends the invitation again: there is
// no password to reset.
func TestAdminUsersOffersToResendAnInvitation(t *testing.T) {
	ts, users := adminServer(t)

	ts.get("/settings/admin/users").
		assertContains(`action="` + resetPath(users.invited.ID) + `"><button type="submit">Resend invitation</button>`).
		assertContains(`action="` + resetPath(users.blocked.ID) + `"><button type="submit">Reset password</button>`)
}

func TestAdminDeletesABlockedUser(t *testing.T) {
	ts, users := adminServer(t)

	ts.post(deletePath(users.blocked.ID), deleteForm()).assertRedirect("/settings/admin/users")

	ts.get("/settings/admin/users").
		assertContains("User deleted").
		assertNotContains(users.blocked.Email)
	if _, err := ts.db.UserByID(t.Context(), users.blocked.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("UserByID after the delete: %v, want ErrNotFound", err)
	}
}

// Delete is how an admin takes an invitation back. The link in the mail
// stops working with it.
func TestAdminDeletesAnInvitation(t *testing.T) {
	ts, users := adminServer(t)
	token := ts.app.invitationToken(users.invited)

	ts.post(deletePath(users.invited.ID), deleteForm()).assertRedirect("/settings/admin/users")

	ts.get("/invitations/" + token).assertRedirect("/passwords/new")
}

// The same refusal as Delete: a request made by hand cannot block an approved
// admin, and that includes you.
func TestAdminCannotBlockAnApprovedAdmin(t *testing.T) {
	ts, users := adminServer(t)

	ts.post(approvePath(users.admin.ID), nil).assertStatus(http.StatusNotFound)

	if !ts.reloadUser(users.admin).Approved {
		t.Error("the admin was blocked")
	}
}

// An invitation is approved by accepting it. Approving it by hand makes an
// account with no password.
func TestAdminCannotApproveAnInvitation(t *testing.T) {
	ts, users := adminServer(t)

	ts.post(approvePath(users.invited.ID), nil).assertStatus(http.StatusNotFound)

	if ts.reloadUser(users.invited).Approved {
		t.Error("the invitation was approved")
	}
}

// The list draws no Delete for these rows, and a request made by hand gets
// the 404 an unknown id gets.
func TestAdminCannotDeleteAnApprovedUserOrAnAdmin(t *testing.T) {
	ts, users := adminServer(t)

	ts.post(deletePath(users.approved.ID), deleteForm()).assertStatus(http.StatusNotFound)
	ts.post(deletePath(users.admin.ID), deleteForm()).assertStatus(http.StatusNotFound)

	// reloadUser fails the test if the account is gone.
	ts.reloadUser(users.approved)
	ts.reloadUser(users.admin)
}

// One button, and the label says which way it goes. Neither way sends mail:
// people arrive through an invitation now, so unblocking is not a welcome.
func TestAdminBlockIsAToggle(t *testing.T) {
	ts, users := adminServer(t)

	ts.get("/settings/admin/users").
		assertContains(`action="` + approvePath(users.blocked.ID) + `"><button type="submit">Unblock</button>`)

	ts.post(approvePath(users.blocked.ID), nil).assertRedirect("/settings/admin/users")
	if !ts.reloadUser(users.blocked).Approved {
		t.Fatal("the user was not unblocked")
	}

	ts.get("/settings/admin/users").
		assertContains(`action="` + approvePath(users.blocked.ID) + `"><button type="submit">Block</button>`)

	ts.post(approvePath(users.blocked.ID), nil)
	if ts.reloadUser(users.blocked).Approved {
		t.Error("the user was not blocked again")
	}
	if sent := ts.mail.messages(); len(sent) != 0 {
		t.Errorf("sent %d messages for the toggle, want 0", len(sent))
	}
}

func TestAdminPasswordResetMailsTheUser(t *testing.T) {
	ts, users := adminServer(t)

	ts.post(resetPath(users.blocked.ID), nil).assertRedirect("/settings/admin/users")
	ts.get("/settings/admin/users").assertContains("Password reset instructions sent")

	sent := ts.mail.messages()
	if len(sent) != 1 {
		t.Fatalf("%d messages sent, want 1", len(sent))
	}
	if sent[0].To != users.blocked.Email {
		t.Errorf("sent to %q, want %q", sent[0].To, users.blocked.Email)
	}
	if sent[0].Subject != "Reset your password" {
		t.Errorf("subject = %q", sent[0].Subject)
	}
}

func TestAdminInvitesSomeone(t *testing.T) {
	ts, _ := adminServer(t)

	ts.post(invitePath, form("email", " New@Example.com")).assertRedirect("/settings/admin/users")

	ts.get("/settings/admin/users").
		assertContains("Invitation sent to new@example.com").
		assertContains("new@example.com</h3>")
	invited := mustUser(t, ts, "new@example.com")
	if !invited.Invited() || invited.Approved || invited.Admin {
		t.Errorf("invited = %+v, want an open invitation", invited)
	}

	sent := ts.mail.messages()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	assertInvitationMail(t, sent[0], "new@example.com")
}

// The list has the form, and the field is an email field, so the browser
// checks the address before it is sent.
func TestAdminUsersHasTheInvitationForm(t *testing.T) {
	ts, _ := adminServer(t)

	ts.get("/settings/admin/users").
		assertContains(`<form class="invite-form" action="` + invitePath + `" method="post">`).
		assertContains(`type="email" name="email"`).
		assertContains(`<button type="submit" class="action-button">Invite</button>`)
}

// The store's messages, word for word. Nothing is created and nothing is
// mailed.
func TestAdminInviteRefusesABlankOrTakenAddress(t *testing.T) {
	ts, users := adminServer(t)

	for _, test := range []struct{ email, want string }{
		{"", "Email can&#39;t be blank"},
		{strings.ToUpper(users.approved.Email), "Email has already been taken"},
	} {
		ts.post(invitePath, form("email", test.email)).assertRedirect("/settings/admin/users")
		ts.get("/settings/admin/users").assertContains(test.want)
	}
	if sent := ts.mail.messages(); len(sent) != 0 {
		t.Errorf("sent %d messages for refused invitations, want 0", len(sent))
	}
}

// The invitation is in the database even when the mail did not go. The admin
// is told, and Resend invitation is right there on the row.
func TestAdminInviteSaysSoWhenTheMailerFails(t *testing.T) {
	ts, _ := adminServer(t)
	ts.mail.failWith = errMailerDown

	ts.post(invitePath, form("email", "new@example.com")).assertRedirect("/settings/admin/users")

	ts.get("/settings/admin/users").
		assertContains("The invitation to new@example.com did not send. Try Resend invitation.")
	if !mustUser(t, ts, "new@example.com").Invited() {
		t.Error("the invitation was not created")
	}
}

func TestAdminResendsAnInvitation(t *testing.T) {
	ts, users := adminServer(t)

	ts.post(resetPath(users.invited.ID), nil).assertRedirect("/settings/admin/users")

	ts.get("/settings/admin/users").assertContains("Invitation sent to invited@example.com")
	sent := ts.mail.messages()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	assertInvitationMail(t, sent[0], users.invited.Email)
}

// An id that addresses nobody is a 404, the same page a bad URL gets.
func TestAdminActionsOnSomebodyWhoIsNotThere(t *testing.T) {
	ts, _ := adminServer(t)

	ts.post(approvePath(9999), nil).assertStatus(http.StatusNotFound)
	ts.post(resetPath(9999), nil).assertStatus(http.StatusNotFound)
	ts.post(deletePath(9999), deleteForm()).assertStatus(http.StatusNotFound)
	ts.post("/settings/admin/users/nobody/approve", nil).assertStatus(http.StatusNotFound)
}

const invitePath = "/settings/admin/users"

func approvePath(id int64) string {
	return fmt.Sprintf("/settings/admin/users/%d/approve", id)
}

func deletePath(id int64) string {
	return fmt.Sprintf("/settings/admin/users/%d", id)
}

// deleteAction is the form's attribute. The bare path is a prefix of the
// other two actions on the same row.
func deleteAction(id int64) string {
	return `action="` + deletePath(id) + `"`
}

// deleteForm is what the button's form sends: a POST that the method
// override makes a DELETE.
func deleteForm() url.Values {
	return form("_method", "delete")
}

func resetPath(id int64) string {
	return fmt.Sprintf("/settings/admin/users/%d/password_reset", id)
}
