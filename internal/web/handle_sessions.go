package web

import (
	"errors"
	"net/http"

	"github.com/jaimerodas/tinystart/internal/store"
)

// sessionsNewData is what the sign-in form needs: the address to put back in
// the field, so that ?email=… prefills it.
type sessionsNewData struct {
	Email string
}

// handleSessionNew is GET /session/new.
func (s *Server) handleSessionNew() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.sendEmptyInstallToSignUp(w, r) {
			return
		}
		s.render(w, r, http.StatusOK, layoutSession, "sessions_new",
			sessionsNewData{Email: r.URL.Query().Get("email")})
	})
}

// handleSessionCreate is POST /session.
//
// The approved check is here and not in store.Authenticate on purpose: a
// correct password for an account that is waiting for an admin still fails,
// and it fails with the same message a wrong password gets. Telling the two
// apart confirms the address exists.
func (s *Server) handleSessionCreate() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.sendEmptyInstallToSignUp(w, r) {
			return
		}

		user, err := s.db.Authenticate(r.Context(), r.PostFormValue("email"), r.PostFormValue("password"))
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			s.serverError(w, r, err)
			return
		}
		if user == nil || !user.Approved {
			s.redirect(w, r, "/session/new", flashAlert, "Try another email address or password.")
			return
		}

		if err := s.startNewSessionFor(w, r, user); err != nil {
			s.serverError(w, r, err)
			return
		}
		s.redirect(w, r, s.afterAuthenticationURL(w, r), "", "")
	})
}

// handleSessionDestroy is DELETE /session, reached through the hidden _method
// field on the header's log-out button.
func (s *Server) handleSessionDestroy() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.terminateSession(w, r); err != nil {
			s.serverError(w, r, err)
			return
		}
		s.redirect(w, r, "/session/new", "", "")
	})
}

// sendEmptyInstallToSignUp is SessionsController#send_empty_install_to_signup.
// A brand new installation has nobody to sign in as, so the form is a dead
// end. The first sign-up bootstraps itself as an approved admin. It reports
// whether it answered the request.
func (s *Server) sendEmptyInstallToSignUp(w http.ResponseWriter, r *http.Request) bool {
	any, err := s.db.AnyUsers(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return true
	}
	if any {
		return false
	}
	http.Redirect(w, r, "/sign_up", http.StatusSeeOther)
	return true
}
