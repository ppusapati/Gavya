package currency

import (
	"errors"
	"testing"
)

// The exceptions are the reason this package exists, so they are checked by
// name rather than left to a general rule.
func TestCurrenciesWithNoMinorUnit(t *testing.T) {
	for _, c := range []string{"JPY", "KRW", "VND", "CLP", "ISK", "XOF", "XAF", "UGX", "RWF"} {
		got, err := Scale(c)
		if err != nil {
			t.Errorf("Scale(%s): %v", c, err)
			continue
		}
		if got != 0 {
			t.Errorf("Scale(%s) = %d, want 0 — an amount in %s is a whole number", c, got, c)
		}
	}
}

func TestCurrenciesWithThreeMinorDigits(t *testing.T) {
	for _, c := range []string{"KWD", "BHD", "JOD", "OMR", "TND", "IQD", "LYD"} {
		got, err := Scale(c)
		if err != nil {
			t.Errorf("Scale(%s): %v", c, err)
			continue
		}
		if got != 3 {
			t.Errorf("Scale(%s) = %d, want 3 — rounding to 2 would discard a real unit", c, got)
		}
	}
}

func TestTheOrdinaryCaseIsTwo(t *testing.T) {
	for _, c := range []string{"INR", "USD", "EUR", "GBP", "KES", "BRL", "AUD", "ZAR"} {
		got, err := Scale(c)
		if err != nil {
			t.Errorf("Scale(%s): %v", c, err)
			continue
		}
		if got != 2 {
			t.Errorf("Scale(%s) = %d, want 2", c, got)
		}
	}
}

// An unrecognised code must not quietly become two decimals. A deployment in a
// country this table has missed should fail on its first day rather than round
// every amount wrongly for a year.
func TestAnUnknownCodeIsRefusedRatherThanAssumed(t *testing.T) {
	for _, c := range []string{"ZZZ", "XYZ", "QQQ"} {
		if _, err := Scale(c); !errors.Is(err, ErrUnknown) {
			t.Errorf("Scale(%s) err = %v, want ErrUnknown", c, err)
		}
	}
}

func TestAMalformedCodeIsRefused(t *testing.T) {
	for _, c := range []string{"", "IN", "INRR", "1NR", "in r", "₹₹₹"} {
		if _, err := Scale(c); !errors.Is(err, ErrMalformed) {
			t.Errorf("Scale(%q) err = %v, want ErrMalformed", c, err)
		}
	}
}

func TestCaseAndSpacingDoNotMatter(t *testing.T) {
	for _, c := range []string{"inr", " INR ", "InR"} {
		got, err := Scale(c)
		if err != nil {
			t.Errorf("Scale(%q): %v", c, err)
			continue
		}
		if got != 2 {
			t.Errorf("Scale(%q) = %d, want 2", c, got)
		}
	}
}

// A record that claims a currency and a scale that do not go together is not a
// record in that currency; reading it back as one would multiply every amount
// in it by a power of ten.
func TestValidateCatchesAScaleThatDoesNotMatchItsCurrency(t *testing.T) {
	if err := Validate("INR", 2); err != nil {
		t.Errorf("INR at scale 2 was refused: %v", err)
	}
	if err := Validate("JPY", 0); err != nil {
		t.Errorf("JPY at scale 0 was refused: %v", err)
	}
	if err := Validate("KWD", 3); err != nil {
		t.Errorf("KWD at scale 3 was refused: %v", err)
	}

	if err := Validate("JPY", 2); err == nil {
		t.Error("JPY at scale 2 was accepted; every amount would be a hundred times too small")
	}
	if err := Validate("KWD", 2); err == nil {
		t.Error("KWD at scale 2 was accepted; a fils would be silently discarded")
	}
	if err := Validate("INR", 0); err == nil {
		t.Error("INR at scale 0 was accepted")
	}
}

// Money columns are sized once, for every country. If a currency existed with a
// finer minor unit than the columns hold, amounts in it would be rounded on the
// way in — so the constant and the table have to agree.
func TestMaxScaleCoversEveryCurrencyInTheTable(t *testing.T) {
	for _, c := range Codes() {
		s, err := Scale(c)
		if err != nil {
			t.Fatalf("Codes returned %s, which Scale rejects: %v", c, err)
		}
		if s > MaxScale {
			t.Errorf("%s has scale %d, beyond MaxScale %d — the money columns cannot hold it", c, s, MaxScale)
		}
		if s < 0 {
			t.Errorf("%s has a negative scale %d", c, s)
		}
	}
}

func TestTheTableCoversTheCountriesThisIsLikelyToBeDeployedIn(t *testing.T) {
	// Dairy economies across every continent. Not exhaustive, but a missing one
	// here would mean a deployment that cannot record its own money.
	for _, c := range []string{
		"INR", "PKR", "BDT", "LKR", "NPR", // South Asia
		"USD", "CAD", "MXN", "BRL", "ARS", "UYU", "CLP", // Americas
		"EUR", "GBP", "CHF", "PLN", "RON", "TRY", "UAH", // Europe
		"KES", "UGX", "TZS", "ETB", "ZAR", "NGN", "MAD", "EGP", // Africa
		"AUD", "NZD", "JPY", "CNY", "IDR", "VND", "THB", "PHP", // Asia-Pacific
		"SAR", "AED", "QAR", "KWD", "OMR", "BHD", "JOD", // Middle East
	} {
		if !IsKnown(c) {
			t.Errorf("%s is not in the table; a deployment there could not record money", c)
		}
	}
}

func TestCodesAreAllWellFormed(t *testing.T) {
	if len(Codes()) < 150 {
		t.Errorf("the table has only %d entries, which is too few to be ISO 4217", len(Codes()))
	}
	for _, c := range Codes() {
		if _, err := Normalise(c); err != nil {
			t.Errorf("the table contains a malformed code %q: %v", c, err)
		}
	}
}
