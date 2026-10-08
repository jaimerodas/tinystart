package web

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jaimerodas/tinystart/internal/store"
)

// settingsServer is one signed-in user on the Settings pages.
func settingsServer(t *testing.T) (*testServer, *store.User) {
	t.Helper()
	ts := newTestServer(t)
	user := ts.createUser("one@example.com")
	ts.signIn(user.Email)
	return ts, user
}

func TestSettingsRequiresAuthentication(t *testing.T) {
	ts := newTestServer(t)
	ts.get("/settings").assertRedirect("/sign_in")
}

// The date is the fact. The relative span is the thing you actually wanted to
// know, and the machine-readable one is what a reader's tooling gets.
func TestSettingsSaysMemberSinceThreeWays(t *testing.T) {
	ts, user := settingsServer(t)
	// The store stamps created_at from the wall clock, not the test's, so the
	// page's clock is set from created_at. 400 days is far from each boundary
	// of "about 1 year".
	ts.clock.set(user.CreatedAt.Add(400 * 24 * time.Hour))

	created := user.CreatedAt.UTC()
	ts.get("/settings").
		assertContains(`<time datetime="` + created.Format("2006-01-02T15:04:05Z") + `">`).
		assertContains(">" + strconv.Itoa(created.Day()) + " " + created.Month().String() + " " + strconv.Itoa(created.Year()) + "</time>").
		assertContains("(about 1 year ago)")
}

// The account's facts are labels and values, not a bulleted list with bold
// labels in it.
func TestSettingsShowsTheAccountAsLabelsAndValues(t *testing.T) {
	ts, user := settingsServer(t)

	ts.get("/settings").
		assertContains("<dt>Email</dt>").
		assertContains("<dd>" + user.Email + "</dd>").
		assertContains("<dt>Member since</dt>").
		assertNotContains("<b>Email:</b>")
}

// Changing the password is a page of its own in the menu, so the account
// section does not also link to it.
func TestTheAccountLeavesThePasswordToTheMenu(t *testing.T) {
	ts, _ := settingsServer(t)

	ts.get("/settings").
		assertContains(`<a href="/settings/password">Password</a>`).
		assertNotContains("Change password</a>")
}

// The column count moved to /start/edit, where the groups a shrink can
// strand are on screen. This page must not quietly continue to write it, or
// the two controls drift apart.
func TestSettingsNeitherOffersNorAcceptsAColumnCount(t *testing.T) {
	ts, user := settingsServer(t)

	ts.get("/settings").assertNotContains(`name="user[columns]"`)

	ts.send(http.MethodPatch, "/settings",
		form("user[columns]", "5", "user[theme_preference]", "dark")).
		assertRedirect("/settings")

	after := ts.reloadUser(user)
	if after.Columns != user.Columns {
		t.Errorf("columns = %d, want the %d Settings is not allowed to change", after.Columns, user.Columns)
	}
	if after.ThemePreference != "dark" {
		t.Errorf("theme = %q, want dark — the rest of the form still applied", after.ThemePreference)
	}
}

func TestSettingsUpdatesThemeAndColor(t *testing.T) {
	ts, user := settingsServer(t)

	ts.send(http.MethodPatch, "/settings",
		form("user[theme_preference]", "light", "user[color_preference]", "pink")).
		assertRedirect("/settings")

	after := ts.reloadUser(user)
	if after.ThemePreference != "light" || after.ColorPreference != "pink" {
		t.Errorf("theme/color = %q/%q, want light/pink", after.ThemePreference, after.ColorPreference)
	}
	ts.get("/settings").
		assertContains("Settings updated successfully.").
		assertContains(`<html data-theme="light" data-color="pink" data-font="geist">`)
}

// Each group of radios is a fieldset named by its legend, so a screen reader
// says "Theme" with "Dark" and not only "Dark, 3 of 3". A pick saves at once,
// so it matters which group it is in. No title is a label without a field.
func TestSettingsNamesEachGroupOfChoices(t *testing.T) {
	ts, _ := settingsServer(t)

	page := ts.get("/settings").assertNotContains("<label>")
	for _, name := range []string{"Theme", "Accent color", "Typeface", "Search engine"} {
		page.assertContains(`<fieldset class="form-group">
        <legend>` + name + `</legend>`)
	}
}

// A swatch shows its colour and says nothing, so the radio behind it carries
// the colour's name.
func TestSettingsNamesEachAccentColor(t *testing.T) {
	ts, _ := settingsServer(t)

	ts.get("/settings").
		assertContains(`<input id="color_purple" type="radio" value="purple" aria-label="Purple"`).
		assertContains(`<input id="color_teal" type="radio" value="teal" checked="checked" aria-label="Teal"`)
}

