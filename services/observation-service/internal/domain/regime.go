package domain

import (
	"fmt"
	"sort"
	"strings"
)

// A Regime is the measurement-control law a deployment operates under.
//
// The eligibility rules here were written for one country. The *shape* of the
// question is universal — was this instrument verified by the relevant
// authority at the moment it measured, for a quantity that determines what
// somebody is paid? — but the answer cites a specific Act, and which quantities
// fall under it is that Act's decision, not this platform's.
//
// So the regime is declared by the deployment rather than assumed. A Kenyan
// co-operative operates under the Weights and Measures Act, a German one under
// the Measuring Instruments Directive, and a verdict that told either of them
// their milk meter was unverified "under the Legal Metrology Act" would be
// citing a law that does not apply to them.
type Regime struct {
	// ID is what is stored against a verdict, so a decision made years ago can
	// still be read against the rules that produced it.
	ID string
	// Name is cited in the reason an auditor reads.
	Name string
	// tradeCritical is the set of quantities this regime regulates. A quantity
	// outside it needs no certificate, because an instrument that does not
	// determine money is not a trade instrument.
	tradeCritical map[QuantityKind]bool
}

// Regulates reports whether this regime governs the given quantity.
func (r Regime) Regulates(q QuantityKind) bool {
	if !q.Valid() {
		return false
	}
	return r.tradeCritical[q]
}

// paying is the set of quantities that enter the price a producer is paid.
// Every regime below starts from it, because the reason a quantity is regulated
// anywhere is that money rests on it.
func paying() map[QuantityKind]bool {
	out := map[QuantityKind]bool{}
	for _, q := range AllQuantityKinds() {
		switch q {
		case QuantityTemperatureC, QuantitySomaticCellCount, QuantityAdulterationIndex:
			// Quality and handling indicators. They can affect acceptance, but
			// they do not multiply into the payment.
		default:
			out[q] = true
		}
	}
	return out
}

// RegimeNone is a deployment with no measurement-control law to apply.
//
// It is a real answer, not an absence of one: an observation under it is
// eligible and says why, so a reader is never left wondering whether the check
// was skipped or passed.
var RegimeNone = Regime{
	ID:            "NONE",
	Name:          "no measurement-control regime",
	tradeCritical: map[QuantityKind]bool{},
}

// RegimeIndiaLegalMetrology is the Legal Metrology Act, 2009.
var RegimeIndiaLegalMetrology = Regime{
	ID:            "IN_LEGAL_METROLOGY",
	Name:          "the Legal Metrology Act",
	tradeCritical: paying(),
}

// RegimeEUMeasuringInstruments is Directive 2014/32/EU, which covers automatic
// weighing and continuous totalisers used in trade across the single market.
var RegimeEUMeasuringInstruments = Regime{
	ID:            "EU_MID",
	Name:          "the Measuring Instruments Directive",
	tradeCritical: paying(),
}

// RegimeUSWeightsAndMeasures is NIST Handbook 44, adopted by the states.
var RegimeUSWeightsAndMeasures = Regime{
	ID:            "US_NIST_HB44",
	Name:          "NIST Handbook 44",
	tradeCritical: paying(),
}

// RegimeKenyaWeightsAndMeasures is the Weights and Measures Act (Cap. 513).
var RegimeKenyaWeightsAndMeasures = Regime{
	ID:            "KE_WEIGHTS_MEASURES",
	Name:          "the Weights and Measures Act",
	tradeCritical: paying(),
}

var regimes = map[string]Regime{
	RegimeNone.ID:                    RegimeNone,
	RegimeIndiaLegalMetrology.ID:     RegimeIndiaLegalMetrology,
	RegimeEUMeasuringInstruments.ID:  RegimeEUMeasuringInstruments,
	RegimeUSWeightsAndMeasures.ID:    RegimeUSWeightsAndMeasures,
	RegimeKenyaWeightsAndMeasures.ID: RegimeKenyaWeightsAndMeasures,
}

// LookupRegime finds a regime by id.
//
// There is deliberately no default. A deployment that has not said which law it
// operates under would otherwise be given India's, and every eligibility verdict
// it issued would cite an Act that does not apply where it runs. Saying NONE is
// a decision; saying nothing is an oversight, and the two should not look alike.
func LookupRegime(id string) (Regime, error) {
	r, ok := regimes[strings.ToUpper(strings.TrimSpace(id))]
	if !ok {
		return Regime{}, fmt.Errorf(
			"measurement regime %q is not one this platform knows; it must be one of %s",
			id, strings.Join(RegimeIDs(), ", "))
	}
	return r, nil
}

// RegimeIDs lists every regime, for a validation message or a picker.
func RegimeIDs() []string {
	out := make([]string, 0, len(regimes))
	for id := range regimes {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
