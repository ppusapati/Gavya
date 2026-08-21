// Package money implements deterministic fixed-point monetary arithmetic.
//
// Every operation that loses precision returns the rounding step that produced
// the loss, so a settlement computation can carry an auditable rounding trail
// rather than an unexplainable residual.
package money

import (
	"errors"
	"fmt"
	"math"
	"math/bits"
	"strconv"
	"strings"
)

var (
	ErrCurrencyMismatch = errors.New("money: currency mismatch")
	ErrScaleMismatch    = errors.New("money: scale mismatch")
	ErrOverflow         = errors.New("money: arithmetic overflow")
	ErrNegativeScale    = errors.New("money: scale must not be negative")
	ErrDivideByZero     = errors.New("money: divide by zero")
)

// RoundingMode enumerates the rounding policies a settlement rule may declare.
type RoundingMode string

const (
	RoundHalfUp     RoundingMode = "HALF_UP"
	RoundHalfEven   RoundingMode = "HALF_EVEN"
	RoundDown       RoundingMode = "DOWN"
	RoundUp         RoundingMode = "UP"
	RoundHalfDown   RoundingMode = "HALF_DOWN"
	RoundTowardZero RoundingMode = "TOWARD_ZERO"
)

// Money is an exact quantity of currency expressed in minor units at Scale.
// Value 12345 at Scale 2 is 123.45.
type Money struct {
	Value    int64  `json:"value"`
	Scale    int32  `json:"scale"`
	Currency string `json:"currency"`
}

// RoundingStep records one precision-losing operation for the provenance trail.
type RoundingStep struct {
	Operation string       `json:"operation"`
	Mode      RoundingMode `json:"mode"`
	FromScale int32        `json:"from_scale"`
	ToScale   int32        `json:"to_scale"`
	Discarded int64        `json:"discarded"`
	Result    Money        `json:"result"`
}

func New(value int64, scale int32, currency string) (Money, error) {
	if scale < 0 {
		return Money{}, ErrNegativeScale
	}
	return Money{Value: value, Scale: scale, Currency: strings.ToUpper(currency)}, nil
}

func Zero(scale int32, currency string) Money {
	return Money{Value: 0, Scale: scale, Currency: strings.ToUpper(currency)}
}

// Parse reads a decimal literal such as "1234.5670" into the given scale.
// Parsing never rounds: a literal with more fractional digits than scale is an
// error, because silently discarding source precision defeats divergence
// analysis.
func Parse(s string, scale int32, currency string) (Money, error) {
	if scale < 0 {
		return Money{}, ErrNegativeScale
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return Money{}, fmt.Errorf("money: empty literal")
	}
	neg := false
	switch s[0] {
	case '-':
		neg, s = true, s[1:]
	case '+':
		s = s[1:]
	}
	intPart, fracPart, _ := strings.Cut(s, ".")
	if intPart == "" {
		intPart = "0"
	}
	if int32(len(fracPart)) > scale {
		return Money{}, fmt.Errorf("money: literal %q carries more precision than scale %d", s, scale)
	}
	digits := intPart + fracPart + strings.Repeat("0", int(scale)-len(fracPart))
	v, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return Money{}, fmt.Errorf("money: parse %q: %w", s, err)
	}
	if neg {
		v = -v
	}
	return Money{Value: v, Scale: scale, Currency: strings.ToUpper(currency)}, nil
}

func (m Money) String() string {
	sign := ""
	v := m.Value
	if v < 0 {
		sign, v = "-", -v
	}
	if m.Scale == 0 {
		return sign + strconv.FormatInt(v, 10)
	}
	digits := strconv.FormatInt(v, 10)
	if int32(len(digits)) <= m.Scale {
		digits = strings.Repeat("0", int(m.Scale)-len(digits)+1) + digits
	}
	split := int32(len(digits)) - m.Scale
	return sign + digits[:split] + "." + digits[split:]
}

func (m Money) IsZero() bool { return m.Value == 0 }

func (m Money) Neg() Money { return Money{Value: -m.Value, Scale: m.Scale, Currency: m.Currency} }

func (m Money) Abs() Money {
	if m.Value < 0 {
		return m.Neg()
	}
	return m
}

func (m Money) compatible(o Money) error {
	if m.Currency != o.Currency {
		return fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.Currency, o.Currency)
	}
	if m.Scale != o.Scale {
		return fmt.Errorf("%w: %d vs %d", ErrScaleMismatch, m.Scale, o.Scale)
	}
	return nil
}

