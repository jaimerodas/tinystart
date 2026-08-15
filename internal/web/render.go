package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/jaimerodas/tinystart/internal/store"
)

// templateFS holds every page, layout and partial. Embedding them means the
// binary is self-contained and that a template that does not parse is a boot
// failure — newRenderer parses all of them at startup — rather than a 500 the
// first time someone opens the page it is on.
//
//go:embed templates
var templateFS embed.FS

// The three layouts, named exactly as the files in app/views/layouts are.
const (
	layoutApplication = "application"
	layoutSession     = "session"
	layoutStart       = "start"
)

// renderer is the parsed templates.
//
// Each page gets its own template set rather than all of them sharing one,
// because every page defines a template called "content" and a layout renders
// "content" — two pages in one set would be a redefinition. Parsing the
// layouts and partials again for each page costs a few hundred microseconds,
// once, at boot.
type renderer struct {
	pages map[string]*template.Template
	mail  *template.Template
}

func newRenderer(assets *assetSet) (*renderer, error) {
	funcs := templateFuncs(assets)

	shared := []string{"templates/layouts/*.html", "templates/shared/*.html"}

	pageFiles, err := fs.Glob(templateFS, "templates/pages/*.html")
	if err != nil {
		return nil, err
	}

	r := &renderer{pages: map[string]*template.Template{}}
	for _, file := range pageFiles {
		set, err := template.New(path.Base(file)).Funcs(funcs).ParseFS(templateFS, append(shared, file)...)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", file, err)
		}
		r.pages[strings.TrimSuffix(path.Base(file), ".html")] = set
	}

	r.mail, err = template.New("mail").ParseFS(templateFS, "templates/mail/*")
	if err != nil {
		return nil, fmt.Errorf("parsing mail templates: %w", err)
	}

	return r, nil
}

// templateFuncs is everything a template can call. They are all pure functions
// of the assets, which are fixed at boot, so binding them once here is enough
// and no request has to carry them.
func templateFuncs(assets *assetSet) template.FuncMap {
	return template.FuncMap{
		// asset is Rails' asset_path: a logical name in, a fingerprinted URL
		// out.
		"asset": assets.path,
		// stylesheetTags and importmapTags are stylesheet_link_tag :app and
		// javascript_importmap_tags. See assets.go for what each emits.
		"stylesheetTags": assets.stylesheetTags,
		"importmapTags":  assets.importmapTags,
		// icon is ApplicationHelper#icon.
		"icon": assets.icon,
		// title composes <title>: the page's own name and the app's, or just
		// the app's. Rails did it with safe_join and select(&:present?).
		"title": func(name string) string {
			if name == "" {
				return "TinyStart"
			}
			return name + " - TinyStart"
		},
		// pluralize is ActionView's, in the one place a view uses it: "1
		// error", "2 errors".
		"pluralize": func(count int, word string) string {
			if count == 1 {
				return fmt.Sprintf("%d %s", count, word)
			}
			return fmt.Sprintf("%d %ss", count, word)
		},
	}
}

// view is what every template is handed: the parts of the page that come from
// the request rather than from the handler, plus the handler's own data under
// Data.
//
// Keeping the page's data in a field rather than embedding it means a page
// template says .Data.Email and there is never a question of which struct a
// name came from.
type view struct {
	Title string
	Theme string
	Color string
	Flash []flashMessage
	User  *store.User
	Data  any
}

// render writes one page.
//
// It renders into a buffer first. A template that fails halfway — a nil
// dereference in a field, a function that panics — would otherwise have
// already written a 200 and half a document, and there is no taking that back;
// with the buffer, a failure is still a clean 500.
func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, layout, page string, data any) {
	set := s.templates.pages[page]
	if set == nil {
		s.serverError(w, r, fmt.Errorf("no template named %q", page))
		return
	}

	user := userFrom(r.Context())
	v := view{
		Title: pageTitles[page],
		Theme: themeFor(user),
		Color: colorFor(user),
		Flash: s.takeFlash(w, r),
		User:  user,
		Data:  data,
	}

	var body bytes.Buffer
	if err := set.ExecuteTemplate(&body, layout, v); err != nil {
		s.serverError(w, r, fmt.Errorf("rendering %s: %w", page, err))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(body.Bytes()) //nolint:errcheck // the client hanging up is not something this can act on
}

// pageTitles is content_for(:title), which in this app is always a constant
// and so belongs next to the page names rather than threaded through every
// handler. A page with no entry gets the bare "TinyStart".
var pageTitles = map[string]string{
	"users_new":     "New user",
	"passwords_new": "Forgot your password?",
}

// themeFor and colorFor are ApplicationHelper's theme_data_attribute and
// color_data_attribute: the signed-in user's preference, or the defaults that
// make the sign-in page look like the app before anyone has said who they are.
func themeFor(user *store.User) string {
	if user == nil {
		return "system"
	}
	return user.ThemePreference
}

func colorFor(user *store.User) string {
	if user == nil {
		return "teal"
	}
	return user.ColorPreference
}

// serverError is the one place an unexpected failure is turned into a page: it
// is logged with the request id, and the visitor gets the same static 500 the
// Rails app served.
func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	s.log.ErrorContext(r.Context(), "server error",
		"error", err,
		"method", r.Method,
		"path", r.URL.Path,
		"request_id", requestIDFrom(r.Context()))
	s.serveErrorPage(w, http.StatusInternalServerError, "/500.html")
}

// serveErrorPage writes one of the static pages out of public/. They are plain
// files with no template in them precisely so that they still work when the
// thing that failed is the template layer.
func (s *Server) serveErrorPage(w http.ResponseWriter, status int, page string) {
	body := s.assets.public[page]
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(body) //nolint:errcheck // as above
}
