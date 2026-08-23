package startpage

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"time"

	yaml "go.yaml.in/yaml/v3"
)

// Export writes a layout as the interchange format: the comment header, then
// the YAML document. today is the date the header stamps on it. It is passed
// in rather than read from the clock, so the same page always exports to the
// same bytes.
//
// Two things here are not obvious from the format doc, because the format
// began as a one-way migration and is now a round trip:
//
//   - This exporter dedupes titles. The unique index is on (group, url), so one
//     group can hold two tiles called the same thing — and a YAML mapping
//     cannot. Left alone, the emitter keeps the last of two identical keys and
//     says nothing, so an undeduped export silently drops a tile. The renames
//     go in the header.
//   - An empty trailing column is not in the file, so re-importing narrows the
//     page. The header says so instead of letting readers discover it on
//     their own.
func Export(layout Layout, today time.Time) ([]byte, error) {
	document, renames := document(layout)

	var body bytes.Buffer
	encoder := yaml.NewEncoder(&body)
	// Two spaces of indent, with the dash counted as part of it, is Psych's
	// layout. That is why an export from here diffs clean against an export
	// from Rails.
	encoder.SetIndent(2)
	encoder.CompactSeqIndent()
	if err := encoder.Encode(document); err != nil {
		return nil, fmt.Errorf("writing the start page: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("writing the start page: %w", err)
	}

	out := header(layout, today, renames)
	// Psych puts the document marker on a line of its own, except when the
	// whole document is the empty mapping it writes as `--- {}`.
	if len(document.Content) == 0 {
		return append(out, append([]byte("--- "), body.Bytes()...)...), nil
	}
	return append(out, append([]byte("---\n"), body.Bytes()...)...), nil
}

// document builds the YAML tree and collects the renames while it dedupes
// the titles forced on it. It uses nodes rather than Go values because the
// format is ordered throughout and a Go map is not. The quoting is Psych's
// (psych.go) rather than the library's.
func document(layout Layout) (*yaml.Node, []string) {
	var renames []string
	root := &yaml.Node{Kind: yaml.MappingNode}

	for _, column := range layout.Columns {
		if len(column.Groups) == 0 {
			continue
		}
		groups := &yaml.Node{Kind: yaml.SequenceNode}
		for _, group := range column.Groups {
			items, groupRenames := itemsNode(group)
			renames = append(renames, groupRenames...)
			groups.Content = append(groups.Content, &yaml.Node{
				Kind: yaml.MappingNode,
				Content: []*yaml.Node{
					scalar("name"), scalar(group.Name),
					scalar("items"), items,
				},
			})
		}
		root.Content = append(root.Content, number(column.Number), groups)
	}
	return root, renames
}

func itemsNode(group Group) (*yaml.Node, []string) {
	var renames []string
	items := &yaml.Node{Kind: yaml.MappingNode}
	taken := make(map[string]bool, len(group.Items))

	for _, item := range group.Items {
		title := item.Title
		if taken[title] {
			// "Fastmail", then "Fastmail (2)", then "Fastmail (3)". The suffix
			// goes on the whole title, so a tile genuinely called
			// "Fastmail (2)" that collides becomes "Fastmail (2) (2)". That is
			// what tinylinks' exporter does, and what the format doc tells a
			// reader to expect.
			suffix := 2
			for taken[fmt.Sprintf("%s (%d)", title, suffix)] {
				suffix++
			}
			numbered := fmt.Sprintf("%s (%d)", title, suffix)
			// Interpolated rather than %q: Ruby put the title in raw, and the
			// header squishes what comes out. Escaping a newline here instead
			// puts a literal backslash in the file.
			renames = append(renames, fmt.Sprintf(
				`Renamed "%s" to "%s" in "%s" so both tiles survive.`, title, numbered, group.Name))
			title = numbered
		}
		taken[title] = true
		items.Content = append(items.Content, scalar(title), scalar(item.URL))
	}
	return items, renames
}

// scalar is a string, quoted the way Ruby quotes it.
//
// The node carries no tag on purpose. When the emitter cannot infer a tag
// from the value, it writes one out — `!!str '123'`. The quoting from
// psych.go is already enough to make every one of these load back as a
// String.
func scalar(value string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.ScalarNode, Value: value, Style: psychStyle(value)}
	// The one string Ruby tags. Quoting `<<` is enough to stop it being a merge
	// key, but Psych says so twice and the files it wrote say `!!str '<<'`.
	// TaggedStyle forces the encoder to write a tag that its own analysis
	// skips by default.
	if value == "<<" {
		node.Tag = "!!str"
		node.Style |= yaml.TaggedStyle
	}
	return node
}

// number is a column key: an Integer in the file. That is the format's own
// version check, because a String key means a later format with an envelope.
// Digits need no tag: they read back as one on their own.
func number(value int) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Value: strconv.Itoa(value)}
}

// header is the comment block above the document marker.
//
// Line 1 names the app that wrote the file, so a reader can tell a tinylinks
// export from one made here at a glance. Line 2 is the counts. The importer
// makes sure they are right. Everything after that is a warning worth
// carrying along with the file rather than showing once and losing.
func header(layout Layout, today time.Time, renames []string) []byte {
	counts := layout.Counts()

	lines := []string{
		"tinystart start page export - " + today.Format(time.DateOnly),
		fmt.Sprintf("%s, %s, %s",
			pluralize(counts.Columns, "column"),
			pluralize(counts.Groups, "group"),
			pluralize(counts.Items, "tile")),
		"format: see docs/start-page-format.md",
	}
	if counts.Columns > 0 && layout.Width > counts.Columns {
		lines = append(lines, fmt.Sprintf(
			"The page is %d columns wide but nothing is past column %d, "+
				"so importing this file will set it to %d.",
			layout.Width, counts.Columns, counts.Columns))
	}
	lines = append(lines, renames...)

	var out bytes.Buffer
	for _, line := range lines {
		// Squished, not just prefixed: the code only makes sure that a title or
		// a group name is present. As a result, one with a newline spills a
		// warning onto a second line above the --- marker. There it is no
		// longer a comment, and the file no longer parses.
		out.WriteString("# " + whitespace.ReplaceAllString(line, " ") + "\n")
	}
	return out.Bytes()
}

// whitespace is Ruby's /\s+/, which includes the vertical tab Go's \s leaves
// out. Anything that can end a line has to be in here or the squish above is
// not a defence.
var whitespace = regexp.MustCompile(`[ \t\r\n\f\v]+`)
