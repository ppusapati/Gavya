// Package exact holds the decimals that are neither money nor milk — a count of
// stock, a kilogram of feed, a fat percentage, a tax rate — without a float64
// anywhere between the wire and the column.
//
// It used to do something narrower. Those values crossed the wire as JSON
// numbers, which reach Go as float64, and this package turned the float into
// the exact literal a NUMERIC column would store, refusing one finer than the
// column rather than letting PostgreSQL round it in silence. That guarded the
// boundary and left everything inside it a float: the domain held 12.5 where the
// column held 12.500, the response sent a number back out, and an audit trail
// written from the domain carried a float somebody could dispute on the third
// decimal.
//
// Fixed is the replacement: a value read from the digits that were written,
// held at a stated scale, compared and added only at that scale, and rendered
// as the literal the column holds. The float path is gone, and with it the
// question of how far from a grid point a float may sit and still count as on
// it. The arithmetic that produces totals still belongs in SQL, in the column's
// own type, or in libs/integrity/money — never in float64.
package exact

import (
	"errors"
	"fmt"
)

var (
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

// Field names a value in an error, so a caller is told which of five amounts in
// a request was refused rather than that "a value" was.
func Field(name string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", name, err)
}
