package ratecard

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
)

func pt(v int64, scale int32) Point { return Point{Value: v, Scale: scale} }

func rate(v int64, scale int32) money.Rate { return money.Rate{Numerator: v, Scale: scale} }

// A small chart in the shape a village society has on the wall: fat down the
// side at 4.0, 4.1 and 4.2; SNF across at 8.5 and 8.6; a rate per litre in each
// cell. The numbers are round so the arithmetic can be checked by hand.
//
//	         SNF 8.5   SNF 8.6
//	fat 4.0    40.00     41.00
//	fat 4.1    42.00     43.00
//	fat 4.2    44.00     45.00
func chartCard(mods ...func(*Card)) *Card {
	c := &Card{
		ID: "RC_1", TenantID: "T_A", Name: "Society chart, February",
		Kind: Chart, Currency: "INR", Scale: 2,
		Basis: PerLitre, Between: Band, Outside: Refuse,
		Rounding:  money.RoundHalfUp,
		ValidFrom: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		Cells: []Cell{
			{pt(40, 1), pt(85, 1), rate(400000, 4)},
			{pt(40, 1), pt(86, 1), rate(410000, 4)},
			{pt(41, 1), pt(85, 1), rate(420000, 4)},
			{pt(41, 1), pt(86, 1), rate(430000, 4)},
			{pt(42, 1), pt(85, 1), rate(440000, 4)},
			{pt(42, 1), pt(86, 1), rate(450000, 4)},
		},
	}
	for _, m := range mods {
		m(c)
	}
	return c
}

func litres(q int64, fat, snf Point) Collection {
	return Collection{Quantity: pt(q, 3), Unit: PerLitre, Fat: fat, SNF: snf}
}

