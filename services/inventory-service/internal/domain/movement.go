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

// FormatQuantity turns a requested quantity into the exact decimal literal the
// database will store, refusing anything the columns cannot hold rather than
// letting PostgreSQL round it on the way in.
func FormatQuantity(q float64) (string, error) {
	return exact.NonNegativeDecimal(q, QuantityScale, QuantityPrecision)
}

// FormatStock renders a stored quantity for a message.
func FormatStock(q float64) string { return exact.Render(q, QuantityScale) }
