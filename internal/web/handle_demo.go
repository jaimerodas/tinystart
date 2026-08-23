package web

import (
	"encoding/json"
	"net/http"

	"github.com/jaimerodas/tinystart/internal/store"
)

// The demo page a signed-out visitor sees at /. Named separately from
// pageStartShow so a typo is a compile error rather than a 500 the first
// time it renders.
const pageStartDemo = "start_demo"

// startDemoData is startShowData's read-only cousin: the same command bar
// payload and shortcut list, plus the two fixed columns. No HasTiles, no
// Federation beyond "off", no connection notice — none of that state exists
// for a visitor with no account.
type startDemoData struct {
	LinksJSON string
	Shortcuts []shortcut
	Left      showColumn
	Right     showColumn
}

// demoLeft and demoRight are the fixed grid a signed-out visitor sees. The
// links are real and clickable, but nothing about them is a database row:
// every ID is left at zero.
var demoLeft = showColumn{
	Groups: []showGroup{
		{
			Name: "Daily",
			Items: []store.Item{
				{Title: "Gmail", URL: "https://mail.google.com"},
				{Title: "YouTube", URL: "https://youtube.com"},
				{Title: "Google Calendar", URL: "https://calendar.google.com"},
			},
		},
		{
			Name: "Reading",
			Items: []store.Item{
				{Title: "Hacker News", URL: "https://news.ycombinator.com"},
				{Title: "jaime.dev", URL: "https://jaime.dev"},
			},
		},
	},
}

var demoRight = showColumn{
	Groups: []showGroup{
		{
			Name: "Dev",
			Items: []store.Item{
				{Title: "GitHub", URL: "https://github.com"},
				{Title: "MDN Web Docs", URL: "https://developer.mozilla.org"},
			},
		},
		{
			Name: "Tools",
			Items: []store.Item{
				{Title: "Wikipedia", URL: "https://wikipedia.org"},
				{Title: "DuckDuckGo", URL: "https://duckduckgo.com"},
			},
		},
	},
}

// demoLinks is the demo grid flattened for the command bar, left column
// first. Every ID stays zero on purpose: track_visit.js treats a falsy id as
// nothing to record, so the demo bar never posts to the auth-gated visit
// route.
func demoLinks() []store.Link {
	var links []store.Link
	for _, column := range []showColumn{demoLeft, demoRight} {
		for _, group := range column.Groups {
			for _, item := range group.Items {
				links = append(links, store.Link{Title: item.Title, URL: item.URL})
			}
		}
	}
	return links
}

// handleStartDemo is what GET / serves when nobody is signed in: a fixed
// grid and a working command bar over it, so the page does something instead
// of bouncing straight to a sign-in form.
func (s *Server) handleStartDemo() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encoded, err := json.Marshal(demoLinks())
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		s.render(w, r, http.StatusOK, layoutStart, pageStartDemo, startDemoData{
			LinksJSON: string(encoded),
			Shortcuts: demoPageShortcuts,
			Left:      demoLeft,
			Right:     demoRight,
		})
	})
}