// Add is exact: same currency and scale in, no rounding out.
func Add(a, b Money) (Money, error) {
	if err := a.compatible(b); err != nil {
		return Money{}, err
	}
	sum := a.Value + b.Value
	if (a.Value > 0 && b.Value > 0 && sum < 0) || (a.Value < 0 && b.Value < 0 && sum > 0) {
		return Money{}, ErrOverflow
	}
	return Money{Value: sum, Scale: a.Scale, Currency: a.Currency}, nil
}

func Sub(a, b Money) (Money, error) { return Add(a, b.Neg()) }

// Sum adds a slice exactly. An empty slice is an error rather than an implied
// zero, because the caller's currency and scale would otherwise be guessed.
func Sum(items []Money) (Money, error) {
	if len(items) == 0 {
		return Money{}, fmt.Errorf("money: cannot sum an empty slice")
	}
	acc := items[0]
	for _, it := range items[1:] {
		var err error
		if acc, err = Add(acc, it); err != nil {
			return Money{}, err
		}
	}
	return acc, nil
}

// Rate is an exact multiplier expressed as Numerator / 10^Scale, used for
// price-per-litre, butterfat differentials and percentage deductions.
type Rate struct {
	Numerator int64 `json:"numerator"`
	Scale     int32 `json:"scale"`
}

func NewRate(numerator int64, scale int32) (Rate, error) {
	if scale < 0 {
		return Rate{}, ErrNegativeScale
	}
	return Rate{Numerator: numerator, Scale: scale}, nil
}

func ParseRate(s string, scale int32) (Rate, error) {
	m, err := Parse(s, scale, "XXX")
	if err != nil {
		return Rate{}, err
	}
	return Rate{Numerator: m.Value, Scale: m.Scale}, nil
}

func (r Rate) String() string {
	return Money{Value: r.Numerator, Scale: r.Scale}.String()
}

// MulRate multiplies money by a rate and rounds the product back to the
// money's scale under the given mode, returning the rounding step taken.
func MulRate(m Money, r Rate, mode RoundingMode) (Money, RoundingStep, error) {
	divisor := pow10(r.Scale)
	if divisor == 0 {
		return Money{}, RoundingStep{}, ErrOverflow
	}
	q, rem, err := div128(mul128(m.Value, r.Numerator), divisor)
	if err != nil {
		return Money{}, RoundingStep{}, err
	}
	q = applyRounding(q, rem, divisor, mode)
	res := Money{Value: q, Scale: m.Scale, Currency: m.Currency}
	return res, RoundingStep{
		Operation: "MUL_RATE",
		Mode:      mode,
		FromScale: m.Scale + r.Scale,
		ToScale:   m.Scale,
		Discarded: rem,
		Result:    res,
	}, nil
}

// Div divides money by an integer divisor, rounding under the given mode.
func Div(m Money, divisor int64, mode RoundingMode) (Money, RoundingStep, error) {
	if divisor == 0 {
		return Money{}, RoundingStep{}, ErrDivideByZero
	}
	q := m.Value / divisor
	rem := m.Value % divisor
	q = applyRounding(q, rem, divisor, mode)
	res := Money{Value: q, Scale: m.Scale, Currency: m.Currency}
	return res, RoundingStep{
		Operation: "DIV",
		Mode:      mode,
		FromScale: m.Scale,
		ToScale:   m.Scale,
		Discarded: rem,
		Result:    res,
	}, nil
}

// Rescale converts money to a different scale. Widening is exact; narrowing
// rounds under the given mode.
func Rescale(m Money, target int32, mode RoundingMode) (Money, RoundingStep, error) {
	if target < 0 {
		return Money{}, RoundingStep{}, ErrNegativeScale
	}
	if target == m.Scale {
		return m, RoundingStep{Operation: "RESCALE", Mode: mode, FromScale: m.Scale, ToScale: target, Result: m}, nil
	}
	if target > m.Scale {
		factor := pow10(target - m.Scale)
		if factor == 0 || willOverflowMul(m.Value, factor) {
			return Money{}, RoundingStep{}, ErrOverflow
		}
		res := Money{Value: m.Value * factor, Scale: target, Currency: m.Currency}
		return res, RoundingStep{Operation: "RESCALE", Mode: mode, FromScale: m.Scale, ToScale: target, Result: res}, nil
	}
	divisor := pow10(m.Scale - target)
	if divisor == 0 {
		return Money{}, RoundingStep{}, ErrOverflow
	}
	q, rem := m.Value/divisor, m.Value%divisor
	q = applyRounding(q, rem, divisor, mode)
	res := Money{Value: q, Scale: target, Currency: m.Currency}
	return res, RoundingStep{
		Operation: "RESCALE",
		Mode:      mode,
		FromScale: m.Scale,
		ToScale:   target,
		Discarded: rem,
		Result:    res,
	}, nil
}

