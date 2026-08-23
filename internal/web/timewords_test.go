package web

import (
	"testing"
	"time"
)

// The expectations are ActionView's own answers, taken from
// `distance_of_time_in_words` in a Rails console rather than derived from the
// port. A table copied from the implementation only proves the
// implementation is itself.

func TestDistanceOfTimeInWords(t *testing.T) {
	base := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		seconds int
		want    string
	}{
		{0, "less than a minute"},
		{29, "less than a minute"},
		{30, "1 minute"}, // rounds up to one minute
		{60, "1 minute"},
		{90, "2 minutes"},
		{120, "2 minutes"},
		{2699, "about 1 hour"},  // 44 minutes 59, still "about an hour"
		{5399, "about 2 hours"}, // 89 minutes 59
		{5400, "about 2 hours"},
		{86399, "1 day"},
		{86400, "1 day"},
		{151200, "2 days"}, // 42 hours, the first "2 days"
		{2591999, "about 1 month"},
		{2592000, "about 1 month"}, // 30 days
		{5184000, "2 months"},      // 60 days
		{31536000, "about 1 year"},
		{63072000, "almost 2 years"},
		{100000000, "about 3 years"},
		{315360000, "almost 10 years"},
		{630720000, "almost 20 years"},
		{1000000000, "over 31 years"},
	}

	for _, test := range tests {
		d := time.Duration(test.seconds) * time.Second
		if got := distanceOfTimeInWords(base, base.Add(d)); got != test.want {
			t.Errorf("distanceOfTimeInWords(+%ds) = %q, want %q", test.seconds, got, test.want)
		}
	}
}

// The years branch discounts the leap days inside the span. That is the only
// place the helper looks at the calendar rather than at a number of minutes.
func TestDistanceOfTimeInWordsDiscountsLeapDays(t *testing.T) {
	tests := []struct {
		from, to time.Time
		want     string
	}{
		{time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), "about 30 years"},
		{time.Date(1990, 3, 15, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), "over 36 years"},
		{time.Date(2020, 2, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC), "over 6 years"},
		{time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "about 1 year"},
	}

	for _, test := range tests {
		if got := distanceOfTimeInWords(test.from, test.to); got != test.want {
			t.Errorf("distanceOfTimeInWords(%s, %s) = %q, want %q",
				test.from.Format(time.DateOnly), test.to.Format(time.DateOnly), got, test.want)
		}
	}
}

// Connections asks how long a token has left, and a token can expire.
// Rails swaps the arguments rather than reporting a negative distance, so both
// directions read the same.
func TestDistanceOfTimeInWordsIgnoresDirection(t *testing.T) {
	base := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	later := base.Add(25 * 24 * time.Hour)

	if forwards, backwards := distanceOfTimeInWords(base, later), distanceOfTimeInWords(later, base); forwards != backwards {
		t.Errorf("forwards = %q, backwards = %q, want them equal", forwards, backwards)
	}
}
