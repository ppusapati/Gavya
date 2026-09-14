package exact

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestFixedReadsTheDigitsThatWereWritten(t *testing.T) {
	for _, tc := range []struct {
		in    string
		scale int32
		want  string
	}{
		{"12.5", 3, "12.500"},
		{"0.1", 1, "0.1"},
		{"7", 0, "7"},
		{"-3.25", 2, "-3.25"},
		{"19.995", 3, "19.995"},
		{"999999999.999", 3, "999999999.999"},
	} {
		got, err := ParseFixed(tc.in, tc.scale)
		if err != nil {
			t.Errorf("ParseFixed(%q, %d): %v", tc.in, tc.scale, err)
			continue
		}
		if got.String() != tc.want {
			t.Errorf("ParseFixed(%q, %d) = %s, want %s", tc.in, tc.scale, got, tc.want)
		}
		if got.Scale != tc.scale {
			t.Errorf("ParseFixed(%q, %d) landed at scale %d", tc.in, tc.scale, got.Scale)
		}
	}
}

func TestFixedRefusesALiteralFinerThanTheScale(t *testing.T) {
	if _, err := ParseFixed("12.3456", 3); err == nil {
		t.Error("12.3456 at scale 3 was accepted; the fourth decimal was meant by somebody")
	}
}

// The JSON path is the one that matters: a JSON number must be read from its
// own digits, not from the float64 encoding/json would otherwise make of it.
func TestFixedReadsAJSONNumberExactly(t *testing.T) {
	var got struct {
		Q Fixed `json:"q"`
	}
	for _, tc := range []struct {
		body string
		want Fixed
	}{
		{`{"q": 0.1}`, Fixed{1, 1}},
		{`{"q": 12.345}`, Fixed{12345, 3}},
		{`{"q": 33}`, Fixed{33, 0}},
		{`{"q": "12.500"}`, Fixed{12500, 3}},
		{`{"q": "-0.25"}`, Fixed{-25, 2}},
		{`{"q": 10.00025}`, Fixed{1000025, 5}},
		// Eighteen significant digits. A float64 holds about sixteen, so a
		// reader that went through one would lose the last two and land on a
		// neighbouring value; this is the case that tells the two readers apart.
		{`{"q": 123456789012345.678}`, Fixed{123456789012345678, 3}},
		{`{"q": "123456789012345.678"}`, Fixed{123456789012345678, 3}},
	} {
		got.Q = Fixed{}
		if err := json.Unmarshal([]byte(tc.body), &got); err != nil {
			t.Errorf("%s: %v", tc.body, err)
			continue
		}
		if got.Q != tc.want {
			t.Errorf("%s read as %+v, want %+v", tc.body, got.Q, tc.want)
		}
	}
}

func TestFixedRefusesAnExponentAndNonsense(t *testing.T) {
	var got struct {
		Q Fixed `json:"q"`
	}
	if err := json.Unmarshal([]byte(`{"q": 1.25e1}`), &got); !errors.Is(err, ErrExponent) {
		t.Errorf("1.25e1: err = %v, want ErrExponent", err)
	}
	if err := json.Unmarshal([]byte(`{"q": "twelve"}`), &got); err == nil {
		t.Error(`"twelve" was accepted as a quantity`)
	}
	if err := json.Unmarshal([]byte(`{"q": true}`), &got); err == nil {
		t.Error("true was accepted as a quantity")
	}
}

