package exact

import (
	"errors"
	"math"
	"strconv"
	"testing"
)

func TestDecimalKeepsWhatTheColumnCanHold(t *testing.T) {
	cases := []struct {
		v     float64
		scale int32
		want  string
	}{
		{0, 2, "0.00"},
		{54, 2, "54.00"},
		{54.5, 2, "54.50"},
		{54.55, 2, "54.55"},
		{-12.25, 2, "-12.25"},
		{12.345, 3, "12.345"},
		{0.001, 3, "0.001"},
		{1, 0, "1"},
		{9999999999.99, 2, "9999999999.99"},
	}
	for _, c := range cases {
		got, err := Decimal(c.v, c.scale, 0)
		if err != nil {
			t.Errorf("Decimal(%v, %d): %v", c.v, c.scale, err)
			continue
		}
		if got != c.want {
			t.Errorf("Decimal(%v, %d) = %q, want %q", c.v, c.scale, got, c.want)
		}
	}
}

// The whole point: PostgreSQL would round these into the column without saying
// so, and a price of 54.555 silently becoming 54.56 is money changing hands
// because of a type conversion nobody was told about.
func TestDecimalRefusesWhatWouldBeSilentlyRounded(t *testing.T) {
	cases := []struct {
		v     float64
		scale int32
	}{
		{54.555, 2},
		{0.001, 2},
		{12.3456, 3},
		{1.005, 2},
		{0.5, 0},
	}
	for _, c := range cases {
		if got, err := Decimal(c.v, c.scale, 0); !errors.Is(err, ErrTooPrecise) {
			t.Errorf("Decimal(%v, %d) = %q, %v; want ErrTooPrecise", c.v, c.scale, got, err)
		}
	}
}

func TestDecimalRefusesWhatIsNotANumber(t *testing.T) {
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := Decimal(v, 2, 0); !errors.Is(err, ErrNotFinite) {
			t.Errorf("Decimal(%v) err = %v, want ErrNotFinite", v, err)
		}
	}
}

// NUMERIC(12,2) holds ten digits before the point. A value past that would be
// rejected by the database anyway; rejecting it here says which field it was.
func TestDecimalRefusesWhatTheColumnCannotRepresent(t *testing.T) {
	if _, err := Decimal(1e10, 2, 12); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("err = %v, want ErrOutOfRange", err)
	}
	if _, err := Decimal(1e18, 2, 0); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("err = %v, want ErrOutOfRange for a value beyond float64's integer grid", err)
	}
	// Just inside the limit is accepted.
	if _, err := Decimal(9999999999.99, 2, 12); err != nil {
		t.Errorf("a value the column can hold was refused: %v", err)
	}
}

func TestNonNegativeDecimalRefusesADebt(t *testing.T) {
	if _, err := NonNegativeDecimal(-0.01, 2, 0); !errors.Is(err, ErrNegative) {
		t.Errorf("err = %v, want ErrNegative", err)
	}
	if _, err := NonNegativeDecimal(0, 2, 0); err != nil {
		t.Errorf("zero was refused: %v", err)
	}
}

// Everything accepted must read back as the number that was asked for. If this
// ever failed, the package would be introducing exactly the silent change it
// exists to prevent.
func TestEveryAcceptedValueReadsBackUnchanged(t *testing.T) {
	for scale := int32(0); scale <= 3; scale++ {
		factor := math.Pow(10, float64(scale))
		for i := 0; i < 3000; i++ {
			v := float64(i) / factor
			got, err := Decimal(v, scale, 0)
			if err != nil {
				t.Fatalf("Decimal(%v, %d): %v", v, scale, err)
			}
			back, err := strconv.ParseFloat(got, 64)
			if err != nil {
				t.Fatalf("parse %q: %v", got, err)
			}
			if math.Abs(back-v) > 1e-9 {
				t.Fatalf("Decimal(%v, %d) = %q, which reads back as %v", v, scale, got, back)
			}
		}
	}
}

// Large values are representable in NUMERIC but sit on a coarse float64 grid.
// A fixed epsilon would reject them; the tolerance has to scale with magnitude.
func TestLargeValuesOnTheGridAreAccepted(t *testing.T) {
	for _, v := range []float64{1e9 + 0.25, 123456789.5, 9999999999.99} {
		if _, err := Decimal(v, 2, 0); err != nil {
			t.Errorf("Decimal(%v, 2) was refused: %v", v, err)
		}
	}
}

func TestFieldNamesTheValueThatWasRefused(t *testing.T) {
	err := Field("unit_price", ErrTooPrecise)
	if !errors.Is(err, ErrTooPrecise) {
		t.Fatalf("the cause was lost: %v", err)
	}
	if got := err.Error(); got != "unit_price: value is finer than this field is recorded to" {
		t.Errorf("message = %q", got)
	}
	if Field("x", nil) != nil {
		t.Error("Field turned a success into an error")
	}
}

func TestRenderDropsTrailingZeroes(t *testing.T) {
	cases := []struct {
		v     float64
		scale int32
		want  string
	}{
		{0, 2, "0"},
		{54, 2, "54"},
		{54.5, 2, "54.5"},
		{54.55, 2, "54.55"},
		{12.345, 3, "12.345"},
	}
	for _, c := range cases {
		if got := Render(c.v, c.scale); got != c.want {
			t.Errorf("Render(%v, %d) = %q, want %q", c.v, c.scale, got, c.want)
		}
	}
}
