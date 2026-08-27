package web

import (
	"net/http"
	"testing"
)

// --- the signed-out demo ---
//
// A visitor with no account should see the page do something, not bounce
// straight to a sign-in form. / now serves a fixed demo grid instead of
// redirecting when nobody is signed in.

func TestDemoPageShownToSignedOutVisitor(t *testing.T) {
	ts := newTestServer(t)
	ts.createApprovedUser("one@example.com")

	resp := ts.get("/").assertStatus(http.StatusOK)
	resp.assertContains("Gmail")
	resp.assertContains("Hacker News")
	resp.assertContains("GitHub")
	resp.assertContains("Wikipedia")
	resp.assertContains(`class="demo-cta"`)
	resp.assertContains(`href="/sign_in"`)

	// The demo is a look, not an invitation to sign up here, and not the real
	// editing surface — none of that belongs to someone with no account. The
	// Settings check is anchored on href because every page links settings.css
	// in its <head>; a stylesheet is not a way in.
	resp.assertNotContains("/sign_up")
	resp.assertNotContains("start-page-header")
	resp.assertNotContains("/start/edit")
	resp.assertNotContains(`href="/settings"`)
	resp.assertNotContains("visit-tracker")
	resp.assertNotContains("data-item-id")
}

func TestDemoPageEmbedsTheCommandBarsLinks(t *testing.T) {
	ts := newTestServer(t)
	ts.createApprovedUser("one@example.com")

	resp := ts.get("/").assertStatus(http.StatusOK)

	links := ts.commandBarLinks(resp)
	if len(links) != 9 {
		t.Fatalf("links = %d, want 9", len(links))
	}
	if links[0].Title != "Gmail" || links[0].URL != "https://mail.google.com" {
		t.Errorf("first link = %+v", links[0])
	}
	for _, link := range links {
		if link.ID != 0 {
			t.Errorf("link %+v has a real id, want 0: nothing behind the demo is a real row", link)
		}
	}

	// There is no connection to federate against without a signed-in user.
	resp.assertContains(`data-command-bar-federation-value="off"`)
}

func TestDemoPageDoesNotListTheEditShortcut(t *testing.T) {
	ts := newTestServer(t)
	ts.createApprovedUser("one@example.com")

	resp := ts.get("/").assertStatus(http.StatusOK)
	resp.assertNotContains("edit the start page")
	resp.assertContains("show this list")
}

func TestSignedInVisitorGetsTheirOwnStartPageNotTheDemo(t *testing.T) {
	ts, user := startPageServer(t)
	group := ts.newGroup(user.ID, "Mine", 1)
	ts.newItem(user.ID, group.ID, "Mine", "https://mine.example.com")

	resp := ts.get("/").assertStatus(http.StatusOK)
	resp.assertContains("Mine")
	resp.assertContains("/start/edit")
	resp.assertNotContains("demo-cta")
	resp.assertNotContains("Gmail")
}
