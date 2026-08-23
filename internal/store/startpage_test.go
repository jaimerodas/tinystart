package store

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jaimerodas/tinystart/internal/startpage"
)

// documented is the page from docs/start-page-format.md, as a layout.
func documented() startpage.Layout {
	return startpage.Layout{Width: 2, Columns: []startpage.Column{
		{Number: 1, Groups: []startpage.Group{
			{Name: "Test 2", Items: []startpage.Item{
				{Title: "NaN Fonts", URL: "https://nanfonts.com"},
				{Title: "Feedbin", URL: "https://feedbin.com"},
			}},
		}},
		{Number: 2, Groups: []startpage.Group{
			{Name: "Lo de siempre", Items: []startpage.Item{
				{Title: "My Synology Admin", URL: "https://synology.local"},
				{Title: "Fastmail", URL: "https://app.fastmail.com"},
			}},
			{Name: "Otras cosas", Items: []startpage.Item{
				{Title: "YouTube", URL: "https://youtube.com"},
				{Title: "LinkedIn", URL: "https://linkedin.com"},
			}},
		}},
	}}
}

var documentedPage = []string{
	"1/0 Test 2: NaN Fonts, Feedbin",
	"2/0 Lo de siempre: My Synology Admin, Fastmail",
	"2/1 Otras cosas: YouTube, LinkedIn",
}

// pageSummary reads the page back the long way — through the queries the grid
// itself uses — so that these tests do not compare ReplaceStartPage only
// against StartPageLayout's own idea of what it wrote.
func pageSummary(t *testing.T, db *DB, userID int64) []string {
	t.Helper()

	byColumn, err := db.GroupsByColumn(t.Context(), userID)
	if err != nil {
		t.Fatalf("reading the page: %v", err)
	}

	var lines []string
	for column := 1; column <= MaxColumns; column++ {
		for _, group := range byColumn[column] {
			items, err := db.ItemsInGroup(t.Context(), group.ID)
			if err != nil {
				t.Fatalf("reading group %q: %v", group.Name, err)
			}
			titles := make([]string, len(items))
			for i, item := range items {
				titles[i] = item.Title
			}
			lines = append(lines, fmt.Sprintf("%d/%d %s: %s",
				group.Column, group.Position, group.Name, strings.Join(titles, ", ")))
		}
	}
	return lines
}

func assertPageIs(t *testing.T, db *DB, userID int64, want ...string) {
	t.Helper()

	if got := pageSummary(t, db, userID); !reflect.DeepEqual(got, want) {
		t.Errorf("page =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func assertColumns(t *testing.T, db *DB, userID int64, want int) {
	t.Helper()

	user, err := db.UserByID(t.Context(), userID)
	if err != nil {
		t.Fatalf("reading the user: %v", err)
	}
	if user.Columns != want {
		t.Errorf("columns = %d, want %d", user.Columns, want)
	}
}

// The table in docs/start-page-format.md, asserted from the other end.
func TestReplaceStartPageBuildsTheDocumentedPage(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "one@example.com")

	if err := db.ReplaceStartPage(t.Context(), user.ID, documented()); err != nil {
		t.Fatalf("replacing: %v", err)
	}

	assertPageIs(t, db, user.ID, documentedPage...)
	assertColumns(t, db, user.ID, 2)
}

func TestReplaceStartPageLeavesVisitCountsAtZero(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "one@example.com")

	if err := db.ReplaceStartPage(t.Context(), user.ID, documented()); err != nil {
		t.Fatalf("replacing: %v", err)
	}

	groups, err := db.GroupsInColumn(t.Context(), user.ID, 1)
	if err != nil {
		t.Fatalf("reading column 1: %v", err)
	}
	items, err := db.ItemsInGroup(t.Context(), groups[0].ID)
	if err != nil {
		t.Fatalf("reading the tiles: %v", err)
	}
	for _, item := range items {
		if item.VisitCount != 0 {
			t.Errorf("%q came in with %d visits", item.Title, item.VisitCount)
		}
	}
}

func TestReplaceStartPageReplacesWhatWasThere(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "one@example.com")
	old := newGroup(t, db, user.ID, "Old", 1)
	newItem(t, db, user.ID, old.ID, "Gone", "https://gone.example")

	if err := db.ReplaceStartPage(t.Context(), user.ID, documented()); err != nil {
		t.Fatalf("replacing: %v", err)
	}

	assertPageIs(t, db, user.ID, documentedPage...)
	if _, err := db.GroupByID(t.Context(), user.ID, old.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("the old group is still there")
	}
}

