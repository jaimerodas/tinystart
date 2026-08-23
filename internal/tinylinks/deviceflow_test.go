package tinylinks

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStartSendsTheGrantRequest(t *testing.T) {
	server, got := fake(t, http.StatusOK, `{"device_code":"abc"}`)
	flow := NewDeviceFlow(server.URL, "start.pati.to", server.Client())

	if _, err := flow.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	request := got.request(t)
	if request.method != http.MethodPost {
		t.Errorf("method = %s, want POST", request.method)
	}
	if request.path != "/api/v1/device_authorizations" {
		t.Errorf("path = %s", request.path)
	}
	if contentType := request.header.Get("Content-Type"); contentType != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", contentType)
	}
	if accept := request.header.Get("Accept"); accept != "application/json" {
		t.Errorf("Accept = %q", accept)
	}
	if scopes := request.form.Get("scopes"); scopes != RequestedScopes {
		t.Errorf("scopes = %q, want %q", scopes, RequestedScopes)
	}
	// The grant is anonymous: the whole point of the flow is that this app
	// has no token yet.
	if auth := request.header.Get("Authorization"); auth != "" {
		t.Errorf("Authorization = %q, want none", auth)
	}
}

// One person can have two tinystarts pointed at the same app — a laptop and
// the real one. The other app lists its tokens by this name. Without the
// host both read "tinystart" and revoking the right one is guesswork.
func TestStartNamesTheHostItIsAskingFrom(t *testing.T) {
	tests := []struct {
		name       string
		clientHost string
		want       string
	}{
		{"the deployed app", "start.pati.to", "tinystart (start.pati.to)"},
		{"a dev instance names its port", "localhost:3000", "tinystart (localhost:3000)"},
		{"the bare name when no host is known", "", "tinystart"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, got := fake(t, http.StatusOK, `{"device_code":"abc"}`)
			flow := NewDeviceFlow(server.URL, test.clientHost, server.Client())

			if _, err := flow.Start(t.Context()); err != nil {
				t.Fatalf("Start: %v", err)
			}
			if name := got.request(t).form.Get("client_name"); name != test.want {
				t.Errorf("client_name = %q, want %q", name, test.want)
			}
		})
	}
}

func TestStartReturnsTheGrantDetails(t *testing.T) {
	server, _ := fake(t, http.StatusOK, `{
		"device_code": "abc",
		"verification_url": "https://links.example.com/device/new?code=abc",
		"expires_in": 600,
		"interval": 5
	}`)
	flow := NewDeviceFlow(server.URL, "start.pati.to", server.Client())

	grant, err := flow.Start(t.Context())
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	want := Grant{
		DeviceCode:      "abc",
		VerificationURL: "https://links.example.com/device/new?code=abc",
		ExpiresIn:       600,
		Interval:        5,
	}
	if *grant != want {
		t.Errorf("grant = %+v, want %+v", *grant, want)
	}
}

func TestStartFailures(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		message string
	}{
		// RFC 8628 refusals come back as 400s, so the status is not what
		// decides anything here — the "error" field in the body is.
		{"the app refuses the scopes", http.StatusBadRequest, `{"error":"invalid_scope"}`, "invalid_scope"},
		{"the body is not JSON", http.StatusOK, "<html>nope</html>", "isn't JSON"},
		{"a server error is not JSON either", http.StatusInternalServerError, "boom", "isn't JSON"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, _ := fake(t, test.status, test.body)
			flow := NewDeviceFlow(server.URL, "start.pati.to", server.Client())

			grant, err := flow.Start(t.Context())
			if grant != nil {
				t.Errorf("grant = %+v, want none", grant)
			}
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want it to contain %q", err, test.message)
			}
		})
	}
}

func TestStartWithAnUnreachableApp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := server.URL
	server.Close()

	flow := NewDeviceFlow(address, "start.pati.to", &http.Client{Timeout: time.Second})

	grant, err := flow.Start(t.Context())
	if grant != nil {
		t.Errorf("grant = %+v, want none", grant)
	}
	if err == nil || !strings.Contains(err.Error(), "could not reach") {
		t.Fatalf("error = %v, want an unreachable app", err)
	}
}

func TestCheckSendsTheDeviceCode(t *testing.T) {
	server, got := fake(t, http.StatusOK, `{"error":"authorization_pending"}`)
	flow := NewDeviceFlow(server.URL, "", server.Client())

	if _, _, err := flow.Check(t.Context(), "abc"); err != nil {
		t.Fatalf("Check: %v", err)
	}

	request := got.request(t)
	if request.method != http.MethodPost {
		t.Errorf("method = %s, want POST", request.method)
	}
	if request.path != "/api/v1/device_authorizations/token" {
		t.Errorf("path = %s", request.path)
	}
	if code := request.form.Get("device_code"); code != "abc" {
		t.Errorf("device_code = %q", code)
	}
	// check does not name the client. Only the grant does.
	if name := request.form.Get("client_name"); name != "" {
		t.Errorf("client_name = %q, want none", name)
	}
}

