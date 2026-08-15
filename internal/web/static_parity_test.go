package web

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DELETE THIS FILE IN PHASE 9, with the Rails tree it compares against.
//
// The CSS, the JavaScript and the icons ship byte-identical: they are the
// same files, copied into static/ so the Go binary can embed them. While both
// trees exist, one of them can be edited and the other forgotten, and the
// symptom would be a page that looks right in `bin/dev` and wrong in
// production. This walks the embedded copy and insists every file still equals
// the one it came from.
//
// The vendored Turbo and Stimulus builds are not checked: they come out of the
// gems, whose path depends on the machine, and they change only when the
// Gemfile does.
func TestEmbeddedAssetsMatchTheRailsTree(t *testing.T) {
	// Where each embedded directory came from, relative to the repository
	// root — which is two levels up from internal/web.
	sources := map[string]string{
		"static/css":    "app/assets/stylesheets",
		"static/js":     "app/javascript",
		"static/icons":  "app/assets/icons",
		"static/public": "public",
	}

	root := filepath.Join("..", "..")

	for embedded, source := range sources {
		err := fs.WalkDir(staticFS, embedded, func(name string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}

			ours, err := staticFS.ReadFile(name)
			if err != nil {
				return err
			}

			relative := strings.TrimPrefix(name, embedded+"/")
			theirs, err := os.ReadFile(filepath.Join(root, source, filepath.FromSlash(relative)))
			if err != nil {
				t.Errorf("%s has no counterpart in %s: %v", name, source, err)
				return nil
			}

			if !bytes.Equal(ours, theirs) {
				t.Errorf("%s differs from %s/%s; copy it across rather than editing one of them",
					name, source, relative)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", embedded, err)
		}
	}
}

// And the other direction: a file added to the Rails tree and not copied
// across would be missing from every page the Go app serves.
func TestTheRailsTreeHasNoAssetsTheBinaryIsMissing(t *testing.T) {
	sources := map[string]string{
		"app/assets/stylesheets": "static/css",
		"app/javascript":         "static/js",
		"app/assets/icons":       "static/icons",
	}

	root := filepath.Join("..", "..")

	for source, embedded := range sources {
		base := filepath.Join(root, source)
		err := filepath.WalkDir(base, func(name string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			relative, err := filepath.Rel(base, name)
			if err != nil {
				return err
			}
			if _, err := staticFS.ReadFile(embedded + "/" + filepath.ToSlash(relative)); err != nil {
				t.Errorf("%s/%s is not embedded; copy it into internal/web/%s", source, relative, embedded)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", source, err)
		}
	}
}