// The preferences save as they change, so a save answers in place: a word in
// the section's heading row, and no redirect or flash to redraw the page
// under the pointer.
func TestSettingsSavesAPreferenceInPlace(t *testing.T) {
	ts, user := settingsServer(t)

	ts.turbo(http.MethodPatch, "/settings", form("user[search_engine]", "kagi")).
		assertStatus(http.StatusOK).
		assertContains(`<turbo-stream action="update" target="preferences_status">`).
		assertContains("Saved")

	if got := ts.reloadUser(user).SearchEngine; got != "kagi" {
		t.Errorf("search engine = %q, want kagi", got)
	}
	ts.get("/settings").assertNotContains("Settings updated successfully.")
}

// A refusal answers in the same place, and changes nothing.
func TestSettingsSaysInPlaceWhenAPreferenceIsRefused(t *testing.T) {
	ts, user := settingsServer(t)

	ts.turbo(http.MethodPatch, "/settings", form("user[theme_preference]", "neon")).
		assertStatus(http.StatusUnprocessableEntity).
		assertContains(`<turbo-stream action="update" target="preferences_status">`).
		assertContains("Failed to update settings: Theme preference neon is not a valid theme")

	if got := ts.reloadUser(user).ThemePreference; got != "system" {
		t.Errorf("theme = %q, want the refusal to have changed nothing", got)
	}
}

// The typeface picker offers each font, with the stored one checked. A new
// account starts on Geist. The labels name the kind of typeface, not the
// font: the value is Geist, the reader sees Sans-serif. Each label carries
// its font, so it can be set in it.
func TestSettingsOffersTheFontChoices(t *testing.T) {
	ts, _ := settingsServer(t)

	ts.get("/settings").
		assertContains(`name="user[font_preference]"`).
		assertContains(`id="font_geist" type="radio" value="geist" checked="checked"`).
		assertContains(`<label for="font_geist" data-font="geist">Sans-serif</label>`).
		assertContains(`id="font_literata" type="radio" value="literata"`).
		assertContains(`<label for="font_literata" data-font="literata">Serif</label>`)
}

// Settings sets each choice in its own typeface, so it links every family it
// offers. The other pages link only the reader's.
func TestSettingsLinksEveryTypefaceItOffers(t *testing.T) {
	ts, _ := settingsServer(t)

	ts.get("/settings").
		assertContains("family=Geist").
		assertContains("family=Literata")
	ts.get("/").assertNotContains("family=Literata")
}

func TestSettingsUpdatesTheFont(t *testing.T) {
	ts, user := settingsServer(t)

	ts.send(http.MethodPatch, "/settings", form("user[font_preference]", "literata")).
		assertRedirect("/settings")

	if got := ts.reloadUser(user).FontPreference; got != "literata" {
		t.Errorf("font = %q, want literata", got)
	}
	ts.get("/settings").
		assertContains("Settings updated successfully.").
		assertContains(`data-font="literata"`).
		assertContains(`family=Literata`)
}

func TestSettingsRefusesAnInvalidFont(t *testing.T) {
	ts, user := settingsServer(t)

	ts.send(http.MethodPatch, "/settings", form("user[font_preference]", "comic-sans")).
		assertRedirect("/settings")

	if got := ts.reloadUser(user).FontPreference; got != "geist" {
		t.Errorf("font = %q, want the refusal to have changed nothing", got)
	}
	ts.get("/settings").
		assertContains("Failed to update settings: Font preference comic-sans is not a valid font")
}

// A theme-only submission keeps the stored font, the same as it keeps the
// search engine.
func TestSettingsLeavesTheFontAloneWhenNotSent(t *testing.T) {
	ts, user := settingsServer(t)
	ts.send(http.MethodPatch, "/settings", form("user[font_preference]", "literata"))

	ts.send(http.MethodPatch, "/settings", form("user[theme_preference]", "dark"))

	if got := ts.reloadUser(user).FontPreference; got != "literata" {
		t.Errorf("font = %q, want the stored literata", got)
	}
}

func TestSettingsAcceptsABodyWithOnlyAFont(t *testing.T) {
	ts, _ := settingsServer(t)

	ts.send(http.MethodPatch, "/settings", form("user[font_preference]", "literata")).
		assertStatus(http.StatusSeeOther)
}

// A field the form did not send keeps what is stored. The theme form posts
// both, but a request carrying one must not blank the other.
func TestSettingsLeavesAnUnsentPreferenceAlone(t *testing.T) {
	ts, user := settingsServer(t)
	ts.send(http.MethodPatch, "/settings", form("user[color_preference]", "pink"))

	ts.send(http.MethodPatch, "/settings", form("user[theme_preference]", "dark"))

	if got := ts.reloadUser(user).ColorPreference; got != "pink" {
		t.Errorf("color = %q, want the stored pink", got)
	}
}

