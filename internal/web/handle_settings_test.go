package web

import (
	"net/http"
	"strconv"
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

// Links lead: it is the number worth glancing at, and the one that grows.
func TestSettingsShowsTheLinkAndGroupCountsLinksFirst(t *testing.T) {
	ts, user := settingsServer(t)
	group := ts.newGroup(user.ID, "Work", 1)
	ts.newItem(user.ID, group.ID, "Example", "https://example.com")
	ts.newItem(user.ID, group.ID, "Other", "https://example.org")

	ts.get("/settings").
		assertStatus(http.StatusOK).
		assertContains(`<span class="stat-value">2</span>`).
		assertContains(`<span class="stat-label">links</span>`).
		assertContains(`<span class="stat-value">1</span>`).
		assertContains(`<span class="stat-label">group</span>`)
}

// A page with nothing on it still counts, in the plural: "0 links", not "0
// link".
func TestSettingsCountsAnEmptyPage(t *testing.T) {
	ts, _ := settingsServer(t)

	ts.get("/settings").
		assertContains(`<span class="stat-value">0</span>`).
		assertContains(`<span class="stat-label">links</span>`).
		assertContains(`<span class="stat-label">groups</span>`)
}

// The date is the fact. The relative span is the thing you actually wanted to
// know, and the machine-readable one is what a reader's tooling gets.
func TestSettingsSaysMemberSinceThreeWays(t *testing.T) {
	ts, user := settingsServer(t)
	// Far enough that the account's real created_at cannot drift the answer
	// across a boundary — the store stamps it from the wall clock, not the
	// test's. A day either side of a year is still "about 1 year".
	ts.clock.advance(400 * 24 * time.Hour)

	created := user.CreatedAt.UTC()
	ts.get("/settings").
		assertContains(`<time datetime="` + created.Format("2006-01-02T15:04:05Z") + `">`).
		assertContains(">\n          " + strconv.Itoa(created.Day()) + " " + created.Month().String() + " " + strconv.Itoa(created.Year())).
		assertContains("(about 1 year ago)")
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
		assertContains(`<html data-theme="light" data-color="pink">`)
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

func TestPasswordEdit(t *testing.T) {
	ts, _ := settingsServer(t)

	ts.get("/settings/password/edit").
		assertStatus(http.StatusOK).
		assertContains(`name="user[existing_password]"`).
		assertContains(`name="user[new_password]"`)
}

func TestPasswordUpdate(t *testing.T) {
	ts, user := settingsServer(t)

	ts.send(http.MethodPatch, "/settings/password",
		form("user[existing_password]", testPassword, "user[new_password]", "testtesttest")).
		assertRedirect("/settings")

	if _, err := ts.db.Authenticate(ts.t.Context(), user.Email, "testtesttest"); err != nil {
		t.Errorf("the new password does not work: %v", err)
	}
	ts.get("/settings").assertContains("Password was successfully changed.")
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

			ts.send(http.MethodPatch, "/settings/password",
				form("user[existing_password]", test.existing, "user[new_password]", test.password)).
				assertStatus(http.StatusUnprocessableEntity).
				assertContains("1 error prohibited this from being saved:").
				assertContains("<li>" + test.message + "</li>").
				assertContains(`<div class="field_with_errors"><label for="` + test.field + `">`)

			if _, err := ts.db.Authenticate(ts.t.Context(), user.Email, testPassword); err != nil {
				t.Errorf("the old password stopped working after a refusal: %v", err)
			}
		})
	}
}

func TestPasswordUpdateRefusesABodyWithNoUserKey(t *testing.T) {
	ts, _ := settingsServer(t)

	ts.send(http.MethodPatch, "/settings/password", form("new_password", "testtesttest")).
		assertStatus(http.StatusBadRequest)
}

func TestPasswordRequiresAuthentication(t *testing.T) {
	ts := newTestServer(t)
	ts.get("/settings/password/edit").assertRedirect("/sign_in")
	ts.send(http.MethodPatch, "/settings/password", form("user[new_password]", "x")).
		assertRedirect("/sign_in")
}
