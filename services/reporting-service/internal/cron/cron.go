// Package cron reads a five-field schedule and says when it next fires.
//
// Written here rather than taken from a library for one reason: the answer has
// to be in the co-operative's own timezone, and the timezone has to be the
// schedule's rather than the process's. A society that asks for its collection
// report at seven is asking for seven in the morning where the society is. Run
// in UTC that is half past twelve in the afternoon in Maharashtra, and the
// report covering "yesterday" covers a day that ended five and a half hours
// before the one everybody means.
//
// So Next takes a location, every comparison happens inside it, and nothing in
// this package reads the machine's clock or the machine's zone.
//
// # What is supported
//
//	minute        0-59
//	hour          0-23
//	day of month  1-31
//	month         1-12
//	day of week   0-6, Sunday is 0 and 7 is also Sunday
//
// Each field is `*`, a number, a range `a-b`, a step `*/n` or `a-b/n`, or a
// comma-separated list of those. Names — JAN, MON — are not supported, and a
// schedule using one is refused rather than silently read as something else.
//
// The extensions some cron implementations add are deliberately absent: no
// `@daily`, no `L`, no `W`, no seconds field. A schedule written for one of
// those is refused by name, which is the outcome somebody can act on. Accepting
// the string and firing at a different time is the outcome they cannot.
package cron

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	// Embedded so a named zone resolves inside the container.
	//
	// These images are distroless/static, which carries no /usr/share/zoneinfo.
	// Without this import time.LoadLocation("Asia/Kolkata") returns an error on
	// every deployment and succeeds on every developer's machine — the schedule
	// would refuse to run in exactly the place it matters, and the test that
	// proves otherwise would pass locally.
	_ "time/tzdata"
)

// ErrMalformed marks a schedule this package will not read.
var ErrMalformed = errors.New("malformed schedule")

func malformed(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMalformed, fmt.Sprintf(format, args...))
}

// Schedule is a parsed five-field expression.
//
// The fields are bitmaps rather than lists: membership is the only question
// ever asked of them, and Next asks it once per candidate minute.
type Schedule struct {
	expr string

	minute uint64 // bits 0-59
	hour   uint64 // bits 0-23
	dom    uint64 // bits 1-31
	month  uint64 // bits 1-12
	dow    uint64 // bits 0-6

	// domRestricted and dowRestricted record whether each day field was
	// written as `*`. The pair decides how the two are combined, which is the
	// one rule in cron that surprises people — see matchesDay.
	domRestricted bool
	dowRestricted bool
}

// String returns the expression this was parsed from.
func (s *Schedule) String() string { return s.expr }

type field struct {
	name     string
	min, max uint
}

var (
	fMinute = field{"minute", 0, 59}
	fHour   = field{"hour", 0, 23}
	fDOM    = field{"day of month", 1, 31}
	fMonth  = field{"month", 1, 12}
	fDOW    = field{"day of week", 0, 7} // 7 is Sunday, normalised to 0
)

// Parse reads a five-field expression.
func Parse(expr string) (*Schedule, error) {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return nil, malformed("a schedule cannot be empty")
	}
	if strings.HasPrefix(trimmed, "@") {
		return nil, malformed("%q is a shorthand this platform does not read; "+
			"write it out as five fields, so that what it means is on the page", trimmed)
	}

	parts := strings.Fields(trimmed)
	if len(parts) != 5 {
		return nil, malformed("%q has %d fields and a schedule has five: "+
			"minute, hour, day of month, month, day of week", trimmed, len(parts))
	}

	s := &Schedule{expr: trimmed}
	var err error
	if s.minute, err = parseField(parts[0], fMinute); err != nil {
		return nil, err
	}
	if s.hour, err = parseField(parts[1], fHour); err != nil {
		return nil, err
	}
	if s.dom, err = parseField(parts[2], fDOM); err != nil {
		return nil, err
	}
	if s.month, err = parseField(parts[3], fMonth); err != nil {
		return nil, err
	}
	if s.dow, err = parseField(parts[4], fDOW); err != nil {
		return nil, err
	}

	// Sunday is both 0 and 7, so a schedule written either way means the same
	// day. Normalised here rather than at every comparison.
	if s.dow&(1<<7) != 0 {
		s.dow |= 1 << 0
		s.dow &^= 1 << 7
	}

	s.domRestricted = parts[2] != "*"
	s.dowRestricted = parts[4] != "*"

	// A schedule that can never fire is a schedule somebody will wait on
	// forever. 30 February is the one this catches, and catching it at
	// creation is the difference between an error message and a report that
	// simply never arrives.
	if !s.everPossible() {
		return nil, malformed("%q names a date that does not occur: "+
			"no day it allows falls in any month it allows", trimmed)
	}
	return s, nil
}

