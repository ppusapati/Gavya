package domain

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// MovementType is the closed vocabulary the schema's CHECK constraint enforces.
// Naming the values stops a typo in a string literal from reaching the database
// as a constraint violation reported as an internal failure.
type MovementType = string

const (
	// MovementIn adds to what is on the shelf.
	MovementIn MovementType = "in"
	// MovementOut takes from it.
	MovementOut MovementType = "out"
	// MovementAdjustment states the count after a stocktake, replacing the
	// running total rather than moving it.
	MovementAdjustment MovementType = "adjustment"
	// MovementTransfer takes from this warehouse to send elsewhere. The matching
	// arrival is a separate movement at the receiving warehouse.
	MovementTransfer MovementType = "transfer"
)

func ValidMovementType(t MovementType) bool {
	switch t {
	case MovementIn, MovementOut, MovementAdjustment, MovementTransfer:
		return true
	default:
		return false
	}
}

// QuantityScale is the number of decimal places the stock columns keep.
const QuantityScale = 3

var (
	// ErrQuantityNotFinite is a quantity that is not a number at all. JSON can
	// carry neither, but a caller constructing one in Go can.
	ErrQuantityNotFinite = errors.New("quantity must be a finite number")
	// ErrQuantityNegative is a movement whose direction contradicts its type.
	ErrQuantityNegative = errors.New("quantity cannot be negative; the direction is the movement type")
	// ErrQuantityTooPrecise is a quantity the stock columns cannot hold.
	ErrQuantityTooPrecise = errors.New("quantity is finer than the stock is counted in")
	// ErrQuantityTooLarge is a quantity beyond what the columns can represent.
	ErrQuantityTooLarge = errors.New("quantity is larger than the stock columns can hold")
)

// maxQuantity is the largest value NUMERIC(12,3) holds: twelve significant
// digits, three of them after the point.
const maxQuantity = 1e9

// FormatQuantity turns a requested quantity into the exact decimal literal the
// database will store.
//
// It refuses a value the columns cannot hold rather than letting PostgreSQL
// round it on the way in. Silently turning a request for 12.3456 into 12.346 is
// the kind of quiet correction this platform does not make: a caller who meant
// a finer unit has a unit problem, and should be told so rather than have it
// papered over.
func FormatQuantity(q float64) (string, error) {
	if math.IsNaN(q) || math.IsInf(q, 0) {
		return "", ErrQuantityNotFinite
	}
	if q < 0 {
		return "", ErrQuantityNegative
	}
	if q >= maxQuantity {
		return "", ErrQuantityTooLarge
	}

	// A float64 does not exactly hold most three-decimal values, so the test is
	// whether it sits within half a ulp of the grid rather than exactly on it.
	scaled := q * 1000
	if math.Abs(scaled-math.Round(scaled)) > 1e-6 {
		return "", ErrQuantityTooPrecise
	}
	return strconv.FormatFloat(math.Round(scaled)/1000, 'f', QuantityScale, 64), nil
}

// FormatStock renders a stored quantity for a message. The stock columns keep
// three decimals, so this shows three and no more.
func FormatStock(q float64) string {
	s := strconv.FormatFloat(q, 'f', QuantityScale, 64)
	// Trailing zeroes say nothing about how the stock is counted.
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	return s
}
