package startpage

import (
	"regexp"
	"strings"
	"unicode/utf8"

	yaml "go.yaml.in/yaml/v3"
)

// Psych's quoting rules, ported, so that a file written here is byte for byte
// the file Rails wrote for the same page.
//
// That matters for one release and then forever after. During the rewrite,
// we diff the two exports against each other. After that, the files people
// already have on disk have to keep looking like the files this app
// produces. Every start page export in existence came out of Ruby, and Ruby
// quotes a scalar on rules of its own that no YAML library reproduces.
//
// The emitter underneath (go.yaml.in/yaml/v3) is a port of the same libyaml
// that Psych drives. As a result, the *analysis* — is this plain-safe, where
// does an escape go, how is a line broken — already agrees. What does not
// agree is the style Psych asks for before the analysis runs, which is
// Psych::Visitors::YAMLTree#visit_String, below.
//
// Everything here is about writing a string, never about what it means.
// A wrong answer is an uglier file, not a broken one.

var (
	// Psych's "this is obviously prose" shortcut: an optional leading
	// character that is not a digit, dot, colon or dash, then a run of
	// letters, spaces and punctuation. It is a prefix match, not a whole
	// match, and it is what keeps ordinary titles off the slow path below.
	psychProse = regexp.MustCompile(`^[^\d.:\-]?[\pL_ \t\r\n\f\v!@#$%^&*(){}<>|/\\~;=]+`)

	// The five-character shortcut's exception list: a short string starting
	// with one of these letters can be a boolean or a null.
	psychShortNotWord = regexp.MustCompile(`(?i)^[^ytonf~]`)
	psychNull         = regexp.MustCompile(`(?i)^null$`)
	psychTrue         = regexp.MustCompile(`(?i)^(yes|true|on)$`)
	psychFalse        = regexp.MustCompile(`(?i)^(no|false|off)$`)

	// Psych::ScalarScanner's own constants, verbatim. A string matching any
	// of them loads back as something that is not a String. So it has to be
	// quoted, or the file does not round trip.
	psychTime        = regexp.MustCompile(`^-?\d{4}-\d{1,2}-\d{1,2}(?:[Tt]|\s+)\d{1,2}:\d\d:\d\d(?:\.\d*)?(?:\s*(?:Z|[-+]\d{1,2}:?(?:\d\d)?))?$`)
	psychDate        = regexp.MustCompile(`^\d{4}-(?:1[012]|0\d|\d)-(?:[12]\d|3[01]|0\d|\d)$`)
	psychInfinity    = regexp.MustCompile(`(?i)^[-+]?\.inf$`)
	psychNaN         = regexp.MustCompile(`(?i)^\.nan$`)
	psychSymbol      = regexp.MustCompile(`^:.`)
	psychSexagesimal = regexp.MustCompile(`^[-+]?[0-9][0-9_]*(:[0-5]?[0-9]){1,2}(\.[0-9_]*)?$`)
	psychFloat       = regexp.MustCompile(`^(?:[-+]?([0-9][0-9_,]*)?\.[0-9]*([eE][-+][0-9]+)?)$`)
	psychInteger     = regexp.MustCompile(`^(?:[-+]?0b[_,]*[0-1][0-1_,]*` +
		`|[-+]?0[_,]*[0-7][0-7_,]*` +
		`|[-+]?(?:0|[1-9](?:[0-9]|,[0-9]|_[0-9])*)` +
		`|[-+]?0x[_,]*[0-9a-fA-F][0-9a-fA-F_,]*)$`)

	// visit_String's own extra rule, for a number that looks octal and is not:
	// "089" scans as a String and still confuses a reader.
	psychFalseOctal = regexp.MustCompile(`^0[0-7]*[89]`)

	// The other half of visit_String: a string whose first character is not a
	// word character is double-quoted rather than single-quoted. That holds as
	// long as there is no double quote inside it to escape. "# hash", "- dash",
	// "¿Qué?" — and, in the real data, nothing at all, until somebody names a
	// tile in the way people actually name things.
	psychNonWordStart = regexp.MustCompile(`^[^\pL\pM\pN_][^"]*$`)
)

// psychStyle is the style Psych asks the emitter for. The zero Style is
// "plain if the emitter can manage it", which is Psych's default too. libyaml
// falls back to single quotes and then to double quotes on its own, and both
// libraries fall back the same way.
func psychStyle(s string) yaml.Style {
	switch {
	// A newline anywhere but the very end makes it a literal block. A string
	// that only *ends* in a newline is not one, and drops through to the
	// quoted styles. If left to its own analysis, go-yaml chooses a literal
	// block, so the answer has to be spelled out rather than left to the
	// emitter.
	case hasInteriorNewline(s):
		return yaml.LiteralStyle
	case strings.Contains(s, "\n"):
		return yaml.DoubleQuotedStyle
	// The merge key, and the four letters YAML 1.1 reads as true and false.
	case s == "<<":
		return yaml.SingleQuotedStyle
	case s == "y", s == "Y", s == "n", s == "N":
		return yaml.DoubleQuotedStyle
	case psychNonWordStart.MatchString(s):
		return yaml.DoubleQuotedStyle
	case !psychScansAsString(s), psychFalseOctal.MatchString(s):
		return yaml.SingleQuotedStyle
	}
	return 0
}

// hasInteriorNewline is Ruby's /\n(?!\Z)/: a newline that is not the last
// character, and not the last character before a single trailing newline.
func hasInteriorNewline(s string) bool {
	for i, r := range s {
		if r != '\n' {
			continue
		}
		rest := s[i+1:]
		if rest != "" && rest != "\n" {
			return true
		}
	}
	return false
}

// psychScansAsString is Psych::ScalarScanner#tokenize reduced to the only
// question this file asks it: does loading this back give a String? Every
// branch that produces a Date, a Time, a Symbol, a number, a boolean or nil is
// a reason to quote.
func psychScansAsString(s string) bool {
	if s == "" {
		return false // tokenize returns nil for an empty string
	}

	if psychProse.MatchString(s) || strings.Contains(s, "\n") {
		if utf8.RuneCountInString(s) > 5 {
			return true
		}
		switch {
		case psychShortNotWord.MatchString(s):
			return true
		case s == "~", psychNull.MatchString(s):
			return false
		case psychTrue.MatchString(s), psychFalse.MatchString(s):
			return false
		}
		return true
	}

	switch {
	case psychTime.MatchString(s),
		psychDate.MatchString(s),
		psychInfinity.MatchString(s),
		psychNaN.MatchString(s),
		psychSymbol.MatchString(s),
		psychSexagesimal.MatchString(s),
		psychInteger.MatchString(s):
		return false
	case psychFloat.MatchString(s):
		// Ruby hands back a lone "." or "+." as a String. If called on it,
		// Float() raises.
		return s == "." || s == "+." || s == "-."
	}
	return true
}
