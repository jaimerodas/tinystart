package store

import (
	"errors"
	"strings"
	"unicode"
)

// The two sentinels every caller can branch on with errors.Is.
var (
	// ErrNotFound stands in for sql.ErrNoRows, so nothing outside this
	// package needs database/sql to read an error.
	ErrNotFound = errors.New("store: not found")

	// ErrConflict means a unique index refused a write and there is no form
	// field to hang the message on — a second connection for the same user,
	// say. Everything a person typed comes back as a ValidationError instead,
	// carrying "has already been taken" the way Rails did.
	ErrConflict = errors.New("store: conflict")
)

// FieldError is one rejected attribute: the column it belongs to and what is
// wrong with it, worded exactly as ActiveRecord worded it.
type FieldError struct {
	// Attribute is the column name, so a form can mark the field it belongs
	// to: "name", "url", "theme_preference".
	Attribute string
	// Message is the sentence fragment on its own, without the attribute:
	// "can't be blank", "has already been taken".
	Message string
}

// FullMessage is Rails' errors.full_messages entry: the humanised attribute
// name and the message. "Url must be a valid URL", "Theme preference neon is
// not a valid theme". The pages show these strings verbatim, so FullMessage
// must return them already assembled.
func (f FieldError) FullMessage() string {
	return humanize(f.Attribute) + " " + f.Message
}

// ValidationError is what a write returns when validation refuses the record.
// It is a slice because ActiveRecord runs every validation and reports all of
// them at once, and the editor joins them with ", ". A single value cannot
// carry every message, and that changes what the page says.
//
// Callers pull it out with errors.As:
//
//	var invalid store.ValidationError
//	if errors.As(err, &invalid) { … invalid.FullMessages() … }
type ValidationError []FieldError

func (v ValidationError) Error() string {
	return strings.Join(v.FullMessages(), ", ")
}

// FullMessages returns every message in the order the model declares its
// validations. This code runs them in that same order, which matches Rails'
// own order for errors.full_messages.
func (v ValidationError) FullMessages() []string {
	messages := make([]string, len(v))
	for i, field := range v {
		messages[i] = field.FullMessage()
	}
	return messages
}

// On reports whether the named attribute was one of the rejected ones, for a
// form that decides which field to outline.
func (v ValidationError) On(attribute string) bool {
	for _, field := range v {
		if field.Attribute == attribute {
			return true
		}
	}
	return false
}

// invalid turns a list of field errors into an error, or into nil when the
// list is empty. Returning ValidationError(nil) directly gives the caller a
// non-nil error interface that wraps an empty slice, which is the classic Go
// way to make `if err != nil` lie.
func invalid(fields ...FieldError) error {
	if len(fields) == 0 {
		return nil
	}
	return ValidationError(fields)
}

// humanize is ActiveModel's attribute name in a sentence: underscores become
// spaces and the first letter is capitalised. It is deliberately that
// simple — every attribute in this schema is a lowercase, underscored word,
// and Rails has no custom human_attribute_name here either.
func humanize(attribute string) string {
	spaced := strings.ReplaceAll(attribute, "_", " ")
	if spaced == "" {
		return spaced
	}
	runes := []rune(spaced)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// The messages themselves, named so a typo is a compile error rather than a
// page that says something slightly different from the one it replaced. Each
// is ActiveRecord's default for the validation it belongs to.
const (
	msgBlank = "can't be blank"
	msgTaken = "has already been taken"
)
