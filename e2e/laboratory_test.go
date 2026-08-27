//go:build e2e

// The laboratory, and what makes a reading fit to price milk.
//
// Fat and SNF decide what a producer is paid. A result that priced a fortnight
// and cannot be traced to a sealed sample, held by known hands, read on an
// instrument somebody had certified, is a number a society cannot defend when a
// member asks about it — and the member is entitled to ask.
//
// Every one of these readings is recorded. That is deliberate: a platform that
// refused to write down what happened would simply be kept alongside a paper
// book, which is the failure this whole thing exists to end. What differs is
// whether the reading comes back marked fit to pay on.
package e2e

import (
	"context"
	"strings"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

const laboratorySvc = "laboratory.v1.LaboratoryService"

type readingProto struct {
	Value string `json:"value"`
	Scale int32  `json:"scale"`
}

type drawSampleReq struct {
	TenantID           string `json:"tenant_id"`
	Code               string `json:"code"`
	SourceKind         string `json:"source_kind"`
	SourceRef          string `json:"source_ref"`
	DrawnAt            string `json:"drawn_at"`
	DrawnBy            string `json:"drawn_by"`
	SealNumber         string `json:"seal_number,omitempty"`
	Purpose            string `json:"purpose"`
	DuplicatesSampleID string `json:"duplicates_sample_id,omitempty"`
	Actor              string `json:"actor"`
}

type sampleProto struct {
	ID               string `json:"id"`
	Code             string `json:"code"`
	SourceKind       string `json:"source_kind"`
	SourceRef        string `json:"source_ref"`
	DrawnAt          string `json:"drawn_at"`
	DrawnBy          string `json:"drawn_by"`
	SealNumber       string `json:"seal_number,omitempty"`
	SealBrokenAt     string `json:"seal_broken_at,omitempty"`
	SealBrokenBy     string `json:"seal_broken_by,omitempty"`
	SealBrokenReason string `json:"seal_broken_reason,omitempty"`
	Sealed           bool   `json:"sealed"`
	Purpose          string `json:"purpose"`
}

type sampleResp struct {
	Sample *sampleProto `json:"sample"`
}

type breakSealReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
	Reason   string `json:"reason"`
	At       string `json:"at,omitempty"`
	Actor    string `json:"actor"`
}

type recordHandoverReq struct {
	TenantID string `json:"tenant_id"`
	SampleID string `json:"sample_id"`
	At       string `json:"at"`
	From     string `json:"from"`
	To       string `json:"to"`
	Note     string `json:"note,omitempty"`
	Actor    string `json:"actor"`
}

type handoverProto struct {
	Sequence int32  `json:"sequence"`
	At       string `json:"at"`
	From     string `json:"from"`
	To       string `json:"to"`
}

type handoverResp struct {
	Handover *handoverProto `json:"handover"`
}

type recordResultReq struct {
	TenantID       string       `json:"tenant_id"`
	SampleID       string       `json:"sample_id"`
	Analyte        string       `json:"analyte"`
	Reading        readingProto `json:"reading"`
	Method         string       `json:"method"`
	InstrumentRef  string       `json:"instrument_ref"`
	ValidUntil     string       `json:"instrument_valid_until,omitempty"`
	CertificateRef string       `json:"instrument_certificate,omitempty"`
	AnalysedAt     string       `json:"analysed_at"`
	AnalysedBy     string       `json:"analysed_by"`
	Actor          string       `json:"actor"`
}

type labResultProto struct {
	ID            string       `json:"id"`
	Analyte       string       `json:"analyte"`
	Reading       readingProto `json:"reading"`
	InstrumentRef string       `json:"instrument_ref"`
	Eligibility   string       `json:"eligibility"`
	Reason        string       `json:"eligibility_reason"`
}

type labResultResp struct {
	Result *labResultProto `json:"result"`
}

type analyteGroupProto struct {
	Analyte    string            `json:"analyte"`
	Results    []*labResultProto `json:"results"`
	Spread     *readingProto     `json:"spread,omitempty"`
	SpreadNote string            `json:"spread_unavailable_reason,omitempty"`
}

type sampleReportReq struct {
	TenantID string `json:"tenant_id"`
	SampleID string `json:"sample_id"`
}

