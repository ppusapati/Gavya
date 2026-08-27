package domain

import (
	"errors"
	"testing"
	"time"
)

func ppmOf(v int64) *int64 { return &v }

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func recipe() *Formulation {
	return &Formulation{
		TenantID: "t", Code: "PANEER-STD", Name: "Standard paneer",
		OutputProductRef: "PANEER", OutputUnit: "KILOGRAMS",
		ValidFrom: at("2026-01-01T00:00:00Z"), CreatedBy: "u",
	}
}

// ---------------------------------------------------------------------------
// What a recipe may say about itself
// ---------------------------------------------------------------------------

// A declared target has to say where it came from.
//
// This is the whole reason the field exists. A plant that derives 18% from two
// hundred of its own vats and a plant that reads 18% off a supplier's leaflet
// are making different claims, and a variance report that shows them
// identically is one where the same argument happens every month.
func TestADeclaredYieldMustSayWhereItCameFrom(t *testing.T) {
	f := recipe()
	f.ExpectedYieldPPM = ppmOf(180000)
	if err := f.Validate(); !errors.Is(err, ErrExpectationNeedsBasis) {
		t.Errorf("a target with no provenance was accepted: %v", err)
	}
	f.ExpectationBasis = "median of 214 vats, Jan-Jun 2026"
	if err := f.Validate(); err != nil {
		t.Errorf("a target with a basis was refused: %v", err)
	}
}

// No target at all is fine, and is the ordinary case. A plant that has run a
// process four times has no business declaring one.
func TestARecipeWithNoTargetIsOrdinary(t *testing.T) {
	if err := recipe().Validate(); err != nil {
		t.Errorf("a recipe with no declared yield was refused: %v", err)
	}
}

// A basis with no figure beside it explains nothing.
func TestABasisWithoutAFigureIsRefused(t *testing.T) {
	f := recipe()
	f.ExpectationBasis = "we have always done it this way"
	if err := f.Validate(); err == nil {
		t.Error("a provenance with no number attached was accepted")
	}
}

func TestARecipeMustSayWhenItCameIntoForce(t *testing.T) {
	f := recipe()
	f.ValidFrom = time.Time{}
	if err := f.Validate(); !errors.Is(err, ErrNoValidFrom) {
		t.Errorf("a recipe with no period was accepted (%v); it would silently apply to every "+
			"batch ever made, including the ones made before it existed", err)
	}
}

func TestARecipePeriodRunsForwards(t *testing.T) {
	f := recipe()
	end := at("2025-06-01T00:00:00Z")
	f.ValidTo = &end
	if err := f.Validate(); !errors.Is(err, ErrBackwardsPeriod) {
		t.Errorf("a recipe that ended before it started was accepted: %v", err)
	}
}

// The period is half-open. A recipe ending on the first of April and its
// replacement starting there must not both apply that morning — the alternative
// is a day on which two targets are in force and the variance depends on which
// row was read first.
func TestARecipesPeriodIsHalfOpen(t *testing.T) {
	f := recipe()
	end := at("2026-04-01T00:00:00Z")
	f.ValidTo = &end

	for _, c := range []struct {
		when string
		want bool
	}{
		{"2025-12-31T23:59:59Z", false},
		{"2026-01-01T00:00:00Z", true}, // inclusive start
		{"2026-03-31T23:59:59Z", true},
		{"2026-04-01T00:00:00Z", false}, // exclusive end
		{"2026-04-01T00:00:01Z", false},
	} {
		if got := f.InForceAt(at(c.when)); got != c.want {
			t.Errorf("InForceAt(%s) = %v, want %v", c.when, got, c.want)
		}
	}

	open := recipe()
	open.ValidFrom = at("2026-04-01T00:00:00Z")
	if !open.InForceAt(at("2026-04-01T00:00:00Z")) {
		t.Error("the replacement is not in force on the day it starts, so the boundary belongs " +
			"to neither version and a batch made that morning has no recipe")
	}
	if !open.InForceAt(at("2030-01-01T00:00:00Z")) {
		t.Error("a recipe with no end date stopped applying")
	}
}