func TestCheckStatuses(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   Status
	}{
		{"approved", http.StatusOK,
			`{"token":"t","scopes":["search","visit"],"expires_at":"2026-11-05T00:00:00Z"}`, StatusApproved},
		// RFC 8628 has the token endpoint answer 400 while it waits. So this
		// code ignores the status, and the "error" field decides. Ruby did
		// the same: it never looked at the code at all.
		{"pending", http.StatusBadRequest, `{"error":"authorization_pending"}`, StatusPending},
		{"denied", http.StatusBadRequest, `{"error":"access_denied"}`, StatusDenied},
		{"expired", http.StatusBadRequest, `{"error":"expired_token"}`, StatusExpired},
		// This package treats anything the other app invents as the end of
		// the grant. A status this app cannot act on is over, whatever it is
		// called.
		{"an unknown error is the end of it", http.StatusBadRequest, `{"error":"slow_down"}`, StatusExpired},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, _ := fake(t, test.status, test.body)
			flow := NewDeviceFlow(server.URL, "", server.Client())

			status, token, err := flow.Check(t.Context(), "abc")
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if status != test.want {
				t.Errorf("status = %q, want %q", status, test.want)
			}
			if (token != nil) != (test.want == StatusApproved) {
				t.Errorf("token = %+v for status %q", token, status)
			}
		})
	}
}

func TestCheckHandsBackTheApprovedToken(t *testing.T) {
	server, _ := fake(t, http.StatusOK,
		`{"token":"t","scopes":["search","visit"],"expires_at":"2026-11-05T00:00:00Z"}`)
	flow := NewDeviceFlow(server.URL, "", server.Client())

	status, token, err := flow.Check(t.Context(), "abc")
	if err != nil || status != StatusApproved {
		t.Fatalf("Check: %q, %v", status, err)
	}
	if token.Token != "t" {
		t.Errorf("token = %q", token.Token)
	}
	// store.ReplaceConnection takes the scopes the way the other app stores
	// them, which is how Rails wrote them: one comma-joined string.
	if list := token.Scopes.String(); list != "search,visit" {
		t.Errorf("scopes = %q, want %q", list, "search,visit")
	}
	if want := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC); !token.ExpiresAt.Equal(want) {
		t.Errorf("expires_at = %v, want %v", token.ExpiresAt, want)
	}
}

// Rails' Array(token["scopes"]) took either shape, so this does too: a token
// that arrives with a string instead of a list is still a token.
func TestCheckAcceptsScopesAsAStringOrAList(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"a list", `{"token":"t","scopes":["search","visit"]}`, "search,visit"},
		{"a comma-joined string", `{"token":"t","scopes":"search,visit"}`, "search,visit"},
		{"nothing at all", `{"token":"t"}`, ""},
		{"null", `{"token":"t","scopes":null}`, ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, _ := fake(t, http.StatusOK, test.body)
			flow := NewDeviceFlow(server.URL, "", server.Client())

			_, token, err := flow.Check(t.Context(), "abc")
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if list := token.Scopes.String(); list != test.want {
				t.Errorf("scopes = %q, want %q", list, test.want)
			}
		})
	}
}

// A token with no expiry is a token that does not expire, not a broken reply:
// the column it lands in is nullable.
func TestCheckAcceptsATokenWithoutAnExpiry(t *testing.T) {
	server, _ := fake(t, http.StatusOK, `{"token":"t","expires_at":null}`)
	flow := NewDeviceFlow(server.URL, "", server.Client())

	status, token, err := flow.Check(t.Context(), "abc")
	if err != nil || status != StatusApproved {
		t.Fatalf("Check: %q, %v", status, err)
	}
	if !token.ExpiresAt.IsZero() {
		t.Errorf("expires_at = %v, want the zero time", token.ExpiresAt)
	}
}

// A blip mid-flow is not a denial — the page keeps waiting until the grant
// runs out on its own.
func TestCheckReportsUnreachableSeparatelyFromDenial(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := server.URL
	server.Close()

	flow := NewDeviceFlow(address, "", &http.Client{Timeout: time.Second})

	status, token, err := flow.Check(t.Context(), "abc")
	if status != StatusUnreachable {
		t.Errorf("status = %q, want %q", status, StatusUnreachable)
	}
	if token != nil {
		t.Errorf("token = %+v, want none", token)
	}
	if err == nil {
		t.Error("Check: want the error that made it unreachable")
	}
}

func TestCheckTreatsABodyThatIsNotJSONAsUnreachable(t *testing.T) {
	server, _ := fake(t, http.StatusOK, "<html>nope</html>")
	flow := NewDeviceFlow(server.URL, "", server.Client())

	status, _, err := flow.Check(t.Context(), "abc")
	if status != StatusUnreachable {
		t.Errorf("status = %q, want %q", status, StatusUnreachable)
	}
	if err == nil || !strings.Contains(err.Error(), "isn't JSON") {
		t.Errorf("error = %v", err)
	}
}

// The poll endpoint renders these straight into {"status": "…"} for the
// Stimulus poller, which switches on the strings.
func TestStatusStrings(t *testing.T) {
	want := map[Status]string{
		StatusApproved:    "approved",
		StatusPending:     "pending",
		StatusDenied:      "denied",
		StatusExpired:     "expired",
		StatusUnreachable: "unreachable",
	}
	for status, text := range want {
		if string(status) != text {
			t.Errorf("status = %q, want %q", status, text)
		}
	}
}