func TestReplaceStartPageTwiceIsTheSamePage(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "one@example.com")

	for range 2 {
		if err := db.ReplaceStartPage(t.Context(), user.ID, documented()); err != nil {
			t.Fatalf("replacing: %v", err)
		}
	}

	assertPageIs(t, db, user.ID, documentedPage...)
}

func TestReplaceStartPageLeavesAnotherUsersPageAlone(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "one@example.com")
	other := newUser(t, db, "two@example.com")
	group := newGroup(t, db, other.ID, "Theirs", 1)
	newItem(t, db, other.ID, group.ID, "Theirs", "https://theirs.example")

	if err := db.ReplaceStartPage(t.Context(), user.ID, documented()); err != nil {
		t.Fatalf("replacing: %v", err)
	}

	assertPageIs(t, db, other.ID, "1/0 Theirs: Theirs")
	assertColumns(t, db, other.ID, 3)
}

// users.columns defaults to 1 and validation refuses a group past it, so this
// code must write the width before the first group. This test fails if the
// two are reordered.
func TestReplaceStartPageWidensBeforeCreatingGroups(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "one@example.com")
	if err := db.UpdateColumns(t.Context(), user.ID, 1); err != nil {
		t.Fatalf("narrowing to one column: %v", err)
	}

	layout := startpage.Layout{Width: 3, Columns: []startpage.Column{
		{Number: 3, Groups: []startpage.Group{{Name: "Far right",
			Items: []startpage.Item{{Title: "R", URL: "https://r.example"}}}}},
	}}
	if err := db.ReplaceStartPage(t.Context(), user.ID, layout); err != nil {
		t.Fatalf("replacing: %v", err)
	}

	assertPageIs(t, db, user.ID, "3/0 Far right: R")
	assertColumns(t, db, user.ID, 3)
}

// And narrowing is safe for the mirror-image reason: this code already
// deleted the groups a narrower page cannot show.
func TestReplaceStartPageNarrowsThePage(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "one@example.com")
	newGroup(t, db, user.ID, "Stranded", 3)

	layout := startpage.Layout{Width: 1, Columns: []startpage.Column{
		{Number: 1, Groups: []startpage.Group{{Name: "Only",
			Items: []startpage.Item{{Title: "A", URL: "https://a.example"}}}}},
	}}
	if err := db.ReplaceStartPage(t.Context(), user.ID, layout); err != nil {
		t.Fatalf("replacing: %v", err)
	}

	assertPageIs(t, db, user.ID, "1/0 Only: A")
	assertColumns(t, db, user.ID, 1)
}

// This code creates columns in ascending order however they arrive in the
// layout. That is the order the file has them in and the order Rails created
// them in. Nothing on the page shows it, so the proof is which group was made
// first.
func TestReplaceStartPageCreatesColumnsInAscendingOrder(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "one@example.com")

	layout := startpage.Layout{Width: 2, Columns: []startpage.Column{
		{Number: 2, Groups: []startpage.Group{{Name: "Right"}}},
		{Number: 1, Groups: []startpage.Group{{Name: "Left"}}},
	}}
	if err := db.ReplaceStartPage(t.Context(), user.ID, layout); err != nil {
		t.Fatalf("replacing: %v", err)
	}

	assertPageIs(t, db, user.ID, "1/0 Left: ", "2/0 Right: ")

	left, err := db.GroupsInColumn(t.Context(), user.ID, 1)
	if err != nil {
		t.Fatalf("reading column 1: %v", err)
	}
	right, err := db.GroupsInColumn(t.Context(), user.ID, 2)
	if err != nil {
		t.Fatalf("reading column 2: %v", err)
	}
	if left[0].ID > right[0].ID {
		t.Errorf("column 1 was created after column 2 (%d > %d)", left[0].ID, right[0].ID)
	}
}

