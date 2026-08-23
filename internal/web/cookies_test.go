package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSignedValuesRoundTrip(t *testing.T) {
	s := newBareServer(t)

	// The last two are the reason the value is encoded before it goes into
	// the cookie. net/http silently drops a byte a cookie cannot carry, so
	// either of them comes back changed and fails the check.
	for _, value := range []string{
		"", "42", "alert:Try another email address or password.", "/start/edit?x=1",
		`alert:Nothing was imported: the link "Bare" (example.com) was rejected`,
		"notice:Imported 6 links — expected if you edited the file",
	} {
		signed := s.signValue(flashCookie, value)
		got, err := s.verifyValue(flashCookie, signed)
		if err != nil {
			t.Errorf("verifying %q: %v", value, err)
			continue
		}
		if got != value {
			t.Errorf("round trip of %q gave %q", value, got)
		}
	}
}

func TestATamperedValueIsRefused(t *testing.T) {
	s := newBareServer(t)
	signed := s.signValue(sessionCookie, "42")

	tampered := []struct {
		name  string
		value string
	}{
		{"a changed payload", "43" + signed[1:]},
		{"a changed signature", signed[:len(signed)-1] + "X"},
		{"no signature at all", "42"},
		{"an empty string", ""},
		{"a signature that is not base64", "42.not base64"},
	}

	for _, tt := range tampered {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.verifyValue(sessionCookie, tt.value); err == nil {
				t.Errorf("verifyValue(%q) returned no error", tt.value)
			}
		})
	}
}

// The cookie's name is mixed into the signature, so a value lifted out of one
// cookie and dropped into another fails the check. A flash message presented
// as a session id, say.
func TestASignatureIsOnlyValidForItsOwnCookie(t *testing.T) {
	s := newBareServer(t)
	signed := s.signValue(flashCookie, "42")

	if _, err := s.verifyValue(sessionCookie, signed); err == nil {
		t.Error("a flash signature verified as a session cookie")
	}
}

// A different key is a different app, which is what makes "everyone signs in
// once after the cutover" true.
func TestASignatureFromAnotherKeyIsRefused(t *testing.T) {
	mine := newBareServer(t)
	theirs, err := newServer(Config{SecretKey: []byte(strings.Repeat("x", 32))}, nil, mine.log, nil, nil)
	if err != nil {
		t.Fatalf("building the second server: %v", err)
	}

	if _, err := mine.verifyValue(sessionCookie, theirs.signValue(sessionCookie, "42")); err == nil {
		t.Error("a cookie signed with another key verified")
	}
}

func TestCookieAttributes(t *testing.T) {
	s := newBareServer(t)
	rec := httptest.NewRecorder()
	s.setSignedCookie(rec, sessionCookie, "42", noExpiry)

	cookie := rec.Result().Cookies()[0]
	if !cookie.HttpOnly {
		t.Error("HttpOnly is off; no script has any business reading these")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Errorf("Path = %q, want /", cookie.Path)
	}
	if cookie.Secure {
		t.Error("Secure is on with SecureCookies false; the cookie would never be stored over http")
	}
}

func TestSecureCookiesInProduction(t *testing.T) {
	s, err := newServer(Config{SecretKey: []byte(strings.Repeat("k", 32)), SecureCookies: true},
		nil, newBareServer(t).log, nil, nil)
	if err != nil {
		t.Fatalf("building the server: %v", err)
	}

	rec := httptest.NewRecorder()
	s.setSignedCookie(rec, sessionCookie, "42", noExpiry)

	if !rec.Result().Cookies()[0].Secure {
		t.Error("Secure is off in production")
	}
}

// The flash is one shot: the page that shows it clears it, so a reload shows
// nothing.
func TestTheFlashIsShownOnceAndThenGone(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")

	ts.post("/session", form("email", "one@example.com", "password", "wrong"))

	ts.get("/session/new").assertContains("Try another email address or password.")
	ts.get("/session/new").assertNotContains("Try another email address or password.")
}

// A notice carries the tick, an alert does not. The class on the card carries
// the difference in color, and the icon carries it in a screenshot without
// color.
func TestNoticeAndAlertRenderDifferently(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")

	ts.post("/passwords", form("email", "one@example.com"))
	ts.get("/session/new").
		assertContains(`<div class="flash-card notice" role="status" aria-live="polite">`).
		assertContains(`<svg aria-hidden="true" focusable="false"`)

	ts.post("/session", form("email", "one@example.com", "password", "wrong"))
	ts.get("/session/new").
		assertContains(`<div class="flash-card alert" role="status" aria-live="polite">`).
		assertNotContains(`<svg aria-hidden="true" focusable="false"`)
}

// The same thing end to end. The round trip above makes sure that the
// signature is correct. This test makes sure that the browser gets to keep
// the value. The flash after a refused import is the longest, punctuated,
// non-ASCII message the app produces. It disappeared entirely when the value
// went into the cookie raw.
func TestAFlashWithQuotesAndAnEmDashSurvivesTheCookie(t *testing.T) {
	ts := newTestServer(t)
	user := ts.createUser("one@example.com")
	ts.signIn(user.Email)

	message := `Nothing was imported: the link "Bare" (example.com) — rejected`
	rec := httptest.NewRecorder()
	ts.app.setFlash(rec, flashNotice, message)

	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	for _, cookie := range rec.Result().Cookies() {
		req.AddCookie(cookie)
	}

	flash := ts.app.takeFlash(httptest.NewRecorder(), req)
	if len(flash) != 1 || flash[0].Message != message {
		t.Errorf("flash = %+v, want one message %q", flash, message)
	}
}
