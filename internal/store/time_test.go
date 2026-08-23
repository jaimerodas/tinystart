package store

import (
	"testing"
	"time"
)

// ActiveRecord appends ".%06d" only when the microseconds are non-zero. Both
// shapes are on disk in production. Anything written here has to make the
// same choice, or the two images disagree about the same row.
func TestRailsTimeValue(t *testing.T) {
	tests := []struct {
		name  string
		given time.Time
		want  any
	}{
		{
			"a whole second gets no fractional part",
			time.Date(2026, 11, 8, 17, 46, 42, 0, time.UTC),
			"2026-11-08 17:46:42",
		},
		{
			"microseconds are six digits, zero padded",
			time.Date(2026, 8, 10, 16, 0, 49, 65150000, time.UTC),
			"2026-08-10 16:00:49.065150",
		},
		{
			"trailing zeros are kept, not trimmed",
			time.Date(2026, 8, 10, 16, 0, 49, 123400000, time.UTC),
			"2026-08-10 16:00:49.123400",
		},
		{
			"nanoseconds are dropped, because Rails has nowhere to put them",
			time.Date(2026, 8, 10, 16, 0, 49, 65150999, time.UTC),
			"2026-08-10 16:00:49.065150",
		},
		{
			"another zone is converted, not annotated",
			time.Date(2026, 8, 10, 16, 0, 49, 0, time.FixedZone("CEST", 2*60*60)),
			"2026-08-10 14:00:49",
		},
		{
			"the zero time is NULL",
			time.Time{},
			nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := railsTime(test.given).Value()
			if err != nil {
				t.Fatalf("Value: %v", err)
			}
			if got != test.want {
				t.Errorf("Value = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRailsTimeScan(t *testing.T) {
	tests := []struct {
		name  string
		given any
		want  time.Time
	}{
		{"with a fractional part", "2026-08-10 16:00:49.065150",
			time.Date(2026, 8, 10, 16, 0, 49, 65150000, time.UTC)},
		{"without one", "2026-11-08 17:46:42",
			time.Date(2026, 11, 8, 17, 46, 42, 0, time.UTC)},
		{"as bytes", []byte("2026-11-08 17:46:42"),
			time.Date(2026, 11, 8, 17, 46, 42, 0, time.UTC)},
		{"already parsed by the driver", time.Date(2026, 11, 8, 17, 46, 42, 0, time.UTC),
			time.Date(2026, 11, 8, 17, 46, 42, 0, time.UTC)},
		{"NULL", nil, time.Time{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got railsTime
			if err := got.Scan(test.given); err != nil {
				t.Fatalf("Scan: %v", err)
			}
			if !time.Time(got).Equal(test.want) {
				t.Errorf("Scan = %v, want %v", time.Time(got), test.want)
			}
		})
	}
}

func TestRailsTimeScanRejectsWhatItCannotRead(t *testing.T) {
	for _, given := range []any{"yesterday", "2026-08-10T16:00:49Z", 17, 3.5} {
		var got railsTime
		if err := got.Scan(given); err == nil {
			t.Errorf("Scan(%v) accepted it, giving %v", given, time.Time(got))
		}
	}
}

// The round trip is what matters in the end. A timestamp written by the store
// and read back has to be the same instant. The text in between has to match
// the text Rails writes for it.
func TestRailsTimeRoundTripsThroughTheDatabase(t *testing.T) {
	tests := []struct {
		name  string
		given time.Time
		want  string
	}{
		{"with microseconds", time.Date(2026, 8, 10, 16, 0, 49, 65150000, time.UTC),
			"2026-08-10 16:00:49.065150"},
		{"on a whole second", time.Date(2026, 11, 8, 17, 46, 42, 0, time.UTC),
			"2026-11-08 17:46:42"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newTestDB(t)
			user := newUser(t, db, "test@example.com")

			if _, err := db.ReplaceConnection(t.Context(), user.ID,
				"https://links.example.com", "a-token", "", test.given); err != nil {
				t.Fatalf("ReplaceConnection: %v", err)
			}

			var text string
			if err := db.sql.QueryRowContext(t.Context(),
				`SELECT token_expires_at FROM connections WHERE user_id = ?`, user.ID).Scan(&text); err != nil {
				t.Fatalf("reading the raw column: %v", err)
			}
			if text != test.want {
				t.Errorf("on disk: %q, want %q", text, test.want)
			}

			stored, err := db.ConnectionForUser(t.Context(), user.ID)
			if err != nil {
				t.Fatalf("ConnectionForUser: %v", err)
			}
			if !stored.TokenExpiresAt.Equal(test.given) {
				t.Errorf("read back %v, want %v", stored.TokenExpiresAt, test.given)
			}
		})
	}
}

// SQLite has no boolean type, and a driver left to itself can store the text
// "true". Rails stores 1 and 0, and so does this.
func TestBooleansAreStoredAsRailsStoresThem(t *testing.T) {
	db := newTestDB(t)
	first := newUser(t, db, "first@example.com")
	second := newUser(t, db, "second@example.com")

	for _, test := range []struct {
		user *User
		want int64
	}{{first, 1}, {second, 0}} {
		var admin, approved any
		if err := db.sql.QueryRowContext(t.Context(),
			`SELECT admin, approved FROM users WHERE id = ?`, test.user.ID).Scan(&admin, &approved); err != nil {
			t.Fatalf("reading the raw columns: %v", err)
		}
		if admin != test.want || approved != test.want {
			t.Errorf("stored admin=%v approved=%v, want %v", admin, approved, test.want)
		}
	}
}
