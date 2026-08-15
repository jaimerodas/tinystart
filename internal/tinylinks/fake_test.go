package tinylinks

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

// The other app, faked. Every test in this package talks to one of these
// rather than to a mock of net/http: the request that goes over the wire is
// then the thing under test, headers and encoding included.

// received is one request, as the fake saw it.
type received struct {
	method      string
	path        string
	escapedPath string
	query       url.Values
	header      http.Header
	body        string
	form        url.Values
}

// recorder keeps the requests a fake handled. The mutex is not decoration:
// the handler runs on the server's goroutine and the assertions on the test's,
// and `go test -race` is part of the gate.
type recorder struct {
	mu       sync.Mutex
	requests []received
}

func (r *recorder) record(request *http.Request) {
	body, _ := io.ReadAll(request.Body)
	form, _ := url.ParseQuery(string(body))

	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, received{
		method:      request.Method,
		path:        request.URL.Path,
		escapedPath: request.URL.EscapedPath(),
		query:       request.URL.Query(),
		header:      request.Header.Clone(),
		body:        string(body),
		form:        form,
	})
}

func (r *recorder) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}

// request is the only request the fake was sent, and a failure otherwise —
// every test here expects exactly one call.
func (r *recorder) request(t *testing.T) received {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.requests) != 1 {
		t.Fatalf("the fake handled %d requests, want 1", len(r.requests))
	}
	return r.requests[0]
}

// fake answers everything with the same status and body, and remembers what it
// was asked.
func fake(t *testing.T, status int, body string) (*httptest.Server, *recorder) {
	t.Helper()
	got := &recorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.record(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server, got
}
