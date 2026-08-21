package domain

import (
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/money"
)

func inr(t *testing.T, s string) money.Money {
	t.Helper()
	m, err := money.Parse(s, 2, "INR")
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return m
}

func comp(t *testing.T, kind ComponentKind, amount string) Component {
	t.Helper()
	return Component{Kind: kind, Amount: inr(t, amount)}
}

func compWith(t *testing.T, kind ComponentKind, amount, quantity, rate string) Component {
	t.Helper()
	return Component{Kind: kind, Amount: inr(t, amount), Quantity: quantity, Rate: rate}
}

func assertion(t *testing.T, total string, comps ...Component) *ExternalSettlementAssertion {
	t.Helper()
	return &ExternalSettlementAssertion{
		ID:          "asrt",
		TenantID:    "tnt",
		ProducerRef: "producer:01",
		Total:       inr(t, total),
		Components:  comps,
		AssertedAt:  time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		PeriodStart: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC),
	}
}

func shadow(t *testing.T, total string, comps ...Component) *ShadowSettlementComputation {
	t.Helper()
	return &ShadowSettlementComputation{
		ID:          "shdw",
		TenantID:    "tnt",
		ProducerRef: "producer:01",
		Total:       inr(t, total),
		Components:  comps,
		ComputedAt:  time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
		PeriodStart: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC),
	}
}

func TestClassifyExactMatch(t *testing.T) {
	a := assertion(t, "1000.00", comp(t, ComponentBasePrice, "1000.00"))
	s := shadow(t, "1000.00", comp(t, ComponentBasePrice, "1000.00"))

	v := Classify(a, s)
	if v.Classification != ClassMatch {
		t.Fatalf("got %s, want MATCH: %s", v.Classification, v.Rationale)
	}
	if !v.Delta.IsZero() {
		t.Errorf("delta = %s, want zero", v.Delta)
	}
	if v.Features != nil {
		t.Error("a match must not export ML features")
	}
}

func TestClassifyMissingSide(t *testing.T) {
	if v := Classify(nil, shadow(t, "10.00")); v.Classification != ClassInsufficientEvidence {
		t.Errorf("nil assertion: got %s, want INSUFFICIENT_EVIDENCE", v.Classification)
	}
	if v := Classify(assertion(t, "10.00"), nil); v.Classification != ClassInsufficientEvidence {
		t.Errorf("nil shadow: got %s, want INSUFFICIENT_EVIDENCE", v.Classification)
	}
}

func TestClassifyCurrencyMismatchIsNotComparable(t *testing.T) {
	a := assertion(t, "1000.00")
	s := shadow(t, "1000.00")
	usd, err := money.Parse("1000.00", 2, "USD")
	if err != nil {
		t.Fatal(err)
	}
	s.Total = usd

	v := Classify(a, s)
	if v.Classification != ClassInsufficientEvidence {
		t.Fatalf("got %s, want INSUFFICIENT_EVIDENCE", v.Classification)
	}
}

func TestClassifyDifferentScalesAreComparedAtTheCoarser(t *testing.T) {
	a := assertion(t, "1000.00", comp(t, ComponentBasePrice, "1000.00"))
	whole, err := money.Parse("1000", 0, "INR")
	if err != nil {
		t.Fatal(err)
	}
	a.Total = whole
	s := shadow(t, "1000.00", comp(t, ComponentBasePrice, "1000.00"))

	if v := Classify(a, s); v.Classification != ClassMatch {
		t.Fatalf("got %s, want MATCH after rescaling: %s", v.Classification, v.Rationale)
	}
}

func TestClassifyRoundingWithinOneMinorUnitPerLine(t *testing.T) {
	a := assertion(t, "1000.02",
		comp(t, ComponentBasePrice, "900.01"),
		comp(t, ComponentFatIncentive, "100.01"),
	)
	s := shadow(t, "1000.00",
		comp(t, ComponentBasePrice, "900.00"),
		comp(t, ComponentFatIncentive, "100.00"),
	)

	v := Classify(a, s)
	if v.Classification != ClassRounding {
		t.Fatalf("got %s, want ROUNDING_DIFFERENCE: %s", v.Classification, v.Rationale)
	}
	if v.Delta.Value != 2 {
		t.Errorf("delta = %d minor units, want 2", v.Delta.Value)
	}
}

