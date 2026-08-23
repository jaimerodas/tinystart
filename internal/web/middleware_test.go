package web

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// okHandler is the handler the middleware tests wrap: it reports what it was
// handed rather than doing anything.
func okHandler(record func(*http.Request)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if record != nil {
			record(r)
		}
		w.Write([]byte("ok")) //nolint:errcheck // a recorder never fails
	})
}

func TestMethodOverrideRewritesAPostCarryingMethod(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		formMethod string
		want       string
	}{
		{"delete", http.MethodPost, "delete", http.MethodDelete},
		{"patch", http.MethodPost, "patch", http.MethodPatch},
		{"put, uppercase", http.MethodPost, "PUT", http.MethodPut},
		// Anything else is left alone. A form asking to be a GET turns a
		// state-changing request into one the cross-origin check waves through.
		{"get is not a method a form may ask for", http.MethodPost, "get", http.MethodPost},
		{"nonsense is ignored", http.MethodPost, "nonsense", http.MethodPost},
		// Only a POST can be overridden: otherwise a plain link can be made
		// to delete something.
		{"a GET is never rewritten", http.MethodGet, "delete", http.MethodGet},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			handler := methodOverride(okHandler(func(r *http.Request) { got = r.Method }))

			form := url.Values{"_method": {tt.formMethod}}
			req := httptest.NewRequest(tt.method, "/session?"+form.Encode(), strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			handler.ServeHTTP(httptest.NewRecorder(), req)

			if got != tt.want {
				t.Errorf("method = %s, want %s", got, tt.want)
			}
		})
	}
}

// The body the override consumed has to still be there for the handler.
// Otherwise every form that uses it arrives empty.
func TestMethodOverrideLeavesTheRestOfTheFormReadable(t *testing.T) {
	var got string
	handler := methodOverride(okHandler(func(r *http.Request) { got = r.PostFormValue("password") }))

	form := url.Values{"_method": {"put"}, "password": {"hunter2"}}
	req := httptest.NewRequest(http.MethodPost, "/passwords/x", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if got != "hunter2" {
		t.Errorf("password = %q, want %q", got, "hunter2")
	}
}

// A multipart upload is not a form the override reads: parsing it here
// buffers a file to find a field that is never in one.
func TestMethodOverrideIgnoresNonFormBodies(t *testing.T) {
	var got string
	handler := methodOverride(okHandler(func(r *http.Request) { got = r.Method }))

	req := httptest.NewRequest(http.MethodPost, "/settings/import_export", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if got != http.MethodPost {
		t.Errorf("method = %s, want POST", got)
	}
}

func TestRequestIDIsPresentAndDifferentEachTime(t *testing.T) {
	var seen []string
	handler := requestID(okHandler(func(r *http.Request) {
		seen = append(seen, requestIDFrom(r.Context()))
	}))

	for range 2 {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}

	if len(seen[0]) != 16 {
		t.Errorf("request id = %q, want 16 hex digits", seen[0])
	}
	if seen[0] == seen[1] {
		t.Errorf("two requests shared the id %q", seen[0])
	}
}

func TestStrictTransportSecurity(t *testing.T) {
	on := strictTransportSecurity(true)(okHandler(nil))
	rec := httptest.NewRecorder()
	on.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec.Header().Get("Strict-Transport-Security"); got != hstsHeader {
		t.Errorf("header = %q, want %q", got, hstsHeader)
	}

	// Off in development: a browser told that localhost is HTTPS-only stays
	// told, for two years, for every other project on the machine.
	off := strictTransportSecurity(false)(okHandler(nil))
	rec = httptest.NewRecorder()
	off.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("header = %q with HSTS off, want none", got)
	}
}

func TestRecoverPanicsAnswersWithTheStaticFiveHundred(t *testing.T) {
	s := newBareServer(t)
	handler := s.recoverPanics(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("the template caught fire")
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(rec.Body.String(), "We're sorry, but something went wrong") {
		t.Errorf("body = %q, want public/500.html", rec.Body.String())
	}
}

// The cross-origin check is what replaces the authenticity token: a form on
// somebody else's page is refused, and a safe method is not.
func TestCrossSitePostsAreRejected(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")

	req := ts.request(http.MethodPost, "/session", url.Values{
		"email":    {"one@example.com"},
		"password": {testPassword},
	})
	req.Header.Set("Sec-Fetch-Site", "cross-site")

	ts.do(req).assertStatus(http.StatusForbidden)

	if ts.sessionCookie() != "" {
		t.Error("a cross-site sign-in was allowed")
	}
}

func TestSameOriginPostsAreAllowed(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")

	req := ts.request(http.MethodPost, "/session", url.Values{
		"email":    {"one@example.com"},
		"password": {testPassword},
	})
	req.Header.Set("Sec-Fetch-Site", "same-origin")

	ts.do(req).assertRedirect("/")
}

func TestCrossSiteGetsAreAllowed(t *testing.T) {
	ts := newTestServer(t)
	ts.createUser("one@example.com")

	req := ts.request(http.MethodGet, "/session/new", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")

	ts.do(req).assertStatus(http.StatusOK)
}

// newBareServer is a Server with no HTTP server around it, for the middleware
// that can be tested with a recorder alone.
func newBareServer(t *testing.T) *Server {
	t.Helper()
	s, err := newServer(Config{SecretKey: []byte(strings.Repeat("k", 32))}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil)
	if err != nil {
		t.Fatalf("building the server: %v", err)
	}
	return s
}
