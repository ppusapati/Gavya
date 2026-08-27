//go:build e2e

// Batch genealogy, and the recall it exists for.
//
// A tanker turns out to have been contaminated. Which cartons contain it, and
// where did they go. The answer has to be complete, and where it cannot be
// complete it has to say so — because a partial list of affected cartons does
// not sit there being partial. It gets acted on: the ones named come off the
// shelf and the ones missed stay there with somebody's confidence behind them.
//
// These tests build a small plant — two tankers into a silo, the silo split
// into cream and skim, each of those into a product — and then ask it the
// questions a recall asks.
package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

const productionSvc = "production.v1.ProductionService"

// quantityProto and densityProto are material-service's, defined in
// material_test.go and used here unchanged. That they fit without a field
// changing is the point: one vocabulary for a measured amount across the
// platform, so a figure crossing between services keeps the unit and the scale
// it was written with.

type createBatchReq struct {
	TenantID        string        `json:"tenant_id"`
	Code            string        `json:"code"`
	Kind            string        `json:"kind"`
	ProductRef      string        `json:"product_ref"`
	Produced        quantityProto `json:"produced"`
	ProducedAt      string        `json:"produced_at"`
	ProducedBy      string        `json:"produced_by"`
	SourceKind      string        `json:"source_kind,omitempty"`
	SourceRef       string        `json:"source_ref,omitempty"`
	FormulationID   string        `json:"formulation_id,omitempty"`
	FormulationCode string        `json:"formulation_code,omitempty"`
	Status          string        `json:"status,omitempty"`
	StatusReason    string        `json:"status_reason,omitempty"`
	Actor           string        `json:"actor"`
}

type batchProto struct {
	ID            string        `json:"id"`
	Code          string        `json:"code"`
	Kind          string        `json:"kind"`
	ProductRef    string        `json:"product_ref"`
	Produced      quantityProto `json:"produced"`
	SourceKind    string        `json:"source_kind,omitempty"`
	SourceRef     string        `json:"source_ref,omitempty"`
	FormulationID string        `json:"formulation_id,omitempty"`
	Status        string        `json:"status"`
	StatusReason  string        `json:"status_reason,omitempty"`
	Held          bool          `json:"held"`
}

type batchResp struct {
	Batch *batchProto `json:"batch"`
}

type setStatusReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id,omitempty"`
	Code     string `json:"code,omitempty"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
	Actor    string `json:"actor"`
}

type recordInputReq struct {
	TenantID      string        `json:"tenant_id"`
	OutputBatchID string        `json:"output_batch_id,omitempty"`
	InputBatchID  string        `json:"input_batch_id,omitempty"`
	Consumed      quantityProto `json:"consumed"`
	Actor         string        `json:"actor"`
}

type inputProto struct {
	ID            string        `json:"id"`
	OutputBatchID string        `json:"output_batch_id"`
	InputBatchID  string        `json:"input_batch_id"`
	InputCode     string        `json:"input_code,omitempty"`
	Consumed      quantityProto `json:"consumed"`
}

type inputResp struct {
	Input     *inputProto   `json:"input"`
	Remaining quantityProto `json:"remaining"`
}

type genealogyReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id,omitempty"`
	Code     string `json:"code,omitempty"`
}

type genealogyResp struct {
	Batch     *batchProto   `json:"batch"`
	Inputs    []*inputProto `json:"inputs"`
	Remaining quantityProto `json:"remaining"`
}

type traceReq struct {
	TenantID   string `json:"tenant_id"`
	ID         string `json:"id,omitempty"`
	Code       string `json:"code,omitempty"`
	Direction  string `json:"direction"`
	MaxDepth   int32  `json:"max_depth,omitempty"`
	MaxBatches int32  `json:"max_batches,omitempty"`
}

type affectedProto struct {
	Batch *batchProto `json:"batch"`
	Depth int32       `json:"depth"`
	Via   string      `json:"via,omitempty"`
}

type traceResp struct {
	Batch      *batchProto     `json:"batch"`
	Direction  string          `json:"direction"`
	Affected   []affectedProto `json:"affected"`
	Finished   []affectedProto `json:"finished"`
	Complete   bool            `json:"complete"`
	Frontier   []string        `json:"frontier,omitempty"`
	Warning    string          `json:"warning,omitempty"`
	Unreadable []string        `json:"unreadable,omitempty"`
}

type yieldReq struct {
	TenantID     string        `json:"tenant_id"`
	ID           string        `json:"id,omitempty"`
	Code         string        `json:"code,omitempty"`
	Density      *densityProto `json:"density,omitempty"`
	RoundingMode string        `json:"rounding_mode,omitempty"`
}

