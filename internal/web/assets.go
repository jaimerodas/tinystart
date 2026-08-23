package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"
)

// staticFS carries the CSS, the JavaScript, the icons and the files Rails kept
// in public/. The build embeds them instead of reading them from disk, so the
// binary is the whole deployment. There is no asset directory to copy into the
// image, and no chance that the container serves a stylesheet from a build
// different than the one the handler links to.
//
// The tree under static/ mirrors where the files came from, and the mapping to
// the URLs Rails served is in newAssets:
//
//	css/*.css          → /assets/<name>-<digest>.css
//	js/application.js  → /assets/application-<digest>.js
//	js/controllers/…   → /assets/controllers/<name>-<digest>.js
//	js/lib/…           → /assets/lib/<name>-<digest>.js
//	vendor/*.js        → /assets/<name>-<digest>.js   (turbo, stimulus)
//	icons/*.svg        → not served at all — inlined by the icon template func
//	public/*           → /<name>, the way Rails served public/
//
//go:embed static
var staticFS embed.FS

// asset is one fingerprinted file: what it is called in a template, where the
// browser fetches it, and the bytes themselves.
type asset struct {
	// logical is the name a template asks for — "application.css",
	// "controllers/index.js" — which is Propshaft's "logical path".
	logical string
	// url is the path the browser fetches, digest and all.
	url string
	// contentType comes from the extension, resolved once rather than on
	// every request.
	contentType string
	body        []byte
}

// assetSet is every fingerprinted file, indexed both ways: by logical name for
// the templates, and by URL for the handler.
type assetSet struct {
	byLogical map[string]*asset
	byURL     map[string]*asset

	// stylesheets and modules are the two ordered lists the layouts need.
	// Order is part of the output, so it is settled here once instead of being
	// re-sorted per request.
	stylesheets []*asset
	modules     []module

	// icons are the inline SVGs, already carrying the aria attributes that
	// ApplicationHelper#icon added on the way out.
	icons map[string]template.HTML

	// public is everything Rails served straight out of public/, keyed by the
	// path it is served at.
	public map[string][]byte
}

// module is one importmap entry: the bare specifier the JavaScript imports and
// the file it resolves to.
type module struct {
	specifier string
	asset     *asset
}

// digest fingerprints a file's contents.
//
// Propshaft's own digest is not reproducible from the bytes alone, because it
// folds in the logical path. Nothing depends on matching it. The digest exists
// to make the URL change when the file does, and any collision-resistant hash
// of the contents does that. So this function uses the first eight hex digits
// of the SHA-256. That is the same shape, eight characters, in the same place,
// and the parity check normalizes the digits away.
func digest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])[:8]
}

