package tinylinks

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The envelope the other app really sends: more fields than the command bar
// wants, in an object with a meta block beside the links.
const searchEnvelope = `{
	"links": [
		{"id": 1, "url": "https://a.example", "title": "Alpha", "description": "long", "tags": ["x"], "visit_count": 3},
		{"id": 2, "url": "https://b.example", "title": "Beta", "description": "long", "tags": [], "visit_count": 0}
	],
	"meta": {"page": 1, "per_page": 12, "total_items": 2, "total_pages": 1}
}`

func TestSearchReturnsOnlyIDTitleAndURL(t *testing.T) {
	server, _ := fake(t, http.StatusOK, searchEnvelope)
	client := NewClient(server.URL, "a-token", server.Client())

	links, err := client.Search(t.Context(), "alpha")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	want := []Link{
		{ID: 1, Title: "Alpha", URL: "https://a.example"},
		{ID: 2, Title: "Beta", URL: "https://b.example"},
	}
	if len(links) != len(want) {
		t.Fatalf("got %d links, want %d", len(links), len(want))
	}
	for i := range want {
		if links[i] != want[i] {
			t.Errorf("link %d = %+v, want %+v", i, links[i], want[i])
		}
	}
}

func TestSearchSendsTheRequestTheOtherAppExpects(t *testing.T) {
	server, got := fake(t, http.StatusOK, searchEnvelope)
	client := NewClient(server.URL+"/", "a-token", server.Client())

	if _, err := client.Search(t.Context(), "alpha beta"); err != nil {
		t.Fatalf("Search: %v", err)
	}

	request := got.request(t)
	if request.method != http.MethodGet {
		t.Errorf("method = %s, want GET", request.method)
	}
	if request.path != "/api/v1/search" {
		t.Errorf("path = %s, want /api/v1/search", request.path)
	}
	if q := request.query.Get("q"); q != "alpha beta" {
		t.Errorf("q = %q, want %q", q, "alpha beta")
	}
	if perPage := request.query.Get("per_page"); perPage != "10" {
		t.Errorf("per_page = %q, want 10", perPage)
	}
	if auth := request.header.Get("Authorization"); auth != "Bearer a-token" {
		t.Errorf("Authorization = %q", auth)
	}
	if accept := request.header.Get("Accept"); accept != "application/json" {
		t.Errorf("Accept = %q", accept)
	}
}

func TestSearchCapsResultsAtTen(t *testing.T) {
	var body strings.Builder
	body.WriteString(`{"links":[`)
	for i := range 25 {
		if i > 0 {
			body.WriteString(",")
		}
		fmt.Fprintf(&body, `{"id":%d,"title":"T%d","url":"https://%d.example"}`, i, i, i)
	}
	body.WriteString(`]}`)

	server, _ := fake(t, http.StatusOK, body.String())
	client := NewClient(server.URL, "a-token", server.Client())

	links, err := client.Search(t.Context(), "t")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(links) != MaxResults {
		t.Errorf("got %d links, want %d", len(links), MaxResults)
	}
}

func TestSearchDoesNotCallOutForABlankQuery(t *testing.T) {
	server, got := fake(t, http.StatusOK, searchEnvelope)
	client := NewClient(server.URL, "a-token", server.Client())

	for _, query := range []string{"", "   "} {
		links, err := client.Search(t.Context(), query)
		if !errors.Is(err, ErrEmptyQuery) {
			t.Errorf("Search(%q) error = %v, want ErrEmptyQuery", query, err)
		}
		if len(links) != 0 {
			t.Errorf("Search(%q) returned %d links", query, len(links))
		}
	}
	if got.calls() != 0 {
		t.Errorf("the fake was called %d times, want 0", got.calls())
	}
}

