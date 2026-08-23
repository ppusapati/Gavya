// Package currency answers one question the whole platform depends on: how many
// decimal places does this currency have?
//
// Almost every money column in this codebase is NUMERIC(_, 2), and almost every
// rounding step assumes two decimals. That assumption is an Indian-rupee
// assumption wearing a general-purpose coat. A yen has no minor unit at all, so
// storing ¥100 as 100.00 and displaying it as "¥100.00" is wrong in a way a
// Japanese accountant will notice immediately. A Kuwaiti dinar has three, so
// rounding a dinar amount to two decimals silently discards a fils — the same
// class of error as the float64 arithmetic this platform already refuses.
//
// The table below is ISO 4217. It is data, not logic: the only thing this
// package decides is what to do when a code is not in it, and the answer is to
// refuse rather than to assume two.
package currency

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrUnknown is a code that is not ISO 4217. Guessing two decimals for an
	// unrecognised code is how a deployment ends up quietly wrong in a country
	// nobody tested in.
	ErrUnknown = errors.New("not a currency this platform recognises")
	// ErrMalformed is anything that is not three letters.
	ErrMalformed = errors.New("a currency code is three letters, as in INR or JPY")
)

// minorUnits is the number of digits after the decimal point, per ISO 4217.
//
// Entries that are not 2 are the reason this table exists. They are grouped
// together below so the exceptions can be read at a glance rather than hunted
// for among two hundred ordinary lines.
var minorUnits = map[string]int32{
	// No minor unit at all. A price in these is a whole number, and showing
	// "100.00" is not a formatting nicety — it is a different number.
	"BIF": 0, "CLP": 0, "DJF": 0, "GNF": 0, "ISK": 0, "JPY": 0, "KMF": 0,
	"KRW": 0, "PYG": 0, "RWF": 0, "UGX": 0, "UYI": 0, "VND": 0, "VUV": 0,
	"XAF": 0, "XDR": 0, "XOF": 0, "XPF": 0,

	// Three. Rounding one of these to two decimals throws away a real unit of
	// money that a producer would be entitled to.
	"BHD": 3, "IQD": 3, "JOD": 3, "KWD": 3, "LYD": 3, "OMR": 3, "TND": 3,

	// Four. Both are index units rather than cash, but they are ISO 4217 and a
	// contract can be denominated in them.
	"CLF": 4, "UYW": 4,

	// Everything below has two.
	"AED": 2, "AFN": 2, "ALL": 2, "AMD": 2, "ANG": 2, "AOA": 2, "ARS": 2,
	"AUD": 2, "AWG": 2, "AZN": 2, "BAM": 2, "BBD": 2, "BDT": 2, "BGN": 2,
	"BMD": 2, "BND": 2, "BOB": 2, "BOV": 2, "BRL": 2, "BSD": 2, "BTN": 2,
	"BWP": 2, "BYN": 2, "BZD": 2, "CAD": 2, "CDF": 2, "CHE": 2, "CHF": 2,
	"CHW": 2, "CNY": 2, "COP": 2, "COU": 2, "CRC": 2, "CUP": 2, "CVE": 2,
	"CZK": 2, "DKK": 2, "DOP": 2, "DZD": 2, "EGP": 2, "ERN": 2, "ETB": 2,
	"EUR": 2, "FJD": 2, "FKP": 2, "GBP": 2, "GEL": 2, "GHS": 2, "GIP": 2,
	"GMD": 2, "GTQ": 2, "GYD": 2, "HKD": 2, "HNL": 2, "HTG": 2, "HUF": 2,
	"IDR": 2, "ILS": 2, "INR": 2, "IRR": 2, "JMD": 2, "KES": 2, "KGS": 2,
	"KHR": 2, "KPW": 2, "KYD": 2, "KZT": 2, "LAK": 2, "LBP": 2, "LKR": 2,
	"LRD": 2, "LSL": 2, "MAD": 2, "MDL": 2, "MGA": 2, "MKD": 2, "MMK": 2,
	"MNT": 2, "MOP": 2, "MRU": 2, "MUR": 2, "MVR": 2, "MWK": 2, "MXN": 2,
	"MXV": 2, "MYR": 2, "MZN": 2, "NAD": 2, "NGN": 2, "NIO": 2, "NOK": 2,
	"NPR": 2, "NZD": 2, "PAB": 2, "PEN": 2, "PGK": 2, "PHP": 2, "PKR": 2,
	"PLN": 2, "QAR": 2, "RON": 2, "RSD": 2, "RUB": 2, "SAR": 2, "SBD": 2,
	"SCR": 2, "SDG": 2, "SEK": 2, "SGD": 2, "SHP": 2, "SLE": 2, "SOS": 2,
	"SRD": 2, "SSP": 2, "STN": 2, "SVC": 2, "SYP": 2, "SZL": 2, "THB": 2,
	"TJS": 2, "TMT": 2, "TOP": 2, "TRY": 2, "TTD": 2, "TWD": 2, "TZS": 2,
	"UAH": 2, "USD": 2, "USN": 2, "UYU": 2, "UZS": 2, "VED": 2, "VES": 2,
	"WST": 2, "XCD": 2, "XCG": 2, "YER": 2, "ZAR": 2, "ZMW": 2, "ZWG": 2,
}

// MaxScale is the largest minor-unit count any ISO 4217 currency has. Money
// columns are sized to hold it, so a deployment in one country does not need a
// different schema from a deployment in another.
const MaxScale int32 = 4

// Normalise upper-cases a code and checks its shape.
func Normalise(code string) (string, error) {
	c := strings.ToUpper(strings.TrimSpace(code))
	if len(c) != 3 {
		return "", fmt.Errorf("%w: got %q", ErrMalformed, code)
	}
	for _, r := range c {
		if r < 'A' || r > 'Z' {
			return "", fmt.Errorf("%w: got %q", ErrMalformed, code)
		}
	}
	return c, nil
}

// Scale reports how many digits after the point this currency has.
//
// An unrecognised code is an error rather than a default of two. A deployment
// in a country whose currency is missing from the table should fail loudly on
// its first day, not round every amount wrongly for a year.
func Scale(code string) (int32, error) {
	c, err := Normalise(code)
	if err != nil {
		return 0, err
	}
	s, ok := minorUnits[c]
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrUnknown, c)
	}
	return s, nil
}

// IsKnown reports whether the code is one this platform can handle.
func IsKnown(code string) bool {
	_, err := Scale(code)
	return err == nil
}

// Validate checks a code and the scale a caller believes it has.
//
// The scale is stored alongside the currency wherever money is recorded, so
// this is what stops the two drifting apart: a record claiming INR at scale 3
// is not an INR record, and reading it back as one would multiply every amount
// by ten.
func Validate(code string, scale int32) error {
	want, err := Scale(code)
	if err != nil {
		return err
	}
	if scale != want {
		return fmt.Errorf("%s has %d decimal places, not %d", code, want, scale)
	}
	return nil
}

// Codes returns every recognised code, for a picker or a validation message.
// The order is not meaningful; callers that show them to a person should sort.
func Codes() []string {
	out := make([]string, 0, len(minorUnits))
	for c := range minorUnits {
		out = append(out, c)
	}
	return out
}
