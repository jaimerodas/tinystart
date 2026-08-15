package startpage

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

// sample is the file from docs/start-page-format.md, header and all — the same
// bytes as testdata/start_page.yml, which is the fixture the Rails app's own
// tests read.
const sample = `# tinylinks start page export - 2026-08-10
# 2 columns, 3 groups, 6 tiles
# format: see docs/start-page-format.md in tinystart
---
1:
- name: Test 2
  items:
    NaN Fonts: https://nanfonts.com
    Feedbin: https://feedbin.com
2:
- name: Lo de siempre
  items:
    My Synology Admin: https://synology.local
    Fastmail: https://app.fastmail.com
- name: Otras cosas
  items:
    YouTube: https://youtube.com
    LinkedIn: https://linkedin.com
`

// The page the sample describes, as the tests talk about a page.
var samplePage = []string{
	"1/0 Test 2: NaN Fonts, Feedbin",
	"2/0 Lo de siempre: My Synology Admin, Fastmail",
	"2/1 Otras cosas: YouTube, LinkedIn",
}

// file wraps a body in a header whose counts are right for it, so that a test
// about anything other than the header does not have to count for itself.
func file(body string) string {
	layout, err := Import([]byte("---\n" + body))
	if err != nil {
		// A test asking for a header on a file that does not parse wants the
		// counts to be zero rather than an explosion.
		return "# test file\n# 0 columns, 0 groups, 0 tiles\n---\n" + body
	}
	counts := layout.Layout.Counts()
	return fmt.Sprintf("# test file\n# %s, %s, %s\n---\n%s",
		pluralize(counts.Columns, "column"),
		pluralize(counts.Groups, "group"),
		pluralize(counts.Items, "tile"), body)
}

// summarize renders a layout the way these tests talk about one: a line per
// group, in the order they would be created.
func summarize(layout Layout) []string {
	var lines []string
	for _, column := range layout.Columns {
		for position, group := range column.Groups {
			titles := make([]string, len(group.Items))
			for i, item := range group.Items {
				titles[i] = item.Title
			}
			lines = append(lines, fmt.Sprintf("%d/%d %s: %s",
				column.Number, position, group.Name, strings.Join(titles, ", ")))
		}
	}
	return lines
}

func imported(t *testing.T, source string) Result {
	t.Helper()

	result, err := Import([]byte(source))
	if err != nil {
		t.Fatalf("importing: %v", err)
	}
	return result
}

func assertPage(t *testing.T, layout Layout, want ...string) {
	t.Helper()

	if got := summarize(layout); !reflect.DeepEqual(got, want) {
		t.Errorf("page =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func assertRefused(t *testing.T, source, want string) {
	t.Helper()

	_, err := Import([]byte(source))
	if err == nil {
		t.Fatalf("importing %q was supposed to be refused", source)
	}
	if err.Error() != want {
		t.Errorf("refusal = %q, want %q", err.Error(), want)
	}
}

// The table in docs/start-page-format.md, asserted.
func TestImportReadsTheDocumentedFile(t *testing.T) {
	result := imported(t, sample)

	assertPage(t, result.Layout, samplePage...)
	if result.Layout.Width != 2 {
		t.Errorf("width = %d, want 2", result.Layout.Width)
	}
	if want := (Counts{Columns: 2, Groups: 3, Items: 6}); result.Layout.Counts() != want {
		t.Errorf("counts = %+v, want %+v", result.Layout.Counts(), want)
	}
	if result.Warning != "" {
		t.Errorf("warning = %q, want none", result.Warning)
	}
}

// The same file as the Rails app's fixture, so that both halves of the rewrite
// are reading the same bytes.
func TestImportReadsTheFixtureFile(t *testing.T) {
	source, err := os.ReadFile("testdata/start_page.yml")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}

	result := imported(t, string(source))
	assertPage(t, result.Layout, samplePage...)
}

func TestImportKeepsTheFilesOrderAndUrls(t *testing.T) {
	result := imported(t, sample)

	items := result.Layout.Columns[0].Groups[0].Items
	want := []Item{
		{Title: "NaN Fonts", URL: "https://nanfonts.com"},
		{Title: "Feedbin", URL: "https://feedbin.com"},
	}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("items = %+v, want %+v", items, want)
	}
}

