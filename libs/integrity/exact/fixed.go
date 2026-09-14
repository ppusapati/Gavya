package exact

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/ppusapati/gavya/libs/integrity/money"
)

// Fixed is a decimal held exactly, at a stated scale, for the things that are
// neither money nor milk: a count of stock, a kilogram of feed, a fat
// percentage, a tax rate, the size of a pack.
//
// It exists because every one of those used to be a float64 from the wire to
// the column and back. The boundary was guarded — a value finer than the column
// was refused rather than rounded — but the domain still held a float, the
// response still sent one, and an audit trail written from the domain carried
// 12.5 where the column held 12.500. Money had already stopped doing this; this
// is the same treatment for the numbers beside it.
//
// The value is Units / 10^Scale. Two Fixed values at different scales are not
// compared or added: a quantity in millilitres and one in litres are not the
// same kind of number even when they name the same amount, and refusing is what
// keeps a rescale from being forgotten. At converts between scales, exactly or
// not at all.
//
// On the wire it is a decimal literal — "12.500" — and is read from either a
// JSON string or a bare JSON number. The number is read from its own digits,
// never through a float64: "0.1" arrives as one tenth exactly, at scale one,
// whatever binary approximation a float would have made of it.
type Fixed struct {
	Units int64
	Scale int32
}

var (
	// ErrScaleMismatch is arithmetic between two scales.
	ErrScaleMismatch = errors.New("these values are held at different scales")
	// ErrExponent is a JSON number written with an exponent. It is a valid
	// number and a decimal literal could be made of it, but nobody records a
	// quantity as 1.25e1 and a value that arrives that way is a bug upstream.
	ErrExponent = errors.New("a value must be a plain decimal, not one with an exponent")
)

// ParseFixed reads a decimal literal at a scale, refusing one finer than that.
func ParseFixed(literal string, scale int32) (Fixed, error) {
	m, err := money.Parse(literal, scale, "")
	if err != nil {
		return Fixed{}, err
	}
	return Fixed{Units: m.Value, Scale: m.Scale}, nil
}

// MustFixed is ParseFixed for a literal written in source, where a bad one is
// a bug rather than an input.
func MustFixed(literal string, scale int32) Fixed {
	f, err := ParseFixed(literal, scale)
	if err != nil {
		panic(err)
	}
	return f
}

// parseLiteral reads a literal at whatever scale it was written to.
func parseLiteral(literal string) (Fixed, error) {
	literal = strings.TrimSpace(literal)
	if strings.ContainsAny(literal, "eE") {
		return Fixed{}, ErrExponent
	}
	_, frac, _ := strings.Cut(literal, ".")
	return ParseFixed(literal, int32(len(frac)))
}

func (f Fixed) String() string { return money.Money{Value: f.Units, Scale: f.Scale}.String() }

func (f Fixed) IsZero() bool { return f.Units == 0 }

// Sign is -1, 0 or 1.
func (f Fixed) Sign() int {
	switch {
	case f.Units < 0:
		return -1
	case f.Units > 0:
		return 1
	}
	return 0
}

// At returns the same value at another scale.
//
// Widening is exact. Narrowing is allowed only when the digits dropped are
// zeros, which is the case for a column's padding — 12.500 read at scale one is
// 12.5 — and refused otherwise with ErrTooPrecise: a value finer than the field
// records is a value somebody meant, and dropping it is a decision they did not
// make.
func (f Fixed) At(scale int32) (Fixed, error) {
	m, step, err := money.Rescale(money.Money{Value: f.Units, Scale: f.Scale}, scale, money.RoundTowardZero)
	if err != nil {
		return Fixed{}, err
	}
	if step.Discarded != 0 {
		return Fixed{}, fmt.Errorf("%w: %s has more than %d decimals", ErrTooPrecise, f, scale)
	}
	return Fixed{Units: m.Value, Scale: m.Scale}, nil
}

// Fits reports whether the value has no more than precision digits in total,
// which is what NUMERIC(precision, scale) can hold.
func (f Fixed) Fits(precision int32) error {
	if precision > 18 {
		return nil // 10^19 is past int64, so anything a Fixed can hold fits
	}
	limit := int64(1)
	for i := int32(0); i < precision; i++ {
		limit *= 10
	}
	v := f.Units
	if v < 0 {
		v = -v
	}
	if v >= limit {
		return fmt.Errorf("%w: %s does not fit NUMERIC(%d,%d)", ErrOutOfRange, f, precision, f.Scale)
	}
	return nil
}

