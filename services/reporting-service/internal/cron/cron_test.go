package cron

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load %s: %v", name, err)
	}
	return loc
}

// The zoneinfo this package needs is embedded, not borrowed from the machine.
//
// These images are distroless/static and carry no /usr/share/zoneinfo. Without
// the time/tzdata import every named zone fails to load inside the container
// and succeeds on every developer's machine, so the schedules would refuse to
// run in the one place it matters and every test would go on passing.
//
// This test cannot prove the import is what made it work — the developer's
// machine has a zoneinfo directory too — so it is paired with the source check
// below, which can.
func TestNamedZonesLoad(t *testing.T) {
	for _, name := range []string{"Asia/Kolkata", "Europe/London", "America/Sao_Paulo", "UTC"} {
		if _, err := time.LoadLocation(name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestParseRejectsWhatItCannotRead(t *testing.T) {
	for _, tc := range []struct{ expr, because string }{
		{"", "empty"},
		{"* * * *", "four fields"},
		{"* * * * * *", "six fields"},
		{"@daily", "a shorthand"},
		{"60 * * * *", "minute out of range"},
		{"* 24 * * *", "hour out of range"},
		{"* * 0 * *", "day of month is one-based"},
		{"* * * 13 *", "month out of range"},
		{"* * * * 8", "day of week out of range"},
		{"* * * * MON", "names are not read"},
		{"*/0 * * * *", "a step of zero"},
		{"*/x * * * *", "a step that is not a number"},
		{"5-1 * * * *", "a backwards range"},
		{"0 0 30 2 *", "30 February never occurs"},
		{"0 0 31 2 *", "31 February never occurs"},
	} {
		t.Run(tc.expr+" ("+tc.because+")", func(t *testing.T) {
			_, err := Parse(tc.expr)
			if err == nil {
				t.Fatalf("Parse(%q) was accepted; it should be refused because %s", tc.expr, tc.because)
			}
			if !errors.Is(err, ErrMalformed) {
				t.Errorf("Parse(%q) failed with %v, which is not ErrMalformed, so a caller "+
					"cannot tell a bad schedule from a broken parser", tc.expr, err)
			}
		})
	}
}

func TestParseAcceptsTheFormsPeopleWrite(t *testing.T) {
	for _, expr := range []string{
		"* * * * *",
		"0 7 * * *",
		"0 8 1,16 * *",
		"*/15 * * * *",
		"30 2 * * 0",
		"30 2 * * 7", // Sunday written the other way
		"0 0-6/2 * * *",
		"15 9 1 1 *",
		"5/10 * * * *",
	} {
		if _, err := Parse(expr); err != nil {
			t.Errorf("Parse(%q): %v", expr, err)
		}
	}
}

// Sunday is 0 and 7, and a schedule written either way means the same day.
func TestSundayIsBothZeroAndSeven(t *testing.T) {
	loc := time.UTC
	from := time.Date(2026, 9, 23, 12, 0, 0, 0, loc) // a Wednesday

	zero, err := MustParse("30 2 * * 0").Next(from, loc)
	if err != nil {
		t.Fatal(err)
	}
	seven, err := MustParse("30 2 * * 7").Next(from, loc)
	if err != nil {
		t.Fatal(err)
	}
	if !zero.Equal(seven) {
		t.Errorf("Sunday as 0 fires %s and Sunday as 7 fires %s; they are the same day", zero, seven)
	}
	if zero.Weekday() != time.Sunday {
		t.Errorf("fired on %s, not Sunday", zero.Weekday())
	}
}

// The rule that surprises everyone, in the direction it surprises them.
//
// With both day fields restricted, cron ORs them. `0 0 1 * 1` fires on the
// first of the month and on every Monday, not only on a Monday that is the
// first. Read the expression as an AND and a monthly report silently becomes an
// almost-daily one — or, depending which way the mistake goes, a weekly report
// fires once a month and nobody notices until somebody asks where four of them
// went.
func TestBothDayFieldsAreOredNotAnded(t *testing.T) {
	loc := time.UTC
	s := MustParse("0 0 1 * 1") // the 1st, or any Monday

	// September 2026: the 1st is a Tuesday. Mondays are the 7th, 14th, 21st, 28th.
	from := time.Date(2026, 9, 1, 12, 0, 0, 0, loc)

	var got []int
	t0 := from
	for i := 0; i < 5; i++ {
		next, err := s.Next(t0, loc)
		if err != nil {
			t.Fatal(err)
		}
		if next.Month() != time.September {
			break
		}
		got = append(got, next.Day())
		t0 = next
	}

	want := []int{7, 14, 21, 28}
	if len(got) != len(want) {
		t.Fatalf("fired on %v; ORing the two day fields gives %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("fired on %v; ORing the two day fields gives %v", got, want)
		}
	}

	// And the 1st of October fires too, although it is a Thursday — which is
	// the other half of the OR and the half an AND would lose.
	oct, err := s.Next(time.Date(2026, 9, 29, 0, 0, 0, 0, loc), loc)
	if err != nil {
		t.Fatal(err)
	}
	if oct.Month() != time.October || oct.Day() != 1 {
		t.Errorf("after 29 September this fired %s; the 1st of October is in the day-of-month "+
			"half of the OR whatever weekday it falls on", oct.Format(time.RFC3339))
	}
}

// Only one day field restricted means that field alone decides.
func TestOneRestrictedDayFieldDecidesAlone(t *testing.T) {
	loc := time.UTC
	from := time.Date(2026, 9, 23, 12, 0, 0, 0, loc)

	dom, err := MustParse("0 0 15 * *").Next(from, loc)
	if err != nil {
		t.Fatal(err)
	}
	if dom.Day() != 15 {
		t.Errorf("day-of-month 15 fired on the %d", dom.Day())
	}

	dow, err := MustParse("0 0 * * 5").Next(from, loc)
	if err != nil {
		t.Fatal(err)
	}
	if dow.Weekday() != time.Friday {
		t.Errorf("day-of-week Friday fired on %s", dow.Weekday())
	}
}

// The whole reason this package exists.
//
// Seven in the morning is seven where the society is. The same expression in
// two zones is two different instants, and the difference is not cosmetic: the
// India instant is 01:30 UTC, so a report covering "yesterday" run in UTC would
// already have crossed into a different day.
func TestTheZoneIsTheSchedulesNotTheProcessSame(t *testing.T) {
	india := mustLoad(t, "Asia/Kolkata")
	s := MustParse("0 7 * * *")
	from := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)

	inIndia, err := s.Next(from, india)
	if err != nil {
		t.Fatal(err)
	}
	inUTC, err := s.Next(from, time.UTC)
	if err != nil {
		t.Fatal(err)
	}

	if h, m, _ := inIndia.Clock(); h != 7 || m != 0 {
		t.Errorf("in Asia/Kolkata it fired at %02d:%02d local, not 07:00", h, m)
	}
	if inIndia.UTC().Hour() != 1 || inIndia.UTC().Minute() != 30 {
		t.Errorf("07:00 in Asia/Kolkata is 01:30 UTC; this is %s", inIndia.UTC().Format("15:04"))
	}
	if inIndia.Equal(inUTC) {
		t.Error("the same expression gave the same instant in two zones, so the zone was ignored")
	}
}

// A half-hour offset is where truncating against the epoch goes wrong.
//
// time.Truncate works in UTC, so truncating to the minute is fine but anything
// coarser lands 30 minutes out in India. The walk starts from an explicitly
// rebuilt local minute for that reason; this holds it to it.
func TestAHalfHourOffsetLandsOnTheMinuteAsked(t *testing.T) {
	india := mustLoad(t, "Asia/Kolkata")
	s := MustParse("45 6 * * *")
	from := time.Date(2026, 9, 23, 3, 17, 42, 0, india)

	next, err := s.Next(from, india)
	if err != nil {
		t.Fatal(err)
	}
	if h, m, sec := next.Clock(); h != 6 || m != 45 || sec != 0 {
		t.Errorf("fired at %02d:%02d:%02d local, not 06:45:00", h, m, sec)
	}
}

// Next is strictly after, so a runner that has just fired does not fire again.
func TestNextIsStrictlyAfter(t *testing.T) {
	loc := time.UTC
	s := MustParse("0 7 * * *")
	at := time.Date(2026, 9, 23, 7, 0, 0, 0, loc)

	next, err := s.Next(at, loc)
	if err != nil {
		t.Fatal(err)
	}
	if !next.After(at) {
		t.Fatalf("asked for the firing after %s and got %s; a runner would fire it twice", at, next)
	}
	if next.Day() != 24 {
		t.Errorf("the next daily firing after 07:00 on the 23rd is the 24th, not the %d", next.Day())
	}
}

// A clock going forward skips the hour that does not exist.
//
// Europe/London jumps 01:00 to 02:00 on 29 March 2026, so 01:30 does not occur
// that day. The schedule does not fire, and it is not quietly moved — a report
// labelled 01:30 that ran at 02:30 covers a period nobody agreed to.
func TestSpringForwardSkipsTheMissingTime(t *testing.T) {
	london := mustLoad(t, "Europe/London")
	s := MustParse("30 1 * * *")

	next, err := s.Next(time.Date(2026, 3, 28, 12, 0, 0, 0, london), london)
	if err != nil {
		t.Fatal(err)
	}
	if next.Day() != 30 {
		t.Errorf("01:30 does not exist on 29 March 2026 in London, so the next firing is the "+
			"30th; this fired on the %d at %s", next.Day(), next.Format("15:04 MST"))
	}
}

// A clock going back repeats an hour, and the walk must still move forward.
//
// This is the case that made an earlier version of Next walk between two
// instants until its five-year bound gave up, and the zone matters: rebuilding
// "today at 01:00" resolves the ambiguity differently in different zones, and
// Go does not guarantee which. In Europe/London it lands in the second 01:00
// and the walk escapes on its own; in America/New_York it lands in the first,
// and from the second 01:00 the rebuild goes backwards and the step forward
// returns to where it started.
//
// So the schedule would have fired correctly all year and reported, on one
// morning in November, that it does not fire at all — and only for societies in
// some zones. That is why `onward` exists and why this is pinned to the zone
// that shows it.
//
// The hours below are chosen not to match, because a matching hour returns
// before the rebuild is ever reached.
func TestFallBackStillAdvances(t *testing.T) {
	for _, zone := range []string{"America/New_York", "Europe/London"} {
		loc := mustLoad(t, zone)
		// The Sunday each zone puts its clocks back in 2026: 1 November in New
		// York, 25 October in London. Starting the day before covers both.
		from := time.Date(2026, 10, 24, 12, 0, 0, 0, loc)

		for _, expr := range []string{"0 3 * * *", "0 0 * * *", "30 5 * * *", "0 12 * * *"} {
			t.Run(zone+" "+expr, func(t *testing.T) {
				s := MustParse(expr)
				at := from
				// Walk well past both transitions. A stall shows up here as a
				// failure rather than as a silence in production.
				for i := 0; i < 14; i++ {
					next, err := s.Next(at, loc)
					if err != nil {
						t.Fatalf("step %d from %s: %v", i, at.Format(time.RFC3339), err)
					}
					if !next.After(at) {
						t.Fatalf("step %d did not advance: %s then %s", i, at, next)
					}
					at = next
				}
				if at.Before(time.Date(2026, 11, 5, 0, 0, 0, 0, loc)) {
					t.Errorf("fourteen firings of %q from 24 October reached only %s; "+
						"the walk is not getting past the transition",
						expr, at.Format(time.RFC3339))
				}
			})
		}
	}
}

// A schedule that fires rarely still resolves, and quickly.
func TestARareScheduleResolves(t *testing.T) {
	loc := time.UTC
	s := MustParse("0 0 29 2 *") // 29 February

	next, err := s.Next(time.Date(2026, 3, 1, 0, 0, 0, 0, loc), loc)
	if err != nil {
		t.Fatal(err)
	}
	if next.Year() != 2028 || next.Month() != time.February || next.Day() != 29 {
		t.Errorf("the next 29 February after March 2026 is 29 February 2028; got %s",
			next.Format(time.RFC3339))
	}
}

func TestNextRefusesWithoutAZone(t *testing.T) {
	_, err := MustParse("0 7 * * *").Next(time.Now(), nil)
	if err == nil {
		t.Fatal("Next accepted a nil location; the process's own zone is not the co-operative's")
	}
	if !strings.Contains(err.Error(), "timezone") {
		t.Errorf("the refusal does not mention the timezone: %v", err)
	}
}

func TestStepsAndLists(t *testing.T) {
	loc := time.UTC
	from := time.Date(2026, 9, 23, 10, 0, 0, 0, loc)

	quarter := MustParse("*/15 * * * *")
	var minutes []int
	at := from
	for i := 0; i < 4; i++ {
		next, err := quarter.Next(at, loc)
		if err != nil {
			t.Fatal(err)
		}
		minutes = append(minutes, next.Minute())
		at = next
	}
	want := []int{15, 30, 45, 0}
	for i := range want {
		if minutes[i] != want[i] {
			t.Fatalf("*/15 gave minutes %v, want %v", minutes, want)
		}
	}

	// A bare number with a step runs from that number to the end of the field.
	from5 := MustParse("5/20 * * * *")
	at = from
	var m2 []int
	for i := 0; i < 3; i++ {
		next, err := from5.Next(at, loc)
		if err != nil {
			t.Fatal(err)
		}
		m2 = append(m2, next.Minute())
		at = next
	}
	want2 := []int{5, 25, 45}
	for i := range want2 {
		if m2[i] != want2[i] {
			t.Fatalf("5/20 gave minutes %v, want %v", m2, want2)
		}
	}
}