func TestImportKeepsAccentedNames(t *testing.T) {
	result := imported(t, file("1:\n- name: Diseño\n  items:\n    Tipografía: https://a.example\n"))

	assertPage(t, result.Layout, "1/0 Diseño: Tipografía")
}

// The single most likely way to get this wrong: iterating the values would put
// "Right" in column 2 and shift the page left.
func TestImportReadsTheColumnFromTheKey(t *testing.T) {
	result := imported(t, file(`1:
- name: Left
  items:
    L: https://l.example
3:
- name: Right
  items:
    R: https://r.example
`))

	assertPage(t, result.Layout, "1/0 Left: L", "3/0 Right: R")
	if result.Layout.Width != 3 {
		t.Errorf("width = %d, want 3", result.Layout.Width)
	}
}

// Columns come back in ascending order however they were written, because that
// is the order they have to be created in.
func TestImportSortsTheColumns(t *testing.T) {
	result := imported(t, file(`3:
- name: Right
  items: {}
1:
- name: Left
  items: {}
`))

	assertPage(t, result.Layout, "1/0 Left: ", "3/0 Right: ")
}

func TestImportAcceptsAGroupWithNoTiles(t *testing.T) {
	result := imported(t, file("1:\n- name: Empty\n  items: {}\n"))

	assertPage(t, result.Layout, "1/0 Empty: ")
}

// A hand edit can leave an unquoted key that YAML does not hand back as a
// String. The format doc says coerce rather than reject.
func TestImportCoercesANumericTitle(t *testing.T) {
	result := imported(t, file("1:\n- name: Group\n  items:\n    123: https://a.example\n"))

	assertPage(t, result.Layout, "1/0 Group: 123")
}

// The emitter keeps the last of two identical keys and says nothing, which is
// the one kind of damage a hand edit does silently — the tile really is gone.
func TestImportCollapsesARepeatedTitle(t *testing.T) {
	result := imported(t, "---\n1:\n- name: Group\n  items:\n    Same: https://a.example\n    Same: https://b.example\n")

	assertPage(t, result.Layout, "1/0 Group: Same")
	if url := result.Layout.Columns[0].Groups[0].Items[0].URL; url != "https://b.example" {
		t.Errorf("url = %q, want the last of the two", url)
	}
}

