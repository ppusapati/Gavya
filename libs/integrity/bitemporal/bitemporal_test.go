package bitemporal

import (
	"errors"
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func ptr(t time.Time) *time.Time { return &t }

// ---------------------------------------------------------------------------
// Valid time
// ---------------------------------------------------------------------------

// The interval is half-open: [From, To). A fact true from the first of March
// until the first of April is not true on the first of April, because that is
// when its replacement starts.
//
// Closed at both ends there would be a day on which two versions of the same
// fact are both effective, and which one a query returns depends on the order
// the rows came back in.
func TestTheValidIntervalIsHalfOpen(t *testing.T) {
	iv, err := NewInterval(at("2026-03-01T00:00:00Z"), at("2026-04-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("interval: %v", err)
	}
	for _, c := range []struct {
		when string
		want bool
	}{
		{"2026-02-28T23:59:59Z", false},
		{"2026-03-01T00:00:00Z", true}, // inclusive start
		{"2026-03-31T23:59:59Z", true},
		{"2026-04-01T00:00:00Z", false}, // exclusive end
	} {
		if got := iv.Contains(at(c.when)); got != c.want {
			t.Errorf("Contains(%s) = %v, want %v", c.when, got, c.want)
		}
	}
}

// Abutting intervals do not overlap, which is what makes replacing a fact
// expressible at all. If they did, a record and its successor would both be
// effective at the boundary.
func TestAbuttingIntervalsDoNotOverlap(t *testing.T) {
	first, _ := NewInterval(at("2026-01-01T00:00:00Z"), at("2026-04-01T00:00:00Z"))
	second, _ := NewInterval(at("2026-04-01T00:00:00Z"), at("2026-07-01T00:00:00Z"))

	if first.Overlaps(second) || second.Overlaps(first) {
		t.Error("a fact ending on the first of April and its replacement starting there were " +
			"reported as overlapping, so nothing could ever replace anything")
	}

	// One minute of genuine overlap is an overlap, in both directions.
	late, _ := NewInterval(at("2026-03-31T23:59:00Z"), at("2026-07-01T00:00:00Z"))
	if !first.Overlaps(late) || !late.Overlaps(first) {
		t.Error("a genuine one-minute overlap was not reported; the exclusion constraints that " +
			"stop two rate cards being in force at once rest on this being right")
	}
}

// A fact that was true for no time at all is a row no query can ever return.
//
// Half-open means [t, t) contains nothing — not even t. Accepting it writes a
// record that exists, occupies a slot, and is invisible to every read. That is
// worse than refusing it, because nothing ever reports it as missing.
func TestAnIntervalThatIsTrueForNoTimeIsRefused(t *testing.T) {
	moment := at("2026-03-01T00:00:00Z")
	iv, err := NewInterval(moment, moment)
	if err == nil {
		t.Errorf("an interval [%s, %s) was accepted, and it contains its own start: %v",
			moment.Format(time.RFC3339), moment.Format(time.RFC3339), iv.Contains(moment))
	}
	if !errors.Is(err, ErrEmptyInterval) {
		t.Errorf("refused with %v, want ErrEmptyInterval", err)
	}
}

func TestAnInvertedIntervalIsRefused(t *testing.T) {
	_, err := NewInterval(at("2026-04-01T00:00:00Z"), at("2026-01-01T00:00:00Z"))
	if !errors.Is(err, ErrInvertedInterval) {
		t.Errorf("an interval that ends before it starts was accepted: %v", err)
	}
}

func TestAnIntervalMustSayWhenItStarts(t *testing.T) {
	_, err := NewInterval(time.Time{}, at("2026-04-01T00:00:00Z"))
	if !errors.Is(err, ErrZeroValidFrom) {
		t.Errorf("an interval with no start was accepted: %v", err)
	}
}

// EndOfTime is a concrete sentinel rather than NULL, so interval overlap stays
// expressible as a plain SQL predicate. An open interval has to behave like one
// that runs a very long time, not like one with a hole in it.
func TestAnOpenIntervalRunsUntilSuperseded(t *testing.T) {
	open := OpenInterval(at("2026-01-01T00:00:00Z"))
	if !open.IsOpen() {
		t.Error("an open interval does not report itself as open")
	}
	if !open.Contains(at("2999-01-01T00:00:00Z")) {
		t.Error("an open interval stopped containing anything before EndOfTime")
	}
	if open.Contains(at("2025-12-31T23:59:59Z")) {
		t.Error("an open interval contains a moment before it started")
	}
	closed, _ := NewInterval(at("2026-01-01T00:00:00Z"), at("2026-04-01T00:00:00Z"))
	if closed.IsOpen() {
		t.Error("a closed interval reports itself as open")
	}
}

// ---------------------------------------------------------------------------
// Transaction time
// ---------------------------------------------------------------------------

// At the instant a correction lands, exactly one version is the platform's
// answer: the new one is recorded and the old one is superseded, both at the
// same moment.
//
// Get either boundary wrong and there is an instant with two current versions or
// none. A settlement replayed at exactly that instant then returns two answers
// or an empty one, and neither is defensible.
func TestAtTheInstantOfACorrectionExactlyOneVersionIsCurrent(t *testing.T) {
	moment := at("2026-05-01T12:00:00Z")
	iv := OpenInterval(at("2026-01-01T00:00:00Z"))

	old := Version{Valid: iv, RecordedAt: at("2026-02-01T00:00:00Z"), SupersededAt: ptr(moment)}
	replacement := Version{Valid: iv, RecordedAt: moment}

	valid := at("2026-03-01T00:00:00Z")
	for _, c := range []struct {
		asOf         string
		oldEffective bool
		newEffective bool
	}{
		{"2026-04-30T00:00:00Z", true, false}, // before the correction
		{"2026-05-01T11:59:59Z", true, false}, // one second before
		{"2026-05-01T12:00:00Z", false, true}, // the instant itself
		{"2026-06-01T00:00:00Z", false, true}, // after
	} {
		gotOld := old.EffectiveAt(valid, at(c.asOf))
		gotNew := replacement.EffectiveAt(valid, at(c.asOf))
		if gotOld != c.oldEffective || gotNew != c.newEffective {
			t.Errorf("as of %s: old=%v new=%v, want old=%v new=%v",
				c.asOf, gotOld, gotNew, c.oldEffective, c.newEffective)
		}
		if gotOld && gotNew {
			t.Errorf("as of %s both versions are the platform's answer", c.asOf)
		}
		if !gotOld && !gotNew {
			t.Errorf("as of %s neither version is the platform's answer, so the fact has "+
				"vanished for that instant", c.asOf)
		}
	}
}

// A version is not known before it was recorded. Without this a settlement
// replayed "as we knew it in March" would include an April correction, which is
// the one thing replay exists to prevent.
func TestAVersionIsNotKnownBeforeItWasRecorded(t *testing.T) {
	v := Version{Valid: OpenInterval(at("2026-01-01T00:00:00Z")),
		RecordedAt: at("2026-04-01T00:00:00Z")}
	if v.KnownAt(at("2026-03-01T00:00:00Z")) {
		t.Error("a correction recorded in April was known in March")
	}
	if !v.KnownAt(at("2026-04-01T00:00:00Z")) {
		t.Error("a version is not known at the instant it was recorded, so there is a moment " +
			"where it exists and cannot be seen")
	}
}

func TestIsCurrentIsAboutSupersessionNotTime(t *testing.T) {
	live := Version{RecordedAt: at("2026-01-01T00:00:00Z")}
	if !live.IsCurrent() {
		t.Error("a version nothing superseded is not current")
	}
	dead := Version{RecordedAt: at("2026-01-01T00:00:00Z"),
		SupersededAt: ptr(at("2026-02-01T00:00:00Z")), SupersededBy: "v2"}
	if dead.IsCurrent() {
		t.Error("a superseded version is still current")
	}
}

// ---------------------------------------------------------------------------
// The property the package exists for
// ---------------------------------------------------------------------------

type record struct {
	name string
	v    Version
}

// A correction recorded later must not change the answer to a question asked
// about an earlier moment.
//
// This is what "replayable as we knew it on date X" means, and it is the whole
// basis of shadow mode: a settlement recomputed a year later has to reach for
// the facts as they stood then, not as they stand now. If a later correction
// leaked into an earlier replay, every reconciliation the platform has ever
// produced would be unreproducible.
func TestALaterCorrectionDoesNotChangeAnEarlierReplay(t *testing.T) {
	valid := OpenInterval(at("2026-01-01T00:00:00Z"))
	history := []record{
		{"as first recorded", Version{Valid: valid,
			RecordedAt:   at("2026-01-15T00:00:00Z"),
			SupersededAt: ptr(at("2026-06-01T00:00:00Z")), SupersededBy: "corrected"}},
		{"corrected in June", Version{Valid: valid, RecordedAt: at("2026-06-01T00:00:00Z")}},
	}
	versionOf := func(r record) Version { return r.v }
	askAbout := at("2026-02-01T00:00:00Z")

	// The question asked in March, before the correction existed.
	inMarch := Snapshot(history, versionOf, askAbout, at("2026-03-01T00:00:00Z"))
	if len(inMarch) != 1 || inMarch[0].name != "as first recorded" {
		t.Fatalf("replayed as of March and got %v; a correction that did not exist yet has "+
			"leaked into it", names(inMarch))
	}

	// The same question asked in July, after it.
	inJuly := Snapshot(history, versionOf, askAbout, at("2026-07-01T00:00:00Z"))
	if len(inJuly) != 1 || inJuly[0].name != "corrected in June" {
		t.Fatalf("replayed as of July and got %v; the correction is not being applied",
			names(inJuly))
	}

	// And asking about March again gives what it gave the first time. A replay
	// that drifts is not a replay.
	again := Snapshot(history, versionOf, askAbout, at("2026-03-01T00:00:00Z"))
	if len(again) != 1 || again[0].name != inMarch[0].name {
		t.Errorf("the same question asked twice about the same moment gave %v then %v",
			names(inMarch), names(again))
	}
}

// A version valid for a period the question does not fall in is not the answer,
// however recently it was recorded.
func TestValidTimeAndTransactionTimeAreBothRequired(t *testing.T) {
	march, _ := NewInterval(at("2026-03-01T00:00:00Z"), at("2026-04-01T00:00:00Z"))
	april, _ := NewInterval(at("2026-04-01T00:00:00Z"), at("2026-05-01T00:00:00Z"))
	history := []record{
		{"the March rate", Version{Valid: march, RecordedAt: at("2026-01-01T00:00:00Z")}},
		{"the April rate", Version{Valid: april, RecordedAt: at("2026-01-01T00:00:00Z")}},
	}
	versionOf := func(r record) Version { return r.v }

	got := Snapshot(history, versionOf, at("2026-03-15T00:00:00Z"), at("2026-12-01T00:00:00Z"))
	if len(got) != 1 || got[0].name != "the March rate" {
		t.Errorf("asked what was true in March and got %v; both were recorded on the same day, "+
			"so only valid time separates them", names(got))
	}
}

// Snapshot preserves the order it was given. A caller that ordered its rows and
// gets them back shuffled has to sort again, and the sort it reaches for is
// rarely the one it wanted.
func TestSnapshotPreservesInputOrder(t *testing.T) {
	valid := OpenInterval(at("2026-01-01T00:00:00Z"))
	v := Version{Valid: valid, RecordedAt: at("2026-01-01T00:00:00Z")}
	history := []record{{"c", v}, {"a", v}, {"b", v}}
	got := Snapshot(history, func(r record) Version { return r.v },
		at("2026-02-01T00:00:00Z"), at("2026-02-01T00:00:00Z"))
	if len(got) != 3 || got[0].name != "c" || got[1].name != "a" || got[2].name != "b" {
		t.Errorf("Snapshot returned %v, want the order it was given", names(got))
	}
}

func TestSnapshotOfNothingIsEmptyRatherThanEverything(t *testing.T) {
	valid, _ := NewInterval(at("2026-03-01T00:00:00Z"), at("2026-04-01T00:00:00Z"))
	history := []record{{"March", Version{Valid: valid, RecordedAt: at("2026-01-01T00:00:00Z")}}}
	got := Snapshot(history, func(r record) Version { return r.v },
		at("2026-09-01T00:00:00Z"), at("2026-12-01T00:00:00Z"))
	if len(got) != 0 {
		t.Errorf("asked about September and got %v", names(got))
	}
}

func names(rs []record) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.name)
	}
	return out
}
