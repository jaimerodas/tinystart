package startpage

import (
	"strings"
	"testing"
	"time"
)

// The date in the header is the one thing about an export that is not the
// page, so it is passed in and every test uses the same one.
var exportedOn = time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)

func item(title, url string) Item { return Item{Title: title, URL: url} }

func group(name string, items ...Item) Group { return Group{Name: name, Items: items} }

func column(number int, groups ...Group) Column { return Column{Number: number, Groups: groups} }

func exported(t *testing.T, layout Layout) string {
	t.Helper()

	out, err := Export(layout, exportedOn)
	if err != nil {
		t.Fatalf("exporting: %v", err)
	}
	return string(out)
}

// headerOf is every line above the document marker, whether or not it managed
// to be a comment.
func headerOf(file string) []string {
	var lines []string
	for line := range strings.Lines(file) {
		if strings.HasPrefix(line, "---") {
			break
		}
		lines = append(lines, line)
	}
	return lines
}

// The whole format in one assertion: the file from docs/start-page-format.md,
// byte for byte, header and all.
func TestExportWritesTheDocumentedFile(t *testing.T) {
	layout := Layout{Width: 2, Columns: []Column{
		column(1, group("Test 2",
			item("NaN Fonts", "https://nanfonts.com"),
			item("Feedbin", "https://feedbin.com"))),
		column(2,
			group("Lo de siempre",
				item("My Synology Admin", "https://synology.local"),
				item("Fastmail", "https://app.fastmail.com")),
			group("Otras cosas",
				item("YouTube", "https://youtube.com"),
				item("LinkedIn", "https://linkedin.com"))),
	}}

	want := `# tinystart start page export - 2026-08-10
# 2 columns, 3 groups, 6 tiles
# format: see docs/start-page-format.md
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

	if got := exported(t, layout); got != want {
		t.Errorf("export =\n%s\nwant\n%s", got, want)
	}
}

// An empty column is omitted, and the keys around it keep their real numbers.
// Re-indexing here would shift the whole page left on the way back in.
func TestExportKeepsTheRealColumnNumbers(t *testing.T) {
	layout := Layout{Width: 3, Columns: []Column{
		column(1, group("Left", item("L", "https://l.example"))),
		column(3, group("Right", item("R", "https://r.example"))),
	}}

	file := exported(t, layout)
	if !strings.Contains(file, "3:\n- name: Right\n") {
		t.Errorf("column 3 is not in the file:\n%s", file)
	}
	if strings.Contains(file, "2:\n") {
		t.Errorf("an empty column 2 is in the file:\n%s", file)
	}
}

func TestExportGroupWithNoTiles(t *testing.T) {
	file := exported(t, Layout{Width: 1, Columns: []Column{column(1, group("Empty"))}})

	if !strings.Contains(file, "  items: {}\n") {
		t.Errorf("a group with no tiles should have an empty mapping:\n%s", file)
	}
}

// Psych writes an empty page as `--- {}`, on the marker's own line.
func TestExportEmptyPage(t *testing.T) {
	want := "# tinystart start page export - 2026-08-10\n" +
		"# 0 columns, 0 groups, 0 tiles\n" +
		"# format: see docs/start-page-format.md\n" +
		"--- {}\n"

	if got := exported(t, Layout{Width: 1}); got != want {
		t.Errorf("export = %q, want %q", got, want)
	}
}

// tinystart's unique index is on (group, url), so one group may hold two tiles
// with the same title — and a YAML mapping cannot. The emitter would keep the
// last and the tile would vanish from the file, so the exporter numbers them.
func TestExportNumbersRepeatedTitles(t *testing.T) {
	tests := []struct {
		name  string
		items []Item
		want  []string
	}{
		{
			name: "a second tile with the same title",
			items: []Item{
				item("Fastmail", "https://app.fastmail.com"),
				item("Fastmail", "https://www.fastmail.com"),
			},
			want: []string{"Fastmail", "Fastmail (2)"},
		},
		{
			name: "a third repeat too",
			items: []Item{
				item("Fastmail", "https://0.fastmail.com"),
				item("Fastmail", "https://1.fastmail.com"),
				item("Fastmail", "https://2.fastmail.com"),
			},
			want: []string{"Fastmail", "Fastmail (2)", "Fastmail (3)"},
		},
		{
			// The suffix goes on the whole title, so a tile genuinely called
			// "Fastmail (2)" that collides becomes "Fastmail (2) (2)".
			name: "the suffix goes on the whole title",
			items: []Item{
				item("Fastmail (2)", "https://a.fastmail.com"),
				item("Fastmail (2)", "https://b.fastmail.com"),
			},
			want: []string{"Fastmail (2)", "Fastmail (2) (2)"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := exported(t, Layout{Width: 1, Columns: []Column{column(1, group("Only", test.items...))}})

			result, err := Import([]byte(file))
			if err != nil {
				t.Fatalf("reading the export back: %v", err)
			}
			assertTitles(t, result.Layout.Columns[0].Groups[0], test.want)
		})
	}
}

func TestExportLeavesTheSameTitleInAnotherGroupAlone(t *testing.T) {
	file := exported(t, Layout{Width: 1, Columns: []Column{column(1,
		group("One", item("Fastmail", "https://app.fastmail.com")),
		group("Two", item("Fastmail", "https://app.fastmail.com")),
	)}})

	if strings.Contains(file, "Fastmail (2)") {
		t.Errorf("two groups may each hold a Fastmail:\n%s", file)
	}
}

func TestExportHeader(t *testing.T) {
	tests := []struct {
		name   string
		layout Layout
		want   string
	}{
		{
			name:   "names itself and the date",
			layout: Layout{Width: 1, Columns: []Column{column(1, group("One", item("A", "https://a.example")))}},
			want:   "# tinystart start page export - 2026-08-10\n",
		},
		{
			name:   "points at the format",
			layout: Layout{Width: 1, Columns: []Column{column(1, group("One", item("A", "https://a.example")))}},
			want:   "# format: see docs/start-page-format.md\n",
		},
		{
			name: "counts the file's columns, groups and tiles",
			layout: Layout{Width: 2, Columns: []Column{
				column(1, group("One", item("A", "https://a.example"))),
				column(2, group("Two", item("B", "https://b.example"), item("C", "https://c.example"))),
			}},
			want: "# 2 columns, 2 groups, 3 tiles\n",
		},
		{
			name:   "counts in the singular",
			layout: Layout{Width: 1, Columns: []Column{column(1, group("One", item("A", "https://a.example")))}},
			want:   "# 1 column, 1 group, 1 tile\n",
		},
		{
			// The count line describes the file, not the page: an empty
			// trailing column is not in the file, so re-importing narrows the
			// page. Say so rather than let it be discovered.
			name:   "warns when the page is wider than the file",
			layout: Layout{Width: 3, Columns: []Column{column(1, group("One", item("A", "https://a.example")))}},
			want: "# The page is 3 columns wide but nothing is past column 1, " +
				"so importing this file will set it to 1.\n",
		},
		{
			name: "warns about every rename",
			layout: Layout{Width: 1, Columns: []Column{column(1, group("Lo de siempre",
				item("Fastmail", "https://app.fastmail.com"),
				item("Fastmail", "https://www.fastmail.com")))}},
			want: `# Renamed "Fastmail" to "Fastmail (2)" in "Lo de siempre" so both tiles survive.` + "\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			file := exported(t, test.layout)
			if !strings.Contains(file, test.want) {
				t.Errorf("header is\n%s\nwant a line %q", strings.Join(headerOf(file), ""), test.want)
			}
		})
	}
}