type yieldResp struct {
	Batch                 *batchProto   `json:"batch"`
	Produced              quantityProto `json:"produced"`
	Consumed              quantityProto `json:"consumed"`
	ObservedPPM           *int64        `json:"observed_yield_ppm,omitempty"`
	ObservedPercent       string        `json:"observed_yield_percent,omitempty"`
	UnavailableReason     string        `json:"unavailable_reason,omitempty"`
	ExpectedPPM           *int64        `json:"expected_yield_ppm,omitempty"`
	VariancePPM           *int64        `json:"variance_ppm,omitempty"`
	VariancePercent       string        `json:"variance_percent,omitempty"`
	NoExpectationDeclared bool          `json:"no_expectation_declared"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func createBatch(t *testing.T, p *platform, in createBatchReq) (*batchProto, error) {
	t.Helper()
	in.TenantID = p.tenant
	if in.Actor == "" {
		in.Actor = "e2e"
	}
	if in.ProducedAt == "" {
		in.ProducedAt = "2026-04-01T06:00:00Z"
	}
	if in.ProducedBy == "" {
		in.ProducedBy = "PLANT"
	}
	resp, err := svcclient.Call[createBatchReq, batchResp](
		context.Background(), p.production(), productionSvc+"/CreateBatch", in, p.opts())
	if err != nil {
		return nil, err
	}
	return resp.Batch, nil
}

// rawMilk is a tanker's delivery: a batch like any other, pointing back out of
// the plant at the movement it arrived on.
func rawMilk(t *testing.T, p *platform, code string, litres string) *batchProto {
	t.Helper()
	b, err := createBatch(t, p, createBatchReq{
		Code: code, Kind: "RAW", ProductRef: "RAW_MILK",
		Produced:   quantityProto{Value: litres, Unit: "LITRES"},
		SourceKind: "MOVEMENT", SourceRef: "MV-" + code,
	})
	if err != nil {
		t.Fatalf("create raw batch %s: %v", code, err)
	}
	return b
}

func made(t *testing.T, p *platform, code, kind, product, value, unit string) *batchProto {
	t.Helper()
	b, err := createBatch(t, p, createBatchReq{
		Code: code, Kind: kind, ProductRef: product,
		Produced: quantityProto{Value: value, Unit: unit},
	})
	if err != nil {
		t.Fatalf("create %s batch %s: %v", kind, code, err)
	}
	return b
}

func recordInput(t *testing.T, p *platform, out, in *batchProto, value, unit string) (*inputResp, error) {
	t.Helper()
	return svcclient.Call[recordInputReq, inputResp](
		context.Background(), p.production(), productionSvc+"/RecordInput",
		recordInputReq{TenantID: p.tenant, OutputBatchID: out.ID, InputBatchID: in.ID,
			Consumed: quantityProto{Value: value, Unit: unit}, Actor: "e2e"}, p.opts())
}

func mustRecordInput(t *testing.T, p *platform, out, in *batchProto, value, unit string) *inputResp {
	t.Helper()
	r, err := recordInput(t, p, out, in, value, unit)
	if err != nil {
		t.Fatalf("RecordInput %s into %s: %v", in.Code, out.Code, err)
	}
	return r
}

func setStatus(t *testing.T, p *platform, b *batchProto, status, reason string) (*batchProto, error) {
	t.Helper()
	resp, err := svcclient.Call[setStatusReq, batchResp](
		context.Background(), p.production(), productionSvc+"/SetBatchStatus",
		setStatusReq{TenantID: p.tenant, ID: b.ID, Status: status, Reason: reason, Actor: "e2e"},
		p.opts())
	if err != nil {
		return nil, err
	}
	return resp.Batch, nil
}

func trace(t *testing.T, p *platform, in traceReq) *traceResp {
	t.Helper()
	in.TenantID = p.tenant
	resp, err := svcclient.Call[traceReq, traceResp](
		context.Background(), p.production(), productionSvc+"/TraceBatch", in, p.opts())
	if err != nil {
		t.Fatalf("TraceBatch: %v", err)
	}
	return resp
}

func batchYield(t *testing.T, p *platform, in yieldReq) *yieldResp {
	t.Helper()
	in.TenantID = p.tenant
	resp, err := svcclient.Call[yieldReq, yieldResp](
		context.Background(), p.production(), productionSvc+"/GetBatchYield", in, p.opts())
	if err != nil {
		t.Fatalf("GetBatchYield: %v", err)
	}
	return resp
}

func codesOf(list []affectedProto) []string {
	out := make([]string, 0, len(list))
	for _, a := range list {
		out = append(out, a.Batch.Code)
	}
	return out
}

func has(list []affectedProto, code string) *affectedProto {
	for i := range list {
		if list[i].Batch.Code == code {
			return &list[i]
		}
	}
	return nil
}

// plant builds a day's production and returns the batches by role.
//
//	TANKER-A ─┐
//	          ├─→ SILO ─┬─→ CREAM ─→ BUTTER   (finished)
//	TANKER-B ─┘         └─→ SKIM  ─→ POWDER   (finished)
//
// Only TANKER-A is contaminated in the tests below. TANKER-B is there because
// a recall that names it is recalling a producer's milk that was never in the
// carton, and a society that does that once is not trusted again.
type dayPlant struct {
	tankerA, tankerB  *batchProto
	silo, cream, skim *batchProto
	butter, powder    *batchProto
}

func buildPlant(t *testing.T, p *platform, tag string) *dayPlant {
	t.Helper()
	d := &dayPlant{
		tankerA: rawMilk(t, p, tag+"-TANKER-A", "5000.000"),
		tankerB: rawMilk(t, p, tag+"-TANKER-B", "5000.000"),
	}
	d.silo = made(t, p, tag+"-SILO", "INTERMEDIATE", "RAW_MILK", "10000.000", "LITRES")
	mustRecordInput(t, p, d.silo, d.tankerA, "5000.000", "LITRES")
	mustRecordInput(t, p, d.silo, d.tankerB, "5000.000", "LITRES")

	d.cream = made(t, p, tag+"-CREAM", "INTERMEDIATE", "CREAM", "1000.000", "LITRES")
	mustRecordInput(t, p, d.cream, d.silo, "4000.000", "LITRES")
	d.skim = made(t, p, tag+"-SKIM", "INTERMEDIATE", "SKIM", "6000.000", "LITRES")
	mustRecordInput(t, p, d.skim, d.silo, "6000.000", "LITRES")

	d.butter = made(t, p, tag+"-BUTTER", "FINISHED", "BUTTER", "400.000", "KILOGRAMS")
	mustRecordInput(t, p, d.butter, d.cream, "1000.000", "LITRES")
	d.powder = made(t, p, tag+"-POWDER", "FINISHED", "SMP", "540.000", "KILOGRAMS")
	mustRecordInput(t, p, d.powder, d.skim, "6000.000", "LITRES")
	return d
}

// ---------------------------------------------------------------------------
// The recall
// ---------------------------------------------------------------------------

// Forward from a contaminated tanker: everything it became, and specifically
// every finished lot that left the plant.
func TestRecallFindsEveryFinishedLotTheBadMilkReached(t *testing.T) {
	p := startPlatform(t)
	d := buildPlant(t, p, "R1")

	got := trace(t, p, traceReq{ID: d.tankerA.ID, Direction: "FORWARD"})
	if !got.Complete {
		t.Fatalf("the recall reported itself incomplete: %s (frontier %v)",
			got.Warning, got.Frontier)
	}

	for _, want := range []string{"R1-SILO", "R1-CREAM", "R1-SKIM", "R1-BUTTER", "R1-POWDER"} {
		if has(got.Affected, want) == nil {
			t.Errorf("the recall did not name %s; it is downstream of the contaminated tanker "+
				"and would stay on the shelf. Named: %v", want, codesOf(got.Affected))
		}
	}

	// The other producer's milk went into the same silo and did not come out of
	// this tanker. Naming it recalls milk that was never in the carton.
	if a := has(got.Affected, "R1-TANKER-B"); a != nil {
		t.Errorf("the recall named the other tanker at depth %d; it is upstream of the silo, "+
			"not downstream of this tanker", a.Depth)
	}

	// Finished is the list a distributor works down.
	finished := codesOf(got.Finished)
	if len(finished) != 2 {
		t.Fatalf("finished lots are %v, want the butter and the powder", finished)
	}
	for _, a := range got.Finished {
		if a.Batch.Kind != "FINISHED" {
			t.Errorf("%s is %s and appears in the finished list", a.Batch.Code, a.Batch.Kind)
		}
	}

	// Depths explain how far it travelled, which is what somebody reading the
	// report needs in order to believe it.
	if a := has(got.Affected, "R1-SILO"); a == nil || a.Depth != 1 {
		t.Errorf("the silo is one process step from the tanker; the report says %v", a)
	}
	if a := has(got.Affected, "R1-BUTTER"); a == nil || a.Depth != 3 {
		t.Errorf("the butter is three steps from the tanker (silo, cream, butter); "+
			"the report says %v", a)
	}
}

// Backward from a returned carton: what went into it, all the way to the
// tankers. This is the complaint question, and it has to reach past the plant
// gate — which is why raw milk is a batch and not something else.
func TestAReturnedCartonTracesBackToBothTankers(t *testing.T) {
	p := startPlatform(t)
	d := buildPlant(t, p, "R2")

	got := trace(t, p, traceReq{ID: d.butter.ID, Direction: "BACKWARD"})
	if !got.Complete {
		t.Fatalf("the trace reported itself incomplete: %s", got.Warning)
	}
	for _, want := range []string{"R2-CREAM", "R2-SILO", "R2-TANKER-A", "R2-TANKER-B"} {
		if has(got.Affected, want) == nil {
			t.Errorf("tracing back from the butter did not reach %s. Reached: %v",
				want, codesOf(got.Affected))
		}
	}
	// The powder came out of the same silo but is not something the butter was
	// made from. It is found by going forward from the silo, which is the next
	// question, not this one.
	if has(got.Affected, "R2-POWDER") != nil {
		t.Errorf("tracing back from the butter reached the powder; the powder is a sibling, "+
			"not an ancestor: %v", codesOf(got.Affected))
	}
	// Both tankers, and the raw batches still say where they came from.
	if a := has(got.Affected, "R2-TANKER-A"); a != nil && a.Batch.SourceRef == "" {
		t.Error("the raw batch lost its reference back to the movement it arrived on; the " +
			"genealogy stops at the plant gate, which is where a recall needs to keep going")
	}
}

// The two questions are different, and asking one must not answer the other.
func TestForwardAndBackwardAreNotTheSameQuestion(t *testing.T) {
	p := startPlatform(t)
	d := buildPlant(t, p, "R3")

	fwd := trace(t, p, traceReq{ID: d.silo.ID, Direction: "FORWARD"})
	back := trace(t, p, traceReq{ID: d.silo.ID, Direction: "BACKWARD"})

	if has(fwd.Affected, "R3-TANKER-A") != nil {
		t.Errorf("forward from the silo named a tanker: %v", codesOf(fwd.Affected))
	}
	if has(back.Affected, "R3-BUTTER") != nil {
		t.Errorf("backward from the silo named the butter: %v", codesOf(back.Affected))
	}
	if len(back.Affected) != 2 {
		t.Errorf("backward from the silo reached %v, want the two tankers", codesOf(back.Affected))
	}
}

// A recall stopped by its bound says so, and says where to continue.
//
// This is the assertion the whole service exists for. Everything else is
// bookkeeping; this is the difference between an incomplete answer and a wrong
// one.
func TestATruncatedRecallDoesNotLookLikeACompleteOne(t *testing.T) {
	p := startPlatform(t)
	d := buildPlant(t, p, "R4")

	got := trace(t, p, traceReq{
		ID: d.tankerA.ID, Direction: "FORWARD", MaxDepth: 2, MaxBatches: 100,
	})
	if got.Complete {
		t.Fatal("a recall bounded two steps short of the finished product reported itself " +
			"complete; the butter and the powder are on shelves and nothing in this answer " +
			"says the walk stopped before it reached them")
	}
	if got.Warning == "" {
		t.Error("an incomplete recall must carry a warning a person can read at the top of the " +
			"report")
	}
	if len(got.Frontier) == 0 {
		t.Error("an incomplete recall must name where to continue; without it somebody " +
			"finishing the job by hand has nowhere to start")
	}
	// It did reach the intermediates, and it must not have named the finished
	// lots — otherwise the bound did nothing and the test proves nothing.
	if has(got.Affected, "R4-CREAM") == nil {
		t.Errorf("two steps should reach the cream: %v", codesOf(got.Affected))
	}
	if has(got.Affected, "R4-BUTTER") != nil {
		t.Errorf("two steps should not reach the butter: %v", codesOf(got.Affected))
	}

	// Given room, the same walk finishes and finds them.
	full := trace(t, p, traceReq{
		ID: d.tankerA.ID, Direction: "FORWARD", MaxDepth: 20, MaxBatches: 100,
	})
	if !full.Complete {
		t.Fatalf("the unbounded walk still reported itself incomplete: %s", full.Warning)
	}
	if has(full.Affected, "R4-BUTTER") == nil {
		t.Errorf("with room to finish, the recall still missed the butter: %v",
			codesOf(full.Affected))
	}
}

// Half a bound is refused. A caller that gave one and forgot the other would be
// unbounded in the direction it did not think about.
func TestHalfABoundIsRefused(t *testing.T) {
	p := startPlatform(t)
	d := buildPlant(t, p, "R5")
	_, err := svcclient.Call[traceReq, traceResp](
		context.Background(), p.production(), productionSvc+"/TraceBatch",
		traceReq{TenantID: p.tenant, ID: d.silo.ID, Direction: "FORWARD", MaxDepth: 3},
		p.opts())
	if err == nil {
		t.Error("a trace with a depth bound and no batch bound was accepted")
	}
}

func TestATraceMustSayWhichWayItGoes(t *testing.T) {
	p := startPlatform(t)
	d := buildPlant(t, p, "R6")
	_, err := svcclient.Call[traceReq, traceResp](
		context.Background(), p.production(), productionSvc+"/TraceBatch",
		traceReq{TenantID: p.tenant, ID: d.silo.ID}, p.opts())
	if err == nil {
		t.Error("a trace with no direction was accepted; it would answer one of the two " +
			"questions and the caller would not know which")
	}
}

// ---------------------------------------------------------------------------
// What the genealogy refuses
// ---------------------------------------------------------------------------

// A quarantined lot stops moving. Finding the cartons is half a recall; the
// other half is that the milk does not become anything else while the phone
// calls are being made.
func TestAQuarantinedLotCannotBeFedIntoAnything(t *testing.T) {
	p := startPlatform(t)

	// A lot with room left in it, so that anything refused below is refused for
	// the hold and not because the vessel happens to be empty. The silo the
	// plant builder makes is drawn down to nothing, and running this against it
	// would prove the overdraw rule twice and the hold rule never.
	cream := made(t, p, "Q1-CREAM", "INTERMEDIATE", "CREAM", "1000.000", "KILOGRAMS")
	butter := made(t, p, "Q1-BUTTER", "FINISHED", "BUTTER", "300.000", "KILOGRAMS")
	mustRecordInput(t, p, butter, cream, "700.000", "KILOGRAMS")

	if _, err := setStatus(t, p, cream, "QUARANTINED", "antibiotic screen positive"); err != nil {
		t.Fatalf("quarantine: %v", err)
	}

	ghee := made(t, p, "Q1-GHEE", "FINISHED", "GHEE", "50.000", "KILOGRAMS")
	_, err := recordInput(t, p, ghee, cream, "100.000", "KILOGRAMS")
	if err == nil {
		t.Fatal("a quarantined lot was churned into ghee; a hold that does not stop the lot " +
			"moving is not a hold")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "quarantined") {
		t.Errorf("the refusal was %q; it should name the hold so whoever hit it knows what to "+
			"do about it", err)
	}

	// The lines recorded before the hold are untouched. They are the evidence of
	// where the milk already went, which is the one thing the recall needs.
	before := trace(t, p, traceReq{ID: cream.ID, Direction: "FORWARD"})
	if has(before.Affected, "Q1-BUTTER") == nil {
		t.Errorf("quarantining the lot lost the record of what it had already become: %v",
			codesOf(before.Affected))
	}

	// And there is a way out, deliberately: lift the hold, with a reason. Three
	// hundred kilograms are left, so this line is refused only if the hold is
	// still in force.
	if _, err := setStatus(t, p, cream, "RELEASED", "screen repeated, negative"); err != nil {
		t.Fatalf("lifting the hold: %v", err)
	}
	if _, err := recordInput(t, p, ghee, cream, "100.000", "KILOGRAMS"); err != nil {
		t.Fatalf("after the hold was lifted the lot was still refused: %v", err)
	}
}

// Lifting a hold has to say why. A quarantine that was lifted is exactly the
// thing somebody asks about afterwards.
func TestLiftingAHoldMustSayWhy(t *testing.T) {
	p := startPlatform(t)
	b := made(t, p, "Q2-VAT", "INTERMEDIATE", "CURD", "100.000", "KILOGRAMS")
	if _, err := setStatus(t, p, b, "QUARANTINED", "smells wrong"); err != nil {
		t.Fatalf("quarantine: %v", err)
	}
	if _, err := setStatus(t, p, b, "RELEASED", ""); err == nil {
		t.Error("a quarantine was lifted with no explanation; nobody can defend that afterwards")
	}
}

func TestAHoldMustSayWhy(t *testing.T) {
	p := startPlatform(t)
	b := made(t, p, "Q3-VAT", "INTERMEDIATE", "CURD", "100.000", "KILOGRAMS")
	if _, err := setStatus(t, p, b, "RECALLED", ""); err == nil {
		t.Error("a recall was raised with no reason on it; it cannot be explained to whoever " +
			"receives the phone call")
	}
}

// A loop in the genealogy is refused. A recall that walked into one would give
// up at some depth and report a partial answer as a complete one.
func TestABatchCannotBecomeItsOwnAncestor(t *testing.T) {
	p := startPlatform(t)
	d := buildPlant(t, p, "C1")

	// The silo is upstream of the butter. Feeding the butter back into the silo
	// would close the loop.
	_, err := recordInput(t, p, d.silo, d.butter, "10.000", "LITRES")
	if err == nil {
		t.Fatal("a loop was accepted into the genealogy")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "loop") &&
		!strings.Contains(strings.ToLower(err.Error()), "upstream") {
		t.Errorf("the refusal was %q; it should explain that this would put a loop in the "+
			"genealogy", err)
	}

	// A recall still works afterwards, which is the point of refusing it.
	got := trace(t, p, traceReq{ID: d.tankerA.ID, Direction: "FORWARD"})
	if !got.Complete {
		t.Errorf("the recall is incomplete after a refused loop: %s", got.Warning)
	}
}

// A lot cannot give up more than it holds. Five hundred kilograms drawn from a
// four hundred kilogram lot is milk from nowhere, and it makes every yield
// computed downstream look better than it was.
func TestALotCannotGiveUpMoreThanItHolds(t *testing.T) {
	p := startPlatform(t)
	src := made(t, p, "O1-VAT", "INTERMEDIATE", "CURD", "400.000", "KILOGRAMS")
	out := made(t, p, "O1-PANEER", "FINISHED", "PANEER", "100.000", "KILOGRAMS")

	if _, err := recordInput(t, p, out, src, "500.000", "KILOGRAMS"); err == nil {
		t.Fatal("five hundred kilograms were drawn from a four hundred kilogram lot")
	}

	// Up to the whole lot is fine, and the response says what is left.
	r := mustRecordInput(t, p, out, src, "400.000", "KILOGRAMS")
	if r.Remaining.Value != "0.000" {
		t.Errorf("after drawing the whole lot, %s remains", r.Remaining.Value)
	}

	// And nothing more comes out of it.
	out2 := made(t, p, "O1-PANEER-2", "FINISHED", "PANEER", "10.000", "KILOGRAMS")
	if _, err := recordInput(t, p, out2, src, "1.000", "KILOGRAMS"); err == nil {
		t.Error("a further kilogram came out of a lot that was already empty")
	}
}

// Litres consumed from a lot held in kilograms is refused rather than converted.
// Converting needs a density, and the trigger has none — the three per cent
// everybody quotes is not something this platform invents on somebody's behalf.
func TestConsumingInTheWrongUnitIsRefusedNotConverted(t *testing.T) {
	p := startPlatform(t)
	src := made(t, p, "U1-CREAM", "INTERMEDIATE", "CREAM", "400.000", "KILOGRAMS")
	out := made(t, p, "U1-GHEE", "FINISHED", "GHEE", "100.000", "KILOGRAMS")
	_, err := recordInput(t, p, out, src, "100.000", "LITRES")
	if err == nil {
		t.Fatal("litres were drawn from a lot held in kilograms and something converted them")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "density") {
		t.Errorf("the refusal was %q; it should say a density is what is missing", err)
	}
}

// The same lot twice into one vessel double-counts it in the yield and in every
// recall that traverses it.
func TestOneLinePerLotPerVessel(t *testing.T) {
	p := startPlatform(t)
	src := made(t, p, "D1-CREAM", "INTERMEDIATE", "CREAM", "400.000", "KILOGRAMS")
	out := made(t, p, "D1-GHEE", "FINISHED", "GHEE", "100.000", "KILOGRAMS")
	mustRecordInput(t, p, out, src, "100.000", "KILOGRAMS")
	if _, err := recordInput(t, p, out, src, "100.000", "KILOGRAMS"); err == nil {
		t.Error("the same lot was recorded into the same vessel twice")
	}
}

// Raw milk says where it came from, or it is not accepted. Without it the
// genealogy stops at the plant gate.
func TestRawMilkMustPointBackOutOfThePlant(t *testing.T) {
	p := startPlatform(t)
	_, err := createBatch(t, p, createBatchReq{
		Code: "S1-ORPHAN", Kind: "RAW", ProductRef: "RAW_MILK",
		Produced: quantityProto{Value: "1000.000", Unit: "LITRES"},
	})
	if err == nil {
		t.Error("a raw batch that does not say where it came from was accepted; a recall " +
			"reaching it has nowhere to go next")
	}
}

// ---------------------------------------------------------------------------
// Yield
// ---------------------------------------------------------------------------

// Yield is observed. Where the plant declared nothing to expect, the report
// says so rather than showing a variance against a number nobody stated.
func TestYieldIsObservedAndSaysWhenNothingWasExpected(t *testing.T) {
	p := startPlatform(t)
	src := made(t, p, "Y1-MILK", "INTERMEDIATE", "RAW_MILK", "6000.000", "KILOGRAMS")
	pan := made(t, p, "Y1-PANEER", "FINISHED", "PANEER", "1000.000", "KILOGRAMS")
	mustRecordInput(t, p, pan, src, "6000.000", "KILOGRAMS")

	got := batchYield(t, p, yieldReq{ID: pan.ID})
	if got.ObservedPPM == nil {
		t.Fatalf("no observed yield: %s", got.UnavailableReason)
	}
	if *got.ObservedPPM != 166666 {
		t.Errorf("observed %d ppm, want 166666 — one kilogram of paneer from six of milk",
			*got.ObservedPPM)
	}
	if got.ObservedPercent != "16.6666%" {
		t.Errorf("rendered as %q", got.ObservedPercent)
	}
	if !got.NoExpectationDeclared {
		t.Error("no expectation was declared and the report does not say so")
	}
	if got.VariancePPM != nil {
		t.Errorf("a variance of %d against an expectation nobody stated", *got.VariancePPM)
	}
}

// Where the plant did declare one, the variance is against that figure and its
// sign says which way the vat went.
//
// The target comes off the recipe the batch followed, which is the only place
// it lives. There is no way to type one onto a vat, and that is the point: a
// figure retyped per vat by whoever is on shift drifts, and nobody can say
// afterwards when it started.
func TestVarianceIsAgainstTheFigureThePlantDeclared(t *testing.T) {
	p := startPlatform(t)
	expected := int64(180000)
	f := mustCreateFormulation(t, p, createFormulationReq{
		Code: "Y2-PANEER-STD", OutputProductRef: "PANEER", OutputUnit: "KILOGRAMS",
		ExpectedYieldPPM: &expected, ExpectationBasis: "median of the 2025 season",
	})
	src := made(t, p, "Y2-MILK", "INTERMEDIATE", "RAW_MILK", "6000.000", "KILOGRAMS")
	pan, err := createBatch(t, p, createBatchReq{
		Code: "Y2-PANEER", Kind: "FINISHED", ProductRef: "PANEER",
		Produced:      quantityProto{Value: "1000.000", Unit: "KILOGRAMS"},
		FormulationID: f.ID,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	mustRecordInput(t, p, pan, src, "6000.000", "KILOGRAMS")

	got := batchYield(t, p, yieldReq{ID: pan.ID})
	if got.NoExpectationDeclared {
		t.Error("an expectation was declared and the report says none was")
	}
	if got.VariancePPM == nil {
		t.Fatal("no variance against a declared expectation")
	}
	if *got.VariancePPM != -13334 {
		t.Errorf("variance %d ppm, want -13334; the vat came up short and the sign is what "+
			"says so", *got.VariancePPM)
	}
	if got.VariancePercent != "-1.3334%" {
		t.Errorf("variance rendered as %q", got.VariancePercent)
	}
}

// Milk measured in litres, product weighed in kilograms, no density supplied:
// no yield, and a reason. This is every plant, every day.
func TestNoYieldWithoutADensityAndNeverAZero(t *testing.T) {
	p := startPlatform(t)
	d := buildPlant(t, p, "Y3")

	got := batchYield(t, p, yieldReq{ID: d.butter.ID})
	if got.ObservedPPM != nil {
		t.Fatalf("a yield of %d ppm was computed from litres against kilograms with no density",
			*got.ObservedPPM)
	}
	if got.UnavailableReason == "" {
		t.Fatal("no yield and no reason; a blank in a variance report is read as a zero")
	}
	if !strings.Contains(got.UnavailableReason, "density") {
		t.Errorf("reason %q does not say a density is what is missing", got.UnavailableReason)
	}

	// Supply one and the yield is real — and different from what the same
	// numbers give if the density is ignored.
	withDensity := batchYield(t, p, yieldReq{
		ID:           d.butter.ID,
		Density:      &densityProto{KgPerLitre: "1.030", Scale: 3, AtCelsius: 200, Source: "LACTOMETER"},
		RoundingMode: "HALF_UP",
	})
	if withDensity.ObservedPPM == nil {
		t.Fatalf("no yield with a density supplied: %s", withDensity.UnavailableReason)
	}
	// 1000.000 L of cream at 1.030 is 1030.000 kg; 400.000 kg of butter out of
	// that is 388349 ppm. Ignoring the density would give 400000.
	if *withDensity.ObservedPPM != 388349 {
		t.Errorf("observed %d ppm; want 388349. 400000 would mean the density was not applied",
			*withDensity.ObservedPPM)
	}
	if withDensity.Consumed.Value != "1030.000" {
		t.Errorf("consumed %s after conversion, want 1030.000", withDensity.Consumed.Value)
	}
}

// A density with no rounding mode is refused. The choice shows up in the
// variance, and defaulting it would be the platform deciding which way.
func TestADensityWithoutARoundingModeIsRefused(t *testing.T) {
	p := startPlatform(t)
	d := buildPlant(t, p, "Y4")
	_, err := svcclient.Call[yieldReq, yieldResp](
		context.Background(), p.production(), productionSvc+"/GetBatchYield",
		yieldReq{TenantID: p.tenant, ID: d.butter.ID,
			Density: &densityProto{KgPerLitre: "1.030", Scale: 3, AtCelsius: 200,
				Source: "LACTOMETER"}},
		p.opts())
	if err == nil {
		t.Error("a conversion was performed without anybody saying how it rounds")
	}
}

// ---------------------------------------------------------------------------
// One hop
// ---------------------------------------------------------------------------

// GetBatchGenealogy answers the question a person at a terminal asks: what went
// into this vessel, and how much is left of it.
func TestOneHopGenealogyNamesTheLotsAndWhatIsLeft(t *testing.T) {
	p := startPlatform(t)
	d := buildPlant(t, p, "G1")

	resp, err := svcclient.Call[genealogyReq, genealogyResp](
		context.Background(), p.production(), productionSvc+"/GetBatchGenealogy",
		genealogyReq{TenantID: p.tenant, Code: "G1-SILO"}, p.opts())
	if err != nil {
		t.Fatalf("GetBatchGenealogy: %v", err)
	}
	if len(resp.Inputs) != 2 {
		t.Fatalf("the silo took two tankers and the genealogy shows %d", len(resp.Inputs))
	}
	// The codes are what a person reads, so they are carried rather than left
	// as identifiers nobody can look at.
	for _, in := range resp.Inputs {
		if in.InputCode == "" {
			t.Errorf("input %s carries no code; an identifier is not something anybody can "+
				"read off a vessel", in.InputBatchID)
		}
	}
	// 10000 produced, 4000 to cream and 6000 to skim: nothing left.
	if resp.Remaining.Value != "0.000" {
		t.Errorf("the silo has %s left after the cream and the skim were drawn off",
			resp.Remaining.Value)
	}
	_ = d
}

// A batch is findable by the code painted on it, which is what somebody
// standing in a plant actually has.
func TestABatchIsFoundByTheCodeOnTheVessel(t *testing.T) {
	p := startPlatform(t)
	buildPlant(t, p, "G2")
	got := trace(t, p, traceReq{Code: "G2-TANKER-A", Direction: "FORWARD"})
	if got.Batch.Code != "G2-TANKER-A" {
		t.Fatalf("looked up by code and got %s", got.Batch.Code)
	}
	if has(got.Affected, "G2-BUTTER") == nil {
		t.Errorf("the recall by code missed the butter: %v", codesOf(got.Affected))
	}
}

// Two vessels with the same code is a recall that reaches the wrong one, and
// the wrong one is on a lorry.
func TestTwoVesselsCannotShareACode(t *testing.T) {
	p := startPlatform(t)
	made(t, p, "G3-VAT", "INTERMEDIATE", "CURD", "100.000", "KILOGRAMS")
	_, err := createBatch(t, p, createBatchReq{
		Code: "G3-VAT", Kind: "INTERMEDIATE", ProductRef: "CURD",
		Produced: quantityProto{Value: "100.000", Unit: "KILOGRAMS"},
	})
	if err == nil {
		t.Error("two vessels were labelled the same")
	}
}

// ---------------------------------------------------------------------------
// Recipes
// ---------------------------------------------------------------------------

type formulationInputProto struct {
	ProductRef        string `json:"product_ref"`
	ExpectedSharePPM  *int64 `json:"expected_share_ppm,omitempty"`
	ShareTolerancePPM *int64 `json:"share_tolerance_ppm,omitempty"`
	Required          bool   `json:"required"`
}

type formulationProto struct {
	ID                  string `json:"id"`
	Status              string `json:"status"`
	ApprovedBy          string `json:"approved_by,omitempty"`
	ApprovedAt          string `json:"approved_at,omitempty"`
	UsableForProduction bool   `json:"usable_for_production"`
	WithdrawnReason     string `json:"withdrawn_reason,omitempty"`

	Code             string                  `json:"code"`
	Name             string                  `json:"name"`
	OutputProductRef string                  `json:"output_product_ref"`
	OutputUnit       string                  `json:"output_unit"`
	ExpectedYieldPPM *int64                  `json:"expected_yield_ppm,omitempty"`
	ExpectedPercent  string                  `json:"expected_yield_percent,omitempty"`
	ExpectationBasis string                  `json:"expectation_basis,omitempty"`
	ValidFrom        string                  `json:"valid_from"`
	ValidTo          string                  `json:"valid_to,omitempty"`
	Inputs           []formulationInputProto `json:"inputs,omitempty"`
}

type createFormulationReq struct {
	TenantID         string                  `json:"tenant_id"`
	Code             string                  `json:"code"`
	Name             string                  `json:"name"`
	OutputProductRef string                  `json:"output_product_ref"`
	OutputUnit       string                  `json:"output_unit"`
	ExpectedYieldPPM *int64                  `json:"expected_yield_ppm,omitempty"`
	ExpectationBasis string                  `json:"expectation_basis,omitempty"`
	ValidFrom        string                  `json:"valid_from"`
	ValidTo          string                  `json:"valid_to,omitempty"`
	Inputs           []formulationInputProto `json:"inputs,omitempty"`
	Actor            string                  `json:"actor"`
}

type formulationResp struct {
	Formulation *formulationProto `json:"formulation"`
}

type checkRecipeReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id,omitempty"`
	Code     string `json:"code,omitempty"`
}