func TestClassifyRejectsRoundingWhenALineIsOffByMoreThanOne(t *testing.T) {
	a := assertion(t, "1000.05", comp(t, ComponentBasePrice, "1000.05"))
	s := shadow(t, "1000.00", comp(t, ComponentBasePrice, "1000.00"))

	if v := Classify(a, s); v.Classification == ClassRounding {
		t.Fatalf("a five minor unit line difference was excused as rounding: %s", v.Rationale)
	}
}

func TestClassifyRecoveryExplainsTheWholeGap(t *testing.T) {
	a := assertion(t, "900.00",
		comp(t, ComponentBasePrice, "1000.00"),
		comp(t, ComponentLoanRecovery, "-100.00"),
	)
	s := shadow(t, "1000.00",
		comp(t, ComponentBasePrice, "1000.00"),
		comp(t, ComponentLoanRecovery, "0.00"),
	)

	v := Classify(a, s)
	if v.Classification != ClassRecovery {
		t.Fatalf("got %s, want RECOVERY_DIFFERENCE: %s", v.Classification, v.Rationale)
	}
	if v.Delta.Value != -10000 {
		t.Errorf("delta = %d, want -10000", v.Delta.Value)
	}
}

func TestClassifyRejectsRecoveryWhenMilkAlsoDisagrees(t *testing.T) {
	// The recovery line differs, but so does the base price. Attributing the
	// whole gap to the recovery would hide a real measurement disagreement.
	a := assertion(t, "850.00",
		compWith(t, ComponentBasePrice, "950.00", "300.000", "3.1666"),
		comp(t, ComponentLoanRecovery, "-100.00"),
	)
	s := shadow(t, "1000.00",
		compWith(t, ComponentBasePrice, "1000.00", "320.000", "3.1250"),
		comp(t, ComponentLoanRecovery, "0.00"),
	)

	if v := Classify(a, s); v.Classification == ClassRecovery {
		t.Fatalf("a base-price disagreement was absorbed into the recovery class: %s", v.Rationale)
	}
}

func TestClassifyPolicyWhenRateDiffersOnTheSameQuantity(t *testing.T) {
	a := assertion(t, "1050.00", compWith(t, ComponentBasePrice, "1050.00", "300.000", "3.5000"))
	s := shadow(t, "1000.00", compWith(t, ComponentBasePrice, "1000.00", "300.000", "3.3333"))

	v := Classify(a, s)
	if v.Classification != ClassPolicy {
		t.Fatalf("got %s, want POLICY_DIFFERENCE: %s", v.Classification, v.Rationale)
	}
}

func TestClassifyPolicyWhenALineExistsOnOnlyOneSide(t *testing.T) {
	a := assertion(t, "1050.00",
		comp(t, ComponentBasePrice, "1000.00"),
		comp(t, ComponentQualityBonus, "50.00"),
	)
	s := shadow(t, "1000.00", comp(t, ComponentBasePrice, "1000.00"))

	v := Classify(a, s)
	if v.Classification != ClassPolicy {
		t.Fatalf("got %s, want POLICY_DIFFERENCE: %s", v.Classification, v.Rationale)
	}
	var found bool
	for _, e := range v.Evidence {
		if e.Kind == ComponentQualityBonus && e.OnlyExternal {
			found = true
		}
	}
	if !found {
		t.Error("evidence did not record the quality bonus as external-only")
	}
}

func TestClassifyInputWhenQuantityDiffersAtTheSameRate(t *testing.T) {
	a := assertion(t, "1000.00", compWith(t, ComponentBasePrice, "1000.00", "320.000", "3.1250"))
	s := shadow(t, "937.50", compWith(t, ComponentBasePrice, "937.50", "300.000", "3.1250"))

	v := Classify(a, s)
	if v.Classification != ClassInput {
		t.Fatalf("got %s, want INPUT_DIFFERENCE: %s", v.Classification, v.Rationale)
	}
}