// A refusal must change nothing: every check is inside the transaction that
// does the deleting, so a file that fails on its last tile leaves the page as
// it was.
func TestReplaceStartPageRefusalsWriteNothing(t *testing.T) {
	tests := []struct {
		name   string
		layout startpage.Layout
		want   string
	}{
		{
			name: "a url that is not a url",
			layout: startpage.Layout{Width: 1, Columns: []startpage.Column{
				{Number: 1, Groups: []startpage.Group{{Name: "Fine",
					Items: []startpage.Item{{Title: "Bare", URL: "example.com"}}}}},
			}},
			want: `the link "Bare" (example.com) in "Fine" was rejected: Url must be a valid URL`,
		},
		{
			name: "a repeated group name",
			layout: startpage.Layout{Width: 1, Columns: []startpage.Column{
				{Number: 1, Groups: []startpage.Group{
					{Name: "Twice", Items: []startpage.Item{{Title: "A", URL: "https://a.example"}}},
					{Name: "Twice", Items: []startpage.Item{{Title: "B", URL: "https://b.example"}}},
				}},
			}},
			want: `the group "Twice" was rejected: Name has already been taken`,
		},
		{
			name: "a repeated url inside one group",
			layout: startpage.Layout{Width: 1, Columns: []startpage.Column{
				{Number: 1, Groups: []startpage.Group{{Name: "Group", Items: []startpage.Item{
					{Title: "One", URL: "https://same.example"},
					{Title: "Two", URL: "https://same.example"},
				}}}},
			}},
			want: `the link "Two" (https://same.example) in "Group" was rejected: Url has already been taken`,
		},
		{
			name: "a tile with no title",
			layout: startpage.Layout{Width: 1, Columns: []startpage.Column{
				{Number: 1, Groups: []startpage.Group{{Name: "Group",
					Items: []startpage.Item{{Title: "", URL: "https://a.example"}}}}},
			}},
			want: `the link "" (https://a.example) in "Group" was rejected: Title can't be blank`,
		},
		{
			name: "a group with no name",
			layout: startpage.Layout{Width: 1, Columns: []startpage.Column{
				{Number: 1, Groups: []startpage.Group{{Name: ""}}},
			}},
			want: `the group "" was rejected: Name can't be blank`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t)
			user := newUser(t, db, "one@example.com")
			keep := newGroup(t, db, user.ID, "Keep", 1)
			newItem(t, db, user.ID, keep.ID, "Keep me", "https://keep.example")

			err := db.ReplaceStartPage(t.Context(), user.ID, test.layout)

			var rejected *RejectedError
			if !errors.As(err, &rejected) {
				t.Fatalf("expected a RejectedError, got %v", err)
			}
			if err.Error() != test.want {
				t.Errorf("refusal = %q, want %q", err.Error(), test.want)
			}
			assertPageIs(t, db, user.ID, "1/0 Keep: Keep me")
			assertColumns(t, db, user.ID, 3)
		})
	}
}

// The sentence is for the page. The field errors underneath are for anything
// that wants to know which attribute it was.
func TestRejectedErrorCarriesTheFieldErrors(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "one@example.com")

	err := db.ReplaceStartPage(t.Context(), user.ID, startpage.Layout{Width: 1,
		Columns: []startpage.Column{{Number: 1, Groups: []startpage.Group{{Name: "Fine",
			Items: []startpage.Item{{Title: "Bare", URL: "example.com"}}}}}}})

	var invalid ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected the ValidationError underneath, got %v", err)
	}
	if !invalid.On("url") {
		t.Errorf("the error should be on the url, got %v", invalid)
	}
}

func TestReplaceStartPageRefusesAnImpossibleWidth(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "one@example.com")

	assertInvalid(t, db.ReplaceStartPage(t.Context(), user.ID, startpage.Layout{Width: 0}),
		"Columns must be greater than 0")
	assertInvalid(t, db.ReplaceStartPage(t.Context(), user.ID, startpage.Layout{Width: 7}),
		"Columns must be less than or equal to 6")
}

func TestReplaceStartPageUnknownUser(t *testing.T) {
	db := newTestDB(t)

	assertNotFound(t, db.ReplaceStartPage(t.Context(), 404, documented()))
}

func TestStartPageLayoutReadsThePage(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "one@example.com")
	if err := db.ReplaceStartPage(t.Context(), user.ID, documented()); err != nil {
		t.Fatalf("replacing: %v", err)
	}

	layout, err := db.StartPageLayout(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("reading the layout: %v", err)
	}

	if !reflect.DeepEqual(layout, documented()) {
		t.Errorf("layout = %+v, want %+v", layout, documented())
	}
}

// Width is the user's own column count, which can be wider than the widest
// column that holds anything — that difference is what the export warns about.
func TestStartPageLayoutWidthIsThePagesOwn(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "one@example.com") // three columns wide
	newGroup(t, db, user.ID, "Only", 1)

	layout, err := db.StartPageLayout(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("reading the layout: %v", err)
	}

	if layout.Width != 3 {
		t.Errorf("width = %d, want 3", layout.Width)
	}
	if layout.Counts().Columns != 1 {
		t.Errorf("the file would be %d columns wide, want 1", layout.Counts().Columns)
	}
}