type findingProto struct {
	Kind        string `json:"kind"`
	ProductRef  string `json:"product_ref"`
	Serious     bool   `json:"serious"`
	Explanation string `json:"explanation"`
}

type shareReadingProto struct {
	ProductRef       string `json:"product_ref"`
	ExpectedSharePPM int64  `json:"expected_share_ppm"`
	ObservedSharePPM int64  `json:"observed_share_ppm"`
	DifferencePPM    int64  `json:"difference_ppm"`
	TolerancePPM     *int64 `json:"tolerance_ppm,omitempty"`
}

type checkRecipeResp struct {
	Findings                []findingProto      `json:"findings"`
	SeriousCount            int32               `json:"serious_count"`
	Shares                  []shareReadingProto `json:"shares"`
	SharesUnavailableReason string              `json:"shares_unavailable_reason,omitempty"`
}

type observedYieldReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id,omitempty"`
	Code     string `json:"code,omitempty"`
	At       string `json:"at,omitempty"`
}

type observedYieldResp struct {
	Code                   string `json:"code"`
	BatchesCounted         int64  `json:"batches_counted"`
	BatchesNeedingADensity int64  `json:"batches_needing_a_density"`
	LowestPPM              *int64 `json:"lowest_ppm,omitempty"`
	LowerQuartilePPM       *int64 `json:"lower_quartile_ppm,omitempty"`
	MedianPPM              *int64 `json:"median_ppm,omitempty"`
	UpperQuartilePPM       *int64 `json:"upper_quartile_ppm,omitempty"`
	HighestPPM             *int64 `json:"highest_ppm,omitempty"`
	ExpectedPPM            *int64 `json:"expected_yield_ppm,omitempty"`
	ExpectationBasis       string `json:"expectation_basis,omitempty"`
	Note                   string `json:"note"`
}

type approveFormulationReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
	Approver string `json:"approver"`
	At       string `json:"at,omitempty"`
	Note     string `json:"note,omitempty"`
}

type withdrawFormulationReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
	Reason   string `json:"reason"`
	Actor    string `json:"actor"`
}

func approveFormulation(t *testing.T, p *platform, f *formulationProto, at string) (*formulationProto, error) {
	t.Helper()
	resp, err := svcclient.Call[approveFormulationReq, formulationResp](
		context.Background(), p.production(), productionSvc+"/ApproveFormulation",
		approveFormulationReq{TenantID: p.tenant, ID: f.ID, Approver: "plant.manager",
			At: at, Note: "e2e"}, p.opts())
	if err != nil {
		return nil, err
	}
	return resp.Formulation, nil
}

func withdrawFormulation(t *testing.T, p *platform, f *formulationProto, reason string) (*formulationProto, error) {
	t.Helper()
	resp, err := svcclient.Call[withdrawFormulationReq, formulationResp](
		context.Background(), p.production(), productionSvc+"/WithdrawFormulation",
		withdrawFormulationReq{TenantID: p.tenant, ID: f.ID, Reason: reason,
			Actor: "e2e"}, p.opts())
	if err != nil {
		return nil, err
	}
	return resp.Formulation, nil
}

