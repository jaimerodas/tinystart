package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jaimerodas/tinystart/internal/store"
)

// The page names, which are also the template file names under templates/pages.
const (
	pageSettingsShow         = "settings_show"
	pageSettingsPasswordEdit = "settings_password_edit"
	pageSettingsConnections  = "settings_connections"
	pageSettingsImportExport = "settings_import_export"
	pageSettingsBrowsers     = "settings_browsers"
	pageSettingsUsers        = "settings_users"
)

// settingsNavItem is one tab of the row every Settings page carries.
// SettingsHelper built it with content_tag. Here it is data, so the template
// has no decisions left to make.
type settingsNavItem struct {
	Title string
	Path  string
	Class string
}

// settingsNav is SettingsHelper#settings_secondary_nav. The Users tab is only
// built for an admin — absent, not disabled, because someone who cannot reach
// the page has no reason to know it is there.
func settingsNav(user *store.User, active string) []settingsNavItem {
	items := []settingsNavItem{
		{Title: "Main", Path: "/settings"},
		{Title: "Import & Export", Path: "/settings/import_export"},
		{Title: "Connections", Path: "/settings/connections"},
		{Title: "Browsers", Path: "/settings/browsers"},
	}
	if user.Admin {
		items = append(items, settingsNavItem{Title: "Users", Path: "/settings/admin/users"})
	}

	for i := range items {
		if items[i].Title == active {
			items[i].Class = "active"
		}
	}
	return items
}

// settingsShowData is the main Settings page: the account facts and the
// preferences this page owns.
type settingsShowData struct {
	Nav []settingsNavItem

	// The date three ways, because the row says all three: machine-readable,
	// written out, and how long ago that was.
	MemberSince     string
	MemberSinceDate string
	MemberSinceAgo  string

	Themes  []choice
	Colors  []choice
	Fonts   []choice
	Engines []choice
}

// choice is one radio button: its value, its label, and whether it is the one
// stored.
type choice struct {
	Value   string
	Label   string
	Checked bool
}

// handleSettings is GET /settings.
func (s *Server) handleSettings() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := userFrom(r.Context())

		created := user.CreatedAt.UTC()
		data := settingsShowData{
			Nav: settingsNav(user, "Main"),
			// Time#iso8601, which has no fractional part however many
			// microseconds the column holds.
			MemberSince: created.Format("2006-01-02T15:04:05Z"),
			// strftime("%-d %B %Y"): the day with no leading zero.
			MemberSinceDate: strconv.Itoa(created.Day()) + " " + created.Month().String() + " " + strconv.Itoa(created.Year()),
			MemberSinceAgo:  timeAgoInWords(created, s.now()),
			Themes: []choice{
				{Value: "system", Label: "System"},
				{Value: "light", Label: "Light"},
				{Value: "dark", Label: "Dark"},
			},
			Fonts: []choice{
				{Value: "geist", Label: "Sans-serif"},
				{Value: "literata", Label: "Serif"},
			},
			Engines: []choice{
				{Value: "duckduckgo", Label: "DuckDuckGo"},
				{Value: "google", Label: "Google"},
				{Value: "kagi", Label: "Kagi"},
			},
		}
		for i := range data.Themes {
			data.Themes[i].Checked = data.Themes[i].Value == user.ThemePreference
		}
		for i := range data.Fonts {
			data.Fonts[i].Checked = data.Fonts[i].Value == user.FontPreference
		}
		for i := range data.Engines {
			data.Engines[i].Checked = data.Engines[i].Value == user.SearchEngine
		}
		for _, color := range store.ValidColors {
			// The swatch has no text, so the label is the radio's aria-label.
			label := strings.ToUpper(color[:1]) + color[1:]
			data.Colors = append(data.Colors, choice{Value: color, Label: label, Checked: color == user.ColorPreference})
		}

		s.render(w, r, http.StatusOK, layoutApplication, pageSettingsShow, data)
	})
}

