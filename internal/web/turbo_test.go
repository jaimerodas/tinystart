package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWantsTurboStream(t *testing.T) {
	tests := []struct {
		accept string
		want   bool
	}{
		{"text/vnd.turbo-stream.html, text/html, application/xhtml+xml", true},
		{"text/html, application/xhtml+xml", false},
		{"", false},
	}

	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodPost, "/start/items", nil)
		if tt.accept != "" {
			req.Header.Set("Accept", tt.accept)
		}
		if got := wantsTurboStream(req); got != tt.want {
			t.Errorf("wantsTurboStream(%q) = %v, want %v", tt.accept, got, tt.want)
		}
	}
}

// The wire format is turbo_stream_action_tag's, down to the empty <template>
// on a remove and to the joiner between two actions — which is nothing at all.
// Rails concatenated the elements with no separator, and the parity capture
// says so byte for byte. A newline here becomes a text node between them.
func TestTurboStreamResponse(t *testing.T) {
	s := newBareServer(t)
	rec := httptest.NewRecorder()

	s.writeTurboStream(rec, http.StatusOK,
		replaceStream("item_1", "<li>one</li>"),
		updateStream("start_page_notice", "<p>saved</p>"),
		removeStream("group_2"),
	)

	if got := rec.Header().Get("Content-Type"); got != "text/vnd.turbo-stream.html; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}

	want := `<turbo-stream action="replace" target="item_1"><template><li>one</li></template></turbo-stream>` +
		`<turbo-stream action="update" target="start_page_notice"><template><p>saved</p></template></turbo-stream>` +
		`<turbo-stream action="remove" target="group_2"><template></template></turbo-stream>`
	if got := rec.Body.String(); got != want {
		t.Errorf("body =\n%s\nwant\n%s", got, want)
	}
}

func TestRenderPartial(t *testing.T) {
	s := newBareServer(t)

	html, err := s.renderPartial("sessions_new", "flash", flashMessage{Type: flashAlert, Message: "Nope."})
	if err != nil {
		t.Fatalf("rendering the flash partial: %v", err)
	}
	if !strings.Contains(string(html), `<div class="flash-card alert"`) {
		t.Errorf("partial = %q", html)
	}

	if _, err := s.renderPartial("no_such_page", "flash", nil); err == nil {
		t.Error("rendering a partial from a page that does not exist returned no error")
	}
}