func TestImportRefusals(t *testing.T) {
	const emptyFile = "that file has no groups in it — importing it would only empty " +
		"your start page, so nothing was changed"

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "a column past the six a page allows",
			source: file("7:\n- name: Too far\n  items:\n    A: https://a.example\n"),
			want:   "column 7 is outside the 1–6 columns a start page can have",
		},
		{
			name:   "a column of zero",
			source: file("0:\n- name: Nowhere\n  items:\n    A: https://a.example\n"),
			want:   "column 0 is outside the 1–6 columns a start page can have",
		},
		{
			name:   "a column that is not a list of groups",
			source: file("1: not a list\n"),
			want:   "column 1 should be a list of groups, but it isn't",
		},
		{
			name:   "a group with no name",
			source: file("1:\n- items:\n    A: https://a.example\n"),
			want:   "the group at position 1 of column 1 has no name",
		},
		{
			name:   "a group whose name is only spaces",
			source: file("1:\n- name: '   '\n  items: {}\n"),
			want:   "the group at position 1 of column 1 has no name",
		},
		{
			name:   "the second group of a column",
			source: file("1:\n- name: Fine\n  items: {}\n- items: {}\n"),
			want:   "the group at position 2 of column 1 has no name",
		},
		{
			name:   "a group whose items are not a mapping",
			source: file("1:\n- name: Group\n  items: nope\n"),
			want: "the group \"Group\" has no items mapping — " +
				"a group with no tiles is written as `items: {}`",
		},
		{
			name:   "a group with no items key at all",
			source: file("1:\n- name: Group\n"),
			want: "the group \"Group\" has no items mapping — " +
				"a group with no tiles is written as `items: {}`",
		},
		{
			name:   "a file that is not a mapping",
			source: "--- just a string\n",
			want: "that file isn't a mapping of column numbers to groups — " +
				"see docs/start-page-format.md",
		},
		{
			name:   "a file that is a list",
			source: "---\n- one\n- two\n",
			want: "that file isn't a mapping of column numbers to groups — " +
				"see docs/start-page-format.md",
		},
		{
			// Every key in this format is an Integer, so a String key is a
			// later format with an envelope rather than a broken file.
			name:   "a version envelope",
			source: "version: 2\ncolumns:\n  1:\n  - name: Group\n    items: {}\n",
			want: "that file looks like a newer format than this app can read: " +
				"every top-level key should be a column number",
		},
		{
			name:   "an empty file",
			source: "",
			want:   emptyFile,
		},
		{
			name:   "an empty mapping",
			source: "--- {}\n",
			want:   emptyFile,
		},
		{
			// `1: []` is a mapping with a column in it and no groups anywhere:
			// a legal instruction to delete the page, which is never what
			// picking a file meant.
			name:   "a column that holds no groups",
			source: "1: []\n",
			want:   emptyFile,
		},
		{
			name:   "two columns that hold no groups",
			source: file("1: []\n2: []\n"),
			want:   emptyFile,
		},
		{
			name:   "YAML that does not parse",
			source: "1:\n- name: [unclosed\n",
			want: "that file could not be read as YAML — " +
				"line 1: did not find expected ',' or ']'",
		},
		{
			name:   "an alias",
			source: "1: &ref\n- name: Group\n  items:\n    A: https://a.example\n2: *ref\n",
			want: "that file could not be read as YAML — " +
				"aliases are not allowed in a start page file",
		},
		{
			name:   "a tagged object",
			source: "1:\n- name: !ruby/object:Struct {}\n  items: {}\n",
			want: "that file could not be read as YAML — !ruby/object:Struct is not allowed " +
				"in a start page file, which holds only text, numbers, lists and mappings",
		},
		{
			name:   "an unquoted date",
			source: "1:\n- name: Group\n  items:\n    2026-01-01: https://a.example\n",
			want: "that file could not be read as YAML — !!timestamp is not allowed " +
				"in a start page file, which holds only text, numbers, lists and mappings",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertRefused(t, test.source, test.want)
		})
	}
}

