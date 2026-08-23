package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// upload posts a file the way the form does, which is the only way the
// importer is ever reached: multipart, one field, named file.
func (ts *testServer) upload(body []byte) *response {
	ts.t.Helper()

	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	part, err := writer.CreateFormFile("file", "start_page.yml")
	if err != nil {
		ts.t.Fatalf("building the upload: %v", err)
	}
	if _, err := part.Write(body); err != nil {
		ts.t.Fatalf("writing the upload: %v", err)
	}
	if err := writer.Close(); err != nil {
		ts.t.Fatalf("closing the upload: %v", err)
	}

	req, err := http.NewRequestWithContext(ts.t.Context(), http.MethodPost,
		ts.http.URL+"/settings/import_export", &buffer)
	if err != nil {
		ts.t.Fatalf("building the request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return ts.do(req)
}

// page is the user's whole start page as "column/group/title" lines, which is
// enough to say what an import did without asserting on ids.
func (ts *testServer) page(userID int64) []string {
	ts.t.Helper()
	layout, err := ts.db.StartPageLayout(ts.t.Context(), userID)
	if err != nil {
		ts.t.Fatalf("reading the start page: %v", err)
	}

	var lines []string
	for _, column := range layout.Columns {
		for _, group := range column.Groups {
			if len(group.Items) == 0 {
				lines = append(lines, strconv.Itoa(column.Number)+"/"+group.Name)
				continue
			}
			for _, item := range group.Items {
				lines = append(lines, strconv.Itoa(column.Number)+"/"+group.Name+"/"+item.Title)
			}
		}
	}
	return lines
}

// fixtureFile is the sample export the Rails suite used, inherited when that
// suite was deleted.
func fixtureFile(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/start_page.yml")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	return body
}

func TestImportExportRequiresAuthentication(t *testing.T) {
	ts := newTestServer(t)
	ts.get("/settings/import_export").assertRedirect("/session/new")
	ts.get("/settings/export").assertRedirect("/session/new")
	ts.upload(fixtureFile(t)).assertRedirect("/session/new")
}

// One control, and it says what it exports — there is no "Export" heading
// above it repeating the word. The file field has no visible label either, so
// its accessible name has to come from aria-label.
func TestImportExportPage(t *testing.T) {
	ts, _ := settingsServer(t)

	ts.get("/settings/import_export").
		assertStatus(http.StatusOK).
		assertContains(`href="/settings/export">Export all groups and links</a>`).
		assertNotContains("<h3>Export</h3>").
		assertContains(`enctype="multipart/form-data"`).
		assertContains(`aria-label="Start page file to import"`).
		assertContains("data-turbo-confirm")
}

// The layouts once built the title with Array#join, which dropped the
// html_safe flag and escaped the ampersand twice. This is the only page whose
// title contains a character that shows it.
func TestImportExportTitleIsEscapedOnce(t *testing.T) {
	ts, _ := settingsServer(t)

	ts.get("/settings/import_export").
		assertContains("<title>Import &amp; Export - TinyStart</title>").
		assertNotContains("&amp;amp;")
}

func TestExportSendsThePageAsAYamlAttachment(t *testing.T) {
	ts, user := settingsServer(t)
	group := ts.newGroup(user.ID, "Lo de siempre", 1)
	ts.newItem(user.ID, group.ID, "Fastmail", "https://app.fastmail.com")

	resp := ts.get("/settings/export").assertStatus(http.StatusOK)

	if got := resp.Header.Get("Content-Type"); got != "text/yaml" {
		t.Errorf("Content-Type = %q, want text/yaml", got)
	}
	want := `attachment; filename="tinystart-start-page-` + ts.clock.Now().Format("2006-01-02") + `.yml"`
	if got := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(got, want) {
		t.Errorf("Content-Disposition = %q, want it to start with %q", got, want)
	}
	if !strings.Contains(resp.body, "Lo de siempre") || !strings.Contains(resp.body, "https://app.fastmail.com") {
		t.Errorf("body = %s", resp.body)
	}
}

// The date is the application's, not the machine's. Rails dated the file with
// Date.current in UTC. An export made at eight in the evening in Mexico City
// is dated the next day. A file whose name and header disagreed with the one
// Rails wrote breaks the format's one promise.
func TestExportDatesTheFileInUTC(t *testing.T) {
	ts, _ := settingsServer(t)
	ts.clock.set(time.Date(2026, 8, 15, 20, 0, 0, 0, time.FixedZone("CST", -6*60*60)))

	resp := ts.get("/settings/export").assertStatus(http.StatusOK)

	if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, "2026-08-16") {
		t.Errorf("Content-Disposition = %q, want the UTC date", got)
	}
	if !strings.Contains(resp.body, "export - 2026-08-16") {
		t.Errorf("header = %q, want the UTC date", strings.SplitN(resp.body, "\n", 2)[0])
	}
}

func TestExportSendsOnlyYourOwnPage(t *testing.T) {
	ts := newTestServer(t)
	first := ts.createUser("one@example.com")
	second := ts.createApprovedUser("two@example.com")
	group := ts.newGroup(second.ID, "Theirs", 1)
	ts.newItem(second.ID, group.ID, "Theirs", "https://theirs.example")
	ts.signIn(first.Email)

	ts.get("/settings/export").
		assertStatus(http.StatusOK).
		assertNotContains("theirs.example")
}

func TestExportOfAnEmptyPageStillAnswers(t *testing.T) {
	ts, _ := settingsServer(t)
	ts.get("/settings/export").assertStatus(http.StatusOK)
}

func TestImportReplacesThePageAndSaysWhatArrived(t *testing.T) {
	ts, user := settingsServer(t)
	ts.newGroup(user.ID, "Gone after the import", 1)

	ts.upload(fixtureFile(t)).assertRedirect("/settings/import_export")
	ts.get("/settings/import_export").
		assertContains("Imported 6 links in 3 groups across 2 columns.")

	if got := ts.reloadUser(user).Columns; got != 2 {
		t.Errorf("columns = %d, want the file's 2", got)
	}
	want := []string{
		"1/Test 2/NaN Fonts", "1/Test 2/Feedbin",
		"2/Lo de siempre/My Synology Admin", "2/Lo de siempre/Fastmail",
		"2/Otras cosas/YouTube", "2/Otras cosas/LinkedIn",
	}
	assertPageIs(t, ts.page(user.ID), want)
}

func TestImportIsForTheSignedInUserAndNobodyElse(t *testing.T) {
	ts := newTestServer(t)
	first := ts.createUser("one@example.com")
	second := ts.createApprovedUser("two@example.com")
	ts.newGroup(second.ID, "Theirs", 1)
	ts.signIn(first.Email)

	ts.upload(fixtureFile(t))

	assertPageIs(t, ts.page(second.ID), []string{"1/Theirs"})
}

// Every refusal leaves the page exactly as it was: the checks happen before
// the write, and the write is one transaction.
func TestImportRefusals(t *testing.T) {
	tooBig := bytes.Repeat([]byte("# padding\n"), 100_000)

	tests := []struct {
		name string
		body []byte
		want string
	}{
		{"a file that is too large to be a start page", tooBig,
			"That file is too large to be a start page."},
		{"a file that is not UTF-8", []byte("1:\n- name: \xc3\x28Bad\n  items: {}\n"),
			"That file isn&#39;t valid UTF-8 text."},
		{"a file whose tile has no URL", []byte("1:\n- name: Broken\n  items:\n    Bare: example.com\n"),
			// html/template writes a double quote as &#34; where ERB wrote
			// &quot; — the same character, and the same DOM.
			"Nothing was imported: the link &#34;Bare&#34; (example.com) in &#34;Broken&#34; was rejected: Url must be a valid URL"},
		{"a file with no groups in it", []byte("1: []\n"),
			"Nothing was imported: that file has no groups in it"},
		{"a file that is not YAML at all", []byte("{{{\n"),
			"Nothing was imported: that file could not be read as YAML"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ts, user := settingsServer(t)
			ts.newGroup(user.ID, "Still here", 1)

			ts.upload(test.body).assertRedirect("/settings/import_export")
			ts.get("/settings/import_export").assertContains(test.want)

			assertPageIs(t, ts.page(user.ID), []string{"1/Still here"})
		})
	}
}

