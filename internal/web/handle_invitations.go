package web

import (
	"errors"
	"net/http"

	"github.com/jaimerodas/tinystart/internal/store"
)

// Invitations are how everyone after the first admin gets an account. An
// admin invites an address from Settings → Users. The mail carries a signed
// link, and the link sets the password, approves the account and signs it
// in, in that order.

const (
	invitationInvalid  = "Invitation link is invalid or has expired. Enter your email address to get a new one."
	invitationAccepted = "Welcome to TinyStart."
)

// invitationData is the form: the address it is for, and the token to send
// back.
type invitationData struct {
	Email string
	Token string
}

// handleInvitationShow is GET /invitations/{token}: the form that sets the
// password.
func (s *Server) handleInvitationShow() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.PathValue("token")
		user, err := s.userFromInvitationToken(r.Context(), token)
		if err != nil {
			s.redirect(w, r, "/passwords/new", flashAlert, invitationInvalid)
			return
		}
		s.render(w, r, http.StatusOK, layoutSession, "invitations_show",
			invitationData{Email: user.Email, Token: token})
	})
}

// handleInvitationAccept is POST /invitations/{token}.
//
// A refusal goes back to the same link, which still works. The new user lands
// on Settings → Browsers, which tells them how to make TinyStart their home
// page.
func (s *Server) handleInvitationAccept() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.PathValue("token")
		user, err := s.userFromInvitationToken(r.Context(), token)
		if err != nil {
			s.redirect(w, r, "/passwords/new", flashAlert, invitationInvalid)
			return
		}

		password := r.PostFormValue("password")
		if password != r.PostFormValue("password_confirmation") {
			s.redirect(w, r, "/invitations/"+token, flashAlert, resetMismatch)
			return
		}

		user, err = s.db.AcceptInvitation(r.Context(), user.ID, password)
		var invalid store.ValidationError
		switch {
		case errors.As(err, &invalid):
			s.redirect(w, r, "/invitations/"+token, flashAlert, invalid.Error())
			return
		case errors.Is(err, store.ErrNotFound):
			// The user has a password: they accepted after the check above,
			// or the token was signed for someone who was never invited.
			s.redirect(w, r, "/passwords/new", flashAlert, invitationInvalid)
			return
		case err != nil:
			s.serverError(w, r, err)
			return
		}

		if err := s.startNewSessionFor(w, r, user); err != nil {
			s.serverError(w, r, err)
			return
		}
		s.announceAcceptedInvitation(r, user)
		s.redirect(w, r, "/settings/browsers", flashNotice, invitationAccepted)
	})
}

// sendInvitation mails the link that accepts an invitation. The mail says the
// link works for 7 days, which is invitationLifetime.
func (s *Server) sendInvitation(r *http.Request, user *store.User) error {
	base := s.baseURL(r)
	data := struct{ URL, Base string }{base + "/invitations/" + s.invitationToken(user), base + "/"}
	return s.sendMail(r, user.Email, "You're invited to TinyStart", "invitation", data)
}

// announceAcceptedInvitation welcomes the new user, and tells every admin that
// the invitation was accepted. The account is real and signed in by now, so
// a mail that does not go is a logged line, not a failed acceptance.
func (s *Server) announceAcceptedInvitation(r *http.Request, user *store.User) {
	base := s.baseURL(r)
	welcome := struct{ URL, Base string }{base + "/settings/browsers", base + "/"}
	if err := s.sendMail(r, user.Email, "Welcome to TinyStart", "welcome", welcome); err != nil {
		s.log.ErrorContext(r.Context(), "sending welcome mail", "error", err, "user_id", user.ID)
	}

	users, err := s.db.AllUsers(r.Context())
	if err != nil {
		s.log.ErrorContext(r.Context(), "listing admins to mail", "error", err)
		return
	}
	accepted := struct{ Email, URL, Base string }{user.Email, base + "/settings/admin/users", base + "/"}
	for _, admin := range users {
		if !admin.Admin || !admin.Approved {
			continue
		}
		subject := user.Email + " accepted your TinyStart invitation"
		if err := s.sendMail(r, admin.Email, subject, "invitation_accepted", accepted); err != nil {
			s.log.ErrorContext(r.Context(), "sending invitation-accepted mail", "error", err, "user_id", admin.ID)
		}
	}
}
