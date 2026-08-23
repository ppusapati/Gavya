package domain

import (
	"strings"
	"testing"
	"time"
)

// A verdict cites a law. Which law depends on where the deployment runs, and
// getting that wrong tells an operator their instrument failed a statute that
// does not reach them.

func TestEveryKnownRegimeResolves(t *testing.T) {
	for _, id := range RegimeIDs() {
		r, err := LookupRegime(id)
		if err != nil {
			t.Errorf("LookupRegime(%s): %v", id, err)
			continue
		}
		if r.ID != id {
			t.Errorf("LookupRegime(%s) returned %s", id, r.ID)
		}
		if r.Name == "" {
			t.Errorf("%s has no name to cite in a verdict", id)
		}
	}
}

// The default is the whole point: there isn't one. A deployment that says
// nothing would otherwise be given India's law, and every verdict it issued
// would look exactly like one that had been configured deliberately.
func TestAnUnsetRegimeIsRefusedRatherThanDefaulted(t *testing.T) {
	for _, id := range []string{"", "   ", "SOMEWHERE", "IN"} {
		if _, err := LookupRegime(id); err == nil {
			t.Errorf("LookupRegime(%q) was accepted", id)
		}
	}
}

// The message has to tell an operator what to put in the variable, or they will
// guess — and the guess that works is the wrong one.
func TestTheRefusalNamesTheOptions(t *testing.T) {
	_, err := LookupRegime("")
	if err == nil {
		t.Fatal("an empty regime was accepted")
	}
	for _, id := range RegimeIDs() {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("the message does not mention %s: %s", id, err)
		}
	}
}

func TestCaseAndSpacingDoNotMatter(t *testing.T) {
	for _, id := range []string{"in_legal_metrology", " IN_LEGAL_METROLOGY ", "In_Legal_Metrology"} {
		r, err := LookupRegime(id)
		if err != nil {
			t.Errorf("LookupRegime(%q): %v", id, err)
			continue
		}
		if r.ID != RegimeIndiaLegalMetrology.ID {
			t.Errorf("LookupRegime(%q) = %s", id, r.ID)
		}
	}
}

// Quality indicators do not multiply into a payment, so no regime requires a
// trade certificate for them.
func TestNoRegimeRegulatesWhatDoesNotDeterminePayment(t *testing.T) {
	for _, id := range RegimeIDs() {
		r, _ := LookupRegime(id)
		for _, q := range []QuantityKind{QuantityTemperatureC, QuantitySomaticCellCount, QuantityAdulterationIndex} {
			if r.Regulates(q) {
				t.Errorf("%s regulates %s, which does not enter the price", id, q)
			}
		}
	}
}

func TestARegulatingRegimeCoversWhatIsPaidFor(t *testing.T) {
	for _, id := range []string{"IN_LEGAL_METROLOGY", "EU_MID", "US_NIST_HB44", "KE_WEIGHTS_MEASURES"} {
		r, _ := LookupRegime(id)
		for _, q := range []QuantityKind{QuantityVolumeLitres, QuantityMassKG, QuantityFatPercent, QuantitySNFPercent} {
			if !r.Regulates(q) {
				t.Errorf("%s does not regulate %s, which the producer is paid on", id, q)
			}
		}
	}
}

// NONE is an answer, not a gap. An observation under it is eligible and the
// reason says why, so nobody is left wondering whether the check was skipped.
func TestUnderNoRegimeEverythingIsEligibleAndSaysSo(t *testing.T) {
	at := time.Date(2026, 8, 1, 6, 0, 0, 0, time.UTC)

	for _, q := range AllQuantityKinds() {
		got := AssessEligibility(RegimeNone, nil, at, q)
		if got.Verdict != EligibilityEligible {
			t.Errorf("%s under NONE = %s, want ELIGIBLE", q, got.Verdict)
		}
		if !strings.Contains(got.Reason, "no measurement-control regime") {
			t.Errorf("%s under NONE gave reason %q, which does not say the check did not apply", q, got.Reason)
		}
	}
}

// The same missing certificate reads differently in different countries,
// because the verdict names the statute the reader has to satisfy.
func TestTheVerdictCitesTheRegimeItWasDecidedUnder(t *testing.T) {
	at := time.Date(2026, 8, 1, 6, 0, 0, 0, time.UTC)

	india := AssessEligibility(RegimeIndiaLegalMetrology, nil, at, QuantityTemperatureC)
	eu := AssessEligibility(RegimeEUMeasuringInstruments, nil, at, QuantityTemperatureC)

	if !strings.Contains(india.Reason, "Legal Metrology Act") {
		t.Errorf("the Indian verdict does not cite its Act: %q", india.Reason)
	}
	if strings.Contains(eu.Reason, "Legal Metrology Act") {
		t.Errorf("the European verdict cites an Indian statute: %q", eu.Reason)
	}
	if !strings.Contains(eu.Reason, "Measuring Instruments Directive") {
		t.Errorf("the European verdict does not cite its Directive: %q", eu.Reason)
	}
}

// A regulating regime still demands evidence, and absence of a certificate is
// absence of evidence rather than proof the instrument was unverified.
func TestARegulatedQuantityWithNoCertificateIsUnknownNotIneligible(t *testing.T) {
	at := time.Date(2026, 8, 1, 6, 0, 0, 0, time.UTC)

	got := AssessEligibility(RegimeUSWeightsAndMeasures, nil, at, QuantityVolumeLitres)
	if got.Verdict != EligibilityUnknown {
		t.Errorf("verdict = %s, want UNKNOWN — no certificate is not an accusation", got.Verdict)
	}
}
