package startpage

import (
	"bytes"
	"errors"
	"fmt"
	"iter"
	"regexp"
	"slices"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// maxColumns is the widest page there is — the same six as store.MaxColumns.
// This code makes sure that a column number stays within it here, so it
// refuses a file naming column 7 while the file is still bytes, not halfway
// through rebuilding somebody's page.
const maxColumns = 6

// Result is what a file turned out to say.
//
// Layout is for the caller to persist — see store.ReplaceStartPage, which
// replaces rather than merges. That is what makes the workflow this format
// exists for (export, look at it, hand-edit the YAML, import again)
// idempotent. Layout.Counts() is the summary the flash reports back.
type Result struct {
	Layout Layout

	// Warning is set on a file that imported and looked odd, and is empty
	// otherwise. It rides along with the success rather than replacing it: see
	// countMismatch for why this can never be a refusal.
	Warning string
}

// Import reads the interchange format. Every error it returns is a sentence
// meant for the person who picked the file — the caller prints it verbatim.
// Every one of them means Import writes nothing at all.
//
// Nothing here talks to the database, so the checks are the file's shape only.
// The rules about the records themselves — a url that parses, a group name
// nobody else has, one url per group — belong to the models. The models
// enforce them where they live, inside the transaction that does the writing.
func Import(source []byte) (Result, error) {
	// A byte order mark is a legal way to start a UTF-8 file and not a legal
	// way to start a YAML document.
	source = bytes.TrimPrefix(source, []byte("\ufeff"))

	var root yaml.Node
	if err := yaml.Unmarshal(source, &root); err != nil {
		return Result{}, refusef("that file could not be read as YAML — %s", firstLine(err))
	}

	document := documentNode(&root)
	if document == nil {
		return Result{}, errEmptyFile
	}
	if err := checkPermittedTypes(document); err != nil {
		return Result{}, err
	}

	layout, err := readLayout(document)
	if err != nil {
		return Result{}, err
	}

	// This code makes sure that the file is not empty by counting groups, not
	// by inspecting the raw mapping. `1: []` is a mapping with a column in it
	// and no groups anywhere. That is a legal instruction to delete the page,
	// which is never what picking a file meant.
	counts := layout.Counts()
	if counts.Groups == 0 {
		return Result{}, errEmptyFile
	}

	return Result{Layout: layout, Warning: countMismatch(source, counts)}, nil
}

// errEmptyFile covers both an empty document and a file whose columns hold no
// groups. The two are the same instruction, and refusing it is the point.
var errEmptyFile = errors.New(
	"that file has no groups in it — importing it would only empty your start page, " +
		"so nothing was changed")

// documentNode unwraps the document the parser wraps around the top-level
// value, and reports nil for a file with nothing in it.
func documentNode(root *yaml.Node) *yaml.Node {
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil
	}
	return root.Content[0]
}

// readLayout walks the document in the order it was written, because the
// first thing wrong with a file is the thing worth reporting. It hands back
// the columns in ascending order, because that is the order the importer has
// to create them in.
func readLayout(document *yaml.Node) (Layout, error) {
	if document.Kind != yaml.MappingNode {
		return Layout{}, refuse("that file isn't a mapping of column numbers to groups — " +
			"see docs/start-page-format.md")
	}

	// Every top-level key in this format is an Integer, so a String key is a
	// later format with an envelope rather than a broken file. This code makes
	// sure that every key is an Integer across the whole mapping before it
	// reads any column. That way, it names a version envelope correctly
	// however far down the odd key sits.
	for key := range pairs(document) {
		if key.Tag != "!!int" {
			return Layout{}, refuse("that file looks like a newer format than this app can read: " +
				"every top-level key should be a column number")
		}
	}

	var layout Layout
	for key, value := range pairs(document) {
		column, err := readColumn(key, value)
		if err != nil {
			return Layout{}, err
		}
		layout.Columns = append(layout.Columns, column)
	}

	slices.SortFunc(layout.Columns, func(a, b Column) int { return a.Number - b.Number })
	layout.Width = layout.Counts().Columns
	return layout, nil
}

func readColumn(key, value *yaml.Node) (Column, error) {
	number, err := strconv.Atoi(key.Value)
	if err != nil || number < 1 || number > maxColumns {
		return Column{}, refusef("column %s is outside the 1–%d columns a start page can have",
			key.Value, maxColumns)
	}
	if value.Kind != yaml.SequenceNode {
		return Column{}, refusef("column %d should be a list of groups, but it isn't", number)
	}

	column := Column{Number: number}
	for index, node := range value.Content {
		group, err := readGroup(node, number, index)
		if err != nil {
			return Column{}, err
		}
		column.Groups = append(column.Groups, group)
	}
	return column, nil
}

func readGroup(node *yaml.Node, column, index int) (Group, error) {
	name := ""
	if node.Kind == yaml.MappingNode {
		name = text(lookup(node, "name"))
	}
	if strings.TrimSpace(name) == "" {
		return Group{}, refusef("the group at position %d of column %d has no name", index+1, column)
	}

	items := lookup(node, "items")
	if items == nil || items.Kind != yaml.MappingNode {
		return Group{}, refusef(`the group "%s" has no items mapping — `+
			"a group with no tiles is written as `items: {}`", name)
	}

	group := Group{Name: name}
	for title, url := range pairs(items) {
		// The code coerces the title rather than requiring it to be a String.
		// In a hand-edited file, an unquoted `123:` does not arrive as one,
		// and a tile can legitimately be called 123.
		group.Items = append(group.Items, Item{Title: text(title), URL: text(url)})
	}
	return group, nil
}

