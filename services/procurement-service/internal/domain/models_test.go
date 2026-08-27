package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/ratecard"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func point(v int64, scale int32) ratecard.Point { return ratecard.Point{Value: v, Scale: scale} }

func goodCollection() *Collection {
	return &Collection{
		TenantID: "t", ProducerRef: "P-001",
		CollectedOn: day("2026-03-01"), Shift: Morning,
		Quantity: point(12500, 3), Unit: ratecard.PerLitre,
		Actor: "clerk",
	}
}

// ---------------------------------------------------------------------------
// A collection that cannot be settled
// ---------------------------------------------------------------------------

// Each of these produces a row that looks like a collection, joins like a
// collection, and cannot be settled. That is worse than no row at all, because
// the totals it changes are believed.
func TestACollectionThatCannotBeSettledIsRefused(t *testing.T) {
	for _, c := range []struct {
		what  string
		spoil func(*Collection)
		want  error
	}{
		{"no producer, so nobody to pay",
			func(c *Collection) { c.ProducerRef = "" }, ErrNoProducer},
		{"no quantity, so no milk recorded",
			func(c *Collection) { c.Quantity = point(0, 3) }, ErrNoQuantity},
		{"a negative quantity",
			func(c *Collection) { c.Quantity = point(-1, 3) }, ErrNoQuantity},
		{"no shift, so which of the day's two deliveries is unknown",
			func(c *Collection) { c.Shift = "" }, ErrNoShift},
		{"a shift nobody defined",
			func(c *Collection) { c.Shift = Shift("MIDDAY") }, ErrNoShift},
	} {
		coll := goodCollection()
		c.spoil(coll)
		if err := coll.Validate(); !errors.Is(err, c.want) {
			t.Errorf("%s: accepted with %v, want %v", c.what, err, c.want)
		}
	}

	// The ones with no named error still have to be refused.
	for _, c := range []struct {
		what  string
		spoil func(*Collection)
	}{
		{"no tenant", func(c *Collection) { c.TenantID = "" }},
		{"no collection date", func(c *Collection) { c.CollectedOn = time.Time{} }},
		{"no actor", func(c *Collection) { c.Actor = "" }},
	} {
		coll := goodCollection()
		c.spoil(coll)
		if err := coll.Validate(); err == nil {
			t.Errorf("%s: accepted", c.what)
		}
	}

	if err := goodCollection().Validate(); err != nil {
		t.Errorf("an ordinary collection was refused: %v", err)
	}
}

