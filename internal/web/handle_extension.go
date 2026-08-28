package web

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

// handleExtension is GET /settings/extension.zip: the Chrome extension in
// internal/web/static/chrome/, zipped up for the user to load unpacked.
func (s *Server) handleExtension() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const dir = "static/chrome"

		// Built per request, not once at boot: baseURL's dev fallback reads
		// the request's own Host, which isn't known until one arrives.

		// Built into a buffer, not straight onto the response, so a failure
		// partway through still has a response to answer with instead of a
		// half-written zip and a 200.
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		err := fs.WalkDir(staticFS, dir, func(name string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			body, err := staticFS.ReadFile(name)
			if err != nil {
				return err
			}
			if entry.Name() == "newtab.js" {
				// The checked-in file points at production so the directory
				// loads unpacked in Chrome as-is; a zip built for this
				// server has to point back at it instead.
				body = []byte(strings.ReplaceAll(string(body), "https://start.pati.to", s.baseURL(r)))
			}
			zf, err := zw.Create(strings.TrimPrefix(name, dir+"/"))
			if err != nil {
				return err
			}
			_, err = zf.Write(body)
			return err
		})
		if err == nil {
			err = zw.Close()
		}
		if err != nil {
			s.serverError(w, r, err)
			return
		}

		filename := "tinystart-chrome-extension.zip"
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition",
			fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, filename, filename))
		w.Write(buf.Bytes()) //nolint:errcheck // nothing to do if the browser hangs up
	})
}