func TestAnIngredientIsNotItsOwnProduct(t *testing.T) {
	f := recipe()
	in := &FormulationInput{TenantID: "t", FormulationID: "f", ProductRef: "PANEER",
		Required: true, CreatedBy: "u"}
	if err := in.Validate(f); !errors.Is(err, ErrIngredientIsTheProduct) {
		t.Errorf("a recipe made from its own product was accepted: %v", err)
	}
	in.ProductRef = "RAW_MILK"
	if err := in.Validate(f); err != nil {
		t.Errorf("an ordinary ingredient was refused: %v", err)
	}
}

func TestAShareIsBetweenNothingAndEverything(t *testing.T) {
	f := recipe()
	base := func() *FormulationInput {
		return &FormulationInput{TenantID: "t", FormulationID: "f", ProductRef: "RAW_MILK",
			Required: true, CreatedBy: "u"}
	}
	for _, bad := range []int64{0, -1, 1000001} {
		in := base()
		in.ExpectedSharePPM = ppmOf(bad)
		if err := in.Validate(f); err == nil {
			t.Errorf("a share of %d ppm was accepted", bad)
		}
	}
	for _, ok := range []int64{1, 500000, 1000000} {
		in := base()
		in.ExpectedSharePPM = ppmOf(ok)
		if err := in.Validate(f); err != nil {
			t.Errorf("a share of %d ppm was refused: %v", ok, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Was the recipe followed
// ---------------------------------------------------------------------------

func ingredients(specs ...FormulationInput) []FormulationInput { return specs }

func req(product string, share *int64) FormulationInput {
	return FormulationInput{ProductRef: product, ExpectedSharePPM: share, Required: true}
}

func opt(product string, share *int64) FormulationInput {
	return FormulationInput{ProductRef: product, ExpectedSharePPM: share, Required: false}
}

// within is an ingredient whose plant has said how close is close enough.
func within(product string, share, tolerance int64) FormulationInput {
	return FormulationInput{ProductRef: product, ExpectedSharePPM: &share,
		ShareTolerancePPM: &tolerance, Required: true}
}

func findingFor(c *RecipeCheck, kind FindingKind, product string) *Finding {
	for i := range c.Findings {
		if c.Findings[i].Kind == kind && c.Findings[i].ProductRef == product {
			return &c.Findings[i]
		}
	}
	return nil
}

func shareFor(c *RecipeCheck, product string) *ShareReading {
	for i := range c.Shares {
		if c.Shares[i].ProductRef == product {
			return &c.Shares[i]
		}
	}
	return nil
}

// Paneer with no milk in it is the finding this exists for.
func TestAMissingRequiredIngredientIsSerious(t *testing.T) {
	got, err := CheckRecipe(recipe(),
		ingredients(req("RAW_MILK", nil), req("RENNET", nil)),
		[]Lot{NewLot("b1", "RENNET-1", "RENNET", 5000, "KILOGRAMS")})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	f := findingFor(got, MissingRequired, "RAW_MILK")
	if f == nil {
		t.Fatalf("paneer was made with no milk in it and nothing said so: %+v", got.Findings)
	}
	if !f.Serious() {
		t.Error("a required ingredient missing entirely is not marked serious")
	}
	if f.Explanation == "" {
		t.Error("the finding carries no explanation")
	}
}

// A missing optional one is a note, not a fault. Reporting the two the same way
// is how a report full of nothing gets skimmed past the one line that matters.
func TestAMissingOptionalIngredientIsNotSerious(t *testing.T) {
	got, err := CheckRecipe(recipe(),
		ingredients(req("RAW_MILK", nil), opt("CULTURE", nil)),
		[]Lot{NewLot("b1", "MILK-1", "RAW_MILK", 6000, "KILOGRAMS")})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	f := findingFor(got, MissingOptional, "CULTURE")
	if f == nil {
		t.Fatalf("an unused optional ingredient was not mentioned: %+v", got.Findings)
	}
	if f.Serious() {
		t.Error("an optional ingredient nobody used is marked serious; a report where " +
			"everything is serious is one where nothing is")
	}
	if findingFor(got, MissingRequired, "CULTURE") != nil {
		t.Error("an optional ingredient was reported as a required one")
	}
}

// A substitution is reported and not refused. The usual coagulant is out of
// stock and something else goes in; refusing that would mean the vat gets
// recorded wrongly or not at all, and a genealogy with a gap in it is worse
// than one with a note attached.
func TestASubstitutionIsANoteAndNotAFault(t *testing.T) {
	got, err := CheckRecipe(recipe(),
		ingredients(req("RAW_MILK", nil)),
		[]Lot{
			NewLot("b1", "MILK-1", "RAW_MILK", 6000, "KILOGRAMS"),
			NewLot("b2", "ACID-1", "CITRIC_ACID", 50, "KILOGRAMS"),
		})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	f := findingFor(got, Unexpected, "CITRIC_ACID")
	if f == nil {
		t.Fatalf("something went in that the recipe does not name and nothing said so: %+v", got.Findings)
	}
	if f.Serious() {
		t.Error("a substitution is marked serious")
	}
	if findingFor(got, MissingRequired, "RAW_MILK") != nil {
		t.Error("the milk was there and was reported missing")
	}
}

// A recipe followed exactly produces no findings. Without this, every assertion
// above would pass against a CheckRecipe that reports everything.
func TestARecipeFollowedExactlyProducesNothing(t *testing.T) {
	got, err := CheckRecipe(recipe(),
		ingredients(within("RAW_MILK", 990099, 1000), within("RENNET", 9901, 1000)),
		[]Lot{
			NewLot("b1", "MILK-1", "RAW_MILK", 100000, "KILOGRAMS"),
			NewLot("b2", "RENNET-1", "RENNET", 1000, "KILOGRAMS"),
		})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(got.Findings) != 0 {
		t.Errorf("a batch that followed the recipe reported %+v", got.Findings)
	}
}

// The declared proportion and the observed one are always put side by side.
//
// Reported whether or not anything is wrong with them. Putting the two numbers
// next to each other is something the platform can do for any plant; saying
// whether the gap between them matters is not.
func TestTheDeclaredAndObservedSharesAreAlwaysReportedTogether(t *testing.T) {
	// 90000 of 100000 is 900000 ppm; the recipe says 990099.
	got, err := CheckRecipe(recipe(),
		ingredients(req("RAW_MILK", ppmOf(990099)), req("RENNET", ppmOf(9901))),
		[]Lot{
			NewLot("b1", "MILK-1", "RAW_MILK", 90000, "KILOGRAMS"),
			NewLot("b2", "RENNET-1", "RENNET", 10000, "KILOGRAMS"),
		})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	sh := shareFor(got, "RAW_MILK")
	if sh == nil {
		t.Fatalf("the recipe declares a share for the milk and none was reported: %+v", got.Shares)
	}
	if sh.ObservedSharePPM != 900000 {
		t.Errorf("observed share %d ppm, want 900000", sh.ObservedSharePPM)
	}
	if sh.ExpectedSharePPM != 990099 {
		t.Errorf("expected share %d ppm, want the recipe's 990099", sh.ExpectedSharePPM)
	}
	if sh.DifferencePPM != -90099 {
		t.Errorf("difference %d ppm, want -90099; the sign says which way the vat went and "+
			"reversing it turns a shortfall into a surplus", sh.DifferencePPM)
	}
}

// No declared tolerance, no finding — however far off the vat was.
//
// This is the same refusal as everywhere else here: how close is close enough
// is a question about a plant's process and its scales, and inventing an answer
// would put the platform's opinion into a report with the plant's name on it.
// The numbers are still there for whoever wants to look.
func TestNoDeclaredToleranceMeansNoShareFinding(t *testing.T) {
	got, err := CheckRecipe(recipe(),
		ingredients(req("RAW_MILK", ppmOf(990099)), req("RENNET", ppmOf(9901))),
		[]Lot{
			NewLot("b1", "MILK-1", "RAW_MILK", 50000, "KILOGRAMS"),
			NewLot("b2", "RENNET-1", "RENNET", 50000, "KILOGRAMS"),
		})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	for _, f := range got.Findings {
		if f.Kind == ShareDiffers {
			t.Errorf("a vat half milk and half rennet was judged against a tolerance nobody "+
				"declared: %+v", f)
		}
	}
	if shareFor(got, "RAW_MILK") == nil {
		t.Error("the figures were withheld along with the judgement; a reader can decide for " +
			"themselves if they are shown the numbers")
	}
}

// With a tolerance declared, a vat outside it is a finding and one inside it is
// not. Both halves matter: a check that fires on everything is a check nobody
// reads, and one that fires on nothing is not a check.
func TestAToleranceDecidesWhetherAShareIsAFinding(t *testing.T) {
	// The recipe: milk at 90% give or take 1%.
	within1pc := ingredients(within("RAW_MILK", 900000, 10000), opt("RENNET", nil))

	// A vat at 89.5%: inside the tolerance.
	inside, err := CheckRecipe(recipe(), within1pc, []Lot{
		NewLot("b1", "MILK-1", "RAW_MILK", 89500, "KILOGRAMS"),
		NewLot("b2", "RENNET-1", "RENNET", 10500, "KILOGRAMS"),
	})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if f := findingFor(inside, ShareDiffers, "RAW_MILK"); f != nil {
		t.Errorf("a vat within the plant's own stated tolerance was reported: %s", f.Explanation)
	}
	if sh := shareFor(inside, "RAW_MILK"); sh == nil || sh.TolerancePPM == nil {
		t.Errorf("the reading does not carry the tolerance it was judged against: %+v", sh)
	}

	// A vat at 85%: outside it.
	outside, err := CheckRecipe(recipe(), within1pc, []Lot{
		NewLot("b1", "MILK-1", "RAW_MILK", 85000, "KILOGRAMS"),
		NewLot("b2", "RENNET-1", "RENNET", 15000, "KILOGRAMS"),
	})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	f := findingFor(outside, ShareDiffers, "RAW_MILK")
	if f == nil {
		t.Fatalf("a vat five per cent outside a one per cent tolerance was not reported: %+v",
			outside.Findings)
	}
	if f.ObservedSharePPM == nil || *f.ObservedSharePPM != 850000 {
		t.Errorf("the finding reports %v, want 850000 ppm", f.ObservedSharePPM)
	}
	if f.Serious() {
		t.Error("a proportion outside tolerance is marked as serious as paneer with no milk " +
			"in it; a report where everything is serious is one where nothing is")
	}
}

// Integer division moves the observed share by up to a part per million on its
// own. A vat that is exactly right must not be reported because of it.
func TestTheArithmeticsOwnRoundingIsNotAFinding(t *testing.T) {
	// 1000 of 101000 is 9900.99 ppm, which truncates to 9900. The recipe says
	// 9901 — the same proportion, written the other way.
	got, err := CheckRecipe(recipe(),
		ingredients(within("RAW_MILK", 990099, 0), within("RENNET", 9901, 0)),
		[]Lot{
			NewLot("b1", "MILK-1", "RAW_MILK", 100000, "KILOGRAMS"),
			NewLot("b2", "RENNET-1", "RENNET", 1000, "KILOGRAMS"),
		})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if f := findingFor(got, ShareDiffers, "RENNET"); f != nil {
		t.Errorf("a vat that held exactly the declared proportion was reported, because "+
			"9900.99 truncates to 9900 and the recipe rounds it to 9901: %s", f.Explanation)
	}
}

// Litres and kilograms in the same vat: the shares are not computed, because
// the denominator would be a total of two different things.
func TestSharesAreNotComputedAcrossUnits(t *testing.T) {
	got, err := CheckRecipe(recipe(),
		ingredients(within("RAW_MILK", 990099, 1000), within("RENNET", 9901, 1000)),
		[]Lot{
			NewLot("b1", "MILK-1", "RAW_MILK", 90000, "LITRES"),
			NewLot("b2", "RENNET-1", "RENNET", 10000, "KILOGRAMS"),
		})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if len(got.Shares) != 0 {
		t.Errorf("shares were computed against a total of litres and kilograms added together, "+
			"which is not a quantity of anything: %+v", got.Shares)
	}
	if got.SharesUnavailableReason == "" {
		t.Error("no shares and no reason; an empty section of a report is read as nothing " +
			"being wrong")
	}
}

// Two runs of the same report read the same way.
func TestFindingsComeBackInAStableOrder(t *testing.T) {
	in := []Lot{
		NewLot("b1", "X-1", "ZINC", 10, "KILOGRAMS"),
		NewLot("b2", "Y-1", "ALPHA", 10, "KILOGRAMS"),
		NewLot("b3", "Z-1", "MIDDLE", 10, "KILOGRAMS"),
	}
	want, err := CheckRecipe(recipe(), ingredients(req("RAW_MILK", nil), req("RENNET", nil)), in)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	for i := 0; i < 20; i++ {
		got, err := CheckRecipe(recipe(),
			ingredients(req("RAW_MILK", nil), req("RENNET", nil)), in)
		if err != nil {
			t.Fatalf("check: %v", err)
		}
		if len(got.Findings) != len(want.Findings) {
			t.Fatalf("run %d gave %d findings and the first gave %d",
				i, len(got.Findings), len(want.Findings))
		}
		for j := range got.Findings {
			if got.Findings[j].Kind != want.Findings[j].Kind ||
				got.Findings[j].ProductRef != want.Findings[j].ProductRef {
				t.Fatalf("run %d differs at position %d: %v %s against %v %s",
					i, j, got.Findings[j].Kind, got.Findings[j].ProductRef,
					want.Findings[j].Kind, want.Findings[j].ProductRef)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// The history a plant reads to set its own target
// ---------------------------------------------------------------------------

// The note says how many batches the figures rest on, and it says so in every
// case. A median over three vats and a median over three hundred look the same
// on a screen, and only one of them is worth setting a target from.
func TestTheHistoryAlwaysSaysHowManyBatchesItRestsOn(t *testing.T) {
	h := &ObservedHistory{Code: "PANEER-STD", BatchesCounted: 214}
	if got := h.Note(); got == "" {
		t.Fatal("no note")
	} else if want := "214"; !contains(got, want) {
		t.Errorf("note %q does not say how many batches it rests on", got)
	}

	h = &ObservedHistory{Code: "PANEER-STD", BatchesCounted: 3, BatchesNeedingADensity: 40}
	got := h.Note()
	if !contains(got, "3") || !contains(got, "40") {
		t.Errorf("note %q must give both figures; three usable observations beside forty "+
			"unconvertible ones is not a history anybody should set a target from, and a note "+
			"showing only the three would not say so", got)
	}

	h = &ObservedHistory{Code: "PANEER-STD"}
	if got := h.Note(); !contains(got, "no batch") {
		t.Errorf("note %q for a recipe nobody has used", got)
	}

	h = &ObservedHistory{Code: "PANEER-STD", BatchesNeedingADensity: 12}
	if got := h.Note(); !contains(got, "density") {
		t.Errorf("note %q; twelve batches were left out for want of a density and the note "+
			"does not say that is why there are no figures", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
