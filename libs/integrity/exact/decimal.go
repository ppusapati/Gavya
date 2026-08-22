// Package exact turns a float64 that arrived on the wire into the exact decimal
// a NUMERIC column will store — or refuses it.
//
// Several services carry money and settled quantities as JSON numbers, which
// means they reach Go as float64. That is survivable at the boundary: a value
// with two or three decimals, within range, round-trips through float64 and
// back to the same decimal. What is not survivable is doing arithmetic in
// float64, or handing PostgreSQL a value finer than the column holds and
// letting it round silently.
//
// So this package does two things and nothing else. It converts a float64 to a
// decimal literal that the database will store exactly, and it refuses a value
// that cannot be stored exactly rather than allowing a quiet correction. The
// arithmetic itself belongs in SQL, in the column's own NUMERIC type, or in
// libs/integrity/money — never in float64.
package exact

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

var (
	// ErrNotFinite is a value that is not a number. JSON cannot carry NaN or an
	// infinity, but a caller constructing one in Go can.
	ErrNotFinite = errors.New("value must be a finite number")
	// ErrTooPrecise is a value finer than the column can hold. PostgreSQL would
	// round it on the way in without saying so, and a caller who meant grams
	// where the column counts kilos has a unit problem that should be reported
	// rather than papered over.
	ErrTooPrecise = errors.New("value is finer than this field is recorded to")
	// ErrOutOfRange is a value the column cannot represent at all.
	ErrOutOfRange = errors.New("value is outside the range this field can hold")
	// ErrNegative is a value that must not be below zero.
	ErrNegative = errors.New("value cannot be negative")
)

// Decimal renders v as a decimal literal with exactly scale digits after the
// point, refusing anything that would have to be rounded to get there.
//
// precision is the column's total digit count, as in NUMERIC(precision, scale);
// pass 0 to skip the range check.
func Decimal(v float64, scale, precision int32) (string, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "", ErrNotFinite
	}
	if scale < 0 || scale > 12 {
		return "", fmt.Errorf("%w: scale %d is not supported", ErrOutOfRange, scale)
	}

	factor := math.Pow(10, float64(scale))
	scaled := v * factor

	// A float64 does not exactly hold most decimal fractions, so the question is
	// not whether the value sits exactly on the grid but whether it is nearer to
	// a grid point than float64's own error could account for. Anything further
	// away was a genuinely finer number.
	if math.Abs(scaled) >= 1e15 {
		return "", ErrOutOfRange
	}
	rounded := math.Round(scaled)
	if math.Abs(scaled-rounded) > tolerance(scaled) {
		return "", ErrTooPrecise
	}

	if precision > 0 {
		limit := math.Pow(10, float64(precision))
		if math.Abs(rounded) >= limit {
			return "", ErrOutOfRange
		}
	}

	return strconv.FormatFloat(rounded/factor, 'f', int(scale), 64), nil
}

// tolerance is how far from a grid point a value may sit and still be counted as
// on it. It scales with magnitude because float64's spacing does: 1e12 has far
// coarser representable steps than 1.0, and a fixed epsilon would reject
// perfectly ordinary large values.
func tolerance(scaled float64) float64 {
	t := math.Abs(scaled) * 1e-9
	if t < 1e-6 {
		return 1e-6
	}
	return t
}

// NonNegativeDecimal is Decimal for a field that cannot be below zero — a
// quantity, a price, an amount owed.
func NonNegativeDecimal(v float64, scale, precision int32) (string, error) {
	if v < 0 {
		return "", ErrNegative
	}
	return Decimal(v, scale, precision)
}

// Field names a value in an error, so a caller is told which of five amounts in
// a request was refused rather than that "a value" was.
func Field(name string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", name, err)
}

// Render formats a stored value for a message, at the field's own scale and
// without trailing zeroes that say nothing about how it is recorded.
func Render(v float64, scale int32) string {
	s := strconv.FormatFloat(v, 'f', int(scale), 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	return s
}