// The search engine picker offers the three valid engines, with the stored
// one checked.
func TestSettingsOffersTheSearchEngineChoices(t *testing.T) {
	ts, _ := settingsServer(t)

	ts.get("/settings").
		assertContains(`name="user[search_engine]"`).
		assertContains(`id="search_engine_duckduckgo" type="radio" value="duckduckgo" checked="checked"`).
		assertContains(`<label for="search_engine_duckduckgo">DuckDuckGo</label>`).
		assertContains(`id="search_engine_google" type="radio" value="google"`).
		assertContains(`<label for="search_engine_google">Google</label>`).
		assertContains(`id="search_engine_kagi" type="radio" value="kagi"`).
		assertContains(`<label for="search_engine_kagi">Kagi</label>`)
}

func TestSettingsUpdatesTheSearchEngine(t *testing.T) {
	ts, user := settingsServer(t)

	ts.send(http.MethodPatch, "/settings", form("user[search_engine]", "kagi")).
		assertRedirect("/settings")

	if got := ts.reloadUser(user).SearchEngine; got != "kagi" {
		t.Errorf("search engine = %q, want kagi", got)
	}
	ts.get("/settings").assertContains("Settings updated successfully.")
}

func TestSettingsRefusesAnInvalidSearchEngine(t *testing.T) {
	ts, user := settingsServer(t)

	ts.send(http.MethodPatch, "/settings", form("user[search_engine]", "bing")).
		assertRedirect("/settings")

	if got := ts.reloadUser(user).SearchEngine; got != "duckduckgo" {
		t.Errorf("search engine = %q, want the refusal to have changed nothing", got)
	}
	ts.get("/settings").
		assertContains("Failed to update settings: Search engine bing is not a valid search engine")
}

// A theme-only submission must not blank the stored engine — formValueOr's
// fallback applies here exactly as it does for theme and color.
func TestSettingsLeavesTheSearchEngineAloneWhenNotSent(t *testing.T) {
	ts, user := settingsServer(t)

	ts.send(http.MethodPatch, "/settings", form("user[theme_preference]", "dark"))

	if got := ts.reloadUser(user).SearchEngine; got != "duckduckgo" {
		t.Errorf("search engine = %q, want the stored duckduckgo", got)
	}
}

func TestSettingsRefusesAnInvalidTheme(t *testing.T) {
	ts, user := settingsServer(t)

	ts.send(http.MethodPatch, "/settings", form("user[theme_preference]", "neon")).
		assertRedirect("/settings")

	if got := ts.reloadUser(user).ThemePreference; got != "system" {
		t.Errorf("theme = %q, want the refusal to have changed nothing", got)
	}
	ts.get("/settings").
		assertContains("Failed to update settings: Theme preference neon is not a valid theme")
}

// params.require(:user): a body with no user key at all is a bad request, not
// an empty update.
func TestSettingsRefusesABodyWithNoUserKey(t *testing.T) {
	ts, _ := settingsServer(t)

	ts.send(http.MethodPatch, "/settings", form("theme_preference", "dark")).
		assertStatus(http.StatusBadRequest)
}

// A body carrying only user[search_engine] is still a user submission, not an
// empty one.
func TestSettingsAcceptsABodyWithOnlyASearchEngine(t *testing.T) {
	ts, _ := settingsServer(t)

	ts.send(http.MethodPatch, "/settings", form("user[search_engine]", "google")).
		assertStatus(http.StatusSeeOther)
}

// The Users tab is built for an admin and absent for everybody else. Someone
// who cannot reach the page has no reason to know it is there.
func TestSettingsNavOffersUsersToAdminsOnly(t *testing.T) {
	ts := newTestServer(t)
	admin := ts.createUser("admin@example.com")
	plain := ts.createApprovedUser("two@example.com")

	ts.signIn(admin.Email)
	ts.get("/settings").assertContains(`href="/settings/admin/users"`)

	ts.signIn(plain.Email)
	ts.get("/settings").assertNotContains(`href="/settings/admin/users"`)
}

// Every Settings page shares one menu, and it marks the page you are on once,
// as the current page, so a screen reader says so too. The password page
// belongs to Main, where its link is. Each page names itself in a heading of
// its own.
func TestSettingsMenuMarksTheCurrentPage(t *testing.T) {
	ts := newTestServer(t)
	admin := ts.createUser("admin@example.com")
	ts.signIn(admin.Email)

	for _, page := range []struct{ path, current, heading string }{
		{"/settings", "/settings", "General"},
		{"/settings/password", "/settings/password", "Change password"},
		{"/settings/import_export", "/settings/import_export", "Import &amp; Export"},
		{"/settings/connections", "/settings/connections", "Connections"},
		{"/settings/browsers", "/settings/browsers", "Browsers"},
		{"/settings/admin/users", "/settings/admin/users", "Users"},
	} {
		resp := ts.get(page.path).
			assertContains(`<a href="` + page.current + `" aria-current="page">`).
			assertContains("<h1>" + page.heading + "</h1>")
		if n := strings.Count(resp.body, `aria-current`); n != 1 {
			t.Errorf("%s marks %d links as current, want 1", page.path, n)
		}
	}
}