// 12.500 litres at fat 4.1 and SNF 8.6 reads 43.0000 a litre, so 537.50.
func TestAReadingOnAChartPointIsPricedAtThatCell(t *testing.T) {
	got, err := Price(chartCard(), litres(12500, pt(41, 1), pt(86, 1)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Amount.String() != "537.50" {
		t.Errorf("amount = %s, want 537.50", got.Amount.String())
	}
	if got.Rate.String() != "43.0000" {
		t.Errorf("rate = %s, want 43.0000", got.Rate.String())
	}
	if got.CardID != "RC_1" {
		t.Errorf("the price does not cite the card it came from")
	}
	if got.Explanation == "" {
		t.Error("no explanation; a producer disputing this is owed the workings, not a total")
	}
}

// The policy question every chart has and no chart states in its file: a reading
// between two rows. Three answers are in use and the card says which.
func TestAReadingBetweenPointsFollowsWhatTheCardDeclares(t *testing.T) {
	// 4.13 fat lies between the 4.1 and 4.2 rows.
	col := litres(10000, pt(413, 2), pt(86, 1))

	t.Run("band takes the row below", func(t *testing.T) {
		got, err := Price(chartCard(), col)
		if err != nil {
			t.Fatal(err)
		}
		// The 4.1 row at SNF 8.6 is 43.0000, so ten litres is 430.00.
		if got.Amount.String() != "430.00" {
			t.Errorf("amount = %s, want 430.00 — the band below", got.Amount.String())
		}
		if !strings.Contains(got.Explanation, "band below") {
			t.Errorf("the explanation does not say the reading was banded: %s", got.Explanation)
		}
	})

	t.Run("exact only refuses", func(t *testing.T) {
		card := chartCard(func(c *Card) { c.Between = ExactOnly })
		_, err := Price(card, col)
		var e *ErrNotOnAPoint
		if !errors.As(err, &e) {
			t.Fatalf("err = %v, want a refusal", err)
		}
	})

	t.Run("interpolate prices between", func(t *testing.T) {
		card := chartCard(func(c *Card) { c.Between = Interpolate })
		got, err := Price(card, col)
		if err != nil {
			t.Fatal(err)
		}
		// 4.13 is three tenths of the way from 4.1 to 4.2. At SNF 8.6 the rates
		// are 43.0000 and 45.0000, so the rate is 43.6000 and ten litres is
		// 436.00.
		if got.Rate.String() != "43.6000" {
			t.Errorf("rate = %s, want 43.6000", got.Rate.String())
		}
		if got.Amount.String() != "436.00" {
			t.Errorf("amount = %s, want 436.00", got.Amount.String())
		}
	})
}

// Interpolating on both axes at once. 4.15 fat and 8.55 SNF is the middle of
// the four cells 43.0000, 45.0000, 42.0000 and 44.0000 — wait, of 42/43/44/45 —
// whose average is 43.5000.
func TestInterpolationWorksOnBothAxesAtOnce(t *testing.T) {
	card := chartCard(func(c *Card) { c.Between = Interpolate })
	got, err := Price(card, litres(10000, pt(415, 2), pt(855, 2)))
	if err != nil {
		t.Fatal(err)
	}
	// Halfway between fat 4.1 and 4.2, halfway between SNF 8.5 and 8.6:
	// (42.00 + 43.00 + 44.00 + 45.00) / 4 = 43.50.
	if got.Rate.String() != "43.5000" {
		t.Errorf("rate = %s, want 43.5000 — the mean of the four surrounding cells", got.Rate.String())
	}
	if got.Amount.String() != "435.00" {
		t.Errorf("amount = %s, want 435.00", got.Amount.String())
	}
}

// A fat of 12 is a failed analyser. Pricing it at the chart's edge buries the
// failure in a payment that looks ordinary.
func TestAReadingOffTheChartIsRefusedUnlessTheCardSaysOtherwise(t *testing.T) {
	col := litres(10000, pt(120, 1), pt(86, 1))

	_, err := Price(chartCard(), col)
	var e *ErrOffChart
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want a refusal", err)
	}
	if !strings.Contains(e.Error(), "failed analyser") {
		t.Errorf("the refusal does not say why this matters: %v", e)
	}

	// And a society whose chart is deliberately narrower can declare otherwise.
	clamped, err := Price(chartCard(func(c *Card) { c.Outside = Clamp }), col)
	if err != nil {
		t.Fatal(err)
	}
	if clamped.Rate.String() != "45.0000" {
		t.Errorf("clamped rate = %s, want the chart's top row", clamped.Rate.String())
	}
	if !strings.Contains(clamped.Explanation, "outside the chart") {
		t.Error("a clamped price does not say it was clamped")
	}
}

// A card that has not said which of these it is prices every collection wrongly
// and plausibly. Refusing costs one deployment conversation; not refusing costs
// a season of payments.
func TestACardThatHasNotDecidedIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		mod  func(*Card)
		want error
	}{
		{"no kind", func(c *Card) { c.Kind = "" }, ErrNoKind},
		{"no basis", func(c *Card) { c.Basis = "" }, ErrNoBasis},
		{"nothing said about readings between points", func(c *Card) { c.Between = "" }, ErrNoBetween},
		{"nothing said about readings off the chart", func(c *Card) { c.Outside = "" }, ErrNoOutside},
		{"no cells", func(c *Card) { c.Cells = nil }, ErrEmptyChart},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Price(chartCard(tc.mod), litres(10000, pt(41, 1), pt(86, 1)))
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// Litres and kilograms of milk differ by about three per cent — larger than most
// of the divergences this platform exists to find. Converting between them needs
// a density nobody measured, so a mismatch is refused rather than assumed away.
func TestACollectionInTheWrongUnitIsRefusedRatherThanConverted(t *testing.T) {
	col := litres(12500, pt(41, 1), pt(86, 1))
	col.Unit = PerKg

	_, err := Price(chartCard(), col)
	if err == nil {
		t.Fatal("a kilogram collection was priced against a per-litre chart")
	}
	if !strings.Contains(err.Error(), "density") {
		t.Errorf("the refusal does not say why it cannot convert: %v", err)
	}
}

// -------------------------------------------------------------------------
// Formula cards
// -------------------------------------------------------------------------

func formulaCard() *Card {
	return &Card{
		ID: "RC_2", TenantID: "T_A", Name: "Plant formula, February",
		Kind: Formula, Currency: "INR", Scale: 2,
		Rounding:  money.RoundHalfUp,
		ValidFrom: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		Terms: []Term{
			{Component: "FAT", Rate: rate(4000000, 4)}, // 400.0000 per kg of fat
			{Component: "SNF", Rate: rate(2000000, 4)}, // 200.0000 per kg of SNF
		},
	}
}

// 0.500 kg of fat at 400 and 1.100 kg of SNF at 200 is 200.00 + 220.00 = 420.00.
func TestAFormulaPricesEachComponent(t *testing.T) {
	got, err := Price(formulaCard(), Collection{
		Quantity: pt(12500, 3), Unit: PerKg,
		FatKg: pt(500, 3), SNFKg: pt(1100, 3),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Amount.String() != "420.00" {
		t.Errorf("amount = %s, want 420.00", got.Amount.String())
	}
	if !strings.Contains(got.Explanation, "fat") || !strings.Contains(got.Explanation, "snf") {
		t.Errorf("the explanation does not show both components: %s", got.Explanation)
	}
}

// The order the terms happen to be written in must change neither the total nor
// the statement.
//
// The total is order-independent for a reason worth stating: each term is
// rounded to the card's scale before being added, so the addition is of exact
// amounts. The statement is order-independent only because the terms are sorted
// — and a producer comparing this month to last should not find the lines
// swapped because somebody re-entered the card.
func TestTheOrderTermsAreWrittenInChangesNeitherTheTotalNorTheStatement(t *testing.T) {
	col := Collection{Quantity: pt(12500, 3), Unit: PerKg, FatKg: pt(437, 3), SNFKg: pt(1093, 3)}

	forward := formulaCard()
	backward := formulaCard()
	backward.Terms[0], backward.Terms[1] = backward.Terms[1], backward.Terms[0]

	a, err := Price(forward, col)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Price(backward, col)
	if err != nil {
		t.Fatal(err)
	}
	if a.Amount.String() != b.Amount.String() {
		t.Errorf("the same card written in a different order gave %s and %s",
			a.Amount.String(), b.Amount.String())
	}
	if a.Explanation != b.Explanation {
		t.Errorf("the same card written in a different order explains itself differently:\n  %s\n  %s",
			a.Explanation, b.Explanation)
	}
}

func TestAFormulaPricingAComponentTheCollectionDoesNotCarryIsRefused(t *testing.T) {
	card := formulaCard()
	card.Terms = []Term{{Component: "PROTEIN", Rate: rate(1000000, 4)}}

	_, err := Price(card, Collection{Quantity: pt(12500, 3), Unit: PerKg, FatKg: pt(500, 3)})
	if err == nil {
		t.Fatal("a card pricing protein was applied to a collection with none")
	}
	if !strings.Contains(err.Error(), "PROTEIN") {
		t.Errorf("the refusal does not name the component: %v", err)
	}
}

// -------------------------------------------------------------------------
// Reproducibility and time
// -------------------------------------------------------------------------

// A settlement recomputed a year later must land on the paisa it landed on then.
func TestPricingTheSameCollectionTwiceGivesTheSameAnswer(t *testing.T) {
	card := chartCard(func(c *Card) { c.Between = Interpolate })
	col := litres(11375, pt(413, 2), pt(857, 2))

	first, err := Price(card, col)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		again, err := Price(card, col)
		if err != nil {
			t.Fatal(err)
		}
		if again.Amount.String() != first.Amount.String() {
			t.Fatalf("run %d gave %s, the first gave %s", i, again.Amount.String(), first.Amount.String())
		}
		if again.Rate.String() != first.Rate.String() {
			t.Fatalf("run %d used rate %s, the first used %s", i, again.Rate, first.Rate)
		}
	}
}

// A card prices milk collected while it was in force, and not milk collected
// before or after. A settlement recomputed later has to reach for the card that
// was on the wall then.
func TestACardOnlyPricesMilkCollectedWhileItWasInForce(t *testing.T) {
	to := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	card := chartCard(func(c *Card) { c.ValidTo = &to })

	for _, tc := range []struct {
		when time.Time
		want bool
	}{
		{time.Date(2026, 1, 31, 23, 59, 59, 0, time.UTC), false},
		{time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), true},
		{time.Date(2026, 2, 15, 6, 30, 0, 0, time.UTC), true},
		{time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), false},
	} {
		if got := card.InForce(tc.when); got != tc.want {
			t.Errorf("on %s in force = %v, want %v", tc.when.Format(time.RFC3339), got, tc.want)
		}
	}

	// An open-ended card has no end.
	open := chartCard()
	if !open.InForce(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Error("a card with no end date stopped being in force")
	}
}

