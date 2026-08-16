package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"
)

// The cookies this app sets. Rails signed them with the application's secret
// key base and named them session_id, _tinystart_session and so on; these are
// new names with a new signature, so everyone signs in once after the cutover
// and nothing else about the change is visible.
const (
	// sessionCookie carries the id of a row in the sessions table. The row is
	// what makes signing out on one machine take effect on another, so the
	// cookie holds a reference and never a claim.
	sessionCookie = "tinystart_session"

	// flashCookie is Rails' flash: a message set just before a redirect and
	// read exactly once, by the page the redirect lands on.
	flashCookie = "tinystart_flash"

	// returnToCookie remembers where someone was headed when they were sent to
	// the sign-in page, so that signing in finishes the trip. Rails kept this
	// in the session; here it is its own cookie, because there is no
	// server-side session store and a cookie that dies on use is exactly the
	// lifetime it wants.
	returnToCookie = "tinystart_return_to"

	// connectionGrantCookie holds the device authorization that Settings →
	// Connections has opened and is waiting on. Rails kept it in the session;
	// it is a cookie here for the same reason as the one above. The deadline
	// the other app gave is inside the value rather than on the cookie, so
	// that a browser with a slow clock cannot keep a dead grant alive.
	connectionGrantCookie = "tinystart_connection_grant"
)

// noExpiry is the zero time, which http.SetCookie reads as "no Expires
// attribute": a cookie that lasts as long as the browser window. Named
// because `time.Time{}` at a call site says nothing about why.
var noExpiry time.Time

// errBadCookie is what every read returns for a cookie that is absent,
// truncated, tampered with or signed by a different key. Telling those apart
// would only help someone probing the signature.
var errBadCookie = errors.New("web: cookie missing or not valid")

// signValue attaches a signature to a value:
// "<base64url value>.<base64url mac>".
//
// Both halves are encoded, and the value's half is not decoration. A cookie
// value may only contain a narrow range of bytes, and net/http drops the ones
// it may not carry rather than refusing to write the cookie — so a flash
// saying `the link "Bare" was rejected` or anything with an em dash in it
// would come back a different string from the one that was signed, fail to
// verify, and vanish. Encoding first means every value is carried intact.
//
// The name is mixed into the MAC. Without that, the signature says only "this
// app wrote this string", and a value lifted out of one cookie and dropped
// into another would verify — a flash message pasted in as a session id, say.
// With it, a signature is only valid for the cookie it was made for.
func (s *Server) signValue(name, value string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(value)) + "." +
		base64.RawURLEncoding.EncodeToString(s.mac(name, value))
}

// verifyValue undoes signValue.
//
// Cutting at the first dot is safe because neither half can contain one:
// base64url's alphabet has no dot in it. That is the other thing the encoding
// buys — before it, a value ending in a full stop had to be told apart from
// its own signature.
//
// The comparison is constant-time so that a wrong signature takes the same
// time to reject however much of it was right; a byte-at-a-time comparison
// leaks the correct MAC one byte per few thousand requests.
func (s *Server) verifyValue(name, signed string) (string, error) {
	encoded, signature, found := strings.Cut(signed, ".")
	if !found {
		return "", errBadCookie
	}

	value, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", errBadCookie
	}
	given, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil {
		return "", errBadCookie
	}
	if !hmac.Equal(given, s.mac(name, string(value))) {
		return "", errBadCookie
	}
	return string(value), nil
}

func (s *Server) mac(name, value string) []byte {
	h := hmac.New(sha256.New, s.cfg.SecretKey)
	// The name is length-prefixed by the colon only because no cookie name
	// contains one; without a separator, ("ab", "c") and ("a", "bc") would
	// hash the same.
	h.Write([]byte(name + ":" + value))
	return h.Sum(nil)
}

// setSignedCookie writes a signed cookie. expires is the zero time for a
// session cookie — one that lives until the browser is closed — which is what
// the flash and the return-to path want.
//
// The attributes are the same on every cookie here: HttpOnly, because no
// script has any business reading them; SameSite=Lax, so a link from another
// site still arrives signed in but a cross-site form post does not; Secure in
// production, where everything is HTTPS, and off in development, where it is
// not and a Secure cookie would simply never be stored.
func (s *Server) setSignedCookie(w http.ResponseWriter, name, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    s.signValue(name, value),
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// readSignedCookie returns the value of a signed cookie, or errBadCookie.
func (s *Server) readSignedCookie(r *http.Request, name string) (string, error) {
	cookie, err := r.Cookie(name)
	if err != nil {
		return "", errBadCookie
	}
	return s.verifyValue(name, cookie.Value)
}

// deleteCookie tells the browser to drop one. MaxAge below zero is the
// instruction to delete; the attributes have to match the ones it was set with
// or the browser keeps the original alongside the expired one.
func (s *Server) deleteCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}
