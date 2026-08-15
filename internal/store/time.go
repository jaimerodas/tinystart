package store

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// Times and booleans on disk are ActiveRecord's, not Go's, and both images
// have to agree about them: the production database is the one Rails wrote,
// and `kamal rollback` puts Rails back in front of whatever Go has written
// since. Everything about that agreement lives in this file.

// railsTimeLayout is a datetime(6) column without its fractional part. Rails'
// quoted_date appends ".%06d" only when the microseconds are non-zero — see
// the "value.usec > 0" branch in ActiveRecord's Quoting — so a timestamp on a
// whole second is stored as "2026-11-08 17:46:42" and one that isn't as
// "2026-08-10 16:00:49.065150". testdata/rails_rows.sql has one of each.
//
// Parsing, the same layout with a ".999999" suffix would accept both, but
// formatting has to make the choice Rails makes, so the suffix is appended by
// hand in Value.
const railsTimeLayout = "2006-01-02 15:04:05"

// railsTime is a time.Time that reads and writes exactly that text. Every
// query in this package wraps its time columns in it:
//
//	rows.Scan(&u.ID, (*railsTime)(&u.CreatedAt))
//	db.Exec(…, railsTime(now))
//
// The struct fields stay plain time.Time, so nothing outside this package ever
// sees the conversion.
//
// The zero Time is SQL NULL and NULL is the zero Time. Rails never stores a
// year-1 timestamp, so the mapping is unambiguous, and it keeps the nullable
// columns (token_expires_at, last_failed_at) as time.Time in the structs
// rather than as *time.Time that every caller would have to nil-check.
type railsTime time.Time

// Value implements driver.Valuer. UTC because that is what ActiveRecord
// stores — its default_timezone is :utc — and the text is compared as text by
// the expiry queries, which only sorts correctly if every row is in the same
// zone. Truncated to microseconds because Rails' format has no room for
// nanoseconds, and a value that does not survive a round trip is a bug waiting
// for a test to compare a written time against a read one.
func (t railsTime) Value() (driver.Value, error) {
	when := time.Time(t)
	if when.IsZero() {
		return nil, nil
	}
	when = when.UTC().Truncate(time.Microsecond)
	text := when.Format(railsTimeLayout)
	if usec := when.Nanosecond() / 1000; usec > 0 {
		text += fmt.Sprintf(".%06d", usec)
	}
	return text, nil
}

// Scan implements sql.Scanner. Three source shapes are handled because the
// driver picks between them: text columns normally arrive as string, but a
// driver that recognises "datetime" in the declared type may hand back a
// time.Time already, and []byte is the shape database/sql uses when it has to
// copy a buffer. Accepting all three costs six lines and removes a class of
// failure that only shows up after a driver upgrade.
func (t *railsTime) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*t = railsTime(time.Time{})
		return nil
	case time.Time:
		*t = railsTime(v.UTC())
		return nil
	case []byte:
		return t.parse(string(v))
	case string:
		return t.parse(v)
	default:
		return fmt.Errorf("store: cannot read %T as a time", src)
	}
}

func (t *railsTime) parse(text string) error {
	// ".999999" makes the fractional part optional and accepts however many
	// digits are actually there, which is what lets one layout read both
	// shapes Rails writes.
	when, err := time.ParseInLocation(railsTimeLayout+".999999", text, time.UTC)
	if err != nil {
		return fmt.Errorf("store: %q is not a Rails timestamp: %w", text, err)
	}
	*t = railsTime(when)
	return nil
}

// railsBool is 1 or 0, which is what ActiveRecord writes into the columns the
// schema calls "boolean" — SQLite has no boolean type, and a driver left to
// its own devices might just as well store the text "true".
//
// Reading needs no counterpart: database/sql already converts 0 and 1 to a Go
// bool when the destination is a *bool.
func railsBool(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
