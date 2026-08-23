package store

import (
	"database/sql/driver"
	"fmt"
	"time"
)

// Times and booleans on disk are ActiveRecord's, not Go's. The production
// database is the one Rails wrote, and every row written since keeps the
// same shape — one format, whoever wrote the row. Everything about that
// format lives in this file.

// railsTimeLayout is a datetime(6) column without its fractional part. Rails'
// quoted_date appends ".%06d" only when the microseconds are non-zero — see
// the "value.usec > 0" branch in ActiveRecord's Quoting. As a result, Rails
// stores a timestamp on a whole second as "2026-11-08 17:46:42" and one that
// is not as "2026-08-10 16:00:49.065150". testdata/rails_rows.sql has one of
// each.
//
// For parsing, the same layout with a ".999999" suffix accepts both forms.
// But formatting has to make the choice Rails makes, so Value appends the
// suffix by hand.
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
// year-1 timestamp, so the mapping is unambiguous. It keeps the nullable
// columns (token_expires_at, last_failed_at) as time.Time in the structs
// rather than as *time.Time, which forces every caller to nil-check.
type railsTime time.Time

// utcNow returns the moment used to stamp a row, already in the shape it will
// have on disk: UTC and whole microseconds. Every created_at, updated_at and
// expires_at in this package comes from here and not from time.Now()
// directly. As a result, the struct a write hands back carries exactly the
// value a later read returns. time.Now() itself has nanoseconds on Linux and
// microseconds on macOS. That is how a comparison between the two passed on a
// laptop and failed on the first CI run.
func utcNow() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

// Value implements driver.Valuer. It is UTC because that is what ActiveRecord
// stores — its default_timezone is :utc. The expiry queries compare the text
// as text, which only sorts correctly if every row is in the same zone. It is
// truncated to microseconds because Rails' format has no room for
// nanoseconds. A value that does not survive a round trip is a bug that a
// test only catches by comparing a written time against a read one.
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

// Scan implements sql.Scanner, and it handles three source shapes because the
// driver picks between them. Text columns normally arrive as string, but a
// driver that recognizes "datetime" in the declared type can hand back a
// time.Time already. []byte is the shape database/sql uses when it has to
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
// schema calls "boolean". SQLite has no boolean type, and a driver left to
// its own devices can just as well store the text "true".
//
// Reading needs no counterpart: database/sql already converts 0 and 1 to a Go
// bool when the destination is a *bool.
func railsBool(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
