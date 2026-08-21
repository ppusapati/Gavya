package money

import (
	"math"
	"testing"
)

func mustParse(t *testing.T, s string, scale int32) Money {
	t.Helper()
	m, err := Parse(s, scale, "INR")
	if err != nil {
		t.Fatalf("Parse(%q, %d): %v", s, scale, err)
	}
	return m
}

func TestParseString(t *testing.T) {
	cases := []struct {
		in    string
		scale int32
		want  string
	}{
		{"1234.56", 2, "1234.56"},
		{"-1234.56", 2, "-1234.56"},
		{"0.05", 2, "0.05"},
		{"0", 2, "0.00"},
		{"-0.01", 2, "-0.01"},
		{"7", 4, "7.0000"},
		{"12", 0, "12"},
	}
	for _, c := range cases {
		if got := mustParse(t, c.in, c.scale).String(); got != c.want {
			t.Errorf("Parse(%q,%d).String() = %q, want %q", c.in, c.scale, got, c.want)
		}
	}
}

func TestParseRejectsPrecisionLoss(t *testing.T) {
	if _, err := Parse("1.234", 2, "INR"); err == nil {
		t.Fatal("Parse accepted a literal with more precision than the scale")
	}
}

func TestAddRejectsMismatch(t *testing.T) {
	a, _ := Parse("1.00", 2, "INR")
	b, _ := Parse("1.00", 2, "USD")
	if _, err := Add(a, b); err == nil {
		t.Fatal("Add accepted mismatched currencies")
	}
	c, _ := Parse("1.000", 3, "INR")
	if _, err := Add(a, c); err == nil {
		t.Fatal("Add accepted mismatched scales")
	}
}

func TestMulRateRounding(t *testing.T) {
	// 100.00 litres at 32.455 per litre = 3245.50 exactly at scale 2.
	qty := mustParse(t, "100.00", 2)
	rate, err := ParseRate("32.455", 3)
	if err != nil {
		t.Fatal(err)
	}
	got, step, err := MulRate(qty, rate, RoundHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "3245.50" {
		t.Errorf("MulRate = %s, want 3245.50", got)
	}
	if step.Discarded != 0 {
		t.Errorf("exact product reported discarded = %d, want 0", step.Discarded)
	}
}

func TestMulRateHalfUpVsHalfEven(t *testing.T) {
	// 1.00 * 0.125 = 0.125, exactly halfway at scale 2.
	amt := mustParse(t, "1.00", 2)
	rate, _ := ParseRate("0.125", 3)

	up, stepUp, err := MulRate(amt, rate, RoundHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	if up.String() != "0.13" {
		t.Errorf("HALF_UP = %s, want 0.13", up)
	}
	if stepUp.Discarded == 0 {
		t.Error("HALF_UP step did not record the discarded remainder")
	}

	even, _, err := MulRate(amt, rate, RoundHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	if even.String() != "0.12" {
		t.Errorf("HALF_EVEN = %s, want 0.12", even)
	}
}

func TestMulRateNegativeHalfUpIsSymmetric(t *testing.T) {
	amt := mustParse(t, "-1.00", 2)
	rate, _ := ParseRate("0.125", 3)
	got, _, err := MulRate(amt, rate, RoundHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "-0.13" {
		t.Errorf("HALF_UP on negative = %s, want -0.13", got)
	}
}

func TestMulRateUsesFullWidthProduct(t *testing.T) {
	// The intermediate product overflows int64; the 128-bit path must not.
	amt := Money{Value: 9_000_000_000_000_000, Scale: 2, Currency: "INR"}
	rate := Rate{Numerator: 1_000_000, Scale: 6} // exactly 1.0
	got, _, err := MulRate(amt, rate, RoundHalfUp)
	if err != nil {
		t.Fatalf("MulRate overflowed on a product that fits the result: %v", err)
	}
	if got.Value != amt.Value {
		t.Errorf("MulRate by 1.0 = %d, want %d", got.Value, amt.Value)
	}
}

func TestMulRateReportsOverflow(t *testing.T) {
	amt := Money{Value: math.MaxInt64, Scale: 2, Currency: "INR"}
	rate := Rate{Numerator: 1000, Scale: 1} // 100x
	if _, _, err := MulRate(amt, rate, RoundHalfUp); err == nil {
		t.Fatal("MulRate silently produced a result that cannot fit in int64")
	}
}

func TestDivRounding(t *testing.T) {
	amt := mustParse(t, "10.00", 2)
	got, step, err := Div(amt, 3, RoundHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "3.33" {
		t.Errorf("Div = %s, want 3.33", got)
	}
	if step.Discarded != 1 {
		t.Errorf("Div discarded = %d, want 1", step.Discarded)
	}
}

func TestRescaleNarrowAndWiden(t *testing.T) {
	m := mustParse(t, "1.2345", 4)
	narrow, step, err := Rescale(m, 2, RoundHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	if narrow.String() != "1.23" {
		t.Errorf("narrow = %s, want 1.23", narrow)
	}
	if step.Discarded != 45 {
		t.Errorf("narrow discarded = %d, want 45", step.Discarded)
	}
	wide, _, err := Rescale(narrow, 4, RoundHalfUp)
	if err != nil {
		t.Fatal(err)
	}
	if wide.String() != "1.2300" {
		t.Errorf("widen = %s, want 1.2300", wide)
	}
}

func TestAllocateSumsToWhole(t *testing.T) {
	pool := mustParse(t, "100.00", 2)
	weights := []int64{3, 3, 3}
	parts, err := Allocate(pool, weights)
	if err != nil {
		t.Fatal(err)
	}
	total, err := Sum(parts)
	if err != nil {
		t.Fatal(err)
	}
	if total.Value != pool.Value {
		t.Fatalf("allocation lost money: parts sum to %s, pool is %s", total, pool)
	}
	// 10000 / 3 = 3333 r1, so exactly one part carries the extra minor unit.
	counts := map[int64]int{}
	for _, p := range parts {
		counts[p.Value]++
	}
	if counts[3334] != 1 || counts[3333] != 2 {
		t.Errorf("unexpected split: %v", counts)
	}
}

func TestAllocateIsDeterministic(t *testing.T) {
	pool := mustParse(t, "0.10", 2)
	weights := []int64{1, 5, 3, 1}
	first, err := Allocate(pool, weights)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		again, err := Allocate(pool, weights)
		if err != nil {
			t.Fatal(err)
		}
		for j := range first {
			if first[j].Value != again[j].Value {
				t.Fatalf("allocation is not deterministic at index %d: %d vs %d", j, first[j].Value, again[j].Value)
			}
		}
	}
}

func TestAllocateNegativePool(t *testing.T) {
	pool := mustParse(t, "-100.00", 2)
	parts, err := Allocate(pool, []int64{3, 3, 3})
	if err != nil {
		t.Fatal(err)
	}
	total, err := Sum(parts)
	if err != nil {
		t.Fatal(err)
	}
	if total.Value != pool.Value {
		t.Fatalf("negative allocation lost money: %s vs %s", total, pool)
	}
}

func TestAllocateRejectsZeroWeights(t *testing.T) {
	pool := mustParse(t, "10.00", 2)
	if _, err := Allocate(pool, []int64{0, 0}); err == nil {
		t.Fatal("Allocate accepted weights summing to zero")
	}
}

func TestSumEmptyIsError(t *testing.T) {
	if _, err := Sum(nil); err == nil {
		t.Fatal("Sum(nil) returned a zero instead of an error")
	}
}