func TestImportWithNoFileAtAll(t *testing.T) {
	ts, user := settingsServer(t)
	ts.newGroup(user.ID, "Still here", 1)

	ts.post("/settings/import_export", nil).assertRedirect("/settings/import_export")
	ts.get("/settings/import_export").assertContains("Choose a file to import first.")

	assertPageIs(t, ts.page(user.ID), []string{"1/Still here"})
}

// The warning rides along with the success: the import happened, and the
// counts in the file's header did not describe what arrived. It cannot be a
// refusal. A tile deleted by hand lowers the count exactly as a collapsed
// duplicate key does, and refusing blocks the workflow the format is for.
func TestImportReportsAHeaderThatNoLongerMatches(t *testing.T) {
	ts, _ := settingsServer(t)
	body := bytes.Replace(fixtureFile(t),
		[]byte("# 2 columns, 3 groups, 6 tiles"), []byte("# 2 columns, 3 groups, 9 tiles"), 1)

	ts.upload(body)

	ts.get("/settings/import_export").
		assertContains("Imported 6 links in 3 groups across 2 columns. Its header describes " +
			"2 columns, 3 groups and 9 tiles, but 2 columns, 3 groups and 6 tiles came in")
}

// The loop the format exists for: export, edit, import back.
func TestAFileItExportedImportsAgainUnchanged(t *testing.T) {
	ts, user := settingsServer(t)
	group := ts.newGroup(user.ID, "Lo de siempre", 1)
	ts.newItem(user.ID, group.ID, "Fastmail", "https://app.fastmail.com")

	exported := ts.get("/settings/export").body
	ts.upload([]byte(exported))

	ts.get("/settings/import_export").assertContains("Imported 1 link in 1 group across 1 column.")
	assertPageIs(t, ts.page(user.ID), []string{"1/Lo de siempre/Fastmail"})
}

// The real data is in Spanish, so an upload that arrives mangled is worth
// catching at the door. And one that arrives intact has to stay intact.
func TestImportKeepsAccents(t *testing.T) {
	ts, user := settingsServer(t)

	ts.upload([]byte("1:\n- name: Diseño\n  items:\n    Tipografía: https://a.example\n"))

	assertPageIs(t, ts.page(user.ID), []string{"1/Diseño/Tipografía"})
}

// A body larger than the cap never reaches memory. The reader is capped as
// well as the field, so the refusal happens before the upload finishes
// arriving.
func TestImportDoesNotReadAnEnormousBody(t *testing.T) {
	ts, _ := settingsServer(t)

	ts.upload(bytes.Repeat([]byte("x"), 4*maxImportBytes)).assertRedirect("/settings/import_export")
	ts.get("/settings/import_export").assertContains("That file is too large to be a start page.")
}

func assertPageIs(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("page =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
