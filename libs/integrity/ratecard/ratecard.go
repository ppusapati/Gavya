// Package ratecard turns a collection into money.
//
// This is the arithmetic a society's entire relationship with its producers
// rests on. A farmer brings twelve and a half litres at 4.1 fat and is paid some
// number of rupees, and the difference between two defensible answers to that is
// somebody's week.
//
// It lives in libs/integrity rather than in a service because two different
// paths have to reach the same paisa: the platform pricing a collection natively,
// and the platform recomputing what an incumbent system already paid. If those
// two use different code they will disagree, and every disagreement will be
// reported as a divergence in the incumbent — which is exactly the claim the
// product cannot afford to get wrong.
//
// # Two shapes, because both are real
//
// A village society prices from a chart: a printed grid on the wall with fat
// down one side and SNF across, and a rate per litre in each cell. You find the
// row, find the column, read the number. A plant prices from a formula: so much
// per kilogram of fat, so much per kilogram of solids-not-fat.
//
// Both are supported and a card declares which it is. Neither is the default,
// because a card that silently priced one way when the society prices the other
// would be wrong on every collection and plausible on all of them.
//
// # Nothing is decided by this package that a person did not decide first
//
// A chart has finite rows. A reading of 4.13 fat falls between 4.1 and 4.2, and
// what happens there is a policy: some societies round down to the band, some
// interpolate, some only accept readings that land on a point. All three occur.
// The card says which, and a card that does not say is refused rather than
// given one — that difference is a few paise on every collection, every day,
// for every producer.
//
// The same for a reading outside the chart entirely, and for how the final
// amount is rounded.
package ratecard

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
)

// Kind is how a card prices.
type Kind string

const (
	// Chart is a lookup grid: fat against SNF, a rate per unit in each cell.
	// What a village society has on the wall.
	Chart Kind = "CHART"
	// Formula is a rate per kilogram of each component. What a plant uses.
	Formula Kind = "FORMULA"
)

// Basis is what the chart rate is per.
//
// No default. Litres and kilograms of milk differ by about three per cent, which
// is larger than most of the divergences this platform exists to find — so a card
// that does not say is a card that cannot be used.
type Basis string

const (
	PerLitre Basis = "PER_LITRE"
	PerKg    Basis = "PER_KG"
)

// Between says what a chart does with a reading that falls between its points.
//
// All three of these are in use and none is more standard than the others. A
// card must say which its society uses.
type Between string

const (
	// Band: take the highest point at or below the reading. 4.13 fat is priced
	// at the 4.1 row. This is what most printed charts mean.
	Band Between = "BAND"
	// Interpolate: price proportionally between the two surrounding points.
	Interpolate Between = "INTERPOLATE"
	// ExactOnly: refuse a reading that is not on a point. Used where the
	// analyser is configured to report only at chart resolution, and where a
	// reading off the grid means the analyser is misconfigured.
	ExactOnly Between = "EXACT_ONLY"
)

// Outside says what happens to a reading beyond the chart's range.
type Outside string

const (
	// Refuse: a reading off the chart is not priced. The collection is held for
	// somebody to look at, because a fat of 12 is a failed analyser and paying
	// something for it buries the failure.
	Refuse Outside = "REFUSE"
	// Clamp: price at the nearest edge of the chart. Declared by societies whose
	// chart is deliberately narrower than their analyser's range.
	Clamp Outside = "CLAMP"
)

// Point is one axis value in a chart, held exactly.
//
// A fat of 4.1 is 41 at scale 1. Held as a scaled integer rather than a float
// because chart lookup is equality-sensitive at the boundaries, and 4.1 in
// binary floating point is not 4.1.
type Point struct {
	Value int64 `json:"value"`
	Scale int32 `json:"scale"`
}

func (p Point) String() string { return money.Rate{Numerator: p.Value, Scale: p.Scale}.String() }

// cmp orders two points, rescaling to whichever is finer.
func cmp(a, b Point) int {
	as, bs := a, b
	for as.Scale < bs.Scale {
		as.Value *= 10
		as.Scale++
	}
	for bs.Scale < as.Scale {
		bs.Value *= 10
		bs.Scale++
	}
	switch {
	case as.Value < bs.Value:
		return -1
	case as.Value > bs.Value:
		return 1
	}
	return 0
}