// newAssets reads the embedded tree once, at startup, and computes every
// digest and every URL. Doing this here, rather than lazily, means a missing
// file is a boot failure and not a broken page an hour later. It also means
// no request ever hashes anything.
func newAssets() (*assetSet, error) {
	set := &assetSet{
		byLogical: map[string]*asset{},
		byURL:     map[string]*asset{},
		icons:     map[string]template.HTML{},
		public:    map[string][]byte{},
	}

	// The prefixes each directory maps onto. "" means the file keeps its own
	// name at the top of /assets. That is what Rails did for the vendored
	// turbo and stimulus builds, and for application.js.
	served := []struct{ dir, prefix string }{
		{"static/css", ""},
		{"static/js", ""},
		{"static/vendor", ""},
	}
	for _, source := range served {
		err := fs.WalkDir(staticFS, source.dir, func(name string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			body, err := staticFS.ReadFile(name)
			if err != nil {
				return err
			}
			// static/js/controllers/flash_controller.js becomes the logical
			// name controllers/flash_controller.js: the directory below the
			// source root is part of the name, which is how pin_all_from's
			// "under:" prefix worked.
			logical := path.Join(source.prefix, strings.TrimPrefix(name, source.dir+"/"))
			set.add(logical, body)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	if err := set.loadIcons(); err != nil {
		return nil, err
	}
	if err := set.loadPublic(); err != nil {
		return nil, err
	}
	set.buildLists()
	return set, nil
}

func (s *assetSet) add(logical string, body []byte) {
	extension := path.Ext(logical)
	url := "/assets/" + strings.TrimSuffix(logical, extension) + "-" + digest(body) + extension

	contentType := mime.TypeByExtension(extension)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	a := &asset{logical: logical, url: url, contentType: contentType, body: body}
	s.byLogical[logical] = a
	s.byURL[url] = a
}

// loadIcons reads app/assets/icons and applies the one change that
// ApplicationHelper#icon made. Every glyph in this app sits inside a control
// that carries its own label, so the <svg> is decorative and says so. Doing
// this here, rather than in the files, means a new icon cannot forget to.
func (s *assetSet) loadIcons() error {
	entries, err := fs.ReadDir(staticFS, "static/icons")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		body, err := staticFS.ReadFile("static/icons/" + entry.Name())
		if err != nil {
			return err
		}
		marked := strings.Replace(string(body), "<svg", `<svg aria-hidden="true" focusable="false"`, 1)
		s.icons[strings.TrimSuffix(entry.Name(), ".svg")] = template.HTML(marked) //nolint:gosec // the SVGs are ours, embedded at build time
	}
	return nil
}

// loadPublic keeps the files that Rails served out of public/ at the paths it
// served them at: /icon.png, /robots.txt and the static error pages. The
// recovery middleware and the not-found handler render these pages.
func (s *assetSet) loadPublic() error {
	entries, err := fs.ReadDir(staticFS, "static/public")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		body, err := staticFS.ReadFile("static/public/" + entry.Name())
		if err != nil {
			return err
		}
		s.public["/"+entry.Name()] = body
	}
	return nil
}

// buildLists settles the two orders the <head> depends on.
//
// stylesheet_link_tag :app links every file in app/assets/stylesheets
// alphabetically. The importmap is application, the three vendored packages,
// then pin_all_from over controllers/ and lib/ — also alphabetical, by file
// name. Both orders are visible in the HTML, so they are part of what the
// parity check compares.
func (s *assetSet) buildLists() {
	for logical, a := range s.byLogical {
		if path.Ext(logical) == ".css" {
			s.stylesheets = append(s.stylesheets, a)
		}
	}
	slices.SortFunc(s.stylesheets, func(a, b *asset) int {
		return strings.Compare(a.logical, b.logical)
	})

	// The four explicit pins, in config/importmap.rb's order.
	for _, pin := range []struct{ specifier, logical string }{
		{"application", "application.js"},
		{"@hotwired/turbo-rails", "turbo.min.js"},
		{"@hotwired/stimulus", "stimulus.min.js"},
		{"@hotwired/stimulus-loading", "stimulus-loading.js"},
	} {
		if a := s.byLogical[pin.logical]; a != nil {
			s.modules = append(s.modules, module{pin.specifier, a})
		}
	}

	// Then pin_all_from, which walks each directory in name order. index.js is
	// the directory's own name — "controllers", not "controllers/index". It
	// still sorts under "i", which is why the bare specifier lands between
	// grid_keyboard_controller and inline_form_controller.
	for _, prefix := range []string{"controllers", "lib"} {
		var names []string
		for logical := range s.byLogical {
			if path.Dir(logical) == prefix && path.Ext(logical) == ".js" {
				names = append(names, logical)
			}
		}
		slices.Sort(names)
		for _, logical := range names {
			specifier := strings.TrimSuffix(logical, ".js")
			if path.Base(specifier) == "index" {
				specifier = prefix
			}
			s.modules = append(s.modules, module{specifier, s.byLogical[logical]})
		}
	}
}

// contentTypeFor names the media type of one of the files served at the root.
// mime.TypeByExtension knows all of them. The fallback is only there so that a
// file with no extension cannot be served as nothing at all.
func contentTypeFor(name string) string {
	if t := mime.TypeByExtension(path.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

// path returns the fingerprinted URL for a logical name. A name that is not
// there is a programming error, not a runtime condition. The files are
// embedded, so either the template asked for something that does not exist,
// or the build lost a file. Returning the name unchanged makes it show up as
// a 404 in the page rather than as a panic in production.
func (s *assetSet) path(logical string) string {
	if a := s.byLogical[logical]; a != nil {
		return a.url
	}
	return "/assets/" + logical
}

// stylesheetTags is stylesheet_link_tag :app: every stylesheet in the
// directory, in name order. Turbo tracks each one, so a deploy that changes
// one forces a full reload. The tags are joined with a newline and nothing
// else, exactly as ActionView joined them.
func (s *assetSet) stylesheetTags() template.HTML {
	var out strings.Builder
	for i, a := range s.stylesheets {
		if i > 0 {
			out.WriteString("\n")
		}
		fmt.Fprintf(&out, `<link rel="stylesheet" href="%s" data-turbo-track="reload" />`, a.url)
	}
	return template.HTML(out.String()) //nolint:gosec // every part is a path this package computed
}

// importmapTags is javascript_importmap_tags: the import map itself, a
// modulepreload for every module in it, and the one line that starts the app.
//
// The JSON is written by hand rather than marshalled. An import map is an
// ordered document to a reader, even though it is unordered to a parser.
// Go's encoder also sorts map keys, which moves application to the middle
// and adds noise to the diff against Rails' output. Every value is a path
// this package built, so there is nothing to escape.
func (s *assetSet) importmapTags() template.HTML {
	var out strings.Builder
	out.WriteString(`<script type="importmap" data-turbo-track="reload">{` + "\n")
	out.WriteString("  \"imports\": {\n")
	for i, m := range s.modules {
		comma := ","
		if i == len(s.modules)-1 {
			comma = ""
		}
		fmt.Fprintf(&out, "    %q: %q%s\n", m.specifier, m.asset.url, comma)
	}
	out.WriteString("  }\n}</script>\n")

	for _, m := range s.modules {
		fmt.Fprintf(&out, "<link rel=\"modulepreload\" href=%q>\n", m.asset.url)
	}
	out.WriteString(`<script type="module">import "application"</script>`)
	return template.HTML(out.String()) //nolint:gosec // as above: paths this package built
}

// icon is ApplicationHelper#icon. A name with no file behind it renders
// nothing, which is what the Ruby did too. The caller is a layout, and a
// missing glyph must not take the page down with it.
func (s *assetSet) icon(name string) template.HTML {
	return s.icons[name]
}

// handleAsset serves the fingerprinted files. The URL contains the digest of
// the contents, so the answer for a given URL can never change. The cache
// headers say so: a year, immutable, no revalidation. A URL that is not in
// the table is a 404, rather than a fallback to the undigested file. A wrong
// digest means a stale page, and serving it anyway hides that.
func (s *assetSet) handleAsset() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := s.byURL[r.URL.Path]
		if a == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", a.contentType)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		// The zero time tells ServeContent that there is no modification date to
		// report. That is true: the file's identity is its digest. A conditional
		// request against a date is only ever a slower way of asking a question
		// the URL has already answered.
		http.ServeContent(w, r, a.logical, time.Time{}, bytes.NewReader(a.body))
	})
}
