//go:build browser

// The two places where this app talks to another one through JavaScript. The
// Rails suite had no system test for either. The poller and the federated
// half of the command bar were only ever driven from Ruby, where the fetch
// that carries them does not exist.
//
// Both run against a real server standing in for the connected app, so nothing
// is mocked on either side of the wire.
package web

import (
	"net/http"
	"testing"
)

// The connections page waits for approval by polling, and reloads itself when
// the answer changes. Everything after the form post happens in the browser:
// the fetch, the JSON, and the reload that renders the connected state.
func TestBrowserTheConnectionPollerNoticesApproval(t *testing.T) {
	p, user := startPageBrowser(t)
	flow := newFakeFlow(t)
	// Approved before the page can ask, so the poller's first tick is the one
	// that finds it. The alternative is a test that waits out the five second
	// interval to prove the same thing.
	flow.answers(`{"token":"a-token","scopes":["search","visit"],"expires_at":"2027-01-01T00:00:00Z"}`)

	p.visit("/settings/connections")
	p.fillIn("#base_url", flow.URL)
	p.clickOn("", "Connect")

	p.assertText(".connection-status.connected", "Connected")
	p.assertText(".connection-state", flow.URL)

	connection := p.ts.connectionOf(user.ID)
	if connection == nil {
		t.Fatal("no connection was stored")
	}
	if connection.Token != "a-token" || connection.Scopes != "search,visit" {
		t.Errorf("connection = %+v, want the approved grant", connection)
	}
}

// A connected app's results arrive in a section of their own, named after it,
// after the local tiles and after the debounce. The server renders a value
// that decides the three states the bar can be in, in JavaScript, so this is
// the only place they meet.
func TestBrowserCommandBarSearchesTheConnectedApp(t *testing.T) {
	p, user := startPageBrowser(t)
	p.tilesForFiltering(user)
	app := newFakeApp(t)
	app.answer(http.StatusOK, `{"links":[{"id":7,"title":"Apple Support","url":"https://support.apple.com"}]}`)
	p.ts.connect(user, app.URL)

	p.visit("/")
	p.fillIn(".command-bar input", "app")

	// The tiles are filtered in the same tick. The other app is half a second
	// behind it, by design.
	p.assertText(".command-bar-suggestions", "Apple")
	p.assertText(".command-bar-suggestions", "Apple Support")
	// Upper case because that is what is on the screen. The section headers
	// are text-transformed, and innerText reports what was rendered rather
	// than what was written.
	p.assertText(".command-bar-suggestions", "FROM 127.0.0.1")
	p.assertCountNow(".command-bar-section-header", 2)

	if asked := app.askedFor(); len(asked) == 0 || asked[len(asked)-1] != "app" {
		t.Errorf("the connected app was asked for %v, want the query the bar holds", asked)
	}
}
