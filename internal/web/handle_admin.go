package web

import (
	"errors"
	"net/http"

	"github.com/jaimerodas/tinystart/internal/store"
)

// Settings → Users: the admin list, and the two things an admin can do from
// it. Everything here is behind adminOnly, which redirects rather than
// refusing — someone who is not an admin has no reason to be told the section
// exists.

// adminUsersData is the list.
type adminUsersData struct {
	Nav   []settingsNavItem
	Users []adminUserView
}

// adminUserView is one row, with the two helpers in UsersHelper already
// applied: whether each button is drawn at all is a decision about this row,
// and the template should not have to make it.
type adminUserView struct {
	ID    int64
	Email string
	Admin bool
	// Status is the CSS class and StatusLabel the word: "approved"/"Approved".
	Status      string
	StatusLabel string
	// CanReset is false for your own row. Sending yourself reset instructions
	// from the admin list is not a thing anybody meant to do; the password
	// page is one click away.
	CanReset bool
	// CanToggle is false for an approved admin, who would otherwise be able to
	// block themselves out of the only account that can unblock anyone.
	CanToggle   bool
	ToggleLabel string
}

func newAdminUserView(user store.User, viewer *store.User) adminUserView {
	view := adminUserView{
		ID:          user.ID,
		Email:       user.Email,
		Admin:       user.Admin,
		Status:      "blocked",
		StatusLabel: "Blocked",
		CanReset:    user.ID != viewer.ID,
		CanToggle:   !(user.Approved && user.Admin),
		ToggleLabel: "Approve",
	}
	if user.Approved {
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

		data := adminUsersData{Nav: settingsNav(viewer, "Users")}
		for _, user := range users {
			data.Users = append(data.Users, newAdminUserView(user, viewer))
		}

		s.render(w, r, http.StatusOK, layoutApplication, pageSettingsUsers, data)
	})
}

// handleAdminUserApprove is POST /settings/admin/users/{id}/approve.
//
// A toggle rather than two actions, because it is one button: the label says
// which way it will go, and the store reads and writes the flag in one
// transaction so two admins clicking at once cannot cancel each other out.
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

// handleAdminUserPasswordReset is POST
// /settings/admin/users/{id}/password_reset: the same mail the forgotten-
// password form sends, for the case where somebody asks an admin instead.
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
		// it: there is nothing here to give away, and an admin who clicked the
		// button should be told it did not work.
		if err := s.sendPasswordReset(r, user); err != nil {
			s.serverError(w, r, err)
			return
		}
		s.redirect(w, r, "/settings/admin/users", flashNotice, "Password reset instructions sent")
	})
}