// MustParse is Parse for expressions written in this repository's own source.
func MustParse(expr string) *Schedule {
	s, err := Parse(expr)
	if err != nil {
		panic(err)
	}
	return s
}

func parseField(text string, f field) (uint64, error) {
	if text == "" {
		return 0, malformed("the %s field is empty", f.name)
	}
	var bits uint64
	for _, part := range strings.Split(text, ",") {
		b, err := parseRange(part, f)
		if err != nil {
			return 0, err
		}
		bits |= b
	}
	if bits == 0 {
		return 0, malformed("the %s field %q matches nothing", f.name, text)
	}
	return bits, nil
}

func parseRange(text string, f field) (uint64, error) {
	spec, stepText, hasStep := strings.Cut(text, "/")

	step := uint(1)
	if hasStep {
		n, err := strconv.ParseUint(stepText, 10, 32)
		if err != nil {
			return 0, malformed("the %s field has step %q, which is not a number", f.name, stepText)
		}
		if n == 0 {
			return 0, malformed("the %s field has a step of zero, which would match nothing", f.name)
		}
		step = uint(n)
	}

	var lo, hi uint
	switch {
	case spec == "*":
		lo, hi = f.min, f.max
	case strings.Contains(spec, "-"):
		a, b, _ := strings.Cut(spec, "-")
		var err error
		if lo, err = parseValue(a, f); err != nil {
			return 0, err
		}
		if hi, err = parseValue(b, f); err != nil {
			return 0, err
		}
		if lo > hi {
			// Wrapping ranges — FRI-MON — are what somebody writing this
			// meant, and reading it as an empty set would fire never while
			// looking correct. Refused, so they write the two parts.
			return 0, malformed("the %s field has range %q running backwards; "+
				"write it as two parts separated by a comma", f.name, spec)
		}
	default:
		v, err := parseValue(spec, f)
		if err != nil {
			return 0, err
		}
		lo = v
		// A bare number with a step means "from here to the end of the field",
		// which is how every cron reads `5/10`. Without a step it is one value.
		if hasStep {
			hi = f.max
		} else {
			hi = v
		}
	}

	var bits uint64
	for v := lo; v <= hi; v += step {
		bits |= 1 << v
	}
	return bits, nil
}

func parseValue(text string, f field) (uint, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(text), 10, 32)
	if err != nil {
		return 0, malformed("the %s field has %q, which is not a number "+
			"(names such as MON and JAN are not read here)", f.name, text)
	}
	v := uint(n)
	if v < f.min || v > f.max {
		return 0, malformed("the %s field has %d, which is outside %d-%d", f.name, v, f.min, f.max)
	}
	return v, nil
}

// matchesDay applies cron's day rule.
//
// When both day fields are restricted the two are ORed, not ANDed: `0 0 1 * 1`
// fires on the first of the month AND on every Monday, not only on a Monday
// that is the first. This is the behaviour of every cron anybody has used, and
// it is the opposite of what the expression looks like it says — which is
// exactly why it is written down here with a test against it rather than left
// to whoever reads the loop below.
func (s *Schedule) matchesDay(t time.Time) bool {
	domHit := s.dom&(1<<uint(t.Day())) != 0
	dowHit := s.dow&(1<<uint(t.Weekday())) != 0

	switch {
	case s.domRestricted && s.dowRestricted:
		return domHit || dowHit
	case s.domRestricted:
		return domHit
	case s.dowRestricted:
		return dowHit
	default:
		return true
	}
}

