package domain

import (
	"errors"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/exact"
)

func TestAtColumnHoldsWhatTheColumnsCanHoldAtTheirScale(t *testing.T) {
	cases := []struct {
		in   exact.Fixed
		want string
	}{
		{exact.Fixed{Units: 0, Scale: 0}, "0.000"},
		{exact.Fixed{Units: 1, Scale: 0}, "1.000"},
		{exact.Fixed{Units: 125, Scale: 1}, "12.500"},
		{exact.Fixed{Units: 12345, Scale: 3}, "12.345"},
		{exact.Fixed{Units: 1, Scale: 3}, "0.001"},
		{exact.Fixed{Units: 999999999999, Scale: 3}, "999999999.999"},
	}
	for _, c := range cases {
		got, err := AtColumn(c.in)
		if err != nil {
			t.Errorf("AtColumn(%s): %v", c.in, err)
			continue
		}
		if got.String() != c.want || got.Scale != QuantityScale {
			t.Errorf("AtColumn(%s) = %q at scale %d, want %q at scale %d", c.in, got, got.Scale, c.want, QuantityScale)
		}
	}
}

// PostgreSQL would round a finer quantity into NUMERIC(12,3) without saying so.
// A caller who meant grams when the stock is counted in kilos has a unit
// problem, and should be told rather than have it quietly corrected.
func TestAtColumnRefusesWhatWouldBeSilentlyRounded(t *testing.T) {
	for _, q := range []exact.Fixed{{Units: 123456, Scale: 4}, {Units: 1, Scale: 4}, {Units: 100049, Scale: 5}} {
		if got, err := AtColumn(q); !errors.Is(err, exact.ErrTooPrecise) {
			t.Errorf("AtColumn(%s) = %s, %v; want exact.ErrTooPrecise", q, got, err)
		}
	}
}

func TestAtColumnRefusesANegativeQuantity(t *testing.T) {
	if _, err := AtColumn(exact.Fixed{Units: -1, Scale: 0}); !errors.Is(err, exact.ErrNegative) {
		t.Errorf("err = %v, want exact.ErrNegative", err)
	}
}

func TestAtColumnRefusesWhatTheColumnsCannotRepresent(t *testing.T) {
	for _, q := range []exact.Fixed{{Units: 1000000000, Scale: 0}, {Units: 1000000000000, Scale: 0}} {
		if _, err := AtColumn(q); !errors.Is(err, exact.ErrOutOfRange) {
			t.Errorf("AtColumn(%s) err = %v, want exact.ErrOutOfRange", q, err)
		}
	}
}

func TestValidMovementTypeIsClosed(t *testing.T) {
	for _, ok := range []MovementType{MovementIn, MovementOut, MovementAdjustment, MovementTransfer} {
		if !ValidMovementType(ok) {
			t.Errorf("%q was rejected", ok)
		}
	}
	// The schema's CHECK constraint permits exactly these four. Anything else
	// must be refused here, where the caller gets a usable message, rather than
	// reaching the database as a constraint violation.
	for _, bad := range []MovementType{"", "IN", "inbound", "sale", "in "} {
		if ValidMovementType(bad) {
			t.Errorf("%q was accepted", bad)
		}
	}
}
