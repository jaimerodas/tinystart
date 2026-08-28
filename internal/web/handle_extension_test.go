package web

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestExtensionRequiresAuthentication(t *testing.T) {
	ts := newTestServer(t)
	ts.get("/settings/extension.zip").assertRedirect("/sign_in")
}

func TestExtensionZip(t *testing.T) {
	ts, _ := settingsServer(t)

	resp := ts.get("/settings/extension.zip").assertStatus(http.StatusOK)

	if got := resp.Header.Get("Content-Type"); got != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", got)
	}
	if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, "tinystart-chrome-extension.zip") {
		t.Errorf("Content-Disposition = %q, want it to mention tinystart-chrome-extension.zip", got)
	}

	reader, err := zip.NewReader(bytes.NewReader([]byte(resp.body)), int64(len(resp.body)))
	if err != nil {
		t.Fatalf("reading the body as a zip: %v", err)
	}

	var names []string
	files := map[string]*zip.File{}
	for _, file := range reader.File {
		names = append(names, file.Name)
		files[file.Name] = file
	}
	want := []string{"manifest.json", "newtab.html", "newtab.js", "icon16.png", "icon48.png", "icon128.png"}
	slices.Sort(names)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Errorf("entries = %v, want %v", names, want)
	}

	manifest := readZipFile(t, files["manifest.json"])
	var parsed struct {
		ManifestVersion    int `json:"manifest_version"`
		ChromeURLOverrides struct {
			NewTab string `json:"newtab"`
		} `json:"chrome_url_overrides"`
	}
	if err := json.Unmarshal(manifest, &parsed); err != nil {
		t.Fatalf("parsing manifest.json: %v", err)
	}
	if parsed.ManifestVersion != 3 {
		t.Errorf("manifest_version = %d, want 3", parsed.ManifestVersion)
	}
	if parsed.ChromeURLOverrides.NewTab != "newtab.html" {
		t.Errorf("chrome_url_overrides.newtab = %q, want newtab.html", parsed.ChromeURLOverrides.NewTab)
	}

	// The zip is built for this server, so the extension's start page URL
	// has to point back at it rather than at the production site the
	// source file hardcodes.
	script := string(readZipFile(t, files["newtab.js"]))
	if !strings.Contains(script, "https://start.example.com") {
		t.Errorf("newtab.js does not contain the server's host")
	}
	if strings.Contains(script, "start.pati.to") {
		t.Errorf("newtab.js still contains the production host")
	}

	html := string(readZipFile(t, files["newtab.html"]))
	if !strings.Contains(html, `src="newtab.js"`) {
		t.Errorf("newtab.html does not reference newtab.js")
	}
	if strings.Contains(html, "<script>") {
		t.Errorf("newtab.html has an inline script body")
	}
}

func readZipFile(t *testing.T, file *zip.File) []byte {
	t.Helper()
	if file == nil {
		t.Fatal("zip entry not found")
	}
	f, err := file.Open()
	if err != nil {
		t.Fatalf("opening %s: %v", file.Name, err)
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("reading %s: %v", file.Name, err)
	}
	return data
}