// Allocate splits an amount across weights so the parts sum exactly to the
// whole. Remainder minor units go to the largest weights first, then by index,
// which makes the split deterministic and replayable.
func Allocate(m Money, weights []int64) ([]Money, error) {
	if len(weights) == 0 {
		return nil, fmt.Errorf("money: allocate needs at least one weight")
	}
	var total int64
	for _, w := range weights {
		if w < 0 {
			return nil, fmt.Errorf("money: allocate weights must be non-negative")
		}
		total += w
	}
	if total == 0 {
		return nil, fmt.Errorf("money: allocate weights sum to zero")
	}
	parts := make([]Money, len(weights))
	var allocated int64
	for i, w := range weights {
		q, _, err := div128(mul128(m.Value, w), total)
		if err != nil {
			return nil, err
		}
		parts[i] = Money{Value: q, Scale: m.Scale, Currency: m.Currency}
		allocated += q
	}
	remainder := m.Value - allocated
	order := make([]int, len(weights))
	for i := range order {
		order[i] = i
	}
	// Stable descending sort by weight, ties broken by index.
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && weights[order[j]] > weights[order[j-1]]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	step := int64(1)
	if remainder < 0 {
		step, remainder = -1, -remainder
	}
	for i := int64(0); i < remainder; i++ {
		idx := order[int(i)%len(order)]
		parts[idx].Value += step
	}
	return parts, nil
}

// Cmp returns -1, 0 or 1. Incompatible operands are an error.
func Cmp(a, b Money) (int, error) {
	if err := a.compatible(b); err != nil {
		return 0, err
	}
	switch {
	case a.Value < b.Value:
		return -1, nil
	case a.Value > b.Value:
		return 1, nil
	default:
		return 0, nil
	}
}

func applyRounding(q, rem, divisor int64, mode RoundingMode) int64 {
	if rem == 0 {
		return q
	}
	negative := (rem < 0) != (divisor < 0)
	absRem, absDiv := abs64(rem), abs64(divisor)
	twice := absRem * 2
	var bump bool
	switch mode {
	case RoundDown, RoundTowardZero:
		bump = false
	case RoundUp:
		bump = true
	case RoundHalfDown:
		bump = twice > absDiv
	case RoundHalfEven:
		switch {
		case twice > absDiv:
			bump = true
		case twice < absDiv:
			bump = false
		default:
			bump = q%2 != 0
		}
	default: // RoundHalfUp
		bump = twice >= absDiv
	}
	if !bump {
		return q
	}
	if negative {
		return q - 1
	}
	return q + 1
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func pow10(n int32) int64 {
	if n < 0 || n > 18 {
		return 0
	}
	p := int64(1)
	for i := int32(0); i < n; i++ {
		p *= 10
	}
	return p
}

func willOverflowMul(a, b int64) bool {
	if a == 0 || b == 0 {
		return false
	}
	return abs64(a) > math.MaxInt64/abs64(b)
}

// signed128 is a sign-magnitude 128-bit intermediate. Products of two int64
// values do not fit in int64, so multiplication and the division that follows
// it are carried at full width and only narrowed once, after rounding.
type signed128 struct {
	neg    bool
	hi, lo uint64
}

func mul128(a, b int64) signed128 {
	hi, lo := bits.Mul64(uint64(abs64(a)), uint64(abs64(b)))
	return signed128{neg: (a < 0) != (b < 0), hi: hi, lo: lo}
}

// div128 divides a 128-bit product by a 64-bit divisor. Both quotient and
// remainder carry the mathematically correct sign (truncated toward zero).
func div128(p signed128, divisor int64) (int64, int64, error) {
	if divisor == 0 {
		return 0, 0, ErrDivideByZero
	}
	udiv := uint64(abs64(divisor))
	if p.hi >= udiv {
		return 0, 0, ErrOverflow
	}
	q, rem := bits.Div64(p.hi, p.lo, udiv)
	if q > uint64(math.MaxInt64) {
		return 0, 0, ErrOverflow
	}
	sq, sr := int64(q), int64(rem)
	if p.neg != (divisor < 0) {
		sq = -sq
	}
	if p.neg {
		sr = -sr
	}
	return sq, sr, nil
}