func TestExportSaysNothingAboutWidthWhenThePageIsFull(t *testing.T) {
	file := exported(t, Layout{Width: 1, Columns: []Column{column(1, group("One", item("A", "https://a.example")))}})

	if strings.Contains(file, "columns wide") {
		t.Errorf("a full page needs no width warning:\n%s", file)
	}
}

// A warning line is built by interpolating a title into "# …", so a title
// holding a newline would spill onto a second line above the --- marker, where
// it is no longer a comment and the file no longer parses.
func TestExportNewlineInARenamedTitleCannotBreakTheHeader(t *testing.T) {
	file := exported(t, Layout{Width: 1, Columns: []Column{column(1, group("Only",
		item("Two\nlines", "https://a.example"),
		item("Two\nlines", "https://b.example")))}})

	for _, line := range headerOf(file) {
		if !strings.HasPrefix(line, "#") {
			t.Errorf("every header line must be a comment, got %q in\n%s", line, file)
		}
	}
	if !strings.Contains(file, `# Renamed "Two lines" to "Two lines (2)" in "Only" so both tiles survive.`) {
		t.Errorf("the rename warning should be squished onto one line:\n%s", file)
	}

	// And the tiles are both still there, as literal blocks.
	result, err := Import([]byte(file))
	if err != nil {
		t.Fatalf("reading the export back: %v", err)
	}
	assertTitles(t, result.Layout.Columns[0].Groups[0], []string{"Two\nlines", "Two\nlines (2)"})
}

