package web

import (
	"math"
	"strconv"
	"time"
)

// ActionView's distance_of_time_in_words, ported because two pages say a
// duration in prose: Settings says how long ago an account was opened, and
// Connections says how long a token has left.
//
// It lives here rather than in a package of its own because it is a view
// concern and nothing else needs it. The thresholds and the wording are
// Rails' own, down to the odd ones. "About 1 hour" covers forty-five
// minutes to an hour and a half, and a month is always thirty days. The
// point is to say what the deployed app says, not to say it better.

// The boundaries, in minutes, exactly as date_helper.rb writes them.
const (
	minutesInQuarterYear      = 131400
	minutesInThreeQuarterYear = 394200
	minutesInYear             = 525600
)

// distanceOfTimeInWords is the helper itself. The order of the arguments does
// not matter: Rails swaps them when they are the wrong way round, so a token
// that expired yesterday reads the same as one expiring tomorrow.
func distanceOfTimeInWords(from, to time.Time) string {
	if from.After(to) {
		from, to = to, from
	}

	// Rounded to the nearest minute first, and every branch below then works
	// in minutes — including the ones that report hours and days, which round
	// again from this number rather than from the original duration.
	minutes := int(math.Round(to.Sub(from).Minutes()))

	switch {
	case minutes <= 1:
		// include_seconds is never passed by either caller, so the finer
		// wording underneath it ("half a minute", "less than 20 seconds") is
		// unreachable and deliberately not ported.
		if minutes == 0 {
			return "less than a minute"
		}
		return "1 minute"
	case minutes < 45:
		return count(minutes, "minute")
	case minutes < 90:
		return "about 1 hour"
	case minutes < 1440:
		return "about " + count(divRound(minutes, 60), "hour")
	case minutes < 2520:
		return "1 day"
	case minutes < 43200:
		return count(divRound(minutes, 1440), "day")
	case minutes < 86400:
		return "about " + count(divRound(minutes, 43200), "month")
	case minutes < minutesInYear:
		return count(divRound(minutes, 43200), "month")
	}

	// A year is 525600 minutes here, so a span full of leap days creeps:
	// eighty years of them is nearly three months. Rails discounts the leap
	// days inside the span before dividing, which is why this branch is the
	// only one that has to look at the calendar.
	adjusted := minutes - leapDaysBetween(from, to)*1440
	years := adjusted / minutesInYear
	switch remainder := adjusted % minutesInYear; {
	case remainder < minutesInQuarterYear:
		return "about " + count(years, "year")
	case remainder < minutesInThreeQuarterYear:
		return "over " + count(years, "year")
	default:
		return "almost " + count(years+1, "year")
	}
}

// timeAgoInWords is the helper Settings uses, which is the same thing measured
// against the clock.
func timeAgoInWords(from, now time.Time) string {
	return distanceOfTimeInWords(from, now)
}

// leapDaysBetween counts the 29ths of February the span contains. Rails counts
// them by year with a fence either side — a span starting in March misses that
// year's leap day, and one ending before March misses the last year's — rather
// than by walking dates.
func leapDaysBetween(from, to time.Time) int {
	fromYear := from.Year()
	if from.Month() >= time.March {
		fromYear++
	}
	toYear := to.Year()
	if to.Month() < time.March {
		toYear--
	}

	days := 0
	for year := fromYear; year <= toYear; year++ {
		if isLeapYear(year) {
			days++
		}
	}
	return days
}

// divRound is Ruby's `(a.to_f / b).round`: half away from zero, which is what
// math.Round does and what Go's integer division does not.
func divRound(minutes, per int) int {
	return int(math.Round(float64(minutes) / float64(per)))
}

// count is I18n's pluralisation for the six nouns this helper counts, all of
// them regular.
func count(number int, noun string) string {
	if number == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(number) + " " + noun + "s"
}

// isLeapYear is Date.leap?, which is the proleptic Gregorian rule.
func isLeapYear(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}
