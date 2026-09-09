package money

import (
	"strings"
	"testing"
)

// The literals here are the ones PostgreSQL actually produces. They were taken
// from a NUMERIC(18,4) column rather than written down from memory: a zero comes
// back as "0" with no decimal point at all, which is the case that a test
// written from the type declaration would miss.
func TestParseStoredDropsTheColumnsPaddingAndNothingElse(t *testing.T) {
	for _, c := range []struct {
		name        string
		literal     string
		storedScale int32
		scale       int32
		currency    string
		want        string
	}{
		{"a rupee padded to four decimals", "42.5000", 4, 2, "INR", "42.50"},
		{"a rupee that is already exact", "42.5000", 4, 2, "INR", "42.50"},
		{"zero, which PostgreSQL renders without a point", "0", 4, 2, "INR", "0.00"},
		{"a dinar keeps its third decimal", "1.2340", 4, 3, "BHD", "1.234"},
		{"a four-decimal currency keeps all four", "6791947779410.3551", 4, 4, "CLF", "6791947779410.3551"},
		{"a yen has no decimals to keep", "500.0000", 4, 0, "JPY", "500"},
		{"a negative amount", "-7.2500", 4, 2, "INR", "-7.25"},
		{"a scale that does not change", "3.14", 2, 2, "INR", "3.14"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, err := ParseStored(c.literal, c.storedScale, c.scale, c.currency)
			if err != nil {
				t.Fatalf("ParseStored(%q) = %v", c.literal, err)
			}
			if got := m.String(); got != c.want {
				t.Errorf("ParseStored(%q) = %s, want %s", c.literal, got, c.want)
			}
			if m.Scale != c.scale {
				t.Errorf("scale = %d, want %d", m.Scale, c.scale)
			}
			if m.Currency != c.currency {
				t.Errorf("currency = %s, want %s", m.Currency, c.currency)
			}
		})
	}
}

// The whole point of the function. A digit the currency cannot express is not
// padding, and rounding it away would leave the service reporting one figure
// while its database held another.
func TestParseStoredRefusesPrecisionTheCurrencyCannotExpress(t *testing.T) {
	for _, c := range []struct {
		name    string
		literal string
		scale   int32
	}{
		{"a rupee with a third decimal", "42.5050", 2},
		{"a rupee with a fourth decimal", "42.5001", 2},
		{"a dinar with a fourth decimal", "1.2345", 3},
		{"a yen with any decimal at all", "500.5000", 0},
		{"a negative amount is checked too", "-0.0001", 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, err := ParseStored(c.literal, 4, c.scale, "XXX")
			if err == nil {
				t.Fatalf("ParseStored(%q) at scale %d = %s, want an error; rounding it away "+
					"would make the caller disagree with its own database",
					c.literal, c.scale, m.String())
			}
			// The message has to carry the literal, or an operator reading a log
			// cannot tell which row is wrong.
			if !strings.Contains(err.Error(), c.literal) {
				t.Errorf("error %q does not name the value that caused it", err)
			}
		})
	}
}

// Parse's own refusal still applies: a literal finer than the column itself is
// not something ParseStored should quietly widen to accommodate.
func TestParseStoredStillRefusesALiteralFinerThanTheColumn(t *testing.T) {
	if _, err := ParseStored("1.23456", 4, 2, "INR"); err == nil {
		t.Error("a five-decimal literal was accepted for a four-decimal column")
	}
}

func TestParseStoredRefusesNegativeScales(t *testing.T) {
	if _, err := ParseStored("1.00", -1, 2, "INR"); err != ErrNegativeScale {
		t.Errorf("stored scale -1: err = %v, want ErrNegativeScale", err)
	}
	if _, err := ParseStored("1.00", 4, -1, "INR"); err != ErrNegativeScale {
		t.Errorf("target scale -1: err = %v, want ErrNegativeScale", err)
	}
}