// The first page is General in the menu and in its heading, so the two agree.
// The browser tab still says Settings, which is where you are.
func TestTheFirstSettingsPageIsCalledGeneral(t *testing.T) {
	ts, _ := settingsServer(t)

	ts.get("/settings").
		assertContains(`<a href="/settings" aria-current="page">General</a>`).
		assertContains("<h1>General</h1>").
		assertContains("<title>Settings - TinyStart</title>").
		assertNotContains(">Main<")
}

// The page is Password in the menu, and the action on it has one name from
// end to end: the heading, the button, and the notice afterwards all say
// "change password".
func TestPasswordEdit(t *testing.T) {
	ts, _ := settingsServer(t)

	ts.get("/settings/password").
		assertContains(`<a href="/settings/password" aria-current="page">Password</a>`).
		assertStatus(http.StatusOK).
		assertContains("<h1>Change password</h1>").
		assertContains(`name="user[existing_password]"`).
		assertContains(`name="user[new_password]"`).
		assertContains(`<button type="submit" class="action-button">Change password</button>`).
		assertNotContains("Update User")
}

func TestPasswordUpdate(t *testing.T) {
	ts, user := settingsServer(t)

	ts.send(http.MethodPatch, "/settings/password",
		form("user[existing_password]", testPassword, "user[new_password]", "testtesttest")).
		assertRedirect("/settings")

	if _, err := ts.db.Authenticate(ts.t.Context(), user.Email, "testtesttest"); err != nil {
		t.Errorf("the new password does not work: %v", err)
	}
	ts.get("/settings").assertContains("Password changed.")
}

// Every refusal re-renders the form with 422 rather than redirecting, because
// the messages name the fields. "Existing password is incorrect" only means
// anything beside the box it is about.
func TestPasswordUpdateRefusals(t *testing.T) {
	tests := []struct {
		name     string
		existing string
		password string
		message  string
		field    string
	}{
		{"a wrong existing password", "wrong", "testtesttest",
			"Existing password is incorrect", "user_existing_password"},
		{"no existing password", "", "testtesttest",
			"Existing password can&#39;t be blank", "user_existing_password"},
		{"a new password that is too short", testPassword, "short",
			"New password has to be longer", "user_new_password"},
		{"no new password at all", testPassword, "",
			"New password has to be longer", "user_new_password"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ts, user := settingsServer(t)

			resp := ts.send(http.MethodPatch, "/settings/password",
				form("user[existing_password]", test.existing, "user[new_password]", test.password)).
				assertStatus(http.StatusUnprocessableEntity).
				assertContains(`<ul class="form-errors" role="alert">`).
				assertContains("<li>" + test.message + "</li>").
				assertContains(`id="` + test.field + `" aria-invalid="true"`).
				assertNotContains("prohibited")
			if n := strings.Count(resp.body, "aria-invalid"); n != 1 {
				t.Errorf("%d fields are marked invalid, want only %s", n, test.field)
			}

			if _, err := ts.db.Authenticate(ts.t.Context(), user.Email, testPassword); err != nil {
				t.Errorf("the old password stopped working after a refusal: %v", err)
			}
		})
	}
}

// A refusal draws the form again at the address it was sent to, and the menu
// still says where you are.
func TestARefusedPasswordChangeStaysOnPasswordInTheMenu(t *testing.T) {
	ts, _ := settingsServer(t)

	ts.send(http.MethodPatch, "/settings/password",
		form("user[existing_password]", "wrong", "user[new_password]", "testtesttest")).
		assertStatus(http.StatusUnprocessableEntity).
		assertContains(`<a href="/settings/password" aria-current="page">Password</a>`)
}

func TestPasswordUpdateRefusesABodyWithNoUserKey(t *testing.T) {
	ts, _ := settingsServer(t)

	ts.send(http.MethodPatch, "/settings/password", form("new_password", "testtesttest")).
		assertStatus(http.StatusBadRequest)
}

func TestPasswordRequiresAuthentication(t *testing.T) {
	ts := newTestServer(t)
	ts.get("/settings/password").assertRedirect("/sign_in")
	ts.send(http.MethodPatch, "/settings/password", form("user[new_password]", "x")).
		assertRedirect("/sign_in")
}
