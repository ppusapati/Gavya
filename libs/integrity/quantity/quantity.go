// Package quantity is exact arithmetic on measured milk, in the units it is
// actually measured in.
//
// A bulk cooler measures litres with a dipstick. A plant weighs kilograms on a
// weighbridge. The two are not the same number and never will be: milk is
// around 1.03 kilograms to the litre, and the figure moves with fat content and
// with temperature. Subtracting one from the other gives a transit loss of
// about three per cent on every consignment that has crossed a unit boundary —
// which looks exactly like theft, and is arithmetic.
//
// So this package refuses to convert without a density somebody supplied. There
// is no default. 1.03 is a good enough number for a conversation and not for a
// figure a plant manager takes to a transporter.
//
// It is a separate package rather than a method on the balance service because
// two services need it — the one recording movements and the one reconciling
// them — and two implementations of a conversion would eventually disagree by a
// litre somewhere, which is the whole argument this platform exists to win.
package quantity

import (
	"errors"
	"fmt"

	"github.com/ppusapati/gavya/libs/integrity/money"
)

// Unit is what a measurement is in.
//
// A closed set. "L", "ltr", "Litre" and "litres" all mean the same thing to a
// person and would be four units to a database, and a platform that accepted
// all four would compare milk against itself and find a discrepancy.
type Unit string

const (
	Litres    Unit = "LITRES"
	Kilograms Unit = "KILOGRAMS"
)

func ValidUnit(u Unit) bool { return u == Litres || u == Kilograms }

// Scale is the three decimals milk is measured to: litres to the millilitre,
// kilograms to the gram.
//
// The same scale balance-service uses, deliberately. A quantity that changed
// resolution on its way between the two would reconcile against a number that
// is not the one that was measured.
const Scale int32 = 3

// tag is what the underlying money type carries in place of a currency. Milk is
// not money; the money package is used here for its exact integer arithmetic,
// and this makes the two impossible to add together by accident.
const tag = "XXX"

// Quantity is an amount of milk, exact, in a stated unit.
type Quantity struct {
	// Amount holds the value. Its currency is always tag.
	Amount money.Money
	Unit   Unit
}

var (
	ErrNoUnit       = errors.New("a quantity must say whether it is litres or kilograms; the two differ by about three per cent")
	ErrUnitMismatch = errors.New("these quantities are in different units")
	ErrNoDensity    = errors.New("converting between litres and kilograms needs a density, and this platform will not assume one")
	ErrBadDensity   = errors.New("a density must be positive")
	ErrNegative     = errors.New("a negative quantity of milk is not a thing that can be measured")
)

// Parse reads a decimal literal as a quantity.
//
// Never rounds. A literal finer than the scale is refused rather than
// truncated: somebody who wrote 12.3456 litres meant something by the fourth
// decimal, and quietly dropping it is a decision they did not make.
func Parse(s string, u Unit) (Quantity, error) {
	if !ValidUnit(u) {
		return Quantity{}, ErrNoUnit
	}
	m, err := money.Parse(s, Scale, tag)
	if err != nil {
		return Quantity{}, err
	}
	return Quantity{Amount: m, Unit: u}, nil
}

// New builds a quantity from minor units — millilitres, or grams.
func New(value int64, u Unit) (Quantity, error) {
	if !ValidUnit(u) {
		return Quantity{}, ErrNoUnit
	}
	m, err := money.New(value, Scale, tag)
	if err != nil {
		return Quantity{}, err
	}
	return Quantity{Amount: m, Unit: u}, nil
}

func Zero(u Unit) Quantity { return Quantity{Amount: money.Zero(Scale, tag), Unit: u} }

func (q Quantity) Value() int64   { return q.Amount.Value }
func (q Quantity) IsZero() bool   { return q.Amount.Value == 0 }
func (q Quantity) String() string { return q.Amount.String() }

// Describe is the quantity with its unit, for a message a person reads.
func (q Quantity) Describe() string {
	switch q.Unit {
	case Litres:
		return q.Amount.String() + " litres"
	case Kilograms:
		return q.Amount.String() + " kg"
	}
	return q.Amount.String() + " " + string(q.Unit)
}

func (q Quantity) sameUnit(o Quantity) error {
	if q.Unit != o.Unit {
		return fmt.Errorf("%w: %s and %s", ErrUnitMismatch, q.Describe(), o.Describe())
	}
	if !ValidUnit(q.Unit) {
		return ErrNoUnit
	}
	return nil
}

// Add is exact and refuses to cross units.
func Add(a, b Quantity) (Quantity, error) {
	if err := a.sameUnit(b); err != nil {
		return Quantity{}, err
	}
	sum, err := money.Add(a.Amount, b.Amount)
	if err != nil {
		return Quantity{}, err
	}
	return Quantity{Amount: sum, Unit: a.Unit}, nil
}

// Sub is exact and refuses to cross units.
//
// The result may be negative, and that is deliberate: more milk arriving than
// was dispatched is a real thing that happens — a mis-dip, a tanker that was
// not empty when it loaded — and refusing to represent it would mean the
// platform could not report it.
func Sub(a, b Quantity) (Quantity, error) {
	if err := a.sameUnit(b); err != nil {
		return Quantity{}, err
	}
	diff, err := money.Sub(a.Amount, b.Amount)
	if err != nil {
		return Quantity{}, err
	}
	return Quantity{Amount: diff, Unit: a.Unit}, nil
}

