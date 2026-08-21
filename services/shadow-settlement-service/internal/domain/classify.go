package domain

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ppusapati/gavya/libs/integrity/money"
)

// Verdict is the result of the deterministic comparison.
type Verdict struct {
	Classification Classification
	Rationale      string
	Evidence       []Evidence
	Delta          money.Money
	// Features are the scale-free signals handed to the ML tier when, and only
	// when, the classification came back UNEXPLAINED.
	Features map[string]float64
}

// roundingTolerancePerComponent is the largest delta one settlement line can
// accumulate from rounding alone: each side rounds its own line once, so the
// two can differ by at most one minor unit per line.
const roundingTolerancePerComponent = 1

// Classify attributes the difference between an external assertion and a
// shadow computation.
//
// This is the authoritative classifier. It is deterministic, it consults no
// model, and given the same two records it always returns the same verdict —
// which is what makes a shadow settlement defensible to an auditor. The ML
// tier is only ever asked about what this function returns as UNEXPLAINED.
func Classify(assertion *ExternalSettlementAssertion, shadow *ShadowSettlementComputation) Verdict {
	if assertion == nil || shadow == nil {
		return Verdict{
			Classification: ClassInsufficientEvidence,
			Rationale:      "one side of the comparison is missing",
		}
	}

	if assertion.Total.Currency != shadow.Total.Currency {
		return Verdict{
			Classification: ClassInsufficientEvidence,
			Rationale: fmt.Sprintf(
				"currencies differ: external is %s, shadow is %s; the two totals are not comparable",
				assertion.Total.Currency, shadow.Total.Currency),
		}
	}

	external, shadowTotal := assertion.Total, shadow.Total
	if external.Scale != shadowTotal.Scale {
		// Compare at the coarser of the two scales rather than refusing: a
		// source that reports whole rupees against a shadow computed to paise
		// is a normal integration, not a broken one.
		target := external.Scale
		if shadowTotal.Scale < target {
			target = shadowTotal.Scale
		}
		var err error
		if external, _, err = money.Rescale(external, target, money.RoundHalfUp); err != nil {
			return insufficient("external total could not be rescaled for comparison: " + err.Error())
		}
		if shadowTotal, _, err = money.Rescale(shadowTotal, target, money.RoundHalfUp); err != nil {
			return insufficient("shadow total could not be rescaled for comparison: " + err.Error())
		}
	}

	delta, err := money.Sub(external, shadowTotal)
	if err != nil {
		return insufficient("totals could not be differenced: " + err.Error())
	}

	if delta.IsZero() {
		return Verdict{
			Classification: ClassMatch,
			Rationale:      fmt.Sprintf("external and shadow totals agree exactly at %s %s", delta.Currency, external),
			Delta:          delta,
			Evidence:       compare(assertion.Components, shadow.Components),
		}
	}

	evidence := compare(assertion.Components, shadow.Components)
	absDelta := delta.Abs().Value

	// With no line detail on either side there is nothing to attribute the
	// difference to. A one-minor-unit gap is still safely rounding; anything
	// larger is a genuine unknown and must not be guessed at.
	if len(evidence) == 0 {
		if absDelta <= roundingTolerancePerComponent {
			return Verdict{
				Classification: ClassRounding,
				Rationale: fmt.Sprintf(
					"totals differ by %d minor unit with no component detail on either side, which is within single-line rounding",
					absDelta),
				Delta:    delta,
				Evidence: evidence,
			}
		}
		return Verdict{
			Classification: ClassInsufficientEvidence,
			Rationale: fmt.Sprintf(
				"totals differ by %s %s but neither side supplied component detail, so the difference cannot be attributed",
				delta.Currency, delta.Abs()),
			Delta:    delta,
			Evidence: evidence,
			Features: features(delta, evidence, assertion, shadow),
		}
	}

	differing := make([]Evidence, 0, len(evidence))
	for _, e := range evidence {
		if e.DeltaMinorUnits != 0 || e.OnlyExternal || e.OnlyShadow {
			differing = append(differing, e)
		}
	}

	// Ordered strongest evidence first: a line present on only one side, or a
	// recovery that accounts for the whole gap, is far more conclusive than the
	// residual reasoning further down.
	if v, ok := classifyRounding(delta, differing); ok {
		return withFeatures(v, assertion, shadow, evidence)
	}
	if v, ok := classifyRecovery(delta, evidence, differing); ok {
		return withFeatures(v, assertion, shadow, evidence)
	}
	if v, ok := classifyPolicy(delta, differing); ok {
		return withFeatures(v, assertion, shadow, evidence)
	}
	if v, ok := classifyInput(delta, differing); ok {
		return withFeatures(v, assertion, shadow, evidence)
	}

	return withFeatures(Verdict{
		Classification: ClassUnexplained,
		Rationale: fmt.Sprintf(
			"totals differ by %s %s across %d component(s), and no rounding, recovery, rate or quantity difference accounts for it",
			delta.Currency, delta.Abs(), len(differing)),
		Delta:    delta,
		Evidence: evidence,
	}, assertion, shadow, evidence)
}

