package web

import (
	"errors"
	"net/http"

	"github.com/jaimerodas/tinystart/internal/postmark"
	"github.com/jaimerodas/tinystart/internal/store"
)

// usersNewData is the sign-up form: the address as typed, the errors to list
// above it, which fields to outline, and whether to offer the way to sign in
// instead.
type usersNewData struct {
	Email           string
	Errors          []string
	EmailInvalid    bool
	PasswordInvalid bool
	AnyUsers        bool
}

// handleUserNew is GET /sign_up.
func (s *Server) handleUserNew() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if userFrom(r.Context()) != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		data, err := s.signUpForm(r, "", nil)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		s.render(w, r, http.StatusOK, layoutSession, "users_new", data)
	})
}

// handleUserCreate is POST /sign_up.
//
// A new account is not signed in: everyone after the first arrives unapproved
// and waits for an admin, so there is nothing to sign in to. The notice
// is the same either way, which is why the redirect goes to the start page and
// the start page sends them back to the sign-in form.
func (s *Server) handleUserCreate() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if userFrom(r.Context()) != nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		if err := r.ParseForm(); err != nil {
			s.serveErrorPage(w, http.StatusBadRequest, "/400.html")
			return
		}
		// params.expect(user: [:email, :password]) refuses a body with no user
		// key at all rather than treating it as an empty one, and answers 400.
		if !r.PostForm.Has("user[email]") && !r.PostForm.Has("user[password]") {
			s.serveErrorPage(w, http.StatusBadRequest, "/400.html")
			return
		}

		email := r.PostFormValue("user[email]")
		user, err := s.db.CreateUser(r.Context(), email, r.PostFormValue("user[password]"))
		if err == nil {
			// The bootstrap first user comes back already approved, so
			// there is no wait to tell them about.
			if !user.Approved {
				if err := s.sendAwaitingApproval(r, user); err != nil {
					s.log.ErrorContext(r.Context(), "sending awaiting-approval mail", "error", err, "user_id", user.ID)
				}
			}
			s.redirect(w, r, "/", flashNotice, "User was successfully created.")
			return
		}

		var invalid store.ValidationError
		if !errors.As(err, &invalid) {
			s.serverError(w, r, err)
			return
		}

		data, formErr := s.signUpForm(r, email, invalid)
		if formErr != nil {
			s.serverError(w, r, formErr)
			return
		}
		s.render(w, r, http.StatusUnprocessableEntity, layoutSession, "users_new", data)
	})
}

// signUpForm fills in the parts of the form that are the same whether it is
// being shown for the first time or shown again with errors.
func (s *Server) signUpForm(r *http.Request, email string, invalid store.ValidationError) (usersNewData, error) {
	any, err := s.db.AnyUsers(r.Context())
	if err != nil {
		return usersNewData{}, err
	}
	return usersNewData{
		Email:           email,
		Errors:          invalid.FullMessages(),
		EmailInvalid:    invalid.On("email"),
		PasswordInvalid: invalid.On("password"),
		AnyUsers:        any,
	}, nil
}

// sendAwaitingApproval tells a new user their account is waiting on an
// admin. Shaped like sendPasswordReset: both bodies, one send.
func (s *Server) sendAwaitingApproval(r *http.Request, user *store.User) error {
	html, err := s.renderMail("awaiting_approval.html", nil)
	if err != nil {
		return err
	}
	text, err := s.renderMail("awaiting_approval.txt", nil)
	if err != nil {
		return err
	}

	return s.mailer.Send(r.Context(), postmark.Message{
		From:     s.cfg.MailFrom,
		To:       user.Email,
		Subject:  "Your account waits for approval",
		TextBody: text,
		HTMLBody: html,
	})
}