func TestStartPageLayoutIncludesAGroupWithNoTiles(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "one@example.com")
	newGroup(t, db, user.ID, "Empty", 1)
	full := newGroup(t, db, user.ID, "Full", 1)
	newItem(t, db, user.ID, full.ID, "A", "https://a.example")

	layout, err := db.StartPageLayout(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("reading the layout: %v", err)
	}

	want := []startpage.Group{
		{Name: "Empty"},
		{Name: "Full", Items: []startpage.Item{{Title: "A", URL: "https://a.example"}}},
	}
	if !reflect.DeepEqual(layout.Columns[0].Groups, want) {
		t.Errorf("groups = %+v, want %+v", layout.Columns[0].Groups, want)
	}
}

func TestStartPageLayoutReadsOnlyOneUsersPage(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "one@example.com")
	other := newUser(t, db, "two@example.com")
	newGroup(t, db, user.ID, "Mine", 1)
	newGroup(t, db, other.ID, "Theirs", 1)

	layout, err := db.StartPageLayout(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("reading the layout: %v", err)
	}

	want := []startpage.Column{{Number: 1, Groups: []startpage.Group{{Name: "Mine"}}}}
	if !reflect.DeepEqual(layout.Columns, want) {
		t.Errorf("columns = %+v, want %+v", layout.Columns, want)
	}
}

func TestStartPageLayoutOfAnEmptyPage(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "one@example.com")

	layout, err := db.StartPageLayout(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("reading the layout: %v", err)
	}

	if len(layout.Columns) != 0 {
		t.Errorf("columns = %+v, want none", layout.Columns)
	}
}

func TestStartPageLayoutUnknownUser(t *testing.T) {
	db := newTestDB(t)

	_, err := db.StartPageLayout(t.Context(), 404)
	assertNotFound(t, err)
}

// The whole loop the format exists for: a page out as a file, and the file
// back over the page it came from.
func TestStartPageSurvivesAFileRoundTrip(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "one@example.com")
	left := newGroup(t, db, user.ID, "Diseño", 1)
	newItem(t, db, user.ID, left.ID, "Tipografía", "https://nanfonts.com")
	newItem(t, db, user.ID, left.ID, "Feedbin", "https://feedbin.com")
	right := newGroup(t, db, user.ID, "Otras cosas", 3)
	newItem(t, db, user.ID, right.ID, "YouTube", "https://youtube.com")
	before := pageSummary(t, db, user.ID)

	layout, err := db.StartPageLayout(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("reading the layout: %v", err)
	}
	file, err := startpage.Export(layout, time.Now().UTC())
	if err != nil {
		t.Fatalf("exporting: %v", err)
	}
	result, err := startpage.Import(file)
	if err != nil {
		t.Fatalf("importing: %v", err)
	}
	if err := db.ReplaceStartPage(t.Context(), user.ID, result.Layout); err != nil {
		t.Fatalf("replacing: %v", err)
	}

	assertPageIs(t, db, user.ID, before...)
	assertColumns(t, db, user.ID, 3)
	if result.Warning != "" {
		t.Errorf("warning = %q, want none", result.Warning)
	}
}

// Export renumbers a tile whose title collides on the way out. It comes back
// as its own tile, rather than the two of them collapsing into one.
func TestStartPageRoundTripKeepsBothOfTwoIdenticalTitles(t *testing.T) {
	db := newTestDB(t)
	user := newUser(t, db, "one@example.com")
	group := newGroup(t, db, user.ID, "Only", 1)
	newItem(t, db, user.ID, group.ID, "Fastmail", "https://app.fastmail.com")
	newItem(t, db, user.ID, group.ID, "Fastmail", "https://www.fastmail.com")

	layout, err := db.StartPageLayout(t.Context(), user.ID)
	if err != nil {
		t.Fatalf("reading the layout: %v", err)
	}
	file, err := startpage.Export(layout, time.Now().UTC())
	if err != nil {
		t.Fatalf("exporting: %v", err)
	}
	result, err := startpage.Import(file)
	if err != nil {
		t.Fatalf("importing: %v", err)
	}
	if err := db.ReplaceStartPage(t.Context(), user.ID, result.Layout); err != nil {
		t.Fatalf("replacing: %v", err)
	}

	assertPageIs(t, db, user.ID, "1/0 Only: Fastmail, Fastmail (2)")
}