// handleSettingsUpdate is PATCH /settings: the theme, the accent color, the
// font and the search engine, and nothing else.
//
// The column count is deliberately not accepted here, however it is spelled in
// the body. It belongs to the editor, where a refused shrink can answer on
// the page: if it goes through, it strands the groups shown there. If
// Settings keeps a copy of the control, the two drift apart.
func (s *Server) handleSettingsUpdate() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := userFrom(r.Context())

		if err := r.ParseForm(); err != nil {
			s.serveErrorPage(w, http.StatusBadRequest, "/400.html")
			return
		}
		// params.require(:user) answers 400 for a body with no user key at
		// all, rather than treating it as an empty one.
		if !r.PostForm.Has("user[theme_preference]") && !r.PostForm.Has("user[color_preference]") &&
			!r.PostForm.Has("user[search_engine]") && !r.PostForm.Has("user[font_preference]") {
			s.serveErrorPage(w, http.StatusBadRequest, "/400.html")
			return
		}

		// A field the form did not send keeps its stored value, which is what
		// assign_attributes over the permitted keys did: the theme form posts
		// both, and a request carrying one changes only that one.
		theme := formValueOr(r, "user[theme_preference]", user.ThemePreference)
		color := formValueOr(r, "user[color_preference]", user.ColorPreference)
		engine := formValueOr(r, "user[search_engine]", user.SearchEngine)
		font := formValueOr(r, "user[font_preference]", user.FontPreference)

		err := s.db.UpdatePreferences(r.Context(), user.ID, theme, color, engine, font)
		var invalid store.ValidationError
		if err != nil && !errors.As(err, &invalid) {
			s.serverError(w, r, err)
			return
		}

		// The form saves on every change, so Turbo gets its answer in place: a
		// redirect would redraw the page under the pointer. Without Turbo the
		// write redirects, as every other write here does.
		if wantsTurboStream(r) {
			status, answer := http.StatusOK, flashMessage{Type: flashNotice, Message: "Saved"}
			if err != nil {
				status, answer = http.StatusUnprocessableEntity,
					flashMessage{Type: flashAlert, Message: "Failed to update settings: " + invalid.Error()}
			}
			html, err := s.renderPartial(pageSettingsShow, "preferences_status", answer)
			if err != nil {
				s.serverError(w, r, err)
				return
			}
			s.writeTurboStream(w, status, updateStream("preferences_status", html))
			return
		}

		if err != nil {
			s.redirect(w, r, "/settings", flashAlert, "Failed to update settings: "+invalid.Error())
			return
		}
		s.redirect(w, r, "/settings", flashNotice, "Settings updated successfully.")
	})
}

// settingsPasswordData is the change-password form: the errors above it, and
// which of the two fields to outline.
type settingsPasswordData struct {
	Nav             []settingsNavItem
	Errors          []string
	ExistingInvalid bool
	NewInvalid      bool
}

// handleSettingsPasswordEdit is GET /settings/password/edit.
func (s *Server) handleSettingsPasswordEdit() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.render(w, r, http.StatusOK, layoutApplication, pageSettingsPasswordEdit,
			settingsPasswordData{Nav: settingsNav(userFrom(r.Context()), "Main")})
	})
}

// handleSettingsPasswordUpdate is PATCH /settings/password.
//
// A refusal re-renders the form with 422 rather than redirecting, because the
// errors name the fields: "Existing password is incorrect" only means anything
// beside the box it is about.
func (s *Server) handleSettingsPasswordUpdate() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := userFrom(r.Context())

		if err := r.ParseForm(); err != nil {
			s.serveErrorPage(w, http.StatusBadRequest, "/400.html")
			return
		}
		// params.expect(user: […]) again: no user key at all is a 400.
		if !r.PostForm.Has("user[existing_password]") && !r.PostForm.Has("user[new_password]") {
			s.serveErrorPage(w, http.StatusBadRequest, "/400.html")
			return
		}

		err := s.db.UpdatePassword(r.Context(), user.ID,
			r.PostFormValue("user[existing_password]"), r.PostFormValue("user[new_password]"))
		if err == nil {
			s.redirect(w, r, "/settings", flashNotice, "Password was successfully changed.")
			return
		}

		var invalid store.ValidationError
		if !errors.As(err, &invalid) {
			s.serverError(w, r, err)
			return
		}

		s.render(w, r, http.StatusUnprocessableEntity, layoutApplication, pageSettingsPasswordEdit,
			settingsPasswordData{
				Nav:             settingsNav(user, "Main"),
				Errors:          invalid.FullMessages(),
				ExistingInvalid: invalid.On("existing_password"),
				NewInvalid:      invalid.On("new_password"),
			})
	})
}

// noun is String#pluralize for the regular nouns these pages count, without
// the number in front of it.
func noun(number int, word string) string {
	if number == 1 {
		return word
	}
	return word + "s"
}

// formValueOr reads a field, falling back to what is already stored when the
// body did not carry it at all. An empty field is a value, not an absence.
func formValueOr(r *http.Request, name, fallback string) string {
	if !r.PostForm.Has(name) {
		return fallback
	}
	return r.PostForm.Get(name)
}
