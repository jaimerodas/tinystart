package web

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jaimerodas/tinystart/internal/store"
)

// contextKey is this package's own type for context keys. An unexported type
// means no other package can collide with these keys even by using the same
// string, which is the whole reason context keys are not strings.
type contextKey int

const (
	userKey contextKey = iota
	sessionKey
)

// userFrom returns the signed-in user, or nil. Handlers behind
// requireAuthentication can rely on it being there. The ones in front of it —
// sign in, sign up, password reset — read the value themselves.
func userFrom(ctx context.Context) *store.User {
	user, _ := ctx.Value(userKey).(*store.User)
	return user
}

// sessionFrom returns the current session row, or nil. Only signing out and
// the session refresh need it.
func sessionFrom(ctx context.Context) *store.Session {
	session, _ := ctx.Value(sessionKey).(*store.Session)
	return session
}

// resumeSession is Authentication#resume_session, run on every request rather
// than only on the protected ones. Three of the handlers that allow
// unauthenticated access still ask who is signed in. Doing the lookup in one
// place means none of them can forget to.
//
// A cookie that does not pass verification, names a session that has expired,
// or names a user who has since been deleted is simply not signed in. There is
// nothing to tell the visitor in any of those cases, and the one useful side
// effect — dropping the dead cookie — happens the next time they sign in.
func (s *Server) resumeSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, user := s.currentSession(r)
		if session == nil {
			next.ServeHTTP(w, r)
			return
		}

		s.refreshSessionIfNeeded(w, r, session)

		ctx := context.WithValue(r.Context(), sessionKey, session)
		ctx = context.WithValue(ctx, userKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) currentSession(r *http.Request) (*store.Session, *store.User) {
	value, err := s.readSignedCookie(r, sessionCookie)
	if err != nil {
		return nil, nil
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil, nil
	}
	session, err := s.db.ActiveSession(r.Context(), id)
	if err != nil {
		return nil, nil
	}
	user, err := s.db.UserByID(r.Context(), session.UserID)
	if err != nil {
		return nil, nil
	}
	return session, user
}

// requireAuthentication is the before_action that every page but the four
// authentication screens runs. It writes down where an anonymous visitor was
// going and sends them to sign in. after_authenticationURL finishes the trip.
func (s *Server) requireAuthentication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if userFrom(r.Context()) == nil {
			s.setSignedCookie(w, returnToCookie, r.URL.RequestURI(), noExpiry)
			http.Redirect(w, r, "/session/new", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// adminOnly is Authentication#admin_only: not an error page, just the start
// page. Someone who is signed in and not an admin has no business being told
// that the admin section exists.
func (s *Server) adminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := userFrom(r.Context())
		if user == nil || !user.Admin {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// startNewSessionFor signs someone in. It clears out their dead sessions
// first, which is why there is no cron job pruning the table. Then it writes
// a row and sets the cookie to expire with it.
func (s *Server) startNewSessionFor(w http.ResponseWriter, r *http.Request, user *store.User) error {
	ctx := r.Context()
	if err := s.db.DeleteExpiredSessions(ctx, user.ID); err != nil {
		return err
	}

	session, err := s.db.CreateSession(ctx, user.ID, r.UserAgent(), remoteIP(r), s.now().Add(store.SessionLifetime))
	if err != nil {
		return err
	}

	s.setSignedCookie(w, sessionCookie, strconv.FormatInt(session.ID, 10), session.ExpiresAt)
	return nil
}

// terminateSession is signing out. The row goes and the cookie goes. If the
// row is already gone — signed out in another tab — that is the outcome asked
// for, so ErrNotFound is not an error here.
func (s *Server) terminateSession(w http.ResponseWriter, r *http.Request) error {
	if session := sessionFrom(r.Context()); session != nil {
		if err := s.db.DeleteSession(r.Context(), session.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
	}
	s.deleteCookie(w, sessionCookie)
	return nil
}

// sessionRefreshWindow is how close to expiry a session has to be before a
// request extends it. Someone who visits every week is never signed out.
// Someone who disappears for a month is.
const sessionRefreshWindow = 7 * 24 * time.Hour

// refreshSessionIfNeeded is Authentication#refresh_session_if_needed, ported
// as it stands.
//
// Note that nothing in the Rails app ever called this method. It was written
// and left unwired, so today a session really does end thirty days after
// sign-in. Here it runs on every request, which is what the method was
// plainly for. This logs a failure to extend and otherwise ignores it. The
// session is still valid for days, and taking the page down over it is a
// worse answer.
func (s *Server) refreshSessionIfNeeded(w http.ResponseWriter, r *http.Request, session *store.Session) {
	if session.ExpiresAt.After(s.now().Add(sessionRefreshWindow)) {
		return
	}

	expiresAt := s.now().Add(store.SessionLifetime)
	if err := s.db.ExtendSession(r.Context(), session.ID, expiresAt); err != nil {
		s.log.ErrorContext(r.Context(), "extending session", "error", err, "session_id", session.ID)
		return
	}
	session.ExpiresAt = expiresAt
	s.setSignedCookie(w, sessionCookie, strconv.FormatInt(session.ID, 10), expiresAt)
}

// afterAuthenticationURL is where signing in lands: back where the visitor was
// headed, or the start page. Reading the cookie also clears it, so a second
// sign-in does not repeat a month-old detour.
//
// Only a path is ever stored and only a path is ever returned. Rails kept the
// full URL, which works because it also refused to redirect off-host. Keeping
// the path makes an off-host redirect unrepresentable instead of forbidden.
func (s *Server) afterAuthenticationURL(w http.ResponseWriter, r *http.Request) string {
	value, err := s.readSignedCookie(r, returnToCookie)
	if err != nil {
		return "/"
	}
	s.deleteCookie(w, returnToCookie)
	if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return "/"
	}
	return value
}

// remoteIP is who the request came from, for the sessions table and the rate
// limiter.
//
// In production every request arrives through kamal-proxy, which appends the
// client to X-Forwarded-For. The first entry is the client, and the rest is
// the chain behind it. This code trusts the header, because nothing but the
// proxy can reach the container — the port is published to the proxy network
// alone. RemoteAddr, the alternative, always shows the proxy's address, not
// the visitor's, because every request passes through it.
func remoteIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		if first = strings.TrimSpace(first); first != "" {
			return first
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
