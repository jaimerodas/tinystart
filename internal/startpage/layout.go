// Package startpage is the interchange format in docs/start-page-format.md: a
// whole start page as a small YAML file, small enough to read and edit by hand.
//
// It is pure domain — no database, no HTTP. Import parses and validates a file
// and hands back the layout it describes. Export writes a layout back out.
// What it means to *store* a layout belongs to internal/store, which imports
// these types. The dependency only ever points that way, so parsing has no
// opinion about SQL and SQL has no opinion about YAML.
//
// The format is a mapping of column number → ordered list of groups, each group
// a name and an ordered title → url mapping. Order is the whole point. A
// group's index in its list is its position in that column, and a tile's index
// in `items` is its position in the group. That is why nothing here is a Go map
// — a map loses exactly the thing the file is carrying.
package startpage

import "strconv"

// Layout is a start page: how wide it is, and what is in its columns.
//
// Width is users.columns, and it is not the same number as len(Columns).
// A column with nothing in it is not in the file at all. As a result, a
// three-column page whose third column is empty exports as two columns and
// re-imports as a two-column page — see "Trailing empty columns" in the format
// doc. Export says so in the header. Import sets Width to the highest column
// number it found, which is what the importer must write to users.columns.
type Layout struct {
	Width   int
	Columns []Column
}

// Column is one column of the grid. Number is the literal column number from
// the file — 1-based, and never re-derived from the column's place in the
// slice. Columns can be non-contiguous (1 and 3, with 2 empty), and treating
// the index as the number is the single most likely way to get this format
// wrong: it shifts the whole page left, silently.
type Column struct {
	Number int
	Groups []Group
}

// Group is a named box of tiles. Its index in Column.Groups is its position.
type Group struct {
	Name  string
	Items []Item
}

// Item is a tile. Its index in Group.Items is its position. Visit counts are
// deliberately not here: the file carries the layout and nothing else.
type Item struct {
	Title string
	URL   string
}

// Counts is the three numbers the header line states, and the three the flash
// reports back after an import. They describe the *file*, not the page: the
// columns are the highest column number in it. That is what a re-import sets
// the page's width to — an empty trailing column is not in the file at all.
type Counts struct {
	Columns int
	Groups  int
	Items   int
}

// Counts walks the layout. It is cheap enough at this size to do it on demand
// rather than keep a total that can drift.
func (l Layout) Counts() Counts {
	var counts Counts
	for _, column := range l.Columns {
		counts.Columns = max(counts.Columns, column.Number)
		counts.Groups += len(column.Groups)
		for _, group := range column.Groups {
			counts.Items += len(group.Items)
		}
	}
	return counts
}

// pluralize is Rails' String#pluralize for the three nouns this format
// counts — all of them regular, all of them named in a message somebody reads.
func pluralize(number int, noun string) string {
	if number == 1 {
		return strconv.Itoa(number) + " " + noun
	}
	return strconv.Itoa(number) + " " + noun + "s"
}