// classifyRounding accepts only differences small enough that per-line rounding
// could have produced them: every differing line off by at most one minor unit,
// and the total off by at most the number of such lines.
func classifyRounding(delta money.Money, differing []Evidence) (Verdict, bool) {
	if len(differing) == 0 {
		return Verdict{}, false
	}
	for _, e := range differing {
		if e.OnlyExternal || e.OnlyShadow {
			return Verdict{}, false
		}
		if abs64(e.DeltaMinorUnits) > roundingTolerancePerComponent {
			return Verdict{}, false
		}
	}
	tolerance := int64(len(differing)) * roundingTolerancePerComponent
	if delta.Abs().Value > tolerance {
		return Verdict{}, false
	}
	return Verdict{
		Classification: ClassRounding,
		Rationale: fmt.Sprintf(
			"%d component(s) differ by at most one minor unit each and the totals differ by %d, within the %d minor unit bound that per-line rounding can produce",
			len(differing), delta.Abs().Value, tolerance),
		Delta: delta,
	}, true
}

// classifyRecovery accepts when the recovery lines account for the entire gap
// and every non-recovery line agrees. That pattern means the two systems agree
// about the milk and disagree only about which period an advance is recovered
// in — the most common benign divergence in a dairy settlement.
func classifyRecovery(delta money.Money, all, differing []Evidence) (Verdict, bool) {
	var recoveryDelta int64
	var recoveryLines []string
	for _, e := range all {
		if e.Kind.IsRecovery() {
			recoveryDelta += e.DeltaMinorUnits
			if e.DeltaMinorUnits != 0 {
				recoveryLines = append(recoveryLines, string(e.Kind))
			}
		}
	}
	if recoveryDelta == 0 || recoveryDelta != delta.Value {
		return Verdict{}, false
	}
	for _, e := range differing {
		if !e.Kind.IsRecovery() {
			return Verdict{}, false
		}
	}
	sort.Strings(recoveryLines)
	return Verdict{
		Classification: ClassRecovery,
		Rationale: fmt.Sprintf(
			"recovery component(s) %s differ by exactly the %d minor unit total gap while every other component agrees",
			strings.Join(recoveryLines, ", "), delta.Value),
		Delta: delta,
	}, true
}

// classifyPolicy accepts when a differing line applied the same quantity at a
// different rate, or exists on only one side. Both mean the two systems ran
// different rules over the same milk.
func classifyPolicy(delta money.Money, differing []Evidence) (Verdict, bool) {
	var reasons []string
	for _, e := range differing {
		switch {
		case e.OnlyExternal:
			reasons = append(reasons, fmt.Sprintf("%s is present only in the external settlement", e.Kind))
		case e.OnlyShadow:
			reasons = append(reasons, fmt.Sprintf("%s is present only in the shadow computation", e.Kind))
		case e.RateDiffers && !e.QuantityDiffers:
			reasons = append(reasons, fmt.Sprintf("%s applied a different rate to the same quantity", e.Kind))
		}
	}
	if len(reasons) == 0 {
		return Verdict{}, false
	}
	return Verdict{
		Classification: ClassPolicy,
		Rationale: fmt.Sprintf("totals differ by %d minor units because %s",
			delta.Value, strings.Join(reasons, "; ")),
		Delta: delta,
	}, true
}

// classifyInput accepts when a differing line used the same rate over a
// different quantity: the rules agree, the measurements do not.
func classifyInput(delta money.Money, differing []Evidence) (Verdict, bool) {
	var reasons []string
	for _, e := range differing {
		if e.QuantityDiffers {
			reasons = append(reasons, fmt.Sprintf("%s was computed over a different quantity", e.Kind))
		}
	}
	if len(reasons) == 0 {
		return Verdict{}, false
	}
	return Verdict{
		Classification: ClassInput,
		Rationale: fmt.Sprintf("totals differ by %d minor units because %s",
			delta.Value, strings.Join(reasons, "; ")),
		Delta: delta,
	}, true
}