// The rounding step is kept so a recomputation can show it discarded the same
// fraction rather than merely reaching the same total by luck.
func TestTheRoundingIsRecordedAndNotJustPerformed(t *testing.T) {
	// 7.333 litres at 43.0000 is 315.319, which does not land on a paisa.
	got, err := Price(chartCard(), litres(7333, pt(41, 1), pt(86, 1)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Amount.String() != "315.32" {
		t.Errorf("amount = %s, want 315.32", got.Amount.String())
	}
	if got.Rounding.Mode == "" {
		t.Error("the rounding step does not say which mode was used")
	}
}

// Half-up and half-even differ on exactly the values that occur most often, so
// which one a card uses is a declaration rather than a detail.
func TestTheDeclaredRoundingModeIsTheOneUsed(t *testing.T) {
	half := chartCard()
	halfEven := chartCard(func(c *Card) { c.Rounding = money.RoundHalfEven })

	// 7.015 litres at 43.0000 is 301.6450 — exactly half a paisa, with an even
	// digit before it, which is the only case where the two modes disagree.
	//
	// An earlier version of this used 12.345, which lands on a half with an odd
	// digit before it: both modes round up, the test found no difference, and it
	// skipped. A skipping test proves nothing and reads as a pass.
	col := litres(7015, pt(41, 1), pt(86, 1))

	up, err := Price(half, col)
	if err != nil {
		t.Fatal(err)
	}
	even, err := Price(halfEven, col)
	if err != nil {
		t.Fatal(err)
	}
	if up.Amount.String() != "301.65" {
		t.Errorf("half-up gave %s, want 301.65", up.Amount.String())
	}
	if even.Amount.String() != "301.64" {
		t.Errorf("half-even gave %s, want 301.64", even.Amount.String())
	}
	if up.Amount.String() == even.Amount.String() {
		t.Error("the two modes agree, so the card's declaration is not being used")
	}
}

// A chart with a gap in it is a chart somebody typed wrongly, and a collection
// that lands in the gap must not be priced at whatever happened to be nearby.
func TestAChartWithAMissingCellIsRefusedRatherThanFilledIn(t *testing.T) {
	card := chartCard()
	// Remove the cell at fat 4.1, SNF 8.6.
	var kept []Cell
	for _, c := range card.Cells {
		if cmp(c.Fat, pt(41, 1)) == 0 && cmp(c.SNF, pt(86, 1)) == 0 {
			continue
		}
		kept = append(kept, c)
	}
	card.Cells = kept

	_, err := Price(card, litres(10000, pt(41, 1), pt(86, 1)))
	if err == nil {
		t.Fatal("a collection landing on a missing cell was priced anyway")
	}
	if !strings.Contains(err.Error(), "incomplete") && !strings.Contains(err.Error(), "no cell") {
		t.Errorf("the refusal does not say the chart is incomplete: %v", err)
	}
}

// Nothing on this path may be a float. A chart is equality-sensitive at its
// boundaries and 4.1 in binary floating point is not 4.1.
func TestChartPointsCompareExactly(t *testing.T) {
	// 4.10 written at two decimals must be the same point as 4.1 at one.
	if cmp(pt(410, 2), pt(41, 1)) != 0 {
		t.Error("4.10 and 4.1 are not the same chart point")
	}
	if cmp(pt(41, 1), pt(42, 1)) >= 0 {
		t.Error("4.1 does not order below 4.2")
	}
	// And a reading written at finer resolution lands on the point it should.
	got, err := Price(chartCard(), litres(10000, pt(4100, 3), pt(8600, 3)))
	if err != nil {
		t.Fatal(err)
	}
	if got.Rate.String() != "43.0000" {
		t.Errorf("4.100 priced at %s, want the 4.1 row's 43.0000", got.Rate)
	}
}