func TestClassifyUnexplainedExportsFeatures(t *testing.T) {
	// Amounts differ with no quantity, rate, recovery or one-sided line to
	// point at.
	a := assertion(t, "1000.00", comp(t, ComponentBasePrice, "1000.00"))
	s := shadow(t, "940.00", comp(t, ComponentBasePrice, "940.00"))

	v := Classify(a, s)
	if v.Classification != ClassUnexplained {
		t.Fatalf("got %s, want UNEXPLAINED: %s", v.Classification, v.Rationale)
	}
	if len(v.Features) == 0 {
		t.Fatal("an unexplained verdict must export features for the ML tier")
	}
	if _, ok := v.Features["rounding_residual_minor_units"]; !ok {
		t.Error("features are missing the rounding residual")
	}
}

func TestClassifyNoComponentDetailAtAll(t *testing.T) {
	small := Classify(assertion(t, "1000.01"), shadow(t, "1000.00"))
	if small.Classification != ClassRounding {
		t.Errorf("one minor unit with no detail: got %s, want ROUNDING_DIFFERENCE", small.Classification)
	}

	large := Classify(assertion(t, "1050.00"), shadow(t, "1000.00"))
	if large.Classification != ClassInsufficientEvidence {
		t.Errorf("large gap with no detail: got %s, want INSUFFICIENT_EVIDENCE", large.Classification)
	}
}

func TestCompareSumsRepeatedComponentKinds(t *testing.T) {
	// One side itemises two loan recoveries; the other reports their total.
	a := assertion(t, "800.00",
		comp(t, ComponentBasePrice, "1000.00"),
		comp(t, ComponentLoanRecovery, "-120.00"),
		comp(t, ComponentLoanRecovery, "-80.00"),
	)
	s := shadow(t, "800.00",
		comp(t, ComponentBasePrice, "1000.00"),
		comp(t, ComponentLoanRecovery, "-200.00"),
	)

	v := Classify(a, s)
	if v.Classification != ClassMatch {
		t.Fatalf("got %s, want MATCH: %s", v.Classification, v.Rationale)
	}
	for _, e := range v.Evidence {
		if e.Kind == ComponentLoanRecovery && e.DeltaMinorUnits != 0 {
			t.Errorf("itemised recoveries were not folded: delta %d", e.DeltaMinorUnits)
		}
	}
}

func TestClassifyIsDeterministic(t *testing.T) {
	a := assertion(t, "1050.00",
		compWith(t, ComponentBasePrice, "1000.00", "300.000", "3.3333"),
		comp(t, ComponentQualityBonus, "50.00"),
	)
	s := shadow(t, "1000.00", compWith(t, ComponentBasePrice, "1000.00", "300.000", "3.3333"))

	first := Classify(a, s)
	for i := 0; i < 100; i++ {
		again := Classify(a, s)
		if again.Classification != first.Classification || again.Rationale != first.Rationale {
			t.Fatalf("classification is not deterministic:\n first: %s / %s\n again: %s / %s",
				first.Classification, first.Rationale, again.Classification, again.Rationale)
		}
		if len(again.Evidence) != len(first.Evidence) {
			t.Fatal("evidence length varies between runs")
		}
		for j := range first.Evidence {
			if again.Evidence[j].Kind != first.Evidence[j].Kind {
				t.Fatal("evidence ordering varies between runs")
			}
		}
	}
}

func TestNeedsHumanReview(t *testing.T) {
	cases := []struct {
		class Classification
		delta int64
		want  bool
	}{
		{ClassMatch, 0, false},
		{ClassRounding, 1, false},
		{ClassRounding, 2, false},
		{ClassRounding, 9, true},
		{ClassInput, 5000, true},
		{ClassPolicy, 5000, true},
		{ClassRecovery, 5000, true},
		{ClassUnexplained, 5000, true},
		{ClassInsufficientEvidence, 5000, true},
	}
	for _, c := range cases {
		d := &SettlementDivergence{
			Classification: c.class,
			Delta:          money.Money{Value: c.delta, Scale: 2, Currency: "INR"},
		}
		if got := d.NeedsHumanReview(); got != c.want {
			t.Errorf("%s delta %d: NeedsHumanReview = %v, want %v", c.class, c.delta, got, c.want)
		}
	}
}
