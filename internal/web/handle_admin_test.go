package web

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jaimerodas/tinystart/internal/store"
)

// adminServer is the first user — who is bootstrapped as an approved admin —
// signed in, with somebody waiting for approval and somebody already approved.
func adminServer(t *testing.T) (*testServer, *storeUsers) {
	t.Helper()
	ts := newTestServer(t)
	users := &storeUsers{
		admin:    ts.createUser("admin@example.com"),
		waiting:  ts.createUser("waiting@example.com"),
		approved: ts.createApprovedUser("approved@example.com"),
	}
	ts.signIn(users.admin.Email)
	return ts, users
}

type storeUsers struct {
	admin, waiting, approved *store.User
}

func TestAdminUsersRequiresAuthentication(t *testing.T) {
	ts := newTestServer(t)
	ts.get("/settings/admin/users").assertRedirect("/session/new")
}

// Not an error page, just the start page: someone who is signed in and not an
// admin has no business being told the section exists.
func TestAdminSectionSendsEverybodyElseHome(t *testing.T) {
	ts, users := adminServer(t)
	ts.signIn(users.approved.Email)

	ts.get("/settings/admin/users").assertRedirect("/")
	ts.post(approvePath(users.waiting.ID), nil).assertRedirect("/")
	ts.post(resetPath(users.waiting.ID), nil).assertRedirect("/")

	if ts.reloadUser(users.waiting).Approved {
		t.Error("a non-admin's POST approved somebody")
	}
	if len(ts.mail.messages()) != 0 {
		t.Error("a non-admin's POST sent a password reset")
	}
}

// Newest first, and each row says where its account stands.
func TestAdminUsersListsEverybodyNewestFirst(t *testing.T) {
	ts, users := adminServer(t)

	resp := ts.get("/settings/admin/users").assertStatus(http.StatusOK)

	order := []int64{users.approved.ID, users.waiting.ID, users.admin.ID}
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
		assertContains(`<span class="user-admin">Admin</span>`)
}

// The toggle is hidden for an approved admin. Otherwise, that admin can
// block themselves out of the only account that can unblock anyone. The
// reset button is hidden for your own row.
func TestAdminUsersHidesTheActionsThatMakeNoSense(t *testing.T) {
	ts, users := adminServer(t)

	resp := ts.get("/settings/admin/users")

	resp.assertNotContains(approvePath(users.admin.ID)).
		assertNotContains(resetPath(users.admin.ID)).
		assertContains(approvePath(users.waiting.ID)).
		assertContains(resetPath(users.waiting.ID))
}

// One button, and the label says which way it goes.
func TestAdminApproveIsAToggle(t *testing.T) {
	ts, users := adminServer(t)

	ts.get("/settings/admin/users").
		assertContains(`action="` + approvePath(users.waiting.ID) + `"><button type="submit">Approve</button>`)

	ts.post(approvePath(users.waiting.ID), nil).assertRedirect("/settings/admin/users")
	if !ts.reloadUser(users.waiting).Approved {
		t.Fatal("the user was not approved")
	}

	ts.get("/settings/admin/users").
		assertContains(`action="` + approvePath(users.waiting.ID) + `"><button type="submit">Block</button>`)

	ts.post(approvePath(users.waiting.ID), nil)
	if ts.reloadUser(users.waiting).Approved {
		t.Error("the user was not blocked again")
	}
}

func TestAdminPasswordResetMailsTheUser(t *testing.T) {
	ts, users := adminServer(t)

	ts.post(resetPath(users.waiting.ID), nil).assertRedirect("/settings/admin/users")
	ts.get("/settings/admin/users").assertContains("Password reset instructions sent")

	sent := ts.mail.messages()
	if len(sent) != 1 {
		t.Fatalf("%d messages sent, want 1", len(sent))
	}
	if sent[0].To != users.waiting.Email {
		t.Errorf("sent to %q, want %q", sent[0].To, users.waiting.Email)
	}
	if sent[0].Subject != "Reset your password" {
		t.Errorf("subject = %q", sent[0].Subject)
	}
}

// An id that addresses nobody is a 404, the same page a bad URL gets.
func TestAdminActionsOnSomebodyWhoIsNotThere(t *testing.T) {
	ts, _ := adminServer(t)

	ts.post(approvePath(9999), nil).assertStatus(http.StatusNotFound)
	ts.post(resetPath(9999), nil).assertStatus(http.StatusNotFound)
	ts.post("/settings/admin/users/nobody/approve", nil).assertStatus(http.StatusNotFound)
}

func approvePath(id int64) string {
	return fmt.Sprintf("/settings/admin/users/%d/approve", id)
}

func resetPath(id int64) string {
	return fmt.Sprintf("/settings/admin/users/%d/password_reset", id)
}