func createFormulation(t *testing.T, p *platform, in createFormulationReq) (*formulationProto, error) {
	t.Helper()
	in.TenantID = p.tenant
	if in.Actor == "" {
		in.Actor = "e2e"
	}
	if in.Name == "" {
		in.Name = in.Code
	}
	if in.ValidFrom == "" {
		in.ValidFrom = "2026-01-01T00:00:00Z"
	}
	resp, err := svcclient.Call[createFormulationReq, formulationResp](
		context.Background(), p.production(), productionSvc+"/CreateFormulation", in, p.opts())
	if err != nil {
		return nil, err
	}
	return resp.Formulation, nil
}

// mustCreateFormulation creates a recipe and signs it off, because almost every
// test here needs one a batch can actually be made against.
//
// The approval is stamped at the recipe's own valid_from rather than now, so a
// recipe that came into force in January is approved in January. The tests below
// make batches in the periods these recipes cover, and a recipe approved in
// 2026-08 that came into force in 2026-01 would be a recipe nobody had signed
// off when the milk was made.
func mustCreateFormulation(t *testing.T, p *platform, in createFormulationReq) *formulationProto {
	t.Helper()
	f, err := createFormulation(t, p, in)
	if err != nil {
		t.Fatalf("CreateFormulation %s: %v", in.Code, err)
	}
	approved, err := approveFormulation(t, p, f, f.ValidFrom)
	if err != nil {
		t.Fatalf("ApproveFormulation %s: %v", in.Code, err)
	}
	return approved
}

// mustCreateDraft leaves it unapproved, for the tests that are about that.
func mustCreateDraft(t *testing.T, p *platform, in createFormulationReq) *formulationProto {
	t.Helper()
	f, err := createFormulation(t, p, in)
	if err != nil {
		t.Fatalf("CreateFormulation %s: %v", in.Code, err)
	}
	return f
}

func checkRecipe(t *testing.T, p *platform, b *batchProto) *checkRecipeResp {
	t.Helper()
	resp, err := svcclient.Call[checkRecipeReq, checkRecipeResp](
		context.Background(), p.production(), productionSvc+"/CheckRecipe",
		checkRecipeReq{TenantID: p.tenant, ID: b.ID}, p.opts())
	if err != nil {
		t.Fatalf("CheckRecipe %s: %v", b.Code, err)
	}
	return resp
}

func observedYield(t *testing.T, p *platform, f *formulationProto) *observedYieldResp {
	t.Helper()
	resp, err := svcclient.Call[observedYieldReq, observedYieldResp](
		context.Background(), p.production(), productionSvc+"/GetObservedYield",
		observedYieldReq{TenantID: p.tenant, ID: f.ID}, p.opts())
	if err != nil {
		t.Fatalf("GetObservedYield %s: %v", f.Code, err)
	}
	return resp
}

func finding(r *checkRecipeResp, kind, product string) *findingProto {
	for i := range r.Findings {
		if r.Findings[i].Kind == kind && r.Findings[i].ProductRef == product {
			return &r.Findings[i]
		}
	}
	return nil
}