// Sum adds a list, refusing the moment two units disagree.
func Sum(items []Quantity) (Quantity, error) {
	if len(items) == 0 {
		return Quantity{}, ErrNoUnit
	}
	total := Zero(items[0].Unit)
	for i, q := range items {
		var err error
		if total, err = Add(total, q); err != nil {
			return Quantity{}, fmt.Errorf("item %d: %w", i, err)
		}
	}
	return total, nil
}

// Cmp orders two quantities in the same unit.
func Cmp(a, b Quantity) (int, error) {
	if err := a.sameUnit(b); err != nil {
		return 0, err
	}
	return money.Cmp(a.Amount, b.Amount)
}

// Density is how many kilograms a litre of this milk weighs.
//
// Recorded with the temperature it was determined at, because it moves with
// temperature: milk at 4 degrees is denser than the same milk at 30. A density
// with no temperature beside it is a number somebody can argue with, and the
// argument happens after the money has moved.
//
// Also recorded is where it came from. A density read off a lactometer at the
// dock, one computed from a fat and SNF analysis, and one somebody remembered
// are three different qualities of evidence, and a transit loss dispute turns
// on which of them was used.
type Density struct {
	// KgPerLitre is the conversion factor, exact.
	KgPerLitre money.Rate
	// AtCelsius is the temperature it holds at, in tenths of a degree.
	AtCelsius int32
	// Source says how it was arrived at.
	Source DensitySource
}

// DensitySource is how a density was arrived at.
type DensitySource string

const (
	// Lactometer is a reading taken at the dock.
	Lactometer DensitySource = "LACTOMETER"
	// Analysed is computed from a fat and SNF analysis.
	Analysed DensitySource = "ANALYSED"
	// Declared is a figure a person supplied without a measurement behind it —
	// a standing assumption, a contract term. Permitted and labelled, because a
	// society that works this way should be able to, and anybody reading the
	// number afterwards should be able to see that is what it is.
	Declared DensitySource = "DECLARED"
)

func ValidDensitySource(s DensitySource) bool {
	return s == Lactometer || s == Analysed || s == Declared
}

func (d Density) Validate() error {
	if d.KgPerLitre.Numerator <= 0 {
		return ErrBadDensity
	}
	if !ValidDensitySource(d.Source) {
		return errors.New("a density must say how it was arrived at: LACTOMETER, ANALYSED or DECLARED")
	}
	return nil
}

func (d Density) String() string {
	return fmt.Sprintf("%s kg/l at %s°C (%s)", d.KgPerLitre, celsius(d.AtCelsius), d.Source)
}

func celsius(tenths int32) string {
	whole, frac := tenths/10, tenths%10
	if frac < 0 {
		frac = -frac
	}
	return fmt.Sprintf("%d.%d", whole, frac)
}

// Convert changes a quantity's unit using a supplied density.
//
// The rounding mode is required rather than defaulted. Converting is the one
// operation here that cannot be exact — a whole number of millilitres times a
// density is generally not a whole number of grams — so somebody has to say
// which way the fraction goes, and the answer differs between a society that
// rounds in the producer's favour and one that does not.
func Convert(q Quantity, to Unit, d Density, mode money.RoundingMode) (Quantity, error) {
	if !ValidUnit(q.Unit) || !ValidUnit(to) {
		return Quantity{}, ErrNoUnit
	}
	if q.Unit == to {
		return q, nil
	}
	if err := d.Validate(); err != nil {
		return Quantity{}, err
	}
	if mode == "" {
		return Quantity{}, errors.New("converting between units rounds, so the rounding mode has " +
			"to be stated rather than assumed")
	}

	switch {
	case q.Unit == Litres && to == Kilograms:
		out, _, err := money.MulRate(q.Amount, d.KgPerLitre, mode)
		if err != nil {
			return Quantity{}, err
		}
		return Quantity{Amount: out, Unit: Kilograms}, nil

	case q.Unit == Kilograms && to == Litres:
		// Dividing by the density. Done by scaling up first so the division
		// keeps its precision, rather than dividing and then rescaling — which
		// loses the digits that make a consignment reconcile.
		lifted, _, err := money.MulRate(q.Amount,
			money.Rate{Numerator: pow10(d.KgPerLitre.Scale), Scale: 0}, mode)
		if err != nil {
			return Quantity{}, err
		}
		out, _, err := money.Div(lifted, d.KgPerLitre.Numerator, mode)
		if err != nil {
			return Quantity{}, err
		}
		return Quantity{Amount: out, Unit: Litres}, nil
	}
	return Quantity{}, fmt.Errorf("%w: %s to %s", ErrUnitMismatch, q.Unit, to)
}

// Comparable puts two quantities into one unit so they can be subtracted.
//
// Returns ErrNoDensity when they differ and no density was supplied, which is
// the case this package exists for: a dispatch in litres and a receipt in
// kilograms cannot be compared, and the honest answer is to say so rather than
// to produce a difference of three per cent and call it loss.
func Comparable(a, b Quantity, d *Density, mode money.RoundingMode) (Quantity, Quantity, error) {
	if a.Unit == b.Unit {
		return a, b, nil
	}
	if d == nil {
		return Quantity{}, Quantity{}, fmt.Errorf("%w: %s was dispatched and %s received",
			ErrNoDensity, a.Describe(), b.Describe())
	}
	// Converted into the first quantity's unit, so the answer is expressed in
	// the unit the movement started in — which is the one the person who
	// dispatched it will be asked about.
	converted, err := Convert(b, a.Unit, *d, mode)
	if err != nil {
		return Quantity{}, Quantity{}, err
	}
	return a, converted, nil
}

func pow10(n int32) int64 {
	out := int64(1)
	for i := int32(0); i < n; i++ {
		out *= 10
	}
	return out
}