func TestFixedLeavesAMissingOrNullFieldAlone(t *testing.T) {
	got := struct {
		Q Fixed `json:"q"`
	}{Q: Fixed{5, 0}}
	if err := json.Unmarshal([]byte(`{"q": null}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Q != (Fixed{5, 0}) {
		t.Errorf("null overwrote the value: %+v", got.Q)
	}
	if err := json.Unmarshal([]byte(`{}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.Q != (Fixed{5, 0}) {
		t.Errorf("a missing field overwrote the value: %+v", got.Q)
	}
}

func TestFixedWritesADecimalLiteralInAString(t *testing.T) {
	out, err := json.Marshal(struct {
		Q Fixed `json:"q"`
	}{Fixed{12500, 3}})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"q":"12.500"}` {
		t.Errorf("marshalled as %s, want a quoted literal at the value's scale", out)
	}
}

func TestFixedRoundTripsThroughJSON(t *testing.T) {
	for _, f := range []Fixed{{12500, 3}, {0, 3}, {-1, 2}, {123456789012, 4}, {7, 0}} {
		out, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		var back Fixed
		if err := json.Unmarshal(out, &back); err != nil {
			t.Fatal(err)
		}
		if back != f {
			t.Errorf("%+v went out as %s and came back as %+v", f, out, back)
		}
	}
}

func TestAtWidensExactlyAndNarrowsOnlyPadding(t *testing.T) {
	q := Fixed{125, 1} // 12.5
	wide, err := q.At(3)
	if err != nil || wide != (Fixed{12500, 3}) {
		t.Errorf("12.5 at scale 3 = %+v, %v; want 12.500", wide, err)
	}
	narrow, err := (Fixed{12500, 3}).At(1)
	if err != nil || narrow != (Fixed{125, 1}) {
		t.Errorf("12.500 at scale 1 = %+v, %v; want 12.5", narrow, err)
	}
	if _, err := (Fixed{12345, 3}).At(1); !errors.Is(err, ErrTooPrecise) {
		t.Errorf("12.345 at scale 1: err = %v, want ErrTooPrecise", err)
	}
}

func TestFitsIsTheColumnsPrecision(t *testing.T) {
	// NUMERIC(5,2): at most 999.99.
	if err := (Fixed{99999, 2}).Fits(5); err != nil {
		t.Errorf("999.99 should fit NUMERIC(5,2): %v", err)
	}
	if err := (Fixed{100000, 2}).Fits(5); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("1000.00 in NUMERIC(5,2): err = %v, want ErrOutOfRange", err)
	}
	if err := (Fixed{-100000, 2}).Fits(5); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("-1000.00 in NUMERIC(5,2): err = %v, want ErrOutOfRange", err)
	}
	if err := (Fixed{1 << 62, 0}).Fits(19); err != nil {
		t.Errorf("a precision wider than int64 refuses nothing: %v", err)
	}
}

func TestNonNegativeColumnIsTheWholeCheckAValueGetsAtTheDoor(t *testing.T) {
	got, err := (Fixed{125, 1}).NonNegativeColumn(3, 10)
	if err != nil || got != (Fixed{12500, 3}) {
		t.Errorf("12.5 into NUMERIC(10,3) = %+v, %v", got, err)
	}
	if _, err := (Fixed{-125, 1}).NonNegativeColumn(3, 10); !errors.Is(err, ErrNegative) {
		t.Errorf("-12.5: err = %v, want ErrNegative", err)
	}
	if _, err := (Fixed{1000025, 5}).NonNegativeColumn(3, 10); !errors.Is(err, ErrTooPrecise) {
		t.Errorf("10.00025 into NUMERIC(10,3): err = %v, want ErrTooPrecise", err)
	}
	if _, err := (Fixed{10000000000, 0}).NonNegativeColumn(3, 10); !errors.Is(err, ErrOutOfRange) {
		t.Errorf("10^10 into NUMERIC(10,3): err = %v, want ErrOutOfRange", err)
	}
}

func TestArithmeticRefusesToCrossScales(t *testing.T) {
	sum, err := (Fixed{12500, 3}).Add(Fixed{7250, 3})
	if err != nil || sum != (Fixed{19750, 3}) {
		t.Errorf("12.500 + 7.250 = %+v, %v", sum, err)
	}
	diff, err := (Fixed{12500, 3}).Sub(Fixed{7250, 3})
	if err != nil || diff != (Fixed{5250, 3}) {
		t.Errorf("12.500 - 7.250 = %+v, %v", diff, err)
	}
	if _, err := (Fixed{12500, 3}).Add(Fixed{125, 1}); !errors.Is(err, ErrScaleMismatch) {
		t.Errorf("adding across scales: err = %v, want ErrScaleMismatch", err)
	}
	if c, err := (Fixed{1, 3}).Cmp(Fixed{2, 3}); err != nil || c != -1 {
		t.Errorf("0.001 cmp 0.002 = %d, %v", c, err)
	}
	if _, err := (Fixed{1, 3}).Cmp(Fixed{1, 2}); !errors.Is(err, ErrScaleMismatch) {
		t.Errorf("comparing across scales: err = %v, want ErrScaleMismatch", err)
	}
}

func TestSign(t *testing.T) {
	for v, want := range map[int64]int{-3: -1, 0: 0, 9: 1} {
		if got := (Fixed{v, 2}).Sign(); got != want {
			t.Errorf("Sign(%d) = %d, want %d", v, got, want)
		}
	}
}

// Scan is what a NUMERIC column arrives through; the column renders at its own
// scale and that is the scale the value lands at.
func TestScanReadsWhatTheColumnRendered(t *testing.T) {
	var f Fixed
	if err := f.Scan("12.500"); err != nil || f != (Fixed{12500, 3}) {
		t.Errorf("scan \"12.500\" = %+v, %v", f, err)
	}
	if err := f.Scan([]byte("0")); err != nil || f != (Fixed{0, 0}) {
		t.Errorf("scan 0 = %+v, %v", f, err)
	}
	if err := f.Scan(nil); err != nil || f != (Fixed{}) {
		t.Errorf("scan NULL = %+v, %v", f, err)
	}
	if err := f.Scan(12.5); err == nil {
		t.Error("a float64 was scanned into a Fixed; that is the path this type exists to close")
	}
	v, err := (Fixed{12500, 3}).Value()
	if err != nil || v != "12.500" {
		t.Errorf("Value() = %v, %v; want the literal", v, err)
	}
}

func TestMustFixedPanicsOnABadLiteralInSource(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a bad literal in source did not panic")
		}
	}()
	MustFixed("12.3456", 3)
}

// Float64 is the boundary to the statistics tier, and the only place a value
// here becomes a float. It has to be the same number on the way out.
func TestFloat64IsTheValueAtTheBoundary(t *testing.T) {
	for _, tc := range []struct {
		in   Fixed
		want float64
	}{
		{Fixed{12500, 3}, 12.5},
		{Fixed{0, 6}, 0},
		{Fixed{-25, 2}, -0.25},
		{Fixed{410, 2}, 4.10},
		{Fixed{7, 0}, 7},
	} {
		if got := tc.in.Float64(); got != tc.want {
			t.Errorf("%s.Float64() = %v, want %v", tc.in, got, tc.want)
		}
	}
}
