package domain

import (
	"strings"
	"testing"
	"time"
)

func at(h, m int) time.Time { return time.Date(2026, time.March, 1, h, m, 0, 0, time.UTC) }

func day(y int, mo time.Month, d int) time.Time {
	return time.Date(y, mo, d, 0, 0, 0, 0, time.UTC)
}

// sealed is the ordinary payment sample: drawn at the collection centre at six,
// sealed, and sent to the laboratory.
func sealed() *Sample {
	return &Sample{
		ID: "S1", TenantID: "T", Code: "SMP-001",
		SourceKind: FromCollection, SourceRef: "COL-1",
		DrawnAt: at(6, 0), DrawnBy: "CLERK",
		SealNumber: "SEAL-9001", Purpose: ForPayment,
		CreatedBy: "u",
	}
}

// certified is an instrument whose certificate runs to the end of the year.
func certified() Instrument {
	until := day(2027, time.January, 1)
	return Instrument{Ref: "MA-1", ValidUntil: &until, CertificateRef: "CERT-MA-1"}
}

func fatResult(analysedBy string, analysedAt time.Time, inst Instrument) *Result {
	return &Result{
		ID: "R1", TenantID: "T", SampleID: "S1",
		Analyte: Fat, Reading: Reading{Value: 41, Scale: 1},
		Method: "Gerber", Instrument: inst,
		AnalysedAt: analysedAt, AnalysedBy: analysedBy,
	}
}

// The chain that gets a sample from a collection centre to a laboratory.
func chainToLab() []Handover {
	return []Handover{
		{Sequence: 1, At: at(7, 0), From: "CLERK", To: "DRIVER"},
		{Sequence: 2, At: at(8, 30), From: "DRIVER", To: "ANALYST"},
	}
}

// The ordinary case: everything in order, and the result prices milk.
func TestASealedSampleWithAnUnbrokenChainAndACurrentCertificateIsEligible(t *testing.T) {
	e, why := Judge(sealed(), chainToLab(), fatResult("ANALYST", at(9, 30), certified()))
	if e != Eligible {
		t.Errorf("a sample in perfect order reads %s: %s", e, why)
	}
	if why != "" {
		t.Errorf("an eligible result carries a reason: %q", why)
	}
}

// A sample nobody sealed cannot support a payment.
//
// Nothing rules out its having been changed between the can and the bench, and
// the point of a seal is that the question does not have to be argued.
func TestAnUnsealedPaymentSampleIsNotEligible(t *testing.T) {
	s := sealed()
	s.SealNumber = ""
	e, why := Judge(s, chainToLab(), fatResult("ANALYST", at(9, 30), certified()))
	if e != NotEligible {
		t.Errorf("an unsealed payment sample reads %s", e)
	}
	if !strings.Contains(why, "never sealed") {
		t.Errorf("the reason does not say what is missing: %q", why)
	}
}

// A process check does not need a seal.
//
// A society looking at its own plant is not paying anybody, and a platform that
// demanded a seal for it would be tiresome about a reading that decides nothing.
func TestAProcessCheckNeedsNoSeal(t *testing.T) {
	s := sealed()
	s.SealNumber, s.Purpose = "", AsProcessCheck
	if e, why := Judge(s, nil, fatResult("ANALYST", at(9, 30), certified())); e != Eligible {
		t.Errorf("an unsealed process check reads %s: %s", e, why)
	}
}

// A temperature reading does not price milk, so it does not need the apparatus
// either — even on a payment sample.
func TestAnAnalyteThatDoesNotPriceMilkNeedsNoApparatus(t *testing.T) {
	s := sealed()
	s.SealNumber = ""
	r := fatResult("SOMEBODY_ELSE", at(9, 30), Instrument{Ref: "THERMOMETER"})
	r.Analyte = Temperature
	if e, why := Judge(s, nil, r); e != Eligible {
		t.Errorf("a temperature reading reads %s: %s", e, why)
	}
	// And the same reading of fat is not eligible, so this test is about the
	// analyte and not about the sample.
	r.Analyte = Fat
	if e, _ := Judge(s, nil, r); e == Eligible {
		t.Error("a fat reading on the same unsealed sample reads as eligible")
	}
}

