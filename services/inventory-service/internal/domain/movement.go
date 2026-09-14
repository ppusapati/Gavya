package domain

import "github.com/ppusapati/gavya/libs/integrity/exact"

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

// QuantityScale and QuantityPrecision describe the stock columns:
// NUMERIC(12,3).
const (
	QuantityScale     int32 = 3
	QuantityPrecision int32 = 12
)

// AtColumn checks a requested quantity against the stock columns and returns
// it at their scale, refusing anything they cannot hold rather than letting
// PostgreSQL round it on the way in.
//
// The direction of a movement is its type. A negative quantity would make an
// "in" behave as an "out" and defeat every reading of the movement history, so
// it is refused here too.
func AtColumn(q exact.Fixed) (exact.Fixed, error) {
	return q.NonNegativeColumn(QuantityScale, QuantityPrecision)
}