// A declared target has to say where it came from.
func TestADeclaredTargetMustSayWhereItCameFrom(t *testing.T) {
	p := startPlatform(t)
	target := int64(180000)
	_, err := createFormulation(t, p, createFormulationReq{
		Code: "F1-PANEER", OutputProductRef: "PANEER", OutputUnit: "KILOGRAMS",
		ExpectedYieldPPM: &target,
	})
	if err == nil {
		t.Error("a target with no provenance was accepted; a figure derived from a plant's own " +
			"vats and one read off a supplier's leaflet are different claims and a variance " +
			"report that shows them alike invites the same argument every month")
	}

	f := mustCreateFormulation(t, p, createFormulationReq{
		Code: "F1-PANEER", OutputProductRef: "PANEER", OutputUnit: "KILOGRAMS",
		ExpectedYieldPPM: &target, ExpectationBasis: "median of 214 vats, Jan-Jun 2026",
	})
	if f.ExpectationBasis == "" {
		t.Error("the provenance was accepted and then dropped")
	}
	if f.ExpectedPercent != "18.0000%" {
		t.Errorf("the target renders as %q", f.ExpectedPercent)
	}
}

// Two versions of one recipe cannot be in force at once. Resolved later — by
// taking the newest, say — the choice is invisible, and the plant finds out
// when two vats of the same product report variances against different targets.
func TestTwoApprovedVersionsOfARecipeCannotBeInForceAtOnce(t *testing.T) {
	p := startPlatform(t)
	mustCreateFormulation(t, p, createFormulationReq{
		Code: "F2-CURD", OutputProductRef: "CURD", OutputUnit: "KILOGRAMS",
		ValidFrom: "2026-01-01T00:00:00Z", ValidTo: "2026-04-01T00:00:00Z",
	})

	// A draft over the same period is fine — a plant must be able to write a
	// correction to a recipe already in force. Two of them are fine too: a draft
	// is a piece of paper on somebody's desk.
	overlapping, err := createFormulation(t, p, createFormulationReq{
		Code: "F2-CURD", OutputProductRef: "CURD", OutputUnit: "KILOGRAMS",
		ValidFrom: "2026-03-01T00:00:00Z", ValidTo: "2026-05-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("a draft covering a period already in force was refused (%v); a plant could "+
			"then never write a correction to the recipe it is currently running", err)
	}
	if overlapping.Status != "DRAFT" {
		t.Errorf("a newly created recipe is %q; creating one is not signing it off",
			overlapping.Status)
	}
	if overlapping.UsableForProduction {
		t.Error("a draft reports itself usable for production")
	}

	// Approving it is where the conflict surfaces — at the moment somebody
	// commits to it, which is when there is a decision to make.
	if _, err := approveFormulation(t, p, overlapping, "2026-03-01T00:00:00Z"); err == nil {
		t.Error("a second version was approved over a period another approved version already " +
			"covers; two targets in force on one day means the variance depends on which row " +
			"was read first")
	}

	// Abutting is how a plant actually replaces one, and must be accepted.
	next := mustCreateDraft(t, p, createFormulationReq{
		Code: "F2-CURD", OutputProductRef: "CURD", OutputUnit: "KILOGRAMS",
		ValidFrom: "2026-04-01T00:00:00Z",
	})
	if _, err := approveFormulation(t, p, next, "2026-04-01T00:00:00Z"); err != nil {
		t.Errorf("the replacement starting the day the old one ended was refused: %v", err)
	}
}

