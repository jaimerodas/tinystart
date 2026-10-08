package web

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jaimerodas/tinystart/internal/store"
)

// The application and start layouts have no pages of their own until the next
// phase, so this file exercises them here directly. What they have to get
// right now is the chrome, which is where the theme attributes and the
// log-out button live.

func layoutHTML(t *testing.T, layout string, v view) string {
	t.Helper()
	s := newBareServer(t)
	// The layout needs some page to render inside it. The sign-in form is the
	// smallest one there is, and none of these assertions is about it.
	if v.Data == nil {
		v.Data = sessionsNewData{}
	}
	var out bytes.Buffer
	if err := s.templates.pages["sessions_new"].ExecuteTemplate(&out, layout, v); err != nil {
		t.Fatalf("rendering the %s layout: %v", layout, err)
	}
	return out.String()
}

// Logging out has to reload the document, so the theme and color attributes
// on <html> fall back to the logged-out defaults — hence data-turbo="false".
// The DELETE arrives as a POST carrying _method, which is what button_to wrote.
func TestTheApplicationLayoutsMenu(t *testing.T) {
	user := &store.User{ID: 1, ThemePreference: "dark", ColorPreference: "purple", FontPreference: "literata"}
	html := layoutHTML(t, layoutApplication, view{User: user, Theme: "dark", Color: "purple", Font: "literata"})

	for _, want := range []string{
		`<html data-theme="dark" data-color="purple" data-font="literata">`,
		`<a class="main-menu-link" href="/">Start</a>`,
		`<form data-turbo="false" class="button_to" method="post" action="/session">`,
		`<input type="hidden" name="_method" value="delete" />`,
		`<button type="submit">Log out</button>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the application layout does not contain %s", want)
		}
	}
}

// Signed out there is no menu. The only thing it offers is the way back to
// the start page, and there is nothing to go back to.
func TestTheApplicationLayoutHasNoMenuWhenSignedOut(t *testing.T) {
	html := layoutHTML(t, layoutApplication, view{Theme: themeFor(nil), Color: colorFor(nil), Font: fontFor(nil)})

	if strings.Contains(html, "<header>") {
		t.Error("the application layout drew a menu for an anonymous visitor")
	}
	if !strings.Contains(html, `<html data-theme="system" data-color="teal" data-font="geist">`) {
		t.Error("the logged-out defaults are not system, teal and geist")
	}
}

// A page links the stylesheet of one typeface: the reader's. The browser only
// downloads the font files a page uses, but it fetches every stylesheet it is
// given, and each family's @font-face rules come with it. The sign-in pages
// have no reader yet, so they get the default.
func TestEveryLayoutLinksOnlyTheChosenTypeface(t *testing.T) {
	for _, test := range []struct {
		layout     string
		font       string
		want, skip string
	}{
		{layoutApplication, "literata", "family=Literata", "family=Geist"},
		{layoutStart, "literata", "family=Literata", "family=Geist"},
		{layoutStart, "geist", "family=Geist", "family=Literata"},
		{layoutSession, fontFor(nil), "family=Geist", "family=Literata"},
	} {
		html := layoutHTML(t, test.layout, view{Font: test.font})
		if !strings.Contains(html, `<link href="https://fonts.googleapis.com/css2?`+test.want) {
			t.Errorf("%s with %q does not link %s", test.layout, test.font, test.want)
		}
		if strings.Contains(html, test.skip) {
			t.Errorf("%s with %q also links %s", test.layout, test.font, test.skip)
		}
	}
}

// Each typeface a reader can pick has a stylesheet to link. One missing here
// would leave that reader on the fallback font with no error anywhere.
func TestEveryValidFontHasAStylesheet(t *testing.T) {
	for _, font := range store.ValidFonts {
		if _, ok := fontStylesheets[font]; !ok {
			t.Errorf("no stylesheet for %q", font)
		}
	}
}

// The start page's layout is chrome-free: the page draws the header it has,
// not the layout.
func TestTheStartLayoutIsChromeFree(t *testing.T) {
	html := layoutHTML(t, layoutStart, view{Theme: "light", Color: "red"})

	if strings.Contains(html, "main-menu-link") {
		t.Error("the start layout drew the settings menu")
	}
	if !strings.Contains(html, "    <article>\n      ") {
		t.Error("the start layout does not wrap the page in an <article>")
	}
}

// The sign-in layout has no theme attributes at all: nobody said who they
// are yet.
func TestTheSessionLayoutHasNoThemeAttributes(t *testing.T) {
	html := layoutHTML(t, layoutSession, view{})

	if !strings.Contains(html, "<html>\n") {
		t.Error("the session layout put attributes on <html>")
	}
	if !strings.Contains(html, `<body id="session">`) {
		t.Error("the session layout lost its body id")
	}
}

func TestTitleComposition(t *testing.T) {
	if got := layoutHTML(t, layoutSession, view{}); !strings.Contains(got, "<title>TinyStart</title>") {
		t.Error("a page with no title of its own is not just TinyStart")
	}
	if got := layoutHTML(t, layoutSession, view{Title: "New user"}); !strings.Contains(got, "<title>New user - TinyStart</title>") {
		t.Error("a page with a title does not compose it with the app's")
	}
}

// Every layout links the same stylesheets and the same import map, and no page
// can be served without them.
func TestEveryLayoutLoadsTheAssets(t *testing.T) {
	for _, layout := range []string{layoutApplication, layoutSession, layoutStart} {
		html := layoutHTML(t, layout, view{})
		if !strings.Contains(html, `<link rel="stylesheet" href="/assets/application-`) {
			t.Errorf("%s does not link the stylesheets", layout)
		}
		if !strings.Contains(html, `<script type="importmap" data-turbo-track="reload">`) {
			t.Errorf("%s has no import map", layout)
		}
		if !strings.Contains(html, `<link rel="icon" href="/icon.svg" type="image/svg+xml">`) {
			t.Errorf("%s has no favicon", layout)
		}
	}
}