// An object with no "links" key is a success with nothing in it, not a
// failure: Rails' fetch("links", []) said the same, and the caller has to
// clear a recorded failure on the strength of it.
func TestSearchTreatsABodyWithoutLinksAsEmpty(t *testing.T) {
	server, _ := fake(t, http.StatusOK, `{"meta":{"total_items":0}}`)
	client := NewClient(server.URL, "a-token", server.Client())

	links, err := client.Search(t.Context(), "alpha")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(links) != 0 {
		t.Errorf("got %d links, want 0", len(links))
	}
}

func TestSearchStatusFailures(t *testing.T) {
	tests := []struct {
		name           string
		status         int
		body           string
		needsReconnect bool
		message        string
	}{
		{
			name:           "a rejected token asks for a reconnect",
			status:         http.StatusUnauthorized,
			body:           `{"error":"unauthorized"}`,
			needsReconnect: true,
			message:        "rejected the token — reconnect to restore search",
		},
		{
			name:           "a missing scope asks for a reconnect",
			status:         http.StatusForbidden,
			body:           `{"error":"insufficient_scope"}`,
			needsReconnect: true,
			message:        "token is missing a scope — reconnect to restore search",
		},
		{
			// The other app's problem, not a credential problem.
			name:    "a server error does not",
			status:  http.StatusInternalServerError,
			body:    "boom",
			message: "answered 500",
		},
		{
			name:    "neither does a bad gateway",
			status:  http.StatusBadGateway,
			body:    "",
			message: "answered 502",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, _ := fake(t, test.status, test.body)
			client := NewClient(server.URL, "a-token", server.Client())

			links, err := client.Search(t.Context(), "alpha")
			if len(links) != 0 {
				t.Errorf("got %d links, want none", len(links))
			}
			if err == nil {
				t.Fatal("Search: want an error")
			}
			if !strings.Contains(err.Error(), test.message) {
				t.Errorf("error = %q, want it to contain %q", err, test.message)
			}
			if !strings.Contains(err.Error(), "127.0.0.1") {
				t.Errorf("error = %q, want it to name the host", err)
			}
			if NeedsReconnect(err) != test.needsReconnect {
				t.Errorf("NeedsReconnect = %v, want %v", NeedsReconnect(err), test.needsReconnect)
			}

			var status *StatusError
			if !errors.As(err, &status) || status.Status != test.status {
				t.Errorf("error does not carry the status: %v", err)
			}
		})
	}
}

func TestSearchSurvivesABodyThatIsNotJSON(t *testing.T) {
	server, _ := fake(t, http.StatusOK, "<html>not json</html>")
	client := NewClient(server.URL, "a-token", server.Client())

	links, err := client.Search(t.Context(), "alpha")
	if len(links) != 0 {
		t.Errorf("got %d links, want none", len(links))
	}
	if err == nil || !strings.Contains(err.Error(), "isn't JSON") {
		t.Fatalf("error = %v, want it to name the malformed body", err)
	}
	if NeedsReconnect(err) {
		t.Error("a malformed body is not a credential problem")
	}
}

func TestSearchSurvivesATimeout(t *testing.T) {
	// Never answers. Waiting on the request context rather than sleeping means
	// Close() below returns as soon as the client gives up.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	// A short injected timeout: the point is the shape of the failure, not
	// the four seconds DefaultHTTPClient would really wait.
	client := NewClient(server.URL, "a-token", &http.Client{Timeout: 20 * time.Millisecond})

	links, err := client.Search(t.Context(), "alpha")
	if len(links) != 0 {
		t.Errorf("got %d links, want none", len(links))
	}
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v, want a timeout", err)
	}
	if NeedsReconnect(err) {
		t.Error("a timeout is not a credential problem")
	}
}

func TestSearchSurvivesAnAppThatIsNotThere(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := server.URL
	server.Close()

	client := NewClient(address, "a-token", &http.Client{Timeout: time.Second})

	links, err := client.Search(t.Context(), "alpha")
	if len(links) != 0 {
		t.Errorf("got %d links, want none", len(links))
	}
	if err == nil || !strings.Contains(err.Error(), "could not reach") {
		t.Fatalf("error = %v, want an unreachable app", err)
	}
}

