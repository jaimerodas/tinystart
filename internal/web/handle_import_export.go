package web

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"unicode"
	"unicode/utf8"

	"github.com/jaimerodas/tinystart/internal/startpage"
	"github.com/jaimerodas/tinystart/internal/store"
)

// Settings → Import & Export: a user's start page out as the YAML interchange
// format in docs/start-page-format.md, and back in again. The format and the
// reasons behind it are in that file. The work is in internal/startpage.

// maxImportBytes is what a start page can plausibly weigh. It is a few dozen
// tiles. Anything past this is either a mistake or a file that has no
// business being read into memory.
const maxImportBytes = 512 * 1024

// importExportData is the page, which has nothing on it but the nav.
type importExportData struct {
	Nav []settingsNavItem
}

// handleImportExport is GET /settings/import_export.
func (s *Server) handleImportExport() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.render(w, r, http.StatusOK, layoutApplication, pageSettingsImportExport,
			importExportData{Nav: settingsNav(userFrom(r.Context()), "Import & Export")})
	})
}

// handleExport is GET /settings/export.
//
// Its own action rather than a format on the page, because the file is a .yml
// and the route that produced it read better this way. The response is an
// attachment: the link carries data-turbo="false", because Turbo Drive
// intercepts link clicks and has nothing to do with a download.
func (s *Server) handleExport() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := userFrom(r.Context())

		layout, err := s.db.StartPageLayout(r.Context(), user.ID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		// UTC, because the date in the header and in the filename is what
		// Rails' Date.current gave — the application time zone, which is UTC.
		// An export made at eleven at night in Mexico City must not be dated
		// the day before the one the file says it is.
		today := s.now().UTC()
		body, err := startpage.Export(layout, today)
		if err != nil {
			s.serverError(w, r, err)
			return
		}

		filename := "tinystart-start-page-" + today.Format("2006-01-02") + ".yml"
		w.Header().Set("Content-Type", "text/yaml")
		// Both spellings of the filename, which is what send_data wrote: the
		// plain one for every client, and the RFC 5987 one beside it. Neither
		// needs escaping — the name is a date — and neither is built from
		// anything the user typed.
		w.Header().Set("Content-Disposition",
			fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, filename, filename))
		w.Write(body) //nolint:errcheck // nothing to do if the browser hangs up
	})
}

// handleImportCreate is POST /settings/import_export.
//
// Importing replaces the whole page, so a refusal has to change nothing.
// Every check here happens before the write, and the write itself is one
// transaction that either lands or does not.
func (s *Server) handleImportCreate() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := userFrom(r.Context())

		source, ok := s.uploadedSource(w, r)
		if !ok {
			return
		}

		result, err := startpage.Import(source)
		if err != nil {
			s.redirect(w, r, "/settings/import_export", flashAlert, "Nothing was imported: "+err.Error())
			return
		}

		if err := s.db.ReplaceStartPage(r.Context(), user.ID, result.Layout); err != nil {
			// A record the models refused is a sentence about the file, and
			// belongs on the page for the same reason the parse errors do.
			// Anything else means the database write itself failed.
			var rejected *store.RejectedError
			if !errors.As(err, &rejected) {
				s.serverError(w, r, err)
				return
			}
			s.redirect(w, r, "/settings/import_export", flashAlert, "Nothing was imported: "+rejected.Error())
			return
		}

		// The warning rides along with the success: the import happened, and
		// the counts in the file's header did not describe what arrived.
		notice := imported(result.Layout.Counts())
		if result.Warning != "" {
			notice += " " + result.Warning
		}
		s.redirect(w, r, "/settings/import_export", flashNotice, notice)
	})
}

// uploadedSource returns the file's contents, or answers the request itself
// and reports false.
func (s *Server) uploadedSource(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	// The cap is on the reader as well as on the field, so a body that claims
	// to be a start page and is a gigabyte never reaches memory. This gives
	// the multipart parser a little room above it for the boundaries and
	// headers.
	r.Body = http.MaxBytesReader(w, r.Body, maxImportBytes+4096)
	if err := r.ParseMultipartForm(maxImportBytes); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, s.refuse(w, r, "that file is too large to be a start page.")
		}
		return nil, s.refuse(w, r, "choose a file to import first.")
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		return nil, s.refuse(w, r, "choose a file to import first.")
	}
	defer file.Close()

	if header.Size > maxImportBytes {
		return nil, s.refuse(w, r, "that file is too large to be a start page.")
	}

	source, err := io.ReadAll(file)
	if err != nil {
		return nil, s.refuse(w, r, "that file is too large to be a start page.")
	}
	// The real data is in Spanish, and an upload is bytes. Without this
	// check, a file that is not UTF-8 imports mangled names instead of
	// failing.
	if !utf8.Valid(source) {
		return nil, s.refuse(w, r, "that file isn't valid UTF-8 text.")
	}
	return source, true
}

// refuse sends the visitor back to the page with the reason, capitalised the
// way String#upcase_first did — the messages read as the end of a sentence
// that starts elsewhere in the code, and as a whole one on the page.
//
// It always reports false, so a caller can `return nil, s.refuse(…)`.
func (s *Server) refuse(w http.ResponseWriter, r *http.Request, message string) bool {
	s.redirect(w, r, "/settings/import_export", flashAlert, upcaseFirst(message))
	return false
}

// imported is the notice a successful import leaves.
func imported(counts startpage.Counts) string {
	return fmt.Sprintf("Imported %d %s in %d %s across %d %s.",
		counts.Items, noun(counts.Items, "link"),
		counts.Groups, noun(counts.Groups, "group"),
		counts.Columns, noun(counts.Columns, "column"))
}

// upcaseFirst is ActiveSupport's String#upcase_first: the first character
// only, and the rest left exactly as it is.
func upcaseFirst(text string) string {
	first, width := utf8.DecodeRuneInString(text)
	if width == 0 {
		return text
	}
	return string(unicode.ToUpper(first)) + text[width:]
}
