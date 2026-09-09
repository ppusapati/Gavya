package money

import "fmt"

// ParseStored reads an amount out of a fixed-scale database column.
//
// It exists because a money column and a currency do not have the same number
// of decimals, and the difference is padding rather than precision.
//
// The columns in this platform are NUMERIC(_,4): four is the most any ISO 4217
// currency has, so one schema serves a yen deployment, a rupee one and a dinar
// one. PostgreSQL renders what it stores at the column's scale, so a rupee price
// of 42.50 comes back as the literal "42.5000". Those last two zeros are the
// column type talking, not the amount. Reading such a literal straight into a
// two-decimal currency with Parse fails — correctly, since Parse never discards
// precision — and the fix is not to loosen Parse but to say which of the digits
// were the column's.
//
// So: read at storedScale, then come down to scale, and refuse if anything
// non-zero would be dropped. A rupee amount with a non-zero third decimal is a
// figure that currency cannot express; something upstream put it there, and
// rounding it away here would make a service quietly disagree with its own
// database about what it holds. That is worse than an error, because both ends
// go on believing they agree.
//
// The rounding mode is deliberately absent. Nothing is ever rounded: the only
// accepted outcome is that the discarded digits were zeros, which is a
// decomposition of the column's padding rather than a decision about money. A
// mode here would be a default that looked like a decision.
// A negative scale is refused by Parse and by Rescale, so there is no guard for
// one here. An earlier version had one; removing it changed no test's outcome,
// which is what a redundant check looks like when it is measured rather than
// assumed to be helping.
func ParseStored(literal string, storedScale, scale int32, currency string) (Money, error) {
	stored, err := Parse(literal, storedScale, currency)
	if err != nil {
		return Money{}, err
	}
	// TOWARD_ZERO is named because Rescale requires a mode, and it is the one
	// that alters nothing when the remainder is zero. The check below is what
	// makes the choice inert.
	m, step, err := Rescale(stored, scale, RoundTowardZero)
	if err != nil {
		return Money{}, err
	}
	if step.Discarded != 0 {
		return Money{}, fmt.Errorf(
			"money: stored value %q has more precision than %s records: reading it at %d "+
				"decimals would drop %d, and dropping it silently would leave this service "+
				"disagreeing with the column it read",
			literal, currency, scale, step.Discarded)
	}
	return m, nil
}