// Ruby quotes a scalar on rules of its own, and every start page export that
// exists was written by Ruby. Each of these is what Psych 5.3 emitted for the
// same title — see psych.go for what the rules are and why they are here.
func TestExportQuotesTitlesLikePsych(t *testing.T) {
	tests := []struct{ title, want string }{
		{"Plain", "Plain"},
		{"Fastmail (2)", "Fastmail (2)"},
		{"Diseño", "Diseño"},
		{"¿Qué?", `"¿Qué?"`},
		{"Amazon 🇲🇽", `"Amazon \U0001F1F2\U0001F1FD"`},
		{"emoji 🎉", `"emoji \U0001F389"`},
		{"Un título bastante largo que sigue y sigue y sigue por un buen rato ya ves",
			"Un título bastante largo que sigue y sigue y sigue por un buen rato ya ves"},
		{strings.Repeat("a", 100), strings.Repeat("a", 100)},
		{`Quote"inside`, `Quote"inside`},
		{"Single'quote", "Single'quote"},
		{"e", "e"},
		{"ee", "ee"},

		// Scalars that would load back as something other than a String.
		{"123", "'123'"},
		{"1.5", "'1.5'"},
		{"1_000", "'1_000'"},
		{"1,5", "'1,5'"},
		{"0b101", "'0b101'"},
		{"0x1f", "'0x1f'"},
		{"010", "'010'"},
		{"089", "'089'"},
		{"10:30", "'10:30'"},
		{"2026-01-01", "'2026-01-01'"},
		{"2026-01-01T10:00:00Z", "'2026-01-01T10:00:00Z'"},
		{"yes", "'yes'"},
		{"no", "'no'"},
		{"true", "'true'"},
		{"off", "'off'"},
		{"on", "'on'"},
		{"Off", "'Off'"},
		{"NO", "'NO'"},
		{"null", "'null'"},
		{"<<", "!!str '<<'"},

		// Psych's own rule: a first character that is not a word character.
		{"~", `"~"`},
		{"# hash", `"# hash"`},
		{"- dash", `"- dash"`},
		{"@at", `"@at"`},
		{"[bracket]", `"[bracket]"`},
		{"{brace}", `"{brace}"`},
		{"*star", `"*star"`},
		{"&amp", `"&amp"`},
		{"!bang", `"!bang"`},
		{"|pipe", `"|pipe"`},
		{">gt", `">gt"`},
		{"%pct", `"%pct"`},
		{"?question", `"?question"`},
		{",comma", `",comma"`},
		{":symbol", `":symbol"`},
		{".inf", `".inf"`},
		{".nan", `".nan"`},
		{"-.inf", `"-.inf"`},
		{"+.5", `"+.5"`},
		{".", `"."`},
		{" leading", `" leading"`},
		{"  ", `"  "`},
		{"\tx", `"\tx"`},
		{"y", `"y"`},
		{"Y", `"Y"`},
		{"n", `"n"`},
		{"N", `"N"`},

		// Left to the emitter's own analysis, which agrees with libyaml's
		// because it is libyaml's.
		{"with: colon", "'with: colon'"},
		{"trailing ", "'trailing '"},
		{"tab\there", `"tab\there"`},
	}

	for _, test := range tests {
		t.Run(test.title, func(t *testing.T) {
			file := exported(t, Layout{Width: 1, Columns: []Column{
				column(1, group("Only", item(test.title, "https://a.example"))),
			}})

			want := "    " + test.want + ": https://a.example\n"
			if !strings.Contains(file, want) {
				t.Errorf("export is\n%s\nwant a line %q", file, want)
			}
		})
	}
}

// A title with a newline in it is a literal block, and the key is written in
// the explicit `? …` form because it no longer fits beside its value.
func TestExportWritesAMultilineTitleAsABlock(t *testing.T) {
	file := exported(t, Layout{Width: 1, Columns: []Column{
		column(1, group("Only", item("Two\nlines", "https://a.example"))),
	}})

	want := "    ? |-\n      Two\n      lines\n    : https://a.example\n"
	if !strings.Contains(file, want) {
		t.Errorf("export is\n%s\nwant %q", file, want)
	}
}

func assertTitles(t *testing.T, group Group, want []string) {
	t.Helper()

	got := make([]string, len(group.Items))
	for i, item := range group.Items {
		got[i] = item.Title
	}
	if len(got) != len(want) {
		t.Fatalf("titles = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("titles = %q, want %q", got, want)
		}
	}
}
