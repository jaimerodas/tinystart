package web

import (
	"errors"
	"net/http"

	"github.com/jaimerodas/tinystart/internal/postmark"
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
	// page is one click away.
	CanReset bool
	// CanToggle is false for an approved admin, so they cannot block
	// themselves out of the only account that can unblock anyone.
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
// A toggle rather than two actions, because it is one button. The label says
// which way it will go. The store reads and writes the flag in one
// transaction, so two admins clicking at once cannot cancel each other out.
func (s *Server) handleAdminUserApprove() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathID(r)
		if !ok {
			s.notFound(w)
			return
		}

		user, err := s.db.ToggleApproved(r.Context(), id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.notFound(w)
				return
			}
			s.serverError(w, r, err)
			return
		}
		// The toggle already landed, so a mail failure here is logged and
		// swallowed rather than shown — it would misreport the toggle itself.
		if user.Approved {
			if err := s.sendAccountApproved(r, user); err != nil {
				s.log.ErrorContext(r.Context(), "sending account-approved mail", "error", err, "user_id", user.ID)
			}
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
		// it. There is nothing here to give away, and an admin who clicked the
		// button must know that it did not work.
		if err := s.sendPasswordReset(r, user); err != nil {
			s.serverError(w, r, err)
			return
		}
		s.redirect(w, r, "/settings/admin/users", flashNotice, "Password reset instructions sent")
	})
}

// sendAccountApproved tells a user their account is approved and links back
// to the app. Shaped like sendPasswordReset: both bodies, one send.
func (s *Server) sendAccountApproved(r *http.Request, user *store.User) error {
	data := struct{ URL string }{s.baseURL(r) + "/"}
	html, err := s.renderMail("account_approved.html", data)
	if err != nil {
		return err
	}
	text, err := s.renderMail("account_approved.txt", data)
	if err != nil {
		return err
	}

	return s.mailer.Send(r.Context(), postmark.Message{
		From:     s.cfg.MailFrom,
		To:       user.Email,
		Subject:  "Your account is approved",
		TextBody: text,
		HTMLBody: html,
	})
}