// A gap in the middle of the chain.
//
// The driver handed it to the analyst and the record says somebody else did.
// That is a period nobody has accounted for, and it is where a sample gets
// swapped.
func TestAGapInTheChainOfCustodyMakesTheResultUnusable(t *testing.T) {
	broken := []Handover{
		{Sequence: 1, At: at(7, 0), From: "CLERK", To: "DRIVER"},
		{Sequence: 2, At: at(8, 30), From: "SOMEBODY_ELSE", To: "ANALYST"},
	}
	e, why := Judge(sealed(), broken, fatResult("ANALYST", at(9, 30), certified()))
	if e != NotEligible {
		t.Errorf("a broken chain reads %s", e)
	}
	if !strings.Contains(why, "custody") {
		t.Errorf("the reason does not mention custody: %q", why)
	}
	// It names both people, because somebody has to go and ask one of them.
	if !strings.Contains(why, "SOMEBODY_ELSE") || !strings.Contains(why, "DRIVER") {
		t.Errorf("the reason does not name who the sample went missing between: %q", why)
	}
}

// A chain that does not start with whoever drew the sample has a gap at the
// beginning, which is the easiest place of all to swap a bottle.
func TestAChainThatDoesNotStartWithTheSamplerIsBroken(t *testing.T) {
	orphan := []Handover{
		{Sequence: 1, At: at(7, 0), From: "DRIVER", To: "ANALYST"},
	}
	if finding := Custody(sealed(), orphan, at(9, 30)); finding.Intact {
		t.Error("a chain beginning with somebody who never had the sample reads as intact")
	}
}

// A sample nobody handed on is held by whoever drew it, and that is intact.
//
// The ordinary case at a village society: the same person draws the sample and
// tests it twenty minutes later. A platform that demanded a handover would be
// demanding a fiction.
func TestASampleNobodyHandedOnIsHeldByWhoeverDrewIt(t *testing.T) {
	finding := Custody(sealed(), nil, at(9, 30))
	if !finding.Intact {
		t.Fatalf("a sample with no handovers reads as broken: %s", finding.Reason)
	}
	if finding.Holder != "CLERK" {
		t.Errorf("the holder is %q, want whoever drew it", finding.Holder)
	}
	if e, why := Judge(sealed(), nil, fatResult("CLERK", at(6, 20), certified())); e != Eligible {
		t.Errorf("the sampler testing their own sample reads %s: %s", e, why)
	}
}

// A reading taken by somebody who was not holding the bottle is not evidence of
// what was in it.
func TestAResultRecordedBySomebodyWhoWasNotHoldingTheSampleIsNotEvidence(t *testing.T) {
	e, why := Judge(sealed(), chainToLab(), fatResult("A_DIFFERENT_ANALYST", at(9, 30), certified()))
	if e != NotEligible {
		t.Errorf("a result from somebody who never had the sample reads %s", e)
	}
	if !strings.Contains(why, "A_DIFFERENT_ANALYST") || !strings.Contains(why, "ANALYST") {
		t.Errorf("the reason does not name both: %q", why)
	}
}

// Custody is asked about the moment of the analysis, not about now.
//
// A sample handed on to the store after being tested is still a sample the
// analyst was holding when they tested it.
func TestCustodyIsAskedAboutTheMomentOfTheAnalysis(t *testing.T) {
	afterwards := append(chainToLab(), Handover{
		Sequence: 3, At: at(11, 0), From: "ANALYST", To: "STORE",
	})
	if e, why := Judge(sealed(), afterwards, fatResult("ANALYST", at(9, 30), certified())); e != Eligible {
		t.Errorf("a sample put away after the test reads %s: %s", e, why)
	}
	// And at eleven the store has it.
	if finding := Custody(sealed(), afterwards, at(12, 0)); finding.Holder != "STORE" {
		t.Errorf("after the last handover the holder is %q", finding.Holder)
	}
}