// Cell is one intersection of a chart.
type Cell struct {
	Fat  Point      `json:"fat"`
	SNF  Point      `json:"snf"`
	Rate money.Rate `json:"rate"`
}

// Term is one component of a formula.
type Term struct {
	// Component is FAT or SNF, named the way the rest of the platform names them.
	Component string     `json:"component"`
	Rate      money.Rate `json:"rate"`
}

// Card is a priced policy, in force over a period.
type Card struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`

	Kind     Kind   `json:"kind"`
	Currency string `json:"currency"`
	// Scale is the number of decimal places amounts are held at.
	Scale int32 `json:"amount_scale"`

	// Basis applies to a chart: what its rate is per.
	Basis Basis `json:"basis,omitempty"`

	Cells []Cell `json:"cells,omitempty"`
	Terms []Term `json:"terms,omitempty"`

	Between Between `json:"between_points,omitempty"`
	Outside Outside `json:"outside_chart,omitempty"`

	// Rounding is how the final amount is rounded to Scale. Declared, because
	// half-up and half-even differ on exactly the values that occur most often.
	Rounding money.RoundingMode `json:"rounding"`

	// ValidFrom and ValidTo are when this card prices milk. A settlement
	// recomputed a year later must use the card that was in force then, not the
	// one in force now.
	ValidFrom time.Time  `json:"valid_from"`
	ValidTo   *time.Time `json:"valid_to,omitempty"`
}

var (
	ErrNoKind     = errors.New("ratecard: the card does not say whether it is a chart or a formula")
	ErrNoBasis    = errors.New("ratecard: the card does not say whether its rate is per litre or per kilogram")
	ErrNoBetween  = errors.New("ratecard: the card does not say what to do with a reading between chart points")
	ErrNoOutside  = errors.New("ratecard: the card does not say what to do with a reading outside the chart")
	ErrEmptyChart = errors.New("ratecard: the chart has no cells")
	ErrNoTerms    = errors.New("ratecard: the formula has no terms")
)

// Validate refuses a card that would have to guess.
//
// Every one of these is a policy a society already has and the platform has no
// business inventing. A card missing any of them prices every collection wrongly
// and plausibly, which is the worst combination available.
func (c *Card) Validate() error {
	switch {
	case c.Currency == "":
		return errors.New("ratecard: the card has no currency")
	case c.Scale < 0:
		return errors.New("ratecard: the card has a negative scale")
	case c.Rounding == "":
		return errors.New("ratecard: the card does not say how the amount is rounded")
	}

	switch c.Kind {
	case Chart:
		if len(c.Cells) == 0 {
			return ErrEmptyChart
		}
		if c.Basis == "" {
			return ErrNoBasis
		}
		if c.Between == "" {
			return ErrNoBetween
		}
		if c.Outside == "" {
			return ErrNoOutside
		}
	case Formula:
		if len(c.Terms) == 0 {
			return ErrNoTerms
		}
		for _, t := range c.Terms {
			if t.Component == "" {
				return errors.New("ratecard: a formula term does not say which component it prices")
			}
		}
	case "":
		return ErrNoKind
	default:
		return fmt.Errorf("ratecard: %q is not a kind of rate card", c.Kind)
	}
	return nil
}

// InForce reports whether this card prices milk collected at this instant.
func (c *Card) InForce(at time.Time) bool {
	if at.Before(c.ValidFrom) {
		return false
	}
	return c.ValidTo == nil || at.Before(*c.ValidTo)
}

// Collection is what is being priced.
//
// Quantities and measurements are exact scaled integers, not floats, all the way
// through. The file said 12.500 and the producer is owed for 12.500.
type Collection struct {
	// Quantity in the card's basis. A collection recorded in litres against a
	// per-kilogram card is refused rather than converted: the conversion needs a
	// density nobody measured.
	Quantity Point
	Unit     Basis

	Fat Point
	SNF Point

	// FatKg and SNFKg are the component weights a formula prices. Only needed
	// for a formula card.
	FatKg Point
	SNFKg Point
}

// Priced is what a collection came to, and how.
//
// The workings are part of the answer. A producer disputing their payment is
// owed the rate that was applied and the card it came from, not just a total.
type Priced struct {
	Amount money.Money

	// Rate is the per-unit rate that was applied, for a chart. Zero for a
	// formula, where there is no single rate.
	Rate money.Rate

	CardID   string
	CardName string

	// Explanation says in words how this number was reached, for a statement a
	// producer reads and for a reviewer who has to defend it.
	Explanation string

	// Rounding is the step that took the exact product to the stored amount,
	// kept so a recomputation can show it discarded the same fraction.
	Rounding money.RoundingStep
}

// ErrOffChart is returned for a reading the card will not price.
type ErrOffChart struct {
	What  string
	Value Point
	Low   Point
	High  Point
}

func (e *ErrOffChart) Error() string {
	return fmt.Sprintf("ratecard: %s of %s is outside this chart, which runs %s to %s; the card "+
		"says to refuse rather than price it, because a reading off the chart is usually a failed "+
		"analyser and paying something for it buries the failure",
		e.What, e.Value, e.Low, e.High)
}

// ErrNotOnAPoint is returned when a card accepts only chart points and the
// reading is not one.
type ErrNotOnAPoint struct {
	What  string
	Value Point
}

func (e *ErrNotOnAPoint) Error() string {
	return fmt.Sprintf("ratecard: %s of %s is not a point on this chart, and the card accepts only "+
		"exact points — which usually means the analyser is reporting at a finer resolution than "+
		"the chart was written for", e.What, e.Value)
}

// Price works out what a collection is worth.
func Price(c *Card, col Collection) (*Priced, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	switch c.Kind {
	case Chart:
		return priceFromChart(c, col)
	case Formula:
		return priceFromFormula(c, col)
	}
	return nil, ErrNoKind
}

func priceFromChart(c *Card, col Collection) (*Priced, error) {
	if col.Unit != c.Basis {
		return nil, fmt.Errorf("ratecard: this collection is recorded in %s and the card prices "+
			"per %s; converting between them needs a density that nobody measured",
			unitWord(col.Unit), unitWord(c.Basis))
	}

	rate, how, err := lookup(c, col.Fat, col.SNF)
	if err != nil {
		return nil, err
	}

	qty := money.Money{Value: col.Quantity.Value, Scale: col.Quantity.Scale, Currency: c.Currency}
	amount, step, err := money.MulRate(qty, rate, c.Rounding)
	if err != nil {
		return nil, fmt.Errorf("ratecard: %w", err)
	}
	if amount.Scale != c.Scale {
		amount, step, err = money.Rescale(amount, c.Scale, c.Rounding)
		if err != nil {
			return nil, fmt.Errorf("ratecard: %w", err)
		}
	}

	return &Priced{
		Amount: amount, Rate: rate, CardID: c.ID, CardName: c.Name,
		Rounding: step,
		Explanation: fmt.Sprintf("%s at fat %s and SNF %s reads %s per %s on %s; %s × %s = %s",
			col.Quantity, col.Fat, col.SNF, rate, unitWord(c.Basis), c.Name,
			col.Quantity, rate, amount.String()) + how,
	}, nil
}

func priceFromFormula(c *Card, col Collection) (*Priced, error) {
	total := money.Zero(c.Scale, c.Currency)
	var parts []string
	var last money.RoundingStep

	terms := append([]Term(nil), c.Terms...)
	// Sorted so the explanation reads the same way every time, whatever order
	// the card happened to be written in. A producer comparing this month's
	// statement to last month's should not find the lines swapped.
	//
	// It does not change the total, and it is worth being precise about why:
	// each term is rounded to the card's scale before it is added, so the
	// addition is of exact amounts and is commutative. Rounding per term rather
	// than once at the end is deliberate — a statement shows a line per
	// component, and those lines have to add up to the total printed beneath
	// them.
	sort.Slice(terms, func(i, j int) bool { return terms[i].Component < terms[j].Component })

	for _, t := range terms {
		var weight Point
		switch strings.ToUpper(t.Component) {
		case "FAT":
			weight = col.FatKg
		case "SNF":
			weight = col.SNFKg
		case "QUANTITY":
			weight = col.Quantity
		default:
			return nil, fmt.Errorf("ratecard: this card prices a component called %q, which a "+
				"collection does not carry", t.Component)
		}
		if weight.Value == 0 {
			continue
		}

		w := money.Money{Value: weight.Value, Scale: weight.Scale, Currency: c.Currency}
		product, step, err := money.MulRate(w, t.Rate, c.Rounding)
		if err != nil {
			return nil, fmt.Errorf("ratecard: %s: %w", t.Component, err)
		}
		if product.Scale != c.Scale {
			product, step, err = money.Rescale(product, c.Scale, c.Rounding)
			if err != nil {
				return nil, fmt.Errorf("ratecard: %s: %w", t.Component, err)
			}
		}
		total, err = money.Add(total, product)
		if err != nil {
			return nil, fmt.Errorf("ratecard: %s: %w", t.Component, err)
		}
		last = step
		parts = append(parts, fmt.Sprintf("%s %s at %s = %s",
			weight, strings.ToLower(t.Component), t.Rate, product.String()))
	}

	if len(parts) == 0 {
		return nil, errors.New("ratecard: this collection carries none of the components the card prices")
	}
	return &Priced{
		Amount: total, CardID: c.ID, CardName: c.Name, Rounding: last,
		Explanation: strings.Join(parts, "; ") + "; total " + total.String(),
	}, nil
}

// lookup finds the rate for a fat and SNF reading.
func lookup(c *Card, fat, snf Point) (money.Rate, string, error) {
	fats, snfs := axes(c.Cells)

	fatAt, fatHow, err := place("fat", fat, fats, c)
	if err != nil {
		return money.Rate{}, "", err
	}
	snfAt, snfHow, err := place("SNF", snf, snfs, c)
	if err != nil {
		return money.Rate{}, "", err
	}

	if c.Between == Interpolate {
		return interpolate(c, fat, snf, fats, snfs)
	}

	for _, cell := range c.Cells {
		if cmp(cell.Fat, fatAt) == 0 && cmp(cell.SNF, snfAt) == 0 {
			return cell.Rate, joinHow(fatHow, snfHow), nil
		}
	}
	return money.Rate{}, "", fmt.Errorf("ratecard: the chart has rows for fat %s and columns for "+
		"SNF %s but no cell where they meet; the chart is incomplete", fatAt, snfAt)
}

// place decides which chart point a reading is priced at.
func place(what string, v Point, points []Point, c *Card) (Point, string, error) {
	if len(points) == 0 {
		return Point{}, "", ErrEmptyChart
	}
	low, high := points[0], points[len(points)-1]

	if cmp(v, low) < 0 || cmp(v, high) > 0 {
		if c.Outside == Clamp {
			at := low
			if cmp(v, high) > 0 {
				at = high
			}
			return at, fmt.Sprintf(" (%s %s is outside the chart and was priced at its edge, %s, "+
				"as this card declares)", what, v, at), nil
		}
		return Point{}, "", &ErrOffChart{What: what, Value: v, Low: low, High: high}
	}

	for _, p := range points {
		if cmp(v, p) == 0 {
			return p, "", nil
		}
	}

	switch c.Between {
	case ExactOnly:
		return Point{}, "", &ErrNotOnAPoint{What: what, Value: v}
	case Band, Interpolate:
		// The highest point at or below. Interpolation needs the same lower
		// bound, and computes from it.
		at := points[0]
		for _, p := range points {
			if cmp(p, v) <= 0 {
				at = p
			}
		}
		if c.Between == Band {
			return at, fmt.Sprintf(" (%s %s falls between points and was priced at %s, "+
				"the band below, as this card declares)", what, v, at), nil
		}
		return at, "", nil
	}
	return Point{}, "", ErrNoBetween
}

// interpolate prices proportionally between the four surrounding cells.
//
// Bilinear, and done in one exact rational step rather than as four separate
// roundings: rounding each corner first and then combining moves the answer by
// more than the interpolation itself is worth.
func interpolate(c *Card, fat, snf Point, fats, snfs []Point) (money.Rate, string, error) {
	f0, f1 := bracket(fat, fats)
	s0, s1 := bracket(snf, snfs)

	r00, err := cellAt(c, f0, s0)
	if err != nil {
		return money.Rate{}, "", err
	}
	r01, err := cellAt(c, f0, s1)
	if err != nil {
		return money.Rate{}, "", err
	}
	r10, err := cellAt(c, f1, s0)
	if err != nil {
		return money.Rate{}, "", err
	}
	r11, err := cellAt(c, f1, s1)
	if err != nil {
		return money.Rate{}, "", err
	}

	// Weights as exact integer fractions of the gap between the bracketing
	// points, so nothing here is a float.
	fw, fd := gap(fat, f0, f1)
	sw, sd := gap(snf, s0, s1)

	scale := maxScale(r00.Scale, r01.Scale, r10.Scale, r11.Scale)
	n00, n01 := rescaleRate(r00, scale), rescaleRate(r01, scale)
	n10, n11 := rescaleRate(r10, scale), rescaleRate(r11, scale)

	// (1-fw/fd)(1-sw/sd)n00 + (1-fw/fd)(sw/sd)n01 + (fw/fd)(1-sw/sd)n10 + (fw/fd)(sw/sd)n11
	// multiplied through by fd*sd to stay in integers.
	num := (fd-fw)*(sd-sw)*n00 +
		(fd-fw)*sw*n01 +
		fw*(sd-sw)*n10 +
		fw*sw*n11
	den := fd * sd
	if den == 0 {
		return money.Rate{Numerator: n00, Scale: scale}, "", nil
	}

	// Half-up on the exact rational, so the interpolated rate is deterministic.
	q := num / den
	if rem := num % den; rem*2 >= den {
		q++
	} else if rem*2 <= -den {
		q--
	}
	rate := money.Rate{Numerator: q, Scale: scale}
	return rate, fmt.Sprintf(" (fat %s and SNF %s fall between points; the rate %s was interpolated "+
		"between %s, %s, %s and %s, as this card declares)",
		fat, snf, rate, r00, r01, r10, r11), nil
}

func bracket(v Point, points []Point) (Point, Point) {
	lo, hi := points[0], points[len(points)-1]
	for _, p := range points {
		if cmp(p, v) <= 0 {
			lo = p
		}
	}
	for i := len(points) - 1; i >= 0; i-- {
		if cmp(points[i], v) >= 0 {
			hi = points[i]
		}
	}
	return lo, hi
}

// gap returns how far v is between lo and hi, as an integer fraction.
func gap(v, lo, hi Point) (int64, int64) {
	scale := lo.Scale
	if hi.Scale > scale {
		scale = hi.Scale
	}
	if v.Scale > scale {
		scale = v.Scale
	}
	at := func(p Point) int64 {
		for p.Scale < scale {
			p.Value *= 10
			p.Scale++
		}
		return p.Value
	}
	d := at(hi) - at(lo)
	if d == 0 {
		return 0, 1
	}
	return at(v) - at(lo), d
}

func cellAt(c *Card, fat, snf Point) (money.Rate, error) {
	for _, cell := range c.Cells {
		if cmp(cell.Fat, fat) == 0 && cmp(cell.SNF, snf) == 0 {
			return cell.Rate, nil
		}
	}
	return money.Rate{}, fmt.Errorf("ratecard: the chart has no cell at fat %s, SNF %s", fat, snf)
}

func axes(cells []Cell) (fats, snfs []Point) {
	seenF, seenS := map[string]bool{}, map[string]bool{}
	for _, c := range cells {
		if k := c.Fat.String(); !seenF[k] {
			seenF[k] = true
			fats = append(fats, c.Fat)
		}
		if k := c.SNF.String(); !seenS[k] {
			seenS[k] = true
			snfs = append(snfs, c.SNF)
		}
	}
	sort.Slice(fats, func(i, j int) bool { return cmp(fats[i], fats[j]) < 0 })
	sort.Slice(snfs, func(i, j int) bool { return cmp(snfs[i], snfs[j]) < 0 })
	return fats, snfs
}

func rescaleRate(r money.Rate, scale int32) int64 {
	v := r.Numerator
	for r.Scale < scale {
		v *= 10
		r.Scale++
	}
	return v
}

func maxScale(s ...int32) int32 {
	m := s[0]
	for _, v := range s[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

func joinHow(a, b string) string { return a + b }

func unitWord(b Basis) string {
	if b == PerKg {
		return "kilogram"
	}
	return "litre"
}