// pairs iterates a mapping's keys and values in document order, keeping the
// last of two identical keys at the position of the first. That is what a
// Ruby Hash does, and so what every file in existence was written against.
//
// Losing a repeated key is not a detail: it is the one kind of damage a hand
// edit can do silently. The header counts below are the only thing that sees
// it happen.
func pairs(mapping *yaml.Node) iter.Seq2[*yaml.Node, *yaml.Node] {
	return func(yield func(*yaml.Node, *yaml.Node) bool) {
		seen := make(map[string]int, len(mapping.Content)/2)
		keys := make([]*yaml.Node, 0, len(mapping.Content)/2)
		values := make([]*yaml.Node, 0, len(mapping.Content)/2)

		for i := 0; i+1 < len(mapping.Content); i += 2 {
			key, value := mapping.Content[i], mapping.Content[i+1]
			if at, repeated := seen[key.Value]; repeated {
				values[at] = value
				continue
			}
			seen[key.Value] = len(keys)
			keys = append(keys, key)
			values = append(values, value)
		}

		for i, key := range keys {
			if !yield(key, values[i]) {
				return
			}
		}
	}
}

// lookup is `hash["name"]`, and nil when there is no such key.
func lookup(mapping *yaml.Node, name string) *yaml.Node {
	for key, value := range pairs(mapping) {
		if key.Tag == "!!str" && key.Value == name {
			return value
		}
	}
	return nil
}

// text is Ruby's #to_s on a loaded scalar. It gives the characters as they
// were written, and the empty string for a missing value or an explicit
// null. That is what nil.to_s gives, and what the presence validations then
// refuse.
func text(node *yaml.Node) string {
	if node == nil || node.Tag == "!!null" {
		return ""
	}
	return node.Value
}

// permittedTags is the format's "String, Integer, Hash and Array" as the
// parser sees it, plus the two scalars YAML resolves on its own and Ruby
// permits everywhere. Anything else — a date, a timestamp, a tagged object —
// is a file this app will not load.
var permittedTags = []string{"!!str", "!!int", "!!bool", "!!float", "!!null", "!!seq", "!!map"}

// checkPermittedTypes is YAML.safe_load's half of the parse: no aliases, no
// tags, no dates.
//
// The wording is not Psych's, because Psych's says which Ruby class it refused
// to build. Ruby is what this app is written in until the day it is not.
func checkPermittedTypes(node *yaml.Node) error {
	if node.Kind == yaml.AliasNode {
		return refuse("that file could not be read as YAML — " +
			"aliases are not allowed in a start page file")
	}
	if node.Style&yaml.TaggedStyle != 0 || !slices.Contains(permittedTags, node.Tag) {
		return refusef("that file could not be read as YAML — %s is not allowed in a start "+
			"page file, which holds only text, numbers, lists and mappings", node.Tag)
	}
	for _, child := range node.Content {
		if err := checkPermittedTypes(child); err != nil {
			return err
		}
	}
	return nil
}

// headerCounts is the second line of the comment header: "2 columns, 3 groups,
// 6 tiles".
var headerCounts = regexp.MustCompile(`^\s*#\s*(\d+) columns?, (\d+) groups?, (\d+) tiles?\s*$`)

// countMismatch compares the header's counts with what actually loaded, and
// says so when they disagree.
//
// The emitter keeps the last of two identical keys and says nothing. As a
// result, a hand edit that repeats a title inside one `items` block loses a
// tile with no error from anywhere. The header's counts are the only cheap
// way to see that happened.
//
// It cannot be a refusal, though, and that is the whole subtlety. Deleting a
// tile by hand lowers the count in exactly the way a collapsed key does, and
// nothing in the file says which happened. Refusing blocks the one workflow
// this format exists for — export, edit, import again — so the import goes
// through and reports what it noticed. A file with no counts line says
// nothing at all.
//
// Note what it cannot see: a repeated *group* name. Groups are list items, not
// mapping keys, so duplicating one changes no count. The uniqueness validation
// on the group catches that later and names it properly.
func countMismatch(source []byte, actual Counts) string {
	stated, ok := statedCounts(source)
	if !ok || stated == actual {
		return ""
	}
	return fmt.Sprintf("Its header describes %s, but %s came in — "+
		"expected if you edited the file, worth a look if you didn't.",
		describe(stated), describe(actual))
}

// statedCounts reads every line above the document marker rather than the
// leading run of comments. A byte order mark or a blank line above the header
// ends that run on its first line, which skips the check entirely. This check
// is the only thing that ever sees a collapsed duplicate key.
func statedCounts(source []byte) (Counts, bool) {
	for line := range strings.Lines(string(source)) {
		line = strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(line) == "---" {
			return Counts{}, false
		}
		if match := headerCounts.FindStringSubmatch(line); match != nil {
			columns, _ := strconv.Atoi(match[1])
			groups, _ := strconv.Atoi(match[2])
			items, _ := strconv.Atoi(match[3])
			return Counts{Columns: columns, Groups: groups, Items: items}, true
		}
	}
	return Counts{}, false
}

func describe(counts Counts) string {
	return fmt.Sprintf("%s, %s and %s",
		pluralize(counts.Columns, "column"),
		pluralize(counts.Groups, "group"),
		pluralize(counts.Items, "tile"))
}

// refuse and refusef build the message the page shows. They are errors.New in
// a hat: the wording is the interface here, so it is worth having one place
// that says so.
func refuse(message string) error { return errors.New(message) }

func refusef(format string, args ...any) error { return fmt.Errorf(format, args...) }

// firstLine is the parser's complaint, trimmed to the part that names what it
// tripped over. The library prefixes it with "yaml: ", which reads as noise in
// the middle of a sentence about a file.
func firstLine(err error) string {
	message, _, _ := strings.Cut(err.Error(), "\n")
	return strings.TrimSpace(strings.TrimPrefix(message, "yaml:"))
}