// compare pairs the two sides' components by kind. A kind present on only one
// side still produces evidence, because its absence is itself the finding.
func compare(externalComps, shadowComps []Component) []Evidence {
	ext := index(externalComps)
	shd := index(shadowComps)

	kinds := make([]ComponentKind, 0, len(ext)+len(shd))
	for k := range ext {
		kinds = append(kinds, k)
	}
	for k := range shd {
		if _, seen := ext[k]; !seen {
			kinds = append(kinds, k)
		}
	}
	// Sorted so the evidence list is stable across runs and diffable in review.
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })

	out := make([]Evidence, 0, len(kinds))
	for _, k := range kinds {
		e, hasExt := ext[k]
		s, hasShd := shd[k]

		ev := Evidence{Kind: k}
		switch {
		case hasExt && hasShd:
			ev.ExternalMinorUnits = e.Amount.Value
			ev.ShadowMinorUnits = s.Amount.Value
			ev.DeltaMinorUnits = e.Amount.Value - s.Amount.Value
			ev.QuantityDiffers = e.Quantity != "" && s.Quantity != "" && e.Quantity != s.Quantity
			ev.RateDiffers = e.Rate != "" && s.Rate != "" && e.Rate != s.Rate
		case hasExt:
			ev.ExternalMinorUnits = e.Amount.Value
			ev.DeltaMinorUnits = e.Amount.Value
			ev.OnlyExternal = true
		default:
			ev.ShadowMinorUnits = s.Amount.Value
			ev.DeltaMinorUnits = -s.Amount.Value
			ev.OnlyShadow = true
		}
		out = append(out, ev)
	}
	return out
}

// index folds components onto their kind, summing repeats so a settlement that
// lists two separate loan recoveries is compared against one that lists their
// total.
func index(comps []Component) map[ComponentKind]Component {
	m := make(map[ComponentKind]Component, len(comps))
	for _, c := range comps {
		if prev, ok := m[c.Kind]; ok {
			prev.Amount.Value += c.Amount.Value
			// Quantity and rate are only meaningful for a single line; once a
			// kind repeats, attributing the difference to either is unsound.
			prev.Quantity, prev.Rate = "", ""
			m[c.Kind] = prev
			continue
		}
		m[c.Kind] = c
	}
	return m
}

func withFeatures(v Verdict, a *ExternalSettlementAssertion, s *ShadowSettlementComputation, evidence []Evidence) Verdict {
	v.Evidence = evidence
	if v.Classification == ClassUnexplained {
		v.Features = features(v.Delta, evidence, a, s)
	}
	return v
}

// features reduces the comparison to the scale-free signals the Rust divergence
// service consumes. Only the deterministic findings are exported: the model
// sees what the classifier saw, never the raw records.
func features(delta money.Money, evidence []Evidence, a *ExternalSettlementAssertion, s *ShadowSettlementComputation) map[string]float64 {
	f := make(map[string]float64, 8)

	var recovery, componentDelta int64
	var quantityDiffs, rateDiffs float64
	for _, e := range evidence {
		if e.Kind.IsRecovery() {
			recovery += e.DeltaMinorUnits
		}
		componentDelta += abs64(e.DeltaMinorUnits)
		if e.QuantityDiffers {
			quantityDiffs++
		}
		if e.RateDiffers {
			rateDiffs++
		}
	}

	f["recovery_amount_minor_units"] = float64(recovery)
	f["component_count_delta"] = float64(len(a.Components) - len(s.Components))
	f["input_delta_quantity"] = quantityDiffs
	f["input_delta_rate"] = rateDiffs
	// The residual is what per-line differences fail to account for; a small
	// residual over large line differences points at rounding, a large one does
	// not.
	f["rounding_residual_minor_units"] = float64(abs64(delta.Value) - componentDelta)

	if !a.PeriodStart.IsZero() && !s.PeriodStart.IsZero() {
		f["days_between_computations"] = s.ComputedAt.Sub(a.AssertedAt).Hours() / 24
	}
	return f
}

func insufficient(reason string) Verdict {
	return Verdict{Classification: ClassInsufficientEvidence, Rationale: reason}
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