// A chain whose links go backwards in time was written from memory, and a chain
// written from memory is not a chain of custody.
func TestAChainThatGoesBackwardsInTimeIsBroken(t *testing.T) {
	backwards := []Handover{
		{Sequence: 1, At: at(8, 30), From: "CLERK", To: "DRIVER"},
		{Sequence: 2, At: at(7, 0), From: "DRIVER", To: "ANALYST"},
	}
	finding := Custody(sealed(), backwards, at(9, 30))
	if finding.Intact {
		t.Error("a chain whose second link precedes its first reads as intact")
	}
	if !strings.Contains(finding.Reason, "memory") {
		t.Errorf("the reason does not say what that means: %q", finding.Reason)
	}
}

// A missing link number is a missing handover.
func TestAChainWithALinkMissingIsBroken(t *testing.T) {
	gap := []Handover{
		{Sequence: 1, At: at(7, 0), From: "CLERK", To: "DRIVER"},
		{Sequence: 3, At: at(8, 30), From: "DRIVER", To: "ANALYST"},
	}
	finding := Custody(sealed(), gap, at(9, 30))
	if finding.Intact {
		t.Error("a chain jumping from link 1 to link 3 reads as intact")
	}
	if !strings.Contains(finding.Reason, "missing") {
		t.Errorf("the reason does not say a handover is missing: %q", finding.Reason)
	}
}

// An instrument out of calibration on the day of the analysis.
//
// The same rule material-service applies to a dipstick, and for the same reason:
// the reading is not wrong, it is unvouched for, and a payment computed from it
// cannot be defended.
func TestAnInstrumentOutOfCalibrationOnTheDayIsNotEligible(t *testing.T) {
	expired := day(2026, time.February, 1)
	e, why := Judge(sealed(), chainToLab(),
		fatResult("ANALYST", at(9, 30), Instrument{
			Ref: "MA-1", ValidUntil: &expired, CertificateRef: "CERT-MA-1",
		}))
	if e != NotEligible {
		t.Errorf("a reading from an expired instrument reads %s", e)
	}
	for _, want := range []string{"MA-1", "CERT-MA-1", "2026-02-01", "2026-03-01"} {
		if !strings.Contains(why, want) {
			t.Errorf("the reason does not carry %q: %q", want, why)
		}
	}
}

// The day a certificate expires is the day it stops being current.
func TestTheDayACertificateExpiresItIsNoLongerCurrent(t *testing.T) {
	expires := day(2026, time.March, 1)
	if e, _ := Judge(sealed(), chainToLab(),
		fatResult("ANALYST", at(9, 30), Instrument{Ref: "MA-1", ValidUntil: &expires})); e != NotEligible {
		t.Error("a reading taken on the day the certificate expires reads as eligible")
	}
	nextDay := day(2026, time.March, 2)
	if e, why := Judge(sealed(), chainToLab(),
		fatResult("ANALYST", at(9, 30), Instrument{Ref: "MA-1", ValidUntil: &nextDay})); e != Eligible {
		t.Errorf("a reading taken the day before expiry reads %s: %s", e, why)
	}
}

// An instrument nobody has recorded a calibration for is UNKNOWN, not
// NOT_ELIGIBLE.
//
// A gap in the records is a different thing from a finding, and reporting the
// two identically turns missing paperwork into an accusation.
func TestAnInstrumentWithNoRecordedCalibrationIsUnknownRatherThanRefused(t *testing.T) {
	e, why := Judge(sealed(), chainToLab(),
		fatResult("ANALYST", at(9, 30), Instrument{Ref: "MA-9"}))
	if e != Unknown {
		t.Errorf("an instrument with no recorded calibration reads %s, want UNKNOWN", e)
	}
	if !strings.Contains(why, "MA-9") {
		t.Errorf("the reason does not name the instrument: %q", why)
	}
}