// everPossible reports whether any month this schedule allows contains any day
// of the month it allows.
//
// Only meaningful when the day of week is unrestricted; a restricted day of
// week can always be reached, because every weekday occurs in every month.
func (s *Schedule) everPossible() bool {
	if !s.domRestricted || s.dowRestricted {
		return true
	}
	// Days in each month, taking February at 29 so a leap year counts.
	days := [13]uint{0, 31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	for m := uint(1); m <= 12; m++ {
		if s.month&(1<<m) == 0 {
			continue
		}
		for d := uint(1); d <= days[m]; d++ {
			if s.dom&(1<<d) != 0 {
				return true
			}
		}
	}
	return false
}

// maxLookahead bounds the search.
//
// Five years, which is longer than any schedule this package accepts can go
// without firing — 29 February in a month list of February alone is the worst
// case and recurs within four. The bound exists so that a schedule nobody
// foresaw returns an error rather than spinning a sweep for ever.
const maxLookahead = 5 * 366 * 24 * time.Hour

// maxSteps bounds the search a second way, and the second way is the one that
// matters.
//
// The time bound above assumes the walk moves forward. A walk that does not —
// see `onward` — never reaches it either, and spends two and a half million
// iterations finding that out, on every sweep, for every schedule. The symptom
// is a sweep that stops rather than a schedule that errors, which is the harder
// of the two to diagnose and the one nothing alerts on.
//
// A correct walk skips whole months, days and hours, so it finishes in a few
// thousand steps even for 29 February. This is two orders of magnitude above
// that: high enough never to cut a real answer short, low enough that a stall
// is reported in milliseconds.
const maxSteps = 200_000

// Next is the first firing strictly after after, in loc.
//
// Strictly after, so that a runner which has just fired a schedule and asks for
// the next one does not get the same minute back and fire it again.
//
// # Daylight saving
//
// The search walks minutes in loc and asks each one whether it matches, so the
// two awkward cases fall out rather than being special-cased, and both are the
// conservative answer:
//
//   - A clock going forward skips the times in the gap. A schedule set for 02:30
//     in a zone that jumps 02:00 to 03:00 does not fire that day. It is not
//     silently moved to 03:30, because a report labelled 02:30 that ran at 03:30
//     is a report whose period somebody will later disbelieve.
//   - A clock going back repeats an hour. The schedule fires once, on the first
//     pass, because Next is strictly after the previous firing and the second
//     02:30 is not after the first by wall clock but is by instant — the walk
//     advances by instants, so it moves past both.
func (s *Schedule) Next(after time.Time, loc *time.Location) (time.Time, error) {
	if loc == nil {
		return time.Time{}, errors.New("cron: a schedule has no next firing without a timezone; " +
			"the process's own zone is not the co-operative's")
	}

	// From the start of the next minute, in loc. Truncate is deliberately not
	// used: it truncates against the epoch in UTC, which is wrong in a zone
	// whose offset is not a whole number of hours — India is +05:30, and half
	// this platform's schedules will be there.
	t := after.In(loc)
	t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, loc).Add(time.Minute)

	limit := t.Add(maxLookahead)
	for steps := 0; t.Before(limit); steps++ {
		if steps >= maxSteps {
			return time.Time{}, fmt.Errorf(
				"cron: the walk for %q in %s took %d steps without reaching %s, so it is not "+
					"advancing; this is a bug in this package rather than a fault in the schedule",
				s.expr, loc, steps, limit.Format(time.RFC3339))
		}
		switch {
		case s.month&(1<<uint(t.Month())) == 0:
			// Skip whole months rather than walking minutes: a schedule for
			// February alone would otherwise step through eleven months a
			// minute at a time, half a million iterations, on every sweep.
			t = onward(t, time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, loc))
		case !s.matchesDay(t):
			t = onward(t, time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1))
		case s.hour&(1<<uint(t.Hour())) == 0:
			t = onward(t, time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, loc).Add(time.Hour))
		case s.minute&(1<<uint(t.Minute())) == 0:
			t = t.Add(time.Minute)
		default:
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cron: %q does not fire within five years of %s in %s",
		s.expr, after.In(loc).Format(time.RFC3339), loc)
}

// onward guarantees the walk moves forward in instants.
//
// The three skips above are computed by rebuilding a wall-clock time and
// stepping it, and a wall clock does not always move forward. In a zone whose
// clock goes back, one o'clock happens twice: rebuilding "this day at 01:00"
// while standing in the second one lands in the first, and stepping an hour
// from there returns to the second. The walk would alternate between two
// instants until the five-year bound gave up — a schedule that fires correctly
// on every other day of the year and reports, on one morning in the autumn,
// that it does not fire at all.
//
// So a computed jump is taken only when it is genuinely ahead. When it is not,
// the walk falls back to one minute, which always is.
func onward(from, to time.Time) time.Time {
	if to.After(from) {
		return to
	}
	return from.Add(time.Minute)
}