// Litres and kilograms differ by about three per cent, which is larger than most
// of the margins in this business. A collection that does not say which it is
// prices at whatever the reader assumes.
func TestACollectionMustSayWhichUnitItIsIn(t *testing.T) {
	for _, u := range []ratecard.Basis{"", "GALLONS", "litres"} {
		c := goodCollection()
		c.Unit = u
		if err := c.Validate(); err == nil {
			t.Errorf("unit %q was accepted", u)
		}
	}
	for _, u := range []ratecard.Basis{ratecard.PerLitre, ratecard.PerKg} {
		c := goodCollection()
		c.Unit = u
		if err := c.Validate(); err != nil {
			t.Errorf("unit %q was refused: %v", u, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Corrections
// ---------------------------------------------------------------------------

func goodCorrection() *Correction {
	return &Correction{
		TenantID: "t", ID: "COLL-1",
		Quantity: point(12600, 3), Unit: ratecard.PerLitre,
		Fat: point(41, 1), SNF: point(85, 1),
		Reason: "re-weighed after the scale was recalibrated",
		Actor:  "supervisor",
	}
}

// A figure that changes with no reason recorded is indistinguishable from
// tampering, and it is the row an auditor stops at.
func TestACorrectionMustSayWhyTheFigureChanged(t *testing.T) {
	c := goodCorrection()
	c.Reason = ""
	if err := c.Validate(); !errors.Is(err, ErrNoCorrectionReason) {
		t.Errorf("a correction with no reason was accepted: %v", err)
	}
	if err := goodCorrection().Validate(); err != nil {
		t.Errorf("a correction with a reason was refused: %v", err)
	}
}

func TestACorrectionMustNameWhatItCorrects(t *testing.T) {
	c := goodCorrection()
	c.ID = ""
	if err := c.Validate(); err == nil {
		t.Error("a correction that names no collection was accepted")
	}
}

func TestACorrectionCarriesAQuantityAndItsUnit(t *testing.T) {
	for _, spoil := range []func(*Correction){
		func(c *Correction) { c.Quantity = point(0, 3) },
		func(c *Correction) { c.Quantity = point(-5, 3) },
		func(c *Correction) { c.Unit = "" },
		func(c *Correction) { c.Unit = "GALLONS" },
		func(c *Correction) { c.TenantID = "" },
		func(c *Correction) { c.Actor = "" },
	} {
		c := goodCorrection()
		spoil(c)
		if err := c.Validate(); err == nil {
			t.Errorf("accepted: %+v", c)
		}
	}
}

// ---------------------------------------------------------------------------
// Which day a collection belongs to
// ---------------------------------------------------------------------------

// A collection belongs to a day and a shift, not to an instant.
//
// Keeping the time would mean the same morning's milk sorting differently
// depending on when the operator got to the keyboard, and a producer's fortnight
// would depend on that.
func TestACollectionBelongsToADayNotAnInstant(t *testing.T) {
	early := Day(time.Date(2026, 3, 1, 5, 30, 0, 0, time.UTC))
	late := Day(time.Date(2026, 3, 1, 23, 59, 59, 0, time.UTC))
	if !early.Equal(late) {
		t.Errorf("milk recorded at 05:30 and at 23:59 on the same day landed on %s and %s",
			early.Format(time.RFC3339), late.Format(time.RFC3339))
	}
	if !early.Equal(day("2026-03-01")) {
		t.Errorf("Day gave %s, want the date with the time discarded",
			early.Format(time.RFC3339))
	}
	if h, m, s := early.Clock(); h|m|s != 0 {
		t.Errorf("Day left %02d:%02d:%02d on the clock", h, m, s)
	}
	if early.Location() != time.UTC {
		t.Errorf("Day returned a time in %v; a day that depends on the reader's zone is not "+
			"a day two people can agree about", early.Location())
	}
}

// The day is taken in UTC, so a shift recorded either side of midnight local
// time does not land on two different days depending on the operator's clock.
func TestDayIsTakenInUTC(t *testing.T) {
	kolkata := time.FixedZone("IST", 5*3600+1800)
	// 04:00 IST on the second of March is 22:30 UTC on the first.
	local := time.Date(2026, 3, 2, 4, 0, 0, 0, kolkata)
	if got := Day(local); !got.Equal(day("2026-03-01")) {
		t.Errorf("Day(%s) = %s, want 2026-03-01 — the instant in UTC, not the local wall date",
			local.Format(time.RFC3339), got.Format("2006-01-02"))
	}
}

// ---------------------------------------------------------------------------
// The two deliveries in a day
// ---------------------------------------------------------------------------

func TestOnlyTheTwoRealShiftsAreShifts(t *testing.T) {
	for _, s := range []Shift{Morning, Evening} {
		if !ValidShift(s) {
			t.Errorf("%q is not accepted as a shift", s)
		}
	}
	for _, s := range []Shift{"", "MIDDAY", "morning", "AM", "NIGHT"} {
		if ValidShift(s) {
			t.Errorf("%q was accepted as a shift; a society collects twice a day and a third "+
				"slot is a duplicate entry with a new name on it", s)
		}
	}
}

// ---------------------------------------------------------------------------
// Pricing refusals
// ---------------------------------------------------------------------------

// Milk collected when nothing priced is not priced at zero and not priced at
// yesterday's rate. It is refused, and the refusal names the day so somebody can
// go and put a card up.
func TestNoCardInForceNamesTheDay(t *testing.T) {
	err := &ErrNoCardInForce{TenantID: "t", At: day("2026-03-01")}
	if !errors.Is(err, ErrNoCard) {
		t.Error("a missing card does not unwrap to ErrNoCard, so callers matching on the " +
			"sentinel would treat it as an unknown internal failure and retry it")
	}
	if got := err.Error(); got == "" {
		t.Fatal("no message")
	} else if !contains(got, "2026-03-01") {
		t.Errorf("the refusal is %q and does not say which day had no card, so nobody knows "+
			"which one to put up", got)
	}
}

// A priced collection is current until something supersedes it. Everything
// downstream — a settlement, a statement, a fortnight's total — reads only the
// current versions, so getting this backwards pays a producer twice or not at all.
func TestOnlyAnUnsupersededVersionIsCurrent(t *testing.T) {
	live := &PricedCollection{}
	if !live.IsCurrent() {
		t.Error("a collection nothing has corrected is not current")
	}
	when := day("2026-03-05")
	corrected := &PricedCollection{SupersededAt: &when}
	if corrected.IsCurrent() {
		t.Error("a corrected collection is still current, so a fortnight would count both it " +
			"and its replacement")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