// Creating a recipe does not sign it off, and a batch may only be made against
// one somebody has.
//
// A vat measured against a target nobody agreed to reports a variance saying the
// process is wrong, when what is wrong is that the number is still a draft.
func TestABatchMayOnlyBeMadeAgainstAnApprovedRecipe(t *testing.T) {
	p := startPlatform(t)

	draft := mustCreateDraft(t, p, createFormulationReq{
		Code: "FA1-PANEER", OutputProductRef: "PANEER", OutputUnit: "KILOGRAMS",
	})
	if draft.Status != "DRAFT" || draft.UsableForProduction {
		t.Fatalf("a newly created recipe is %q, usable=%v", draft.Status,
			draft.UsableForProduction)
	}

	if _, err := createBatch(t, p, createBatchReq{
		Code: "FA1-VAT", Kind: "FINISHED", ProductRef: "PANEER",
		Produced:   quantityProto{Value: "100.000", Unit: "KILOGRAMS"},
		ProducedAt: "2026-02-01T06:00:00Z", FormulationID: draft.ID,
	}); err == nil {
		t.Error("a vat was made against a recipe still in draft")
	}

	// Signed off, it works — and says who signed it.
	approved, err := approveFormulation(t, p, draft, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Status != "APPROVED" || !approved.UsableForProduction {
		t.Errorf("after approval the recipe is %q, usable=%v", approved.Status,
			approved.UsableForProduction)
	}
	if approved.ApprovedBy == "" || approved.ApprovedAt == "" {
		t.Errorf("the approval carries no name or no time: by=%q at=%q",
			approved.ApprovedBy, approved.ApprovedAt)
	}
	if _, err := createBatch(t, p, createBatchReq{
		Code: "FA1-VAT", Kind: "FINISHED", ProductRef: "PANEER",
		Produced:   quantityProto{Value: "100.000", Unit: "KILOGRAMS"},
		ProducedAt: "2026-02-01T06:00:00Z", FormulationID: approved.ID,
	}); err != nil {
		t.Errorf("a vat against an approved recipe was refused: %v", err)
	}
}

// Withdrawing stops new production without disturbing what was already made.
//
// That second half is the one that matters: a recall traverses batches made
// under recipes the plant has since stopped using, and it is often the change of
// recipe that prompted the recall.
func TestWithdrawingARecipeStopsNewBatchesAndLeavesOldOnesAlone(t *testing.T) {
	p := startPlatform(t)

	f := mustCreateFormulation(t, p, createFormulationReq{
		Code: "FA2-CURD", OutputProductRef: "CURD", OutputUnit: "KILOGRAMS",
	})
	made, err := createBatch(t, p, createBatchReq{
		Code: "FA2-VAT", Kind: "FINISHED", ProductRef: "CURD",
		Produced:   quantityProto{Value: "100.000", Unit: "KILOGRAMS"},
		ProducedAt: "2026-02-01T06:00:00Z", FormulationID: f.ID,
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}

	if _, err := withdrawFormulation(t, p, f, ""); err == nil {
		t.Error("a recipe was withdrawn with no reason; nobody could explain reintroducing it")
	}
	gone, err := withdrawFormulation(t, p, f, "coagulant discontinued")
	if err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if gone.Status != "WITHDRAWN" || gone.UsableForProduction {
		t.Errorf("after withdrawal the recipe is %q, usable=%v", gone.Status,
			gone.UsableForProduction)
	}
	if gone.WithdrawnReason == "" {
		t.Error("the withdrawal carries no reason")
	}

	// No new batch under it.
	if _, err := createBatch(t, p, createBatchReq{
		Code: "FA2-VAT-2", Kind: "FINISHED", ProductRef: "CURD",
		Produced:   quantityProto{Value: "100.000", Unit: "KILOGRAMS"},
		ProducedAt: "2026-02-02T06:00:00Z", FormulationID: f.ID,
	}); err == nil {
		t.Error("a vat was made under a withdrawn recipe")
	}

	// But the batch already made under it is untouched, and can still be
	// quarantined — which is the case that matters, because a recipe being
	// stopped is often what prompted the recall in the first place.
	if _, err := setStatus(t, p, made, "QUARANTINED", "the reason the recipe was withdrawn"); err != nil {
		t.Fatalf("a batch made under a since-withdrawn recipe could not be quarantined: %v", err)
	}
	trace := trace(t, p, traceReq{ID: made.ID, Direction: "BACKWARD"})
	if trace.Batch.Code != "FA2-VAT" {
		t.Errorf("the batch could not be traced after its recipe was withdrawn")
	}
}

// A batch names a recipe by code, and gets the version that was on the wall the
// day it was made — not today's.
func TestABatchGetsTheVersionThatWasOnTheWallThatDay(t *testing.T) {
	p := startPlatform(t)
	old := int64(180000)
	recent := int64(190000)
	v1 := mustCreateFormulation(t, p, createFormulationReq{
		Code: "F3-PANEER", OutputProductRef: "PANEER", OutputUnit: "KILOGRAMS",
		ValidFrom: "2026-01-01T00:00:00Z", ValidTo: "2026-04-01T00:00:00Z",
		ExpectedYieldPPM: &old, ExpectationBasis: "2025 history",
	})
	v2 := mustCreateFormulation(t, p, createFormulationReq{
		Code: "F3-PANEER", OutputProductRef: "PANEER", OutputUnit: "KILOGRAMS",
		ValidFrom:        "2026-04-01T00:00:00Z",
		ExpectedYieldPPM: &recent, ExpectationBasis: "new separator, Q1 history",
	})

	march, err := createBatch(t, p, createBatchReq{
		Code: "F3-MARCH", Kind: "FINISHED", ProductRef: "PANEER",
		Produced:   quantityProto{Value: "1000.000", Unit: "KILOGRAMS"},
		ProducedAt: "2026-03-15T06:00:00Z", FormulationCode: "F3-PANEER",
	})
	if err != nil {
		t.Fatalf("March batch: %v", err)
	}
	if march.FormulationID != v1.ID {
		t.Errorf("a batch made in March got version %s; the recipe on the wall in March was %s, "+
			"and measuring it against the April one is the retroactive problem versioning "+
			"exists to prevent", march.FormulationID, v1.ID)
	}

	may, err := createBatch(t, p, createBatchReq{
		Code: "F3-MAY", Kind: "FINISHED", ProductRef: "PANEER",
		Produced:   quantityProto{Value: "1000.000", Unit: "KILOGRAMS"},
		ProducedAt: "2026-05-15T06:00:00Z", FormulationCode: "F3-PANEER",
	})
	if err != nil {
		t.Fatalf("May batch: %v", err)
	}
	if may.FormulationID != v2.ID {
		t.Errorf("a batch made in May got version %s, want %s", may.FormulationID, v2.ID)
	}

	// And the yield each reports is measured against its own version's target.
	milkM := made(t, p, "F3-MILK-M", "INTERMEDIATE", "RAW_MILK", "6000.000", "KILOGRAMS")
	mustRecordInput(t, p, march, milkM, "6000.000", "KILOGRAMS")
	got := batchYield(t, p, yieldReq{ID: march.ID})
	if got.ExpectedPPM == nil || *got.ExpectedPPM != old {
		t.Errorf("the March batch is measured against %v, want the March target %d",
			got.ExpectedPPM, old)
	}
	// 166666 observed against 180000 declared.
	if got.VariancePPM == nil || *got.VariancePPM != -13334 {
		t.Errorf("variance %v, want -13334 against the March target", got.VariancePPM)
	}
}

// A batch pointed at the recipe for a different product is refused. The
// variance would be against a target for something else: meaningless, and
// alarming to look at.
func TestABatchCannotFollowTheRecipeForSomethingElse(t *testing.T) {
	p := startPlatform(t)
	f := mustCreateFormulation(t, p, createFormulationReq{
		Code: "F4-PANEER", OutputProductRef: "PANEER", OutputUnit: "KILOGRAMS",
	})
	_, err := createBatch(t, p, createBatchReq{
		Code: "F4-GHEE", Kind: "FINISHED", ProductRef: "GHEE",
		Produced:   quantityProto{Value: "100.000", Unit: "KILOGRAMS"},
		ProducedAt: "2026-02-01T06:00:00Z", FormulationID: f.ID,
	})
	if err == nil {
		t.Error("a ghee batch was measured against the paneer recipe")
	}
}

// A batch pointed at a version that came into force after it was made is
// refused: the retroactive problem, arriving through the other door.
func TestABatchCannotFollowARecipeThatDidNotExistYet(t *testing.T) {
	p := startPlatform(t)
	f := mustCreateFormulation(t, p, createFormulationReq{
		Code: "F5-PANEER", OutputProductRef: "PANEER", OutputUnit: "KILOGRAMS",
		ValidFrom: "2026-06-01T00:00:00Z",
	})
	_, err := createBatch(t, p, createBatchReq{
		Code: "F5-EARLY", Kind: "FINISHED", ProductRef: "PANEER",
		Produced:   quantityProto{Value: "100.000", Unit: "KILOGRAMS"},
		ProducedAt: "2026-02-01T06:00:00Z", FormulationID: f.ID,
	})
	if err == nil {
		t.Error("a February batch was measured against a recipe that came into force in June")
	}
}

// Paneer with no milk in it: the finding the whole check exists for.
func TestAVatMissingARequiredIngredientIsReportedAsSerious(t *testing.T) {
	p := startPlatform(t)
	f := mustCreateFormulation(t, p, createFormulationReq{
		Code: "F6-PANEER", OutputProductRef: "PANEER", OutputUnit: "KILOGRAMS",
		Inputs: []formulationInputProto{
			{ProductRef: "RAW_MILK", Required: true},
			{ProductRef: "CULTURE", Required: false},
		},
	})
	vat, err := createBatch(t, p, createBatchReq{
		Code: "F6-VAT", Kind: "FINISHED", ProductRef: "PANEER",
		Produced:   quantityProto{Value: "1000.000", Unit: "KILOGRAMS"},
		ProducedAt: "2026-02-01T06:00:00Z", FormulationID: f.ID,
	})
	if err != nil {
		t.Fatalf("vat: %v", err)
	}
	acid := made(t, p, "F6-ACID", "INTERMEDIATE", "CITRIC_ACID", "50.000", "KILOGRAMS")
	mustRecordInput(t, p, vat, acid, "50.000", "KILOGRAMS")

	got := checkRecipe(t, p, vat)
	missing := finding(got, "MISSING_REQUIRED", "RAW_MILK")
	if missing == nil {
		t.Fatalf("paneer was made with no milk in it and nothing said so: %+v", got.Findings)
	}
	if !missing.Serious {
		t.Error("a vat with no milk in it is not marked serious")
	}
	if got.SeriousCount != 1 {
		t.Errorf("serious count %d, want 1", got.SeriousCount)
	}

	// The unused optional ingredient is a note, and the substitution is a note.
	// Reporting all three the same way is how a reader skims past the one that
	// matters.
	if f := finding(got, "MISSING_OPTIONAL", "CULTURE"); f == nil {
		t.Errorf("the unused optional ingredient was not mentioned: %+v", got.Findings)
	} else if f.Serious {
		t.Error("an unused optional ingredient is marked as serious as paneer with no milk")
	}
	if f := finding(got, "UNEXPECTED", "CITRIC_ACID"); f == nil {
		t.Errorf("a substitution went unreported: %+v", got.Findings)
	} else if f.Serious {
		t.Error("a substitution is marked serious; a plant substitutes and a vat recorded " +
			"with a note beside it beats a vat not recorded at all")
	}
}

// A recipe followed exactly reports nothing. Without this the assertions above
// would pass against a check that reports everything.
func TestAVatThatFollowedTheRecipeReportsNothing(t *testing.T) {
	p := startPlatform(t)
	f := mustCreateFormulation(t, p, createFormulationReq{
		Code: "F7-PANEER", OutputProductRef: "PANEER", OutputUnit: "KILOGRAMS",
		Inputs: []formulationInputProto{{ProductRef: "RAW_MILK", Required: true}},
	})
	vat, err := createBatch(t, p, createBatchReq{
		Code: "F7-VAT", Kind: "FINISHED", ProductRef: "PANEER",
		Produced:   quantityProto{Value: "1000.000", Unit: "KILOGRAMS"},
		ProducedAt: "2026-02-01T06:00:00Z", FormulationID: f.ID,
	})
	if err != nil {
		t.Fatalf("vat: %v", err)
	}
	milk := made(t, p, "F7-MILK", "INTERMEDIATE", "RAW_MILK", "6000.000", "KILOGRAMS")
	mustRecordInput(t, p, vat, milk, "6000.000", "KILOGRAMS")

	got := checkRecipe(t, p, vat)
	if len(got.Findings) != 0 {
		t.Errorf("a vat that followed the recipe reported %+v", got.Findings)
	}
}

// The declared and observed proportions are always put side by side. Whether
// the gap between them is a finding waits on a tolerance the plant declares.
func TestSharesAreReportedAndOnlyJudgedAgainstADeclaredTolerance(t *testing.T) {
	p := startPlatform(t)
	tight := int64(10000) // one per cent
	half := int64(500000)

	// Two recipes, identical but for the tolerance.
	noTol := mustCreateFormulation(t, p, createFormulationReq{
		Code: "F8-A", OutputProductRef: "CURD", OutputUnit: "KILOGRAMS",
		Inputs: []formulationInputProto{
			{ProductRef: "RAW_MILK", ExpectedSharePPM: &half, Required: true},
			{ProductRef: "CREAM", ExpectedSharePPM: &half, Required: true},
		},
	})
	withTol := mustCreateFormulation(t, p, createFormulationReq{
		Code: "F8-B", OutputProductRef: "CURD", OutputUnit: "KILOGRAMS",
		Inputs: []formulationInputProto{
			{ProductRef: "RAW_MILK", ExpectedSharePPM: &half, ShareTolerancePPM: &tight,
				Required: true},
			{ProductRef: "CREAM", ExpectedSharePPM: &half, ShareTolerancePPM: &tight,
				Required: true},
		},
	})

	// The same badly-proportioned vat under each: 90% milk against a declared 50%.
	for _, c := range []struct {
		tag         string
		f           *formulationProto
		wantFinding bool
	}{
		{"A", noTol, false},
		{"B", withTol, true},
	} {
		vat, err := createBatch(t, p, createBatchReq{
			Code: "F8-VAT-" + c.tag, Kind: "FINISHED", ProductRef: "CURD",
			Produced:   quantityProto{Value: "900.000", Unit: "KILOGRAMS"},
			ProducedAt: "2026-02-01T06:00:00Z", FormulationID: c.f.ID,
		})
		if err != nil {
			t.Fatalf("vat %s: %v", c.tag, err)
		}
		milk := made(t, p, "F8-MILK-"+c.tag, "INTERMEDIATE", "RAW_MILK", "900.000", "KILOGRAMS")
		cream := made(t, p, "F8-CREAM-"+c.tag, "INTERMEDIATE", "CREAM", "100.000", "KILOGRAMS")
		mustRecordInput(t, p, vat, milk, "900.000", "KILOGRAMS")
		mustRecordInput(t, p, vat, cream, "100.000", "KILOGRAMS")

		got := checkRecipe(t, p, vat)

		// The figures are reported either way.
		var milkShare *shareReadingProto
		for i := range got.Shares {
			if got.Shares[i].ProductRef == "RAW_MILK" {
				milkShare = &got.Shares[i]
			}
		}
		if milkShare == nil {
			t.Fatalf("%s: the declared share was not reported at all: %+v", c.tag, got.Shares)
		}
		if milkShare.ObservedSharePPM != 900000 || milkShare.ExpectedSharePPM != 500000 {
			t.Errorf("%s: reported %d observed against %d declared, want 900000 against 500000",
				c.tag, milkShare.ObservedSharePPM, milkShare.ExpectedSharePPM)
		}
		if milkShare.DifferencePPM != 400000 {
			t.Errorf("%s: difference %d, want 400000", c.tag, milkShare.DifferencePPM)
		}

		// The judgement waits on the tolerance.
		f := finding(got, "SHARE_DIFFERS", "RAW_MILK")
		if c.wantFinding && f == nil {
			t.Errorf("%s: a vat forty per cent outside a one per cent tolerance was not "+
				"reported: %+v", c.tag, got.Findings)
		}
		if !c.wantFinding && f != nil {
			t.Errorf("%s: a vat was judged against a tolerance nobody declared: %s",
				c.tag, f.Explanation)
		}
	}
}

// What a plant reads in order to set its own target.
//
// The platform does not know what a process should yield and does not invent
// one. It shows the plant its own vats.
func TestObservedHistoryIsWhatAPlantReadsToSetItsOwnTarget(t *testing.T) {
	p := startPlatform(t)
	f := mustCreateFormulation(t, p, createFormulationReq{
		Code: "F9-CURD", OutputProductRef: "CURD", OutputUnit: "KILOGRAMS",
	})

	// Five vats yielding 15%, 16%, 17%, 18% and 19% — recorded out of order, so
	// the median is a property of the figures and not of the insertion order.
	for _, ppm := range []int64{170000, 150000, 190000, 160000, 180000} {
		tag := fmt.Sprintf("%d", ppm)
		src := made(t, p, "F9-MILK-"+tag, "INTERMEDIATE", "RAW_MILK", "1000.000", "KILOGRAMS")
		out, err := createBatch(t, p, createBatchReq{
			Code: "F9-VAT-" + tag, Kind: "FINISHED", ProductRef: "CURD",
			// 1000.000 kg in, so ppm/1000 kg out gives exactly ppm.
			Produced:   quantityProto{Value: fmt.Sprintf("%d.000", ppm/1000), Unit: "KILOGRAMS"},
			ProducedAt: "2026-02-01T06:00:00Z", FormulationID: f.ID,
		})
		if err != nil {
			t.Fatalf("vat %s: %v", tag, err)
		}
		mustRecordInput(t, p, out, src, "1000.000", "KILOGRAMS")
	}

	got := observedYield(t, p, f)
	if got.BatchesCounted != 5 {
		t.Fatalf("the history rests on %d batches, want 5: %+v", got.BatchesCounted, got)
	}
	for _, c := range []struct {
		name string
		got  *int64
		want int64
	}{
		{"lowest", got.LowestPPM, 150000},
		{"lower quartile", got.LowerQuartilePPM, 160000},
		{"median", got.MedianPPM, 170000},
		{"upper quartile", got.UpperQuartilePPM, 180000},
		{"highest", got.HighestPPM, 190000},
	} {
		if c.got == nil || *c.got != c.want {
			t.Errorf("%s is %v, want %d", c.name, c.got, c.want)
		}
	}
	// Every figure is one a vat actually produced, not an interpolation between
	// two that did. A plant setting a target off this list is looking at its own
	// numbers.
	if !strings.Contains(got.Note, "5") {
		t.Errorf("note %q does not say how many batches it rests on; a median over five vats "+
			"and one over five hundred look identical on a screen", got.Note)
	}
	if got.ExpectedPPM != nil {
		t.Errorf("a target of %d appeared on a recipe that declared none", *got.ExpectedPPM)
	}
}

// Batches whose yield needs a density are counted separately and said so. Three
// usable observations beside forty unconvertible ones is not a history anybody
// should set a target from, and a summary showing only the three would not say.
func TestTheHistorySaysHowManyBatchesItHadToLeaveOut(t *testing.T) {
	p := startPlatform(t)
	f := mustCreateFormulation(t, p, createFormulationReq{
		Code: "FA-BUTTER", OutputProductRef: "BUTTER", OutputUnit: "KILOGRAMS",
	})

	// One usable: kilograms in, kilograms out.
	srcKg := made(t, p, "FA-CREAM-KG", "INTERMEDIATE", "CREAM", "1000.000", "KILOGRAMS")
	okBatch, err := createBatch(t, p, createBatchReq{
		Code: "FA-VAT-OK", Kind: "FINISHED", ProductRef: "BUTTER",
		Produced:   quantityProto{Value: "400.000", Unit: "KILOGRAMS"},
		ProducedAt: "2026-02-01T06:00:00Z", FormulationID: f.ID,
	})
	if err != nil {
		t.Fatalf("usable vat: %v", err)
	}
	mustRecordInput(t, p, okBatch, srcKg, "1000.000", "KILOGRAMS")

	// Two unconvertible: litres in, kilograms out.
	for i := 1; i <= 2; i++ {
		tag := fmt.Sprintf("%d", i)
		srcL := made(t, p, "FA-CREAM-L"+tag, "INTERMEDIATE", "CREAM", "1000.000", "LITRES")
		b, err := createBatch(t, p, createBatchReq{
			Code: "FA-VAT-L" + tag, Kind: "FINISHED", ProductRef: "BUTTER",
			Produced:   quantityProto{Value: "400.000", Unit: "KILOGRAMS"},
			ProducedAt: "2026-02-01T06:00:00Z", FormulationID: f.ID,
		})
		if err != nil {
			t.Fatalf("unconvertible vat %s: %v", tag, err)
		}
		mustRecordInput(t, p, b, srcL, "1000.000", "LITRES")
	}

	got := observedYield(t, p, f)
	if got.BatchesCounted != 1 {
		t.Errorf("counted %d batches, want the one whose units line up", got.BatchesCounted)
	}
	if got.BatchesNeedingADensity != 2 {
		t.Errorf("reported %d batches needing a density, want 2; a summary that hides them "+
			"looks like a recipe with a short history rather than one with an unconvertible one",
			got.BatchesNeedingADensity)
	}
	if !strings.Contains(got.Note, "density") {
		t.Errorf("note %q does not say why two batches were left out", got.Note)
	}
}

// A recipe nobody has used yet answers, rather than failing.
func TestARecipeNobodyHasUsedStillAnswers(t *testing.T) {
	p := startPlatform(t)
	f := mustCreateFormulation(t, p, createFormulationReq{
		Code: "FB-NEW", OutputProductRef: "SMP", OutputUnit: "KILOGRAMS",
	})
	got := observedYield(t, p, f)
	if got.BatchesCounted != 0 {
		t.Errorf("a recipe nobody has used counted %d batches", got.BatchesCounted)
	}
	if got.MedianPPM != nil {
		t.Errorf("a median of %d over no batches at all", *got.MedianPPM)
	}
	if got.Note == "" {
		t.Error("no figures and no note; an empty report is read as nothing being wrong")
	}
}

// A recipe named by code has to say which moment to look it up for.
func TestARecipeByCodeMustSayWhichMoment(t *testing.T) {
	p := startPlatform(t)
	mustCreateFormulation(t, p, createFormulationReq{
		Code: "FC-CURD", OutputProductRef: "CURD", OutputUnit: "KILOGRAMS",
	})
	_, err := svcclient.Call[observedYieldReq, observedYieldResp](
		context.Background(), p.production(), productionSvc+"/GetObservedYield",
		observedYieldReq{TenantID: p.tenant, Code: "FC-CURD"}, p.opts())
	if err == nil {
		t.Error("a recipe was looked up by code with no moment given; a recipe is several " +
			"versions and answering with today's when somebody meant March is the retroactive " +
			"problem versioning exists to prevent")
	}
	if _, err := svcclient.Call[observedYieldReq, observedYieldResp](
		context.Background(), p.production(), productionSvc+"/GetObservedYield",
		observedYieldReq{TenantID: p.tenant, Code: "FC-CURD", At: "2026-06-01T00:00:00Z"},
		p.opts()); err != nil {
		t.Errorf("a recipe looked up by code and moment was refused: %v", err)
	}
}
