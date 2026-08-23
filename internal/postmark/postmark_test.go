package postmark

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// received is the request the fake Postmark saw.
type received struct {
	method string
	path   string
	header http.Header
	body   map[string]any
}

// fake answers like Postmark and remembers what it receives. The mutex is not
// decoration: the handler runs on the server's goroutine and the assertions on
// the test's, and `go test -race` is part of the gate.
func fake(t *testing.T, status int, body string) (*Client, func() received) {
	t.Helper()

	var (
		mu  sync.Mutex
		got received
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		decoded := map[string]any{}
		json.Unmarshal(raw, &decoded)

		mu.Lock()
		got = received{method: r.Method, path: r.URL.Path, header: r.Header.Clone(), body: decoded}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)

	client := &Client{Token: "a-token", BaseURL: server.URL, HTTP: server.Client()}
	return client, func() received {
		mu.Lock()
		defer mu.Unlock()
		return got
	}
}

func message() Message {
	return Message{
		From:     DefaultFrom,
		To:       "someone@example.com",
		Subject:  "Reset your password",
		TextBody: "You can reset your password within the next 15 minutes",
		HTMLBody: "<p>You can reset your password within the next 15 minutes</p>",
	}
}

func TestSendPostsTheMessage(t *testing.T) {
	client, got := fake(t, http.StatusOK, `{"ErrorCode":0,"Message":"OK","MessageID":"x"}`)

	if err := client.Send(t.Context(), message()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	request := got()
	if request.method != http.MethodPost {
		t.Errorf("method = %s, want POST", request.method)
	}
	if request.path != "/email" {
		t.Errorf("path = %s, want /email", request.path)
	}
	if token := request.header.Get("X-Postmark-Server-Token"); token != "a-token" {
		t.Errorf("X-Postmark-Server-Token = %q", token)
	}
	if contentType := request.header.Get("Content-Type"); contentType != "application/json" {
		t.Errorf("Content-Type = %q", contentType)
	}
	if accept := request.header.Get("Accept"); accept != "application/json" {
		t.Errorf("Accept = %q", accept)
	}

	// The field names are Postmark's, not Go's — HtmlBody in particular.
	want := map[string]any{
		"From":     "noreply@rodas.mx",
		"To":       "someone@example.com",
		"Subject":  "Reset your password",
		"TextBody": "You can reset your password within the next 15 minutes",
		"HtmlBody": "<p>You can reset your password within the next 15 minutes</p>",
	}
	for field, value := range want {
		if request.body[field] != value {
			t.Errorf("%s = %v, want %v", field, request.body[field], value)
		}
	}
	if len(request.body) != len(want) {
		t.Errorf("body has %d fields, want %d: %v", len(request.body), len(want), request.body)
	}
}

func TestSendReportsWhatPostmarkRefused(t *testing.T) {
	client, _ := fake(t, http.StatusUnprocessableEntity,
		`{"ErrorCode":300,"Message":"Invalid 'From' value."}`)

	err := client.Send(t.Context(), message())
	if err == nil {
		t.Fatal("Send: want an error")
	}

	var apiError *Error
	if !errors.As(err, &apiError) {
		t.Fatalf("error = %v (%T), want a *postmark.Error", err, err)
	}
	if apiError.ErrorCode != 300 {
		t.Errorf("ErrorCode = %d, want 300", apiError.ErrorCode)
	}
	if apiError.Message != "Invalid 'From' value." {
		t.Errorf("Message = %q", apiError.Message)
	}
	if apiError.Status != http.StatusUnprocessableEntity {
		t.Errorf("Status = %d", apiError.Status)
	}
	// The log line must say what to fix without anyone opening Postmark.
	if !strings.Contains(err.Error(), "300") || !strings.Contains(err.Error(), "Invalid 'From' value.") {
		t.Errorf("error = %q, want the code and the message in it", err)
	}
}

// Not every failure comes with Postmark's error body — a proxy in front of it
// answers however it likes. The status alone must still be enough to report.
func TestSendReportsAStatusWithoutAnErrorBody(t *testing.T) {
	client, _ := fake(t, http.StatusInternalServerError, "<html>boom</html>")

	err := client.Send(t.Context(), message())
	if err == nil {
		t.Fatal("Send: want an error")
	}

	var apiError *Error
	if !errors.As(err, &apiError) {
		t.Fatalf("error = %v (%T), want a *postmark.Error", err, err)
	}
	if apiError.Status != http.StatusInternalServerError {
		t.Errorf("Status = %d, want 500", apiError.Status)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want the status in it", err)
	}
}

func TestSendReportsAnUnreachableAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := server.URL
	server.Close()

	client := &Client{Token: "a-token", BaseURL: address, HTTP: &http.Client{Timeout: time.Second}}

	err := client.Send(t.Context(), message())
	if err == nil || !strings.Contains(err.Error(), "sending mail") {
		t.Fatalf("error = %v, want an unreachable API", err)
	}
}

// A client with nothing filled in but the token still points at Postmark, so
// production configuration is one environment variable.
func TestZeroValueClientTalksToPostmark(t *testing.T) {
	client := &Client{Token: "a-token"}

	if client.baseURL() != DefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", client.baseURL(), DefaultBaseURL)
	}
	if client.httpClient() == nil {
		t.Error("httpClient = nil, want a client with a timeout")
	}
	if timeout := client.httpClient().Timeout; timeout == 0 {
		t.Error("the default client has no timeout")
	}
}
