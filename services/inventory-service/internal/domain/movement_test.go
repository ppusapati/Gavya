package domain

import (
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/exact"
)

func TestFormatQuantityKeepsWhatTheColumnsCanHold(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0.000"},
		{1, "1.000"},
		{12.5, "12.500"},
		{12.345, "12.345"},
		{0.001, "0.001"},
		{999999999.999, "999999999.999"},
	}
	for _, c := range cases {
		got, err := FormatQuantity(c.in)
		if err != nil {
			t.Errorf("FormatQuantity(%v): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("FormatQuantity(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// PostgreSQL would round a finer quantity into NUMERIC(12,3) without saying so.
// A caller who meant grams when the stock is counted in kilos has a unit
// problem, and should be told rather than have it quietly corrected.
func TestFormatQuantityRefusesWhatWouldBeSilentlyRounded(t *testing.T) {
	for _, q := range []float64{12.3456, 0.0001, 1.00049} {
		if got, err := FormatQuantity(q); !errors.Is(err, exact.ErrTooPrecise) {
			t.Errorf("FormatQuantity(%v) = %q, %v; want exact.ErrTooPrecise", q, got, err)
		}
	}
}

// The direction of a movement is its type. A negative quantity would make an
// "in" behave as an "out" and defeat every reading of the movement history.
func TestFormatQuantityRefusesANegativeQuantity(t *testing.T) {
	if _, err := FormatQuantity(-1); !errors.Is(err, exact.ErrNegative) {
		t.Errorf("err = %v, want exact.ErrNegative", err)
	}
}

func TestFormatQuantityRefusesWhatTheColumnsCannotRepresent(t *testing.T) {
	for _, q := range []float64{1e9, 1e12} {
		if _, err := FormatQuantity(q); !errors.Is(err, exact.ErrOutOfRange) {
			t.Errorf("FormatQuantity(%v) err = %v, want exact.ErrOutOfRange", q, err)
		}
	}
	for _, q := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := FormatQuantity(q); err == nil {
			t.Errorf("FormatQuantity(%v) was accepted", q)
		}
	}
}

// Every value FormatQuantity accepts must come back out at the column's scale,
// so what is sent to the database is never a rounded version of what was asked.
func TestAcceptedQuantitiesAreExactAtTheColumnScale(t *testing.T) {
	for i := 0; i < 5000; i++ {
		q := float64(i) / 1000
		got, err := FormatQuantity(q)
		if err != nil {
			t.Fatalf("FormatQuantity(%v): %v", q, err)
		}
		back, err := strconv.ParseFloat(got, 64)
		if err != nil {
			t.Fatalf("parse %q: %v", got, err)
		}
		if math.Abs(back-q) > 1e-9 {
			t.Fatalf("FormatQuantity(%v) = %q, which reads back as %v", q, got, back)
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

func TestFormatStockDropsTrailingZeroes(t *testing.T) {
	cases := map[float64]string{0: "0", 1: "1", 12.5: "12.5", 12.345: "12.345", 0.001: "0.001"}
	for in, want := range cases {
		if got := FormatStock(in); got != want {
			t.Errorf("FormatStock(%v) = %q, want %q", in, got, want)
		}
	}
}
