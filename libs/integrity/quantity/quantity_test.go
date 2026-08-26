package quantity

import (
	"errors"
	"strings"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/money"
)

func q(t *testing.T, s string, u Unit) Quantity {
	t.Helper()
	v, err := Parse(s, u)
	if err != nil {
		t.Fatalf("parse %q %s: %v", s, u, err)
	}
	return v
}

// A density of 1.030 kg per litre, read off a lactometer at 4 degrees.
func lactometer(t *testing.T) Density {
	t.Helper()
	r, err := money.ParseRate("1.0300", 4)
	if err != nil {
		t.Fatal(err)
	}
	return Density{KgPerLitre: r, AtCelsius: 40, Source: Lactometer}
}

func TestQuantitiesInOneUnitAddAndSubtractExactly(t *testing.T) {
	a, b := q(t, "5000.000", Litres), q(t, "4960.250", Litres)
	diff, err := Sub(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if diff.String() != "39.750" {
		t.Errorf("difference %s, want 39.750", diff)
	}
	sum, err := Add(diff, b)
	if err != nil {
		t.Fatal(err)
	}
	if sum.String() != "5000.000" {
		t.Errorf("the difference put back gives %s, want 5000.000", sum)
	}
}

// The failure this package exists to prevent.
//
// A tanker leaves a cooler with 5000 litres by dip and the plant weighs 5090
// kilograms. Subtracting the two gives a shortfall of 90, which reads as ninety
// litres of milk gone missing and is the density of milk.
func TestLitresAndKilogramsAreNotSubtractedFromEachOther(t *testing.T) {
	dispatched := q(t, "5000.000", Litres)
	received := q(t, "5090.000", Kilograms)

	if _, err := Sub(dispatched, received); err == nil {
		t.Fatal("litres were subtracted from kilograms, which would report the density of milk as a loss")
	} else if !errors.Is(err, ErrUnitMismatch) {
		t.Errorf("the refusal is not about the units: %v", err)
	}

	// And without a density there is no comparing them either.
	_, _, err := Comparable(dispatched, received, nil, money.RoundHalfUp)
	if err == nil {
		t.Fatal("a litre dispatch and a kilogram receipt were compared with no density")
	}
	if !errors.Is(err, ErrNoDensity) {
		t.Errorf("the refusal is not about the missing density: %v", err)
	}
	// The message has to carry both figures, because the person reading it is
	// looking at a consignment and needs to know which two numbers are the
	// problem.
	if !strings.Contains(err.Error(), "5000.000") || !strings.Contains(err.Error(), "5090.000") {
		t.Errorf("the refusal does not name the two quantities: %v", err)
	}
}

// With a density, the same consignment reconciles to something sensible.
//
//	5000 litres at 1.0300 kg/l is 5150 kg dispatched
//	5090 kg received
//	so 60 kg is missing, which is 58.252 litres — not 90 of anything.
func TestWithADensityTheConsignmentReconciles(t *testing.T) {
	d := lactometer(t)

	asKg, err := Convert(q(t, "5000.000", Litres), Kilograms, d, money.RoundHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	if asKg.String() != "5150.000" {
		t.Errorf("5000 litres at 1.0300 is %s kg, want 5150.000", asKg)
	}

	loss, err := Sub(asKg, q(t, "5090.000", Kilograms))
	if err != nil {
		t.Fatal(err)
	}
	if loss.String() != "60.000" {
		t.Errorf("the loss is %s kg, want 60.000", loss)
	}

	// And back the other way, so the figure can be discussed in the unit the
	// cooler measured in.
	inLitres, err := Convert(loss, Litres, d, money.RoundHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	if inLitres.String() != "58.252" {
		t.Errorf("60 kg back at 1.0300 is %s litres, want 58.252", inLitres)
	}
}

// Comparable puts the receipt into the unit the movement started in, because
// that is the unit the person who dispatched it will be asked about.
func TestComparableAnswersInTheUnitTheMovementStartedIn(t *testing.T) {
	d := lactometer(t)
	a, b, err := Comparable(q(t, "5000.000", Litres), q(t, "5090.000", Kilograms), &d, money.RoundHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	if a.Unit != Litres || b.Unit != Litres {
		t.Fatalf("compared in %s and %s, want both in litres", a.Unit, b.Unit)
	}
	// 5090 kg at 1.0300 is 4941.748 litres.
	if b.String() != "4941.748" {
		t.Errorf("the receipt reads %s litres, want 4941.748", b)
	}
	loss, err := Sub(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if loss.String() != "58.252" {
		t.Errorf("the loss is %s litres, want 58.252", loss)
	}
}

// Converting rounds, so the mode is stated rather than assumed. The two modes
// give different answers on the same consignment, which is why.
func TestTheRoundingModeIsRequiredAndItChangesTheAnswer(t *testing.T) {
	d := lactometer(t)
	// Chosen so the product lands exactly on a half. 50 millilitres at 1.0300
	// is 51.5 grams, and the two modes take that to 52 and to 51.
	//
	// An earlier version used 0.015, where both modes give the same answer, so
	// the test compared a value against itself. It failed rather than passing
	// quietly, which is why the assertion below is a Fatalf: a rounding test
	// that cannot tell two modes apart proves nothing and must say so.
	litres := q(t, "0.050", Litres)

	if _, err := Convert(litres, Kilograms, d, ""); err == nil {
		t.Error("a conversion rounded without anybody saying which way")
	}

	up, err := Convert(litres, Kilograms, d, money.RoundHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	down, err := Convert(litres, Kilograms, d, money.RoundDown)
	if err != nil {
		t.Fatal(err)
	}
	if up.String() == down.String() {
		t.Fatalf("both modes gave %s, so this test does not distinguish them and proves nothing", up)
	}
	if up.String() != "0.052" || down.String() != "0.051" {
		t.Errorf("half up gave %s and down gave %s, want 0.052 and 0.051", up, down)
	}
}

// A density with no source is refused. A lactometer reading, a figure computed
// from an analysis and a number somebody remembered are three different
// qualities of evidence, and a transit-loss dispute turns on which was used.
func TestADensityMustSayWhereItCameFrom(t *testing.T) {
	r, err := money.ParseRate("1.0300", 4)
	if err != nil {
		t.Fatal(err)
	}
	d := Density{KgPerLitre: r, AtCelsius: 40}
	if err := d.Validate(); err == nil {
		t.Error("a density with no stated source was accepted")
	}
	if _, err := Convert(q(t, "100.000", Litres), Kilograms, d, money.RoundHalfUp); err == nil {
		t.Error("a conversion used a density with no stated source")
	}

	// A declared figure is permitted and labelled, because a society that works
	// that way should be able to and anybody reading the number afterwards
	// should see what it is.
	d.Source = Declared
	if err := d.Validate(); err != nil {
		t.Errorf("a declared density was refused: %v", err)
	}
	if !strings.Contains(d.String(), "DECLARED") {
		t.Errorf("a declared density does not say so when written out: %s", d.String())
	}
}

// A density carries the temperature it holds at, because it moves with
// temperature and a figure without one is a number somebody can argue with
// after the money has moved.
func TestADensityCarriesItsTemperature(t *testing.T) {
	d := lactometer(t)
	if !strings.Contains(d.String(), "4.0") {
		t.Errorf("the density does not state its temperature: %s", d.String())
	}
	cold := d
	cold.AtCelsius = 45 // 4.5 degrees
	if d.String() == cold.String() {
		t.Error("two densities at different temperatures read identically")
	}
	// Below freezing is a real reading on a cold morning in the north.
	below := d
	below.AtCelsius = -15
	if !strings.Contains(below.String(), "-1.5") {
		t.Errorf("a sub-zero temperature is written as %s", below.String())
	}
}

func TestADensityOfZeroOrLessIsRefused(t *testing.T) {
	for _, n := range []int64{0, -10300} {
		d := Density{KgPerLitre: money.Rate{Numerator: n, Scale: 4}, Source: Lactometer}
		if err := d.Validate(); err == nil {
			t.Errorf("a density of %d was accepted", n)
		}
	}
}

// A quantity with no unit is refused everywhere, rather than defaulting to
// litres — which is the unit most of this platform's milk is in and therefore
// the assumption that would be wrong least often and hardest to find.
func TestAQuantityWithNoUnitIsRefused(t *testing.T) {
	if _, err := Parse("100.000", ""); !errors.Is(err, ErrNoUnit) {
		t.Errorf("parsing with no unit gave %v", err)
	}
	if _, err := New(100000, "GALLONS"); !errors.Is(err, ErrNoUnit) {
		t.Errorf("an unknown unit gave %v", err)
	}
	bad := Quantity{Amount: money.Zero(Scale, tag)}
	if _, err := Add(bad, bad); !errors.Is(err, ErrNoUnit) {
		t.Errorf("adding two unitless quantities gave %v", err)
	}
}

// A literal finer than the scale is refused rather than truncated. Somebody who
// wrote a fourth decimal meant something by it.
func TestAQuantityFinerThanTheScaleIsRefusedNotTruncated(t *testing.T) {
	if _, err := Parse("12.3456", Litres); err == nil {
		t.Error("12.3456 litres was accepted at a scale of three, so a digit went somewhere")
	}
}

// More milk arriving than left is a real thing — a mis-dip, a tanker that was
// not empty when it loaded — and the platform has to be able to report it.
func TestMoreArrivingThanLeftIsRepresentable(t *testing.T) {
	over, err := Sub(q(t, "5000.000", Litres), q(t, "5010.000", Litres))
	if err != nil {
		t.Fatal(err)
	}
	if over.String() != "-10.000" {
		t.Errorf("the surplus reads %s, want -10.000", over)
	}
}

// Summing refuses the moment two units disagree, and says which item.
func TestSummingAcrossUnitsIsRefusedAndSaysWhere(t *testing.T) {
	_, err := Sum([]Quantity{
		q(t, "100.000", Litres),
		q(t, "200.000", Litres),
		q(t, "300.000", Kilograms),
	})
	if err == nil {
		t.Fatal("litres and kilograms were summed together")
	}
	if !strings.Contains(err.Error(), "item 2") {
		t.Errorf("the refusal does not say which item is the odd one: %v", err)
	}
}

// Converting to the unit a quantity is already in is the identity, and does not
// need a density — so a movement measured in one unit at both ends never has to
// supply one.
func TestConvertingToTheSameUnitNeedsNoDensity(t *testing.T) {
	in := q(t, "5000.000", Litres)
	out, err := Convert(in, Litres, Density{}, "")
	if err != nil {
		t.Fatalf("converting litres to litres needed a density: %v", err)
	}
	if out.String() != in.String() || out.Unit != Litres {
		t.Errorf("the identity conversion changed %s into %s", in.Describe(), out.Describe())
	}
}

// A round trip through kilograms and back does not have to be exact — the
// conversion rounds — but it has to be close, and it has to be close in a way
// that is checked rather than hoped for.
func TestARoundTripThroughKilogramsStaysWithinAMillilitre(t *testing.T) {
	d := lactometer(t)
	for _, litres := range []string{"1.000", "12.345", "5000.000", "9999.999", "0.001"} {
		start := q(t, litres, Litres)
		kg, err := Convert(start, Kilograms, d, money.RoundHalfUp)
		if err != nil {
			t.Fatalf("%s: %v", litres, err)
		}
		back, err := Convert(kg, Litres, d, money.RoundHalfUp)
		if err != nil {
			t.Fatalf("%s: %v", litres, err)
		}
		drift := back.Value() - start.Value()
		if drift < -1 || drift > 1 {
			t.Errorf("%s litres came back as %s, a drift of %d millilitres",
				litres, back, drift)
		}
	}
}

// Describe is what goes in a message a person reads, so it carries the unit.
// "5000.000" alone in a transit-loss report is the ambiguity this package
// exists to remove.
func TestAQuantityWrittenForAPersonCarriesItsUnit(t *testing.T) {
	if got := q(t, "5000.000", Litres).Describe(); got != "5000.000 litres" {
		t.Errorf("litres read as %q", got)
	}
	if got := q(t, "5150.000", Kilograms).Describe(); got != "5150.000 kg" {
		t.Errorf("kilograms read as %q", got)
	}
}