// A base URL with no host names nothing a person would recognise, so the
// messages fall back to "the connected app" the way Connection#hostname did.
func TestFailuresNameTheConnectedAppWhenTheHostIsUnknown(t *testing.T) {
	client := NewClient("links.example.com", "a-token", &http.Client{Timeout: time.Second})

	_, err := client.Search(t.Context(), "alpha")
	if err == nil || !strings.Contains(err.Error(), "the connected app") {
		t.Fatalf("error = %v, want the fallback name", err)
	}
}

func TestRecordVisitPostsToTheLink(t *testing.T) {
	server, got := fake(t, http.StatusNoContent, "")
	client := NewClient(server.URL, "a-token", server.Client())

	if err := client.RecordVisit(t.Context(), "7"); err != nil {
		t.Fatalf("RecordVisit: %v", err)
	}

	request := got.request(t)
	if request.method != http.MethodPost {
		t.Errorf("method = %s, want POST", request.method)
	}
	if request.path != "/api/v1/links/7/visit" {
		t.Errorf("path = %s", request.path)
	}
	if auth := request.header.Get("Authorization"); auth != "Bearer a-token" {
		t.Errorf("Authorization = %q", auth)
	}
}

// The id arrives from the query string, so it is whatever the browser sent:
// escaped, it cannot walk out of /api/v1/links and hit another endpoint.
func TestRecordVisitEscapesTheLinkID(t *testing.T) {
	server, got := fake(t, http.StatusNoContent, "")
	client := NewClient(server.URL, "a-token", server.Client())

	if err := client.RecordVisit(t.Context(), "../../evil"); err != nil {
		t.Fatalf("RecordVisit: %v", err)
	}
	if path := got.request(t).escapedPath; path != "/api/v1/links/..%2F..%2Fevil/visit" {
		t.Errorf("path = %s, want the id escaped inside the links path", path)
	}
}

func TestRecordVisitWithoutAnID(t *testing.T) {
	server, got := fake(t, http.StatusNoContent, "")
	client := NewClient(server.URL, "a-token", server.Client())

	if err := client.RecordVisit(t.Context(), ""); !errors.Is(err, ErrEmptyLinkID) {
		t.Errorf("error = %v, want ErrEmptyLinkID", err)
	}
	if got.calls() != 0 {
		t.Errorf("the fake was called %d times, want 0", got.calls())
	}
}

func TestRecordVisitReportsARejectedToken(t *testing.T) {
	server, _ := fake(t, http.StatusUnauthorized, `{"error":"unauthorized"}`)
	client := NewClient(server.URL, "a-token", server.Client())

	err := client.RecordVisit(t.Context(), "7")
	if !NeedsReconnect(err) {
		t.Errorf("error = %v, want a reconnect", err)
	}
}

func TestRecordVisitReportsAnUnreachableApp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := server.URL
	server.Close()

	client := NewClient(address, "a-token", &http.Client{Timeout: time.Second})

	if err := client.RecordVisit(t.Context(), "7"); err == nil {
		t.Error("RecordVisit: want an error")
	}
}

// The timeouts are the Ruby's, so a regression here would be a silent change
// to how long the command bar can hang.
func TestDefaultHTTPClientTimeout(t *testing.T) {
	if timeout := DefaultHTTPClient().Timeout; timeout != 0 {
		t.Errorf("Timeout = %v, want the transport to do the timing out", timeout)
	}
	transport, ok := DefaultHTTPClient().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http.Transport", DefaultHTTPClient().Transport)
	}
	if transport.ResponseHeaderTimeout != readTimeout {
		t.Errorf("ResponseHeaderTimeout = %v, want %v", transport.ResponseHeaderTimeout, readTimeout)
	}
	if transport.TLSHandshakeTimeout != openTimeout {
		t.Errorf("TLSHandshakeTimeout = %v, want %v", transport.TLSHandshakeTimeout, openTimeout)
	}
}