// A seal broken at the bench moments before the analysis is the seal working. A
// seal broken hours earlier is not.
func TestASealBrokenLongBeforeTheAnalysisIsReported(t *testing.T) {
	s := sealed()
	brokenAtBench := at(9, 20)
	s.SealBrokenAt, s.SealBrokenBy = &brokenAtBench, "ANALYST"
	s.SealBrokenReason = "opened at the bench"
	if e, why := Judge(s, chainToLab(), fatResult("ANALYST", at(9, 30), certified())); e != Eligible {
		t.Errorf("a seal broken ten minutes before the test reads %s: %s", e, why)
	}

	brokenEarly := at(6, 30)
	s.SealBrokenAt, s.SealBrokenBy = &brokenEarly, "DRIVER"
	s.SealBrokenReason = "spillage in the vehicle"
	e, why := Judge(s, chainToLab(), fatResult("ANALYST", at(9, 30), certified()))
	if e != NotEligible {
		t.Errorf("a seal broken three hours earlier by the driver reads %s", e)
	}
	if !strings.Contains(why, "DRIVER") || !strings.Contains(why, "spillage") {
		t.Errorf("the reason does not say who broke it or why: %q", why)
	}
}

// A result timed before its sample was drawn.
//
// The commonest date-entry error in a laboratory book, and one that puts a
// result against a sample that did not exist.
func TestAResultTimedBeforeItsSampleIsNotEligible(t *testing.T) {
	e, why := Judge(sealed(), chainToLab(), fatResult("ANALYST", at(5, 0), certified()))
	if e != NotEligible {
		t.Errorf("a result read an hour before the sample was drawn reads %s", e)
	}
	if !strings.Contains(why, "drawn") {
		t.Errorf("the reason does not say what is wrong: %q", why)
	}
}

// Two machines, one sample: the disagreement is reported and not resolved.
//
// A laboratory that ran a sample twice did so to find out whether the two agree.
// Picking one and moving on throws away the answer to the question that was
// asked, and the platform has no basis for the choice.
func TestTwoReadingsOfOneAnalyteAreReportedTogetherWithTheirSpread(t *testing.T) {
	got := Compare([]*Result{
		{Analyte: Fat, Reading: Reading{Value: 41, Scale: 1}, Instrument: Instrument{Ref: "MA-1"}},
		{Analyte: Fat, Reading: Reading{Value: 44, Scale: 1}, Instrument: Instrument{Ref: "MA-2"}},
		{Analyte: SNF, Reading: Reading{Value: 86, Scale: 1}, Instrument: Instrument{Ref: "MA-1"}},
	})
	if len(got) != 2 {
		t.Fatalf("%d analytes reported, want fat and SNF", len(got))
	}
	fat := got[0]
	if fat.Analyte != Fat {
		t.Fatalf("the first group is %s", fat.Analyte)
	}
	if len(fat.Results) != 2 {
		t.Errorf("%d fat readings reported; both belong", len(fat.Results))
	}
	if fat.Spread == nil || fat.Spread.String() != "0.3" {
		t.Errorf("the fat spread is %v, want 0.3", fat.Spread)
	}
	// One reading of SNF is not a disagreement, so no spread is reported.
	if got[1].Spread != nil {
		t.Errorf("a single SNF reading reports a spread of %v", got[1].Spread)
	}
}

// Two readings written to different resolutions are not subtracted.
//
// 4.1 and 4.15 differ by 0.05 only if the first one meant 4.10, and a society
// writing to one decimal place did not say that.
func TestTwoReadingsAtDifferentResolutionsAreNotSubtracted(t *testing.T) {
	got := Compare([]*Result{
		{Analyte: Fat, Reading: Reading{Value: 41, Scale: 1}, Instrument: Instrument{Ref: "MA-1"}},
		{Analyte: Fat, Reading: Reading{Value: 415, Scale: 2}, Instrument: Instrument{Ref: "MA-2"}},
	})
	if got[0].Spread != nil {
		t.Errorf("readings at one and two decimal places gave a spread of %v", got[0].Spread)
	}
	if !strings.Contains(got[0].SpreadUnavailableReason, "decimal places") {
		t.Errorf("the reason does not say why: %q", got[0].SpreadUnavailableReason)
	}
}

