package web

import (
	"errors"
	"net/http"

	"github.com/jaimerodas/tinystart/internal/store"
)

// usersNewData is the sign-up form: the address as typed, the errors to list
// above it, and which fields to outline.
type usersNewData struct {
	Email           string
	Errors          []string
	EmailInvalid    bool
	PasswordInvalid bool
}

// handleUserNew is GET /sign_up.
func (s *Server) handleUserNew() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.signUpClosed(w, r) {
			return
		}
		s.render(w, r, http.StatusOK, layoutSession, "users_new", usersNewData{})
	})
}

// handleUserCreate is POST /sign_up. It makes the first user, the admin, and
// signs them in.
func (s *Server) handleUserCreate() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.signUpClosed(w, r) {
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
		var invalid store.ValidationError
		switch {
		case errors.As(err, &invalid):
			s.render(w, r, http.StatusUnprocessableEntity, layoutSession, "users_new", usersNewData{
				Email:           email,
				Errors:          invalid.FullMessages(),
				EmailInvalid:    invalid.On("email"),
				PasswordInvalid: invalid.On("password"),
			})
			return
		case errors.Is(err, store.ErrConflict):
			// Somebody else signed up first, after the check above.
			s.notFound(w)
			return
		case err != nil:
			s.serverError(w, r, err)
			return
		}

		if err := s.startNewSessionFor(w, r, user); err != nil {
			s.serverError(w, r, err)
			return
		}
		s.redirect(w, r, "/", flashNotice, "User was successfully created.")
	})
}

// signUpClosed answers 404 once the install has a user, and reports whether
// it answered. Everyone after the admin gets in by invitation, so the page is
// then a URL the app does not have.
func (s *Server) signUpClosed(w http.ResponseWriter, r *http.Request) bool {
	any, err := s.db.AnyUsers(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return true
	}
	if any {
		s.notFound(w)
	}
	return any
}