type sampleReportResp struct {
	Sample        *sampleProto        `json:"sample"`
	Chain         []*handoverProto    `json:"chain"`
	CustodyIntact bool                `json:"custody_intact"`
	CustodyHolder string              `json:"custody_holder,omitempty"`
	CustodyReason string              `json:"custody_reason,omitempty"`
	Analytes      []analyteGroupProto `json:"analytes"`
	Eligible      int32               `json:"eligible"`
	Total         int32               `json:"total"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func drawSample(t *testing.T, p *platform, in drawSampleReq) (*sampleProto, error) {
	t.Helper()
	in.TenantID = p.tenant
	if in.Actor == "" {
		in.Actor = "e2e"
	}
	resp, err := svcclient.Call[drawSampleReq, sampleResp](
		context.Background(), p.laboratory(), laboratorySvc+"/DrawSample", in, p.opts())
	if err != nil {
		return nil, err
	}
	return resp.Sample, nil
}

// paymentSample is the ordinary case: drawn at the collection centre, sealed.
func paymentSample(t *testing.T, p *platform, code string) *sampleProto {
	t.Helper()
	s, err := drawSample(t, p, drawSampleReq{
		Code: code, SourceKind: "COLLECTION", SourceRef: "COL-" + code,
		DrawnAt: "2026-03-01T06:00:00Z", DrawnBy: "CLERK",
		SealNumber: "SEAL-" + code, Purpose: "PAYMENT",
	})
	if err != nil {
		t.Fatalf("DrawSample %s: %v", code, err)
	}
	return s
}

func handover(t *testing.T, p *platform, sampleID, at, from, to string) error {
	t.Helper()
	_, err := svcclient.Call[recordHandoverReq, handoverResp](
		context.Background(), p.laboratory(), laboratorySvc+"/RecordHandover",
		recordHandoverReq{TenantID: p.tenant, SampleID: sampleID,
			At: at, From: from, To: to, Actor: "e2e"}, p.opts())
	return err
}

func recordResult(t *testing.T, p *platform, in recordResultReq) (*labResultProto, error) {
	t.Helper()
	in.TenantID = p.tenant
	if in.Actor == "" {
		in.Actor = "e2e"
	}
	if in.Method == "" {
		in.Method = "Gerber"
	}
	resp, err := svcclient.Call[recordResultReq, labResultResp](
		context.Background(), p.laboratory(), laboratorySvc+"/RecordResult", in, p.opts())
	if err != nil {
		return nil, err
	}
	return resp.Result, nil
}

func sampleReport(t *testing.T, p *platform, sampleID string) *sampleReportResp {
	t.Helper()
	resp, err := svcclient.Call[sampleReportReq, sampleReportResp](
		context.Background(), p.laboratory(), laboratorySvc+"/GetSampleReport",
		sampleReportReq{TenantID: p.tenant, SampleID: sampleID}, p.opts())
	if err != nil {
		t.Fatalf("GetSampleReport: %v", err)
	}
	return resp
}

// fatOn is a fat reading on a certified instrument.
func fatOn(sampleID, instrument, analysedBy string) recordResultReq {
	return recordResultReq{
		SampleID: sampleID, Analyte: "FAT",
		Reading:       readingProto{Value: "4.1", Scale: 1},
		InstrumentRef: instrument, ValidUntil: "2027-01-01",
		CertificateRef: "CERT-" + instrument,
		AnalysedAt:     "2026-03-01T09:30:00Z", AnalysedBy: analysedBy,
	}
}

// ---------------------------------------------------------------------------
// The path
// ---------------------------------------------------------------------------

// A sealed sample, an unbroken chain, a current certificate: the reading is fit
// to price milk, and the report says so in one number.
func TestASampleThatCanBeDefendedProducesAnEligibleReading(t *testing.T) {
	p := startPlatform(t)
	s := paymentSample(t, p, "SMP-101")

	for _, h := range [][3]string{
		{"2026-03-01T07:00:00Z", "CLERK", "DRIVER"},
		{"2026-03-01T08:30:00Z", "DRIVER", "ANALYST"},
	} {
		if err := handover(t, p, s.ID, h[0], h[1], h[2]); err != nil {
			t.Fatalf("RecordHandover %s to %s: %v", h[1], h[2], err)
		}
	}

	r, err := recordResult(t, p, fatOn(s.ID, "MA-1", "ANALYST"))
	if err != nil {
		t.Fatalf("RecordResult: %v", err)
	}
	if r.Eligibility != "ELIGIBLE" {
		t.Errorf("a sample in perfect order reads %s: %s", r.Eligibility, r.Reason)
	}
	if r.Reading.Value != "4.1" {
		t.Errorf("the reading came back as %s", r.Reading.Value)
	}

	rep := sampleReport(t, p, s.ID)
	if !rep.CustodyIntact {
		t.Errorf("the chain reads as broken: %s", rep.CustodyReason)
	}
	if rep.CustodyHolder != "ANALYST" {
		t.Errorf("the holder is %q, want the analyst", rep.CustodyHolder)
	}
	if len(rep.Chain) != 2 {
		t.Errorf("%d links in the chain, want 2", len(rep.Chain))
	}
	if rep.Eligible != 1 || rep.Total != 1 {
		t.Errorf("the report says %d of %d readings are fit to pay on", rep.Eligible, rep.Total)
	}
}

// A payment sample nobody sealed.
//
// The reading is recorded — a society has to be able to write down what
// happened — and it is recorded as not fit, with the reason, so nobody finds out
// afterwards.
func TestAnUnsealedPaymentSampleIsRecordedAndNotFitToPayOn(t *testing.T) {
	p := startPlatform(t)
	s, err := drawSample(t, p, drawSampleReq{
		Code: "SMP-111", SourceKind: "COLLECTION", SourceRef: "COL-1",
		DrawnAt: "2026-03-01T06:00:00Z", DrawnBy: "CLERK", Purpose: "PAYMENT",
	})
	if err != nil {
		t.Fatalf("DrawSample: %v", err)
	}
	if s.Sealed {
		t.Fatal("a sample drawn with no seal number reads as sealed")
	}

	r, err := recordResult(t, p, fatOn(s.ID, "MA-1", "CLERK"))
	if err != nil {
		t.Fatalf("the reading was refused rather than recorded: %v", err)
	}
	if r.Eligibility != "NOT_ELIGIBLE" {
		t.Errorf("an unsealed payment sample reads %s", r.Eligibility)
	}
	if !strings.Contains(r.Reason, "never sealed") {
		t.Errorf("the reason does not say what is missing: %q", r.Reason)
	}
	// It is still on the record, which is the point.
	rep := sampleReport(t, p, s.ID)
	if rep.Total != 1 {
		t.Errorf("%d readings on the sample; the ineligible one belongs there too", rep.Total)
	}
	if rep.Eligible != 0 {
		t.Errorf("%d readings read as fit to pay on", rep.Eligible)
	}
}

// A process check is not a payment, so it needs no seal.
//
// A society looking at its own plant is not paying anybody, and a platform that
// demanded a seal would be tiresome about a reading that decides nothing.
func TestAProcessCheckNeedsNoSealAndItsReadingIsFine(t *testing.T) {
	p := startPlatform(t)
	s, err := drawSample(t, p, drawSampleReq{
		Code: "SMP-121", SourceKind: "NODE", SourceRef: "BMC-04",
		DrawnAt: "2026-03-01T06:00:00Z", DrawnBy: "OPERATOR", Purpose: "PROCESS_CHECK",
	})
	if err != nil {
		t.Fatalf("DrawSample: %v", err)
	}
	r, err := recordResult(t, p, fatOn(s.ID, "MA-1", "OPERATOR"))
	if err != nil {
		t.Fatalf("RecordResult: %v", err)
	}
	if r.Eligibility != "ELIGIBLE" {
		t.Errorf("an unsealed process check reads %s: %s", r.Eligibility, r.Reason)
	}
}

// The chain is checked when somebody is standing there with the bottle.
//
// A handover from somebody who is not holding the sample is refused at the
// moment it is recorded, rather than accepted and discovered three weeks later
// when a member is disputing a payment. The person who can answer the question
// is in the room now.
func TestAHandoverFromSomebodyNotHoldingTheSampleIsRefusedThereAndThen(t *testing.T) {
	p := startPlatform(t)
	s := paymentSample(t, p, "SMP-131")

	if err := handover(t, p, s.ID, "2026-03-01T07:00:00Z", "CLERK", "DRIVER"); err != nil {
		t.Fatalf("RecordHandover: %v", err)
	}

	err := handover(t, p, s.ID, "2026-03-01T08:30:00Z", "SOMEBODY_ELSE", "ANALYST")
	if err == nil {
		t.Fatal("a handover was recorded from somebody who never had the sample")
	}
	if !strings.Contains(err.Error(), "DRIVER") || !strings.Contains(err.Error(), "SOMEBODY_ELSE") {
		t.Errorf("the refusal does not name who actually has it: %v", err)
	}

	// The chain is unchanged, so the refusal did not half-write anything.
	rep := sampleReport(t, p, s.ID)
	if len(rep.Chain) != 1 {
		t.Errorf("%d links after a refused handover, want 1", len(rep.Chain))
	}
	if rep.CustodyHolder != "DRIVER" {
		t.Errorf("the holder is %q after the refusal", rep.CustodyHolder)
	}
}

// A reading taken by somebody who was not holding the bottle is not evidence of
// what was in it.
func TestAReadingBySomebodyWhoWasNotHoldingTheSampleIsNotFitToPayOn(t *testing.T) {
	p := startPlatform(t)
	s := paymentSample(t, p, "SMP-141")
	if err := handover(t, p, s.ID, "2026-03-01T07:00:00Z", "CLERK", "DRIVER"); err != nil {
		t.Fatalf("RecordHandover: %v", err)
	}

	r, err := recordResult(t, p, fatOn(s.ID, "MA-1", "AN_ANALYST_WHO_NEVER_HAD_IT"))
	if err != nil {
		t.Fatalf("RecordResult: %v", err)
	}
	if r.Eligibility != "NOT_ELIGIBLE" {
		t.Errorf("a reading from somebody who never had the sample reads %s", r.Eligibility)
	}
	if !strings.Contains(r.Reason, "DRIVER") {
		t.Errorf("the reason does not say who was holding it: %q", r.Reason)
	}

	// And the same reading by the driver, who is holding it, is fine — so this
	// is about who took it and not about the sample.
	r2, err := recordResult(t, p, fatOn(s.ID, "MA-2", "DRIVER"))
	if err != nil {
		t.Fatalf("RecordResult: %v", err)
	}
	if r2.Eligibility != "ELIGIBLE" {
		t.Errorf("the holder's own reading reads %s: %s", r2.Eligibility, r2.Reason)
	}
}

// An instrument out of calibration on the day.
//
// The same rule material-service applies to a dipstick, and for the same reason:
// the reading is not wrong, it is unvouched for.
func TestAReadingFromAnExpiredInstrumentIsNotFitToPayOn(t *testing.T) {
	p := startPlatform(t)
	s := paymentSample(t, p, "SMP-151")

	in := fatOn(s.ID, "MA-9", "CLERK")
	in.ValidUntil = "2026-02-01"
	r, err := recordResult(t, p, in)
	if err != nil {
		t.Fatalf("RecordResult: %v", err)
	}
	if r.Eligibility != "NOT_ELIGIBLE" {
		t.Errorf("a reading from an expired instrument reads %s", r.Eligibility)
	}
	for _, want := range []string{"MA-9", "CERT-MA-9", "2026-02-01", "2026-03-01"} {
		if !strings.Contains(r.Reason, want) {
			t.Errorf("the reason does not carry %q: %q", want, r.Reason)
		}
	}
}

// An instrument nobody has recorded a calibration for is UNKNOWN, not
// NOT_ELIGIBLE.
//
// A gap in the records is a different thing from a finding, and reporting the
// two identically turns missing paperwork into an accusation.
func TestAnUnrecordedCalibrationIsUnknownRatherThanAnAccusation(t *testing.T) {
	p := startPlatform(t)
	s := paymentSample(t, p, "SMP-161")

	in := fatOn(s.ID, "MA-UNKNOWN", "CLERK")
	in.ValidUntil, in.CertificateRef = "", ""
	r, err := recordResult(t, p, in)
	if err != nil {
		t.Fatalf("RecordResult: %v", err)
	}
	if r.Eligibility != "UNKNOWN" {
		t.Errorf("an instrument with no recorded calibration reads %s, want UNKNOWN", r.Eligibility)
	}
	if !strings.Contains(r.Reason, "MA-UNKNOWN") {
		t.Errorf("the reason does not name the instrument: %q", r.Reason)
	}
	// UNKNOWN is not eligible either, so nothing pays on it.
	if rep := sampleReport(t, p, s.ID); rep.Eligible != 0 {
		t.Errorf("%d readings read as fit to pay on", rep.Eligible)
	}
}

// Two machines, one sample: the disagreement is reported and never resolved.
//
// A laboratory that ran a sample twice did so to find out whether the two agree.
// Picking one and moving on throws away the answer to the question that was
// asked, and the platform has no basis for the choice.
func TestTwoMachinesOnOneSampleAreReportedTogetherWithTheirSpread(t *testing.T) {
	p := startPlatform(t)
	s := paymentSample(t, p, "SMP-171")

	for _, c := range []struct{ instrument, value string }{
		{"MA-1", "4.1"},
		{"MA-2", "4.4"},
	} {
		in := fatOn(s.ID, c.instrument, "CLERK")
		in.Reading = readingProto{Value: c.value, Scale: 1}
		if _, err := recordResult(t, p, in); err != nil {
			t.Fatalf("RecordResult on %s: %v", c.instrument, err)
		}
	}

	rep := sampleReport(t, p, s.ID)
	if len(rep.Analytes) != 1 {
		t.Fatalf("%d analyte groups, want fat", len(rep.Analytes))
	}
	fat := rep.Analytes[0]
	if len(fat.Results) != 2 {
		t.Fatalf("%d fat readings reported; both belong", len(fat.Results))
	}
	if fat.Spread == nil || fat.Spread.Value != "0.3" {
		t.Errorf("the spread is %v, want 0.3", fat.Spread)
	}
	// Neither is picked. Both are there, in instrument order, with their own
	// verdicts.
	if fat.Results[0].InstrumentRef != "MA-1" || fat.Results[1].InstrumentRef != "MA-2" {
		t.Errorf("the readings came back from %s then %s",
			fat.Results[0].InstrumentRef, fat.Results[1].InstrumentRef)
	}

	// The same analyte on the same instrument twice is one of them entered
	// twice, and is refused.
	if _, err := recordResult(t, p, fatOn(s.ID, "MA-1", "CLERK")); err == nil {
		t.Error("the same analyte was read twice on the same instrument")
	}
}

// A seal broken long before the analysis is reported; one broken at the bench is
// the seal doing its job.
func TestASealBrokenLongBeforeTheAnalysisIsReported(t *testing.T) {
	p := startPlatform(t)
	s := paymentSample(t, p, "SMP-181")

	// Breaking a seal has to say why.
	if _, err := svcclient.Call[breakSealReq, sampleResp](
		context.Background(), p.laboratory(), laboratorySvc+"/BreakSeal",
		breakSealReq{TenantID: p.tenant, ID: s.ID, Actor: "DRIVER"}, p.opts()); err == nil {
		t.Error("a seal was broken with no reason recorded")
	}

	broken, err := svcclient.Call[breakSealReq, sampleResp](
		context.Background(), p.laboratory(), laboratorySvc+"/BreakSeal",
		breakSealReq{TenantID: p.tenant, ID: s.ID,
			Reason: "spillage in the vehicle",
			At:     "2026-03-01T06:30:00Z", Actor: "DRIVER"}, p.opts())
	if err != nil {
		t.Fatalf("BreakSeal: %v", err)
	}
	if broken.Sample.SealBrokenReason != "spillage in the vehicle" {
		t.Errorf("the sample does not carry why the seal was broken: %q",
			broken.Sample.SealBrokenReason)
	}

	// Broken at half past six by the driver; the analysis is at half past nine.
	// Three hours is not a seal doing its job.
	r, err := recordResult(t, p, fatOn(s.ID, "MA-1", "CLERK"))
	if err != nil {
		t.Fatalf("RecordResult: %v", err)
	}
	if r.Eligibility != "NOT_ELIGIBLE" {
		t.Errorf("a reading on a sample whose seal was broken long before reads %s: %s",
			r.Eligibility, r.Reason)
	}

	// A seal cannot be broken twice: the second attempt says who broke it first.
	if _, err := svcclient.Call[breakSealReq, sampleResp](
		context.Background(), p.laboratory(), laboratorySvc+"/BreakSeal",
		breakSealReq{TenantID: p.tenant, ID: s.ID,
			Reason: "again", Actor: "SOMEBODY"}, p.opts()); err == nil {
		t.Error("a seal was broken a second time")
	} else if !strings.Contains(err.Error(), "DRIVER") {
		t.Errorf("the refusal does not say who broke it: %v", err)
	}
}

// A duplicate is a second bottle of the same milk.
//
// One that names a different source is a separate sample with a misleading
// label — and the label is what somebody reads when the two disagree.
func TestADuplicateMustBeDrawnFromTheSameMilk(t *testing.T) {
	p := startPlatform(t)
	original := paymentSample(t, p, "SMP-191")

	if _, err := drawSample(t, p, drawSampleReq{
		Code: "SMP-192", SourceKind: "COLLECTION", SourceRef: "A_DIFFERENT_COLLECTION",
		DrawnAt: "2026-03-01T06:00:00Z", DrawnBy: "CLERK", SealNumber: "SEAL-192",
		Purpose: "DUPLICATE", DuplicatesSampleID: original.ID,
	}); err == nil {
		t.Error("a duplicate was drawn from milk the original never came from")
	}

	good, err := drawSample(t, p, drawSampleReq{
		Code: "SMP-193", SourceKind: original.SourceKind, SourceRef: original.SourceRef,
		DrawnAt: "2026-03-01T06:00:00Z", DrawnBy: "CLERK", SealNumber: "SEAL-193",
		Purpose: "DUPLICATE", DuplicatesSampleID: original.ID,
	})
	if err != nil {
		t.Fatalf("a proper duplicate was refused: %v", err)
	}
	if good.Purpose != "DUPLICATE" {
		t.Errorf("the duplicate reads as %s", good.Purpose)
	}

	// A duplicate that names nothing is not a duplicate.
	if _, err := drawSample(t, p, drawSampleReq{
		Code: "SMP-194", SourceKind: "COLLECTION", SourceRef: "COL-1",
		DrawnAt: "2026-03-01T06:00:00Z", DrawnBy: "CLERK", SealNumber: "SEAL-194",
		Purpose: "DUPLICATE",
	}); err == nil {
		t.Error("a duplicate was drawn naming no original")
	}
}

// Two bottles labelled the same is a result attached to the wrong producer, and
// nobody finds out.
func TestTwoSamplesCannotShareACode(t *testing.T) {
	p := startPlatform(t)
	paymentSample(t, p, "SMP-201")
	if _, err := drawSample(t, p, drawSampleReq{
		Code: "SMP-201", SourceKind: "COLLECTION", SourceRef: "COL-OTHER",
		DrawnAt: "2026-03-01T06:00:00Z", DrawnBy: "CLERK", SealNumber: "SEAL-X",
		Purpose: "PAYMENT",
	}); err == nil {
		t.Error("two samples were drawn with the same code on the bottle")
	}
}

// A reading whose stated resolution disagrees with how it is written is refused.
//
// "4.1" at a scale of two is a claim about the hundredths that the digits do not
// support, and a chart indexed at two decimal places would put it in a different
// cell from the one the analyst meant.
func TestAReadingMustAgreeWithTheResolutionItClaims(t *testing.T) {
	p := startPlatform(t)
	s := paymentSample(t, p, "SMP-211")

	in := fatOn(s.ID, "MA-1", "CLERK")
	in.Reading = readingProto{Value: "4.1", Scale: 2}
	if _, err := recordResult(t, p, in); err == nil {
		t.Error("a reading written to one decimal place was accepted as claiming two")
	}

	in.Reading = readingProto{Value: "4.10", Scale: 2}
	if _, err := recordResult(t, p, in); err != nil {
		t.Errorf("a reading that agrees with its scale was refused: %v", err)
	}
}

// A result timed before its sample was drawn is the commonest date-entry error
// in a laboratory book, and it puts a result against a sample that did not
// exist.
func TestAResultCannotBeReadBeforeItsSampleExists(t *testing.T) {
	p := startPlatform(t)
	s := paymentSample(t, p, "SMP-221")

	in := fatOn(s.ID, "MA-1", "CLERK")
	in.AnalysedAt = "2026-02-28T09:30:00Z"
	r, err := recordResult(t, p, in)
	if err != nil {
		// Refused by the database trigger is also correct; what must not happen
		// is its being accepted as eligible.
		if !strings.Contains(err.Error(), "before it exists") {
			t.Errorf("the refusal does not say what is wrong: %v", err)
		}
		return
	}
	if r.Eligibility == "ELIGIBLE" {
		t.Error("a result read the day before the sample was drawn reads as fit to pay on")
	}
}
