package web

import (
	"errors"
	"net/http"

	"github.com/jaimerodas/tinystart/internal/store"
)

// Settings → Users: the admin list, the invitation form, and what an admin can
// do to each row. Everything here is behind adminOnly, which redirects rather
// than refusing — someone who is not an admin has no reason to be told the
// section exists.

// adminUsersData is the list.
type adminUsersData struct {
	Users []adminUserView
}

// adminUserView is one row, with the two helpers in UsersHelper already
// applied: whether each button is drawn at all is a decision about this row,
// and the template does not have to make it.
type adminUserView struct {
	ID    int64
	Email string
	Admin bool
	// Status is the CSS class and StatusLabel the word: "approved"/"Approved".
	Status      string
	StatusLabel string
	// CanReset is false for your own row. Sending yourself reset instructions
	// from the admin list is not a thing anybody meant to do. The password
	// page is one click away. For an invitation, the same button sends the
	// invitation again, and ResetLabel says so.
	CanReset   bool
	ResetLabel string
	// CanToggle is what store.ToggleApproved accepts: anyone but an approved
	// admin, who must not block the only account that can unblock anyone,
	// and an invitation, which accepting approves.
	CanToggle   bool
	ToggleLabel string
	// CanDelete is what store.DeleteUser accepts: an account that is not
	// approved and not an admin. To delete someone, block them first. To take
	// an invitation back, delete it.
	CanDelete bool
}

func newAdminUserView(user store.User, viewer *store.User) adminUserView {
	view := adminUserView{
		ID:          user.ID,
		Email:       user.Email,
		Admin:       user.Admin,
		Status:      "blocked",
		StatusLabel: "Blocked",
		CanReset:    user.ID != viewer.ID,
		ResetLabel:  "Reset password",
		CanToggle:   !(user.Approved && user.Admin) && !user.Invited(),
		ToggleLabel: "Unblock",
		CanDelete:   !user.Approved && !user.Admin,
	}
	switch {
	case user.Invited():
		view.Status = "invited"
		view.StatusLabel = "Invited"
		view.ResetLabel = "Resend invitation"
	case user.Approved:
		view.Status = "approved"
		view.StatusLabel = "Approved"
		view.ToggleLabel = "Block"
	}
	return view
}

// handleAdminUsers is GET /settings/admin/users, newest first.
func (s *Server) handleAdminUsers() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		viewer := userFrom(r.Context())

		users, err := s.db.AllUsers(r.Context())
		if err != nil {
			s.serverError(w, r, err)
			return
		}

		var data adminUsersData
		for _, user := range users {
			data.Users = append(data.Users, newAdminUserView(user, viewer))
		}

		s.render(w, r, http.StatusOK, layoutApplication, pageSettingsUsers, data)
	})
}

// handleAdminUserApprove is POST /settings/admin/users/{id}/approve: block or
// unblock.
//
// A toggle rather than two actions, because it is one button. The label says
// which way it will go. The store reads and writes the flag in one
// transaction, so two admins clicking at once cannot cancel each other out.
// Neither way sends mail. People arrive through an invitation, so unblocking
// someone is not a welcome.
func (s *Server) handleAdminUserApprove() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(r)
		if !ok {
			s.notFound(w)
			return
		}

		if _, err := s.db.ToggleApproved(r.Context(), id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.notFound(w)
				return
			}
			s.serverError(w, r, err)
			return
		}
		s.redirect(w, r, "/settings/admin/users", "", "")
	})
}

// handleAdminUserInvite is POST /settings/admin/users: add the address as an
// open invitation, and mail it the link.
func (s *Server) handleAdminUserInvite() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := s.db.InviteUser(r.Context(), r.PostFormValue("email"))
		var invalid store.ValidationError
		if errors.As(err, &invalid) {
			s.redirect(w, r, "/settings/admin/users", flashAlert, invalid.Error())
			return
		}
		if err != nil {
			s.serverError(w, r, err)
			return
		}

		// The invitation is saved, so a 500 would misreport it. The admin is
		// told instead, and the row has the button that sends it again.
		if err := s.sendInvitation(r, user); err != nil {
			s.log.ErrorContext(r.Context(), "sending invitation", "error", err, "user_id", user.ID)
			s.redirect(w, r, "/settings/admin/users", flashAlert,
				"The invitation to "+user.Email+" did not send. Try Resend invitation.")
			return
		}
		s.redirect(w, r, "/settings/admin/users", flashNotice, "Invitation sent to "+user.Email)
	})
}

// handleAdminUserPasswordReset is POST
// /settings/admin/users/{id}/password_reset: the same mail the forgotten-
// password form sends, for the case where somebody asks an admin instead.
// For an open invitation, that mail is the invitation again.
func (s *Server) handleAdminUserPasswordReset() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(r)
		if !ok {
			s.notFound(w)
			return
		}

		user, err := s.db.UserByID(r.Context(), id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.notFound(w)
				return
			}
			s.serverError(w, r, err)
			return
		}

		// A failure to send is a 500, unlike the public form, which swallows
		// it. There is nothing here to give away, and an admin who clicked the
		// button must know that it did not work.
		if err := s.sendPasswordReset(r, user); err != nil {
			s.serverError(w, r, err)
			return
		}
		notice := "Password reset instructions sent"
		if user.Invited() {
			notice = "Invitation sent to " + user.Email
		}
		s.redirect(w, r, "/settings/admin/users", flashNotice, notice)
	})
}

// handleAdminUserDestroy is DELETE /settings/admin/users/{id}. The store
// refuses everyone the list draws no Delete button for, and that refusal is a
// 404, the same as an id that addresses nobody. The person gets no mail.
func (s *Server) handleAdminUserDestroy() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(r)
		if !ok {
			s.notFound(w)
			return
		}

		if err := s.db.DeleteUser(r.Context(), id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.notFound(w)
				return
			}
			s.serverError(w, r, err)
			return
		}
		s.redirect(w, r, "/settings/admin/users", flashNotice, "User deleted")
	})
}
