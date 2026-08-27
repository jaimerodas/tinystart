package web

import (
	"bytes"
	"errors"
	"net/http"

	"github.com/jaimerodas/tinystart/internal/postmark"
	"github.com/jaimerodas/tinystart/internal/store"
)

// The two sentences the reset flow says. The first is deliberately the same
// whether the address exists or not.
const (
	resetSentNotice   = "Password reset instructions sent (if user with that email address exists)."
	resetDoneNotice   = "Password has been reset."
	resetMismatch     = "Passwords did not match."
	resetTokenInvalid = "Password reset link is invalid or has expired."
)

type passwordsNewData struct {
	Email string
}

type passwordsEditData struct {
	Token string
}

// handlePasswordNew is GET /passwords/new.
func (s *Server) handlePasswordNew() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.render(w, r, http.StatusOK, layoutSession, "passwords_new",
			passwordsNewData{Email: r.URL.Query().Get("email")})
	})
}

// handlePasswordCreate is POST /passwords.
//
// The answer never depends on whether the address is one we know: same notice,
// same redirect, same time to a rough approximation. Anything else turns this
// form into a way of asking who has an account here.
//
// This handler logs a failure to send and swallows it, which is a deliberate
// difference from Rails. There, deliver_now raises on a failure and shows a
// 500, but a 500 here answers the question the notice is carefully not
// answering.
func (s *Server) handlePasswordCreate() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := s.db.UserByEmail(r.Context(), r.PostFormValue("email"))
		if err == nil {
			if err := s.sendPasswordReset(r, user); err != nil {
				s.log.ErrorContext(r.Context(), "sending password reset", "error", err, "user_id", user.ID)
			}
		}
		s.redirect(w, r, "/sign_in", flashNotice, resetSentNotice)
	})
}

// handlePasswordEdit is GET /passwords/{token}/edit: the form that sets the
// new password.
func (s *Server) handlePasswordEdit() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.PathValue("token")
		if _, err := s.userFromPasswordResetToken(r.Context(), token); err != nil {
			s.redirect(w, r, "/passwords/new", flashAlert, resetTokenInvalid)
			return
		}
		s.render(w, r, http.StatusOK, layoutSession, "passwords_edit", passwordsEditData{Token: token})
	})
}

// handlePasswordUpdate is PATCH /passwords/{token}.
//
// Rails wrote this as one `@user.update(password:, password_confirmation:)`,
// so a mismatch and a password ActiveRecord refused outright both came back as
// "Passwords did not match." That is a slightly misleading message for an
// empty password, and it is the message the deployed app shows, so it is the
// message here.
func (s *Server) handlePasswordUpdate() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.PathValue("token")
		user, err := s.userFromPasswordResetToken(r.Context(), token)
		if err != nil {
			s.redirect(w, r, "/passwords/new", flashAlert, resetTokenInvalid)
			return
		}

		password := r.PostFormValue("password")
		confirmation := r.PostFormValue("password_confirmation")
		if password != confirmation {
			s.redirect(w, r, "/passwords/"+token+"/edit", flashAlert, resetMismatch)
			return
		}

		if err := s.db.ResetPassword(r.Context(), user.ID, password); err != nil {
			var invalid store.ValidationError
			if !errors.As(err, &invalid) {
				s.serverError(w, r, err)
				return
			}
			s.redirect(w, r, "/passwords/"+token+"/edit", flashAlert, resetMismatch)
			return
		}

		s.redirect(w, r, "/sign_in", flashNotice, resetDoneNotice)
	})
}

// sendPasswordReset renders both bodies of the mail and hands it to the
// mailer. This sends both, as ActionMailer did: the text part is what a
// client that will not render HTML shows, and a mail with only an HTML part
// scores badly with every spam filter there is.
func (s *Server) sendPasswordReset(r *http.Request, user *store.User) error {
	url := s.baseURL(r) + "/passwords/" + s.passwordResetToken(user) + "/edit"

	data := struct{ URL string }{url}
	html, err := s.renderMail("password_reset.html", data)
	if err != nil {
		return err
	}
	text, err := s.renderMail("password_reset.txt", data)
	if err != nil {
		return err
	}

	return s.mailer.Send(r.Context(), postmark.Message{
		From:     s.cfg.MailFrom,
		To:       user.Email,
		Subject:  "Reset your password",
		TextBody: text,
		HTMLBody: html,
	})
}

// renderMail executes a mail template with the given data, which is whatever
// shape that template expects.
func (s *Server) renderMail(name string, data any) (string, error) {
	var out bytes.Buffer
	if err := s.templates.mail.ExecuteTemplate(&out, name, data); err != nil {
		return "", err
	}
	return out.String(), nil
}

// baseURL is the scheme and host to build absolute links with — which mail
// needs and nothing else does, because every link inside the app is
// relative.
//
// The configured host wins, because a request's Host header is whatever the
// client wrote in it. A forged one puts a forged link in somebody's password
// reset mail. Falling back to the request is what makes `go run` work on
// localhost without any configuration.
func (s *Server) baseURL(r *http.Request) string {
	if s.cfg.Host != "" {
		return s.cfg.Host
	}
	scheme := "http"
	if s.cfg.SecureCookies {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