// The counts are a check on a file that offers them, not a requirement — and
// the check has to survive everything that can sit above them.
func TestImportHeaderCountWarning(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			// The tile really is gone; noticing is the whole point.
			name: "a collapsed duplicate title",
			source: "# 1 column, 1 group, 2 tiles\n---\n1:\n- name: Group\n  items:\n" +
				"    Same: https://a.example\n    Same: https://b.example\n",
			want: "Its header describes 1 column, 1 group and 2 tiles, but 1 column, 1 group " +
				"and 1 tile came in — expected if you edited the file, worth a look if you didn't.",
		},
		{
			// It cannot be a refusal: deleting a tile by hand lowers the count
			// in exactly the way a collapsed key does, and the file cannot say
			// which happened.
			name: "a hand edit that removed a tile",
			source: "# tinystart start page export - 2026-08-10\n# 1 column, 1 group, 2 tiles\n---\n" +
				"1:\n- name: Group\n  items:\n    Kept: https://a.example\n",
			want: "Its header describes 1 column, 1 group and 2 tiles, but 1 column, 1 group " +
				"and 1 tile came in — expected if you edited the file, worth a look if you didn't.",
		},
		{
			name:   "a group count that does not match",
			source: "# 1 column, 2 groups, 1 tile\n---\n1:\n- name: Group\n  items:\n    A: https://a.example\n",
			want: "Its header describes 1 column, 2 groups and 1 tile, but 1 column, 1 group " +
				"and 1 tile came in — expected if you edited the file, worth a look if you didn't.",
		},
		{
			name:   "a column count that does not match",
			source: "# 2 columns, 1 group, 1 tile\n---\n1:\n- name: Group\n  items:\n    A: https://a.example\n",
			want: "Its header describes 2 columns, 1 group and 1 tile, but 1 column, 1 group " +
				"and 1 tile came in — expected if you edited the file, worth a look if you didn't.",
		},
		{
			// A byte order mark ends the leading run of comments on its first
			// line, and this check is the only thing that sees a collapsed key.
			name: "a byte order mark above the header",
			source: "\ufeff# 1 column, 1 group, 2 tiles\n---\n1:\n- name: Group\n  items:\n" +
				"    Same: https://a.example\n    Same: https://b.example\n",
			want: "Its header describes 1 column, 1 group and 2 tiles, but 1 column, 1 group " +
				"and 1 tile came in — expected if you edited the file, worth a look if you didn't.",
		},
		{
			name: "a blank line above the header",
			source: "\n# 1 column, 1 group, 2 tiles\n---\n1:\n- name: Group\n  items:\n" +
				"    Same: https://a.example\n    Same: https://b.example\n",
			want: "Its header describes 1 column, 1 group and 2 tiles, but 1 column, 1 group " +
				"and 1 tile came in — expected if you edited the file, worth a look if you didn't.",
		},
		{
			name:   "counts that agree",
			source: sample,
			want:   "",
		},
		{
			name:   "no header at all",
			source: "---\n1:\n- name: Group\n  items:\n    A: https://a.example\n",
			want:   "",
		},
		{
			name: "header lines it does not recognise",
			source: "# tinylinks start page export - 2026-08-10\n" +
				"# format: see docs/start-page-format.md in tinystart\n" +
				`# Renamed "Fastmail" to "Fastmail (2)" in "Lo de siempre" so both tiles survive.` + "\n" +
				"---\n1:\n- name: Group\n  items:\n    A: https://a.example\n",
			want: "",
		},
		{
			// The counts line has to be above the document, not in it.
			name: "a counts line below the marker",
			source: "---\n1:\n- name: Group\n  items:\n    A: https://a.example\n" +
				"# 9 columns, 9 groups, 9 tiles\n",
			want: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := imported(t, test.source)
			if result.Warning != test.want {
				t.Errorf("warning = %q, want %q", result.Warning, test.want)
			}
		})
	}
}

// The real proof, and what makes "export, hand-edit, import" safe.
func TestRoundTrip(t *testing.T) {
	layout := Layout{Width: 3, Columns: []Column{
		column(1, group("Diseño",
			item("Tipografía", "https://nanfonts.com"),
			item("Feedbin", "https://feedbin.com"))),
		column(3, group("Otras cosas", item("YouTube", "https://youtube.com"))),
	}}

	result := imported(t, exported(t, layout))

	if !reflect.DeepEqual(result.Layout, layout) {
		t.Errorf("round trip = %+v, want %+v", result.Layout, layout)
	}
	if result.Warning != "" {
		t.Errorf("warning = %q, want none", result.Warning)
	}
}

func TestRoundTripRenumbersARepeatedTitleRatherThanLosingTheTile(t *testing.T) {
	layout := Layout{Width: 1, Columns: []Column{column(1, group("Only",
		item("Fastmail", "https://app.fastmail.com"),
		item("Fastmail", "https://www.fastmail.com")))}}

	result := imported(t, exported(t, layout))

	assertPage(t, result.Layout, "1/0 Only: Fastmail, Fastmail (2)")
	if url := result.Layout.Columns[0].Groups[0].Items[1].URL; url != "https://www.fastmail.com" {
		t.Errorf("the second tile's url = %q", url)
	}
}

// An empty page exports to a file that says "delete everything", and that is
// the one thing an import will not do.
func TestRoundTripOfAnEmptyPageIsARefusal(t *testing.T) {
	assertRefused(t, exported(t, Layout{Width: 1}),
		"that file has no groups in it — importing it would only empty your start page, "+
			"so nothing was changed")
}