// A superseded reading is not part of the comparison. A corrected figure and the
// figure it corrected are not two opinions.
func TestASupersededReadingIsNotComparedAgainstItsCorrection(t *testing.T) {
	when := at(10, 0)
	got := Compare([]*Result{
		{Analyte: Fat, Reading: Reading{Value: 41, Scale: 1},
			Instrument: Instrument{Ref: "MA-1"}, SupersededAt: &when},
		{Analyte: Fat, Reading: Reading{Value: 42, Scale: 1}, Instrument: Instrument{Ref: "MA-1"}},
	})
	if len(got) != 1 || len(got[0].Results) != 1 {
		t.Fatalf("the superseded reading is still in the comparison: %v", got)
	}
	if got[0].Spread != nil {
		t.Errorf("a correction and the figure it corrected were reported as a disagreement of %v",
			got[0].Spread)
	}
}

// The same sample compared twice reads the same. A caller diffing two views of
// one sample is looking at a real difference.
func TestTheSameSampleComparesTheSameEveryTime(t *testing.T) {
	build := func() []*Result {
		return []*Result{
			{Analyte: SNF, Reading: Reading{Value: 86, Scale: 1}, Instrument: Instrument{Ref: "MA-2"}},
			{Analyte: Fat, Reading: Reading{Value: 41, Scale: 1}, Instrument: Instrument{Ref: "MA-2"}},
			{Analyte: Fat, Reading: Reading{Value: 44, Scale: 1}, Instrument: Instrument{Ref: "MA-1"}},
		}
	}
	shape := func(ds []Disagreement) string {
		var b strings.Builder
		for _, d := range ds {
			b.WriteString(string(d.Analyte) + ":")
			for _, r := range d.Results {
				b.WriteString(r.Instrument.Ref + "=" + r.Reading.String() + ",")
			}
			b.WriteString("|")
		}
		return b.String()
	}
	first := shape(Compare(build()))
	for i := 0; i < 16; i++ {
		if again := shape(Compare(build())); again != first {
			t.Fatalf("comparison %d differs:\n  first %s\n  again %s", i+2, first, again)
		}
	}
	// Analytes in a fixed order, readings within one in instrument order.
	if !strings.HasPrefix(first, "FAT:MA-1=4.4,MA-2=4.1,") {
		t.Errorf("the comparison is not ordered as expected: %s", first)
	}
}

// A reading writes out at the resolution it was taken to. 4.1 and 4.10 are the
// same number and not the same claim about how precisely it was read.
func TestAReadingKeepsItsResolutionWhenWrittenOut(t *testing.T) {
	for _, c := range []struct {
		r    Reading
		want string
	}{
		{Reading{Value: 41, Scale: 1}, "4.1"},
		{Reading{Value: 410, Scale: 2}, "4.10"},
		{Reading{Value: 4, Scale: 0}, "4"},
		{Reading{Value: -15, Scale: 1}, "-1.5"},
		{Reading{Value: 5, Scale: 2}, "0.05"},
	} {
		if got := c.r.String(); got != c.want {
			t.Errorf("Reading{%d, %d} writes as %q, want %q", c.r.Value, c.r.Scale, got, c.want)
		}
	}
}

// A sample must say why it was taken. A payment sample and a process check are
// held to different standards, and a platform that could not tell them apart
// would have to hold both to one.
func TestASampleMustSayWhyItWasTaken(t *testing.T) {
	s := sealed()
	s.Purpose = ""
	if err := s.Validate(); err != ErrNoPurpose {
		t.Errorf("a sample with no purpose gave %v", err)
	}
	s.Purpose = AsDuplicate
	if err := s.Validate(); err == nil {
		t.Error("a duplicate was accepted with no original named")
	}
	s.DuplicatesSampleID = "S0"
	if err := s.Validate(); err != nil {
		t.Errorf("a complete duplicate was refused: %v", err)
	}
	s.Purpose = ForPayment
	if err := s.Validate(); err == nil {
		t.Error("a payment sample was accepted while naming another sample as its original")
	}
}
