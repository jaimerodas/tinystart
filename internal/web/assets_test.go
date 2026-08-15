package web

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestAssetURLsCarryADigest(t *testing.T) {
	assets, err := newAssets()
	if err != nil {
		t.Fatalf("reading the assets: %v", err)
	}

	digested := regexp.MustCompile(`^/assets/(.+)-[0-9a-f]{8}\.(css|js)$`)
	for logical, a := range assets.byLogical {
		if !digested.MatchString(a.url) {
			t.Errorf("%s is served at %q, which carries no digest", logical, a.url)
		}
	}
}

// Two files with different contents have to get different URLs, or a deploy
// would leave people on the old one.
func TestTheDigestFollowsTheContents(t *testing.T) {
	first, second := digest([]byte("a")), digest([]byte("b"))
	if first == second {
		t.Error("two different files hashed the same")
	}
	if again := digest([]byte("a")); again != first {
		t.Errorf("the same file hashed to %q and then %q", first, again)
	}
}

func TestAssetsAreServedImmutably(t *testing.T) {
	ts := newTestServer(t)
	assets, err := newAssets()
	if err != nil {
		t.Fatalf("reading the assets: %v", err)
	}

	resp := ts.get(assets.path("application.css")).assertStatus(http.StatusOK)

	if got := resp.Header.Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q, want a year and immutable", got)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", got)
	}
}

// A URL with the wrong digest is a 404 rather than a fallback to the current
// file: a wrong digest means a stale page, and serving it anyway hides that.
func TestAnUnknownAssetIsNotFound(t *testing.T) {
	ts := newTestServer(t)

	ts.get("/assets/application-00000000.css").assertStatus(http.StatusNotFound)
	ts.get("/assets/nothing-here-12345678.js").assertStatus(http.StatusNotFound)
}

// The importmap has to name every controller, because stimulus-loading reads
// it to decide what to eager-load: a controller missing from the map is a
// controller that never registers.
func TestTheImportmapNamesEveryModule(t *testing.T) {
	assets, err := newAssets()
	if err != nil {
		t.Fatalf("reading the assets: %v", err)
	}
	tags := string(assets.importmapTags())

	want := []string{
		`"application":`,
		`"@hotwired/turbo-rails":`,
		`"@hotwired/stimulus":`,
		`"@hotwired/stimulus-loading":`,
		// index.js is the directory's own name, not controllers/index.
		`"controllers":`,
		`"controllers/application":`,
		`"controllers/auto_submit_controller":`,
		`"controllers/command_bar_controller":`,
		`"controllers/device_flow_controller":`,
		`"controllers/drag_drop_controller":`,
		`"controllers/flash_controller":`,
		`"controllers/grid_keyboard_controller":`,
		`"controllers/inline_form_controller":`,
		`"controllers/passwords_controller":`,
		`"controllers/start_shortcuts_controller":`,
		`"controllers/theme_controller":`,
		`"controllers/visit_tracker_controller":`,
		`"lib/start_page_moves":`,
		`"lib/track_visit":`,
	}
	for _, specifier := range want {
		if !strings.Contains(tags, specifier) {
			t.Errorf("the import map does not name %s", specifier)
		}
	}

	// Everything in the map is preloaded, and the app is started by one line.
	for _, m := range assets.modules {
		if !strings.Contains(tags, `<link rel="modulepreload" href="`+m.asset.url+`">`) {
			t.Errorf("%s is in the map but not preloaded", m.specifier)
		}
	}
	if !strings.Contains(tags, `<script type="module">import "application"</script>`) {
		t.Error("nothing starts the application")
	}
}

// stylesheet_link_tag :app linked every file in the directory, alphabetically.
func TestEveryStylesheetIsLinkedInNameOrder(t *testing.T) {
	assets, err := newAssets()
	if err != nil {
		t.Fatalf("reading the assets: %v", err)
	}
	tags := string(assets.stylesheetTags())

	if count := strings.Count(tags, "<link rel=\"stylesheet\""); count != len(assets.stylesheets) {
		t.Errorf("linked %d stylesheets, have %d", count, len(assets.stylesheets))
	}
	if !strings.Contains(tags, `data-turbo-track="reload"`) {
		t.Error("the stylesheets are not tracked by Turbo")
	}

	var previous string
	for _, a := range assets.stylesheets {
		if a.logical < previous {
			t.Errorf("%s comes after %s; the order is not alphabetical", a.logical, previous)
		}
		previous = a.logical
	}
	if assets.stylesheets[0].logical != "application.css" {
		t.Errorf("the first stylesheet is %s, want application.css", assets.stylesheets[0].logical)
	}
}

// The files Rails served straight out of public/ keep their paths, because
// they are named in the layouts and in other people's bookmarks.
func TestPublicFilesKeepTheirPaths(t *testing.T) {
	ts := newTestServer(t)

	for path, contentType := range map[string]string{
		"/icon.png":   "image/png",
		"/icon.svg":   "image/svg+xml",
		"/robots.txt": "text/plain",
	} {
		resp := ts.get(path).assertStatus(http.StatusOK)
		if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, contentType) {
			t.Errorf("%s Content-Type = %q, want %s", path, got, contentType)
		}
	}
}

func TestAnUnknownPathIsTheStaticNotFoundPage(t *testing.T) {
	ts := newTestServer(t)

	ts.get("/no/such/page").
		assertStatus(http.StatusNotFound).
		assertContains("The page you were looking for doesn")
}

func TestTheHealthCheckAnswers(t *testing.T) {
	ts := newTestServer(t)

	resp := ts.get("/up").assertStatus(http.StatusOK)
	if strings.TrimSpace(resp.body) != "ok" {
		t.Errorf("body = %q, want ok", resp.body)
	}
}

// The icon helper marks every glyph decorative on the way out, so a new icon
// cannot forget to.
func TestIconsAreMarkedDecorative(t *testing.T) {
	assets, err := newAssets()
	if err != nil {
		t.Fatalf("reading the assets: %v", err)
	}

	approved := string(assets.icon("approved"))
	if !strings.HasPrefix(approved, `<svg aria-hidden="true" focusable="false"`) {
		t.Errorf("the approved icon starts %q", approved[:min(60, len(approved))])
	}
	if assets.icon("no-such-icon") != "" {
		t.Error("a missing icon rendered something")
	}
}