// Column checks a value against a NUMERIC(precision, scale) column and returns
// it at the column's scale: what a service does with a value that has just
// arrived, before it holds it.
func (f Fixed) Column(scale, precision int32) (Fixed, error) {
	at, err := f.At(scale)
	if err != nil {
		return Fixed{}, err
	}
	if err := at.Fits(precision); err != nil {
		return Fixed{}, err
	}
	return at, nil
}

// NonNegativeColumn is Column for a field that cannot be below zero — a
// quantity, a percentage, a size.
func (f Fixed) NonNegativeColumn(scale, precision int32) (Fixed, error) {
	if f.Units < 0 {
		return Fixed{}, fmt.Errorf("%w: %s", ErrNegative, f)
	}
	return f.Column(scale, precision)
}

func (f Fixed) sameScale(o Fixed) error {
	if f.Scale != o.Scale {
		return fmt.Errorf("%w: %d and %d", ErrScaleMismatch, f.Scale, o.Scale)
	}
	return nil
}

// Add is exact and refuses to cross scales.
func (f Fixed) Add(o Fixed) (Fixed, error) {
	if err := f.sameScale(o); err != nil {
		return Fixed{}, err
	}
	sum, err := money.Add(money.Money{Value: f.Units, Scale: f.Scale}, money.Money{Value: o.Units, Scale: o.Scale})
	if err != nil {
		return Fixed{}, err
	}
	return Fixed{Units: sum.Value, Scale: sum.Scale}, nil
}

// Sub is exact and refuses to cross scales.
func (f Fixed) Sub(o Fixed) (Fixed, error) {
	return f.Add(Fixed{Units: -o.Units, Scale: o.Scale})
}

// Cmp orders two values at the same scale: -1, 0 or 1.
func (f Fixed) Cmp(o Fixed) (int, error) {
	if err := f.sameScale(o); err != nil {
		return 0, err
	}
	switch {
	case f.Units < o.Units:
		return -1, nil
	case f.Units > o.Units:
		return 1, nil
	}
	return 0, nil
}

// Float64 is the value as a float, for the one place a float is the right
// answer: handing a measurement to the tier that computes statistics from it.
//
// Uncertainty propagation, reconciliation and anomaly scoring are floating-point
// mathematics — a combined standard uncertainty is a square root of a sum of
// squares and has no exact decimal form — and they run in the ML tier, which
// speaks f64. So the conversion happens, and it happens here, named, at the
// boundary where exactness stops mattering and statistics begin. What comes back
// from that tier is a float and is stored as one; what was measured stays exact.
//
// Not for arithmetic on this side. Use Add, Sub and Cmp, or do the sum in SQL.
func (f Fixed) Float64() float64 {
	return float64(f.Units) / math.Pow(10, float64(f.Scale))
}

// MarshalJSON writes the value as a decimal literal in a string: "12.500".
//
// A string rather than a number, for the reason money is: a JSON number is a
// float64 by the time most readers have parsed it, and a value that has been
// through a float is one nobody can prove was not changed on the way.
func (f Fixed) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.String())
}

// UnmarshalJSON reads a decimal literal from a JSON string or a bare number,
// at the scale it was written to. null leaves the value untouched.
func (f *Fixed) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	switch {
	case s == "null":
		return nil
	case strings.HasPrefix(s, `"`):
		var literal string
		if err := json.Unmarshal(b, &literal); err != nil {
			return err
		}
		s = literal
	}
	parsed, err := parseLiteral(s)
	if err != nil {
		return err
	}
	*f = parsed
	return nil
}

// Scan reads the value a NUMERIC column rendered, at the column's scale:
// 12.500 from NUMERIC(_,3) arrives at scale three. pgx hands a NUMERIC to a
// Scanner as its text.
func (f *Fixed) Scan(src any) error {
	var literal string
	switch v := src.(type) {
	case nil:
		*f = Fixed{}
		return nil
	case string:
		literal = v
	case []byte:
		literal = string(v)
	default:
		return fmt.Errorf("exact: cannot scan %T into a Fixed; select the column as it is, not cast to something else", src)
	}
	parsed, err := parseLiteral(literal)
	if err != nil {
		return fmt.Errorf("exact: scan %q: %w", literal, err)
	}
	*f = parsed
	return nil
}

// Value writes the literal for the database, which reads it into the column's
// own type without a float in between.
func (f Fixed) Value() (driver.Value, error) { return f.String(), nil }
