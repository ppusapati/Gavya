//go:build e2e

// Finding a batch again, and the recipe that was on the wall when it was made.
//
// production_test builds a plant and asks it the questions a recall asks. The
// four routes left are the plainer ones underneath: find a batch by the code
// printed on its side, list the batches made in a shift, and look a recipe up
// for a day.
//
// The recipe routes are not plain at all. A formulation is several versions
// under one code, and GetFormulation answers for a moment rather than for
// today, because answering with today's recipe when somebody asked about March
// is the retroactive problem versioning exists to prevent. It also refuses a
// code with no moment beside it, rather than defaulting to now — a default
// there would look like a decision and would be wrong exactly when it mattered.
package e2e

import (
	"context"
	"fmt"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

type getBatchReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id,omitempty"`
	Code     string `json:"code,omitempty"`
}

type listBatchesReq struct {
	TenantID string `json:"tenant_id"`
	From     string `json:"from"`
	To       string `json:"to,omitempty"`
	Limit    int32  `json:"limit,omitempty"`
}

type listBatchesResp struct {
	Batches []*batchProto `json:"batches"`
}

type getFormulationReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id,omitempty"`
	Code     string `json:"code,omitempty"`
	At       string `json:"at,omitempty"`
}

type listFormulationsReq struct {
	TenantID string `json:"tenant_id"`
}

type listFormulationsResp struct {
	Formulations []*formulationProto `json:"formulations"`
}

func ppm(v int64) *int64 { return &v }

// A batch is found by its id and by the code written on its side.
//
// The code is what is actually on the pallet. Somebody holding a carton in a
// cold store has the code and not the id, which is the whole reason the route
// takes either — and the two must resolve to the same batch, or a recall
// carried out from the shelf covers different lots from one carried out from
// the database.
func TestABatchIsFoundByItsIdAndByTheCodeWrittenOnIt(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	code := "LOT-" + newID("b")[:12]
	made := rawMilk(t, p, code, "5000.000")

	byID, err := svcclient.Call[getBatchReq, batchResp](ctx, p.production(),
		productionSvc+"/GetBatch", getBatchReq{TenantID: p.tenant, ID: made.ID}, p.opts())
	if err != nil {
		t.Fatalf("GetBatch by id: %v", err)
	}
	byCode, err := svcclient.Call[getBatchReq, batchResp](ctx, p.production(),
		productionSvc+"/GetBatch", getBatchReq{TenantID: p.tenant, Code: code}, p.opts())
	if err != nil {
		t.Fatalf("GetBatch by code: %v", err)
	}
	if byID.Batch.ID != made.ID || byCode.Batch.ID != made.ID {
		t.Fatalf("the two lookups found %s and %s, and the batch is %s\n"+
			"A recall run from the shelf and one run from the database would "+
			"cover different lots.", byID.Batch.ID, byCode.Batch.ID, made.ID)
	}
	if byCode.Batch.Code != code {
		t.Errorf("the batch reads back under code %q, want %q", byCode.Batch.Code, code)
	}
	if byCode.Batch.Produced.Value != "5000.000" || byCode.Batch.Produced.Unit != "LITRES" {
		t.Errorf("the batch holds %s %s, want 5000.000 LITRES",
			byCode.Batch.Produced.Value, byCode.Batch.Produced.Unit)
	}

	// Naming neither is refused rather than answered with something.
	if _, err := svcclient.Call[getBatchReq, batchResp](ctx, p.production(),
		productionSvc+"/GetBatch", getBatchReq{TenantID: p.tenant}, p.opts()); err == nil {
		t.Error("a lookup naming neither an id nor a code came back with a batch")
	}

	stranger := newID("ten")
	if _, err := svcclient.Call[getBatchReq, batchResp](ctx, p.production(),
		productionSvc+"/GetBatch", getBatchReq{TenantID: stranger, Code: code},
		svcclient.CallOptions{Tenant: stranger, Actor: "e2e", RequestID: newID("req")}); err == nil {
		t.Error("another tenant found this batch by its code\n" +
			"Batch codes are a plant's own numbering, and two plants number " +
			"from one.")
	}
}

// A listing of batches answers for the window it was asked about.
//
// Three days of production and a window over the middle one. This is the route
// behind "what did we make on Tuesday", and a window that stopped being applied
// answers it with the whole quarter — which still contains Tuesday, so nothing
// looks wrong.
func TestListBatchesAnswersForTheWindowItWasAsked(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	// Its own dates, well away from the other production tests' default, so
	// this window holds only what this test put in it.
	days := []string{"2027-07-01T06:00:00Z", "2027-07-02T06:00:00Z", "2027-07-03T06:00:00Z"}
	var ids []string
	for i, day := range days {
		b, err := createBatch(t, p, createBatchReq{
			// The index, not just a truncated id: newID pads with zeros, so the
			// first ten characters of two ids made in the same millisecond are
			// identical and the second batch collides on its code.
			Code: fmt.Sprintf("WIN-%d-%s", i, newID("b")[:10]), Kind: "RAW", ProductRef: "RAW_MILK",
			Produced:   quantityProto{Value: "1000.000", Unit: "LITRES"},
			SourceKind: "MOVEMENT", SourceRef: "MV-w" + string(rune('a'+i)),
			ProducedAt: day,
		})
		if err != nil {
			t.Fatalf("create batch for %s: %v", day, err)
		}
		ids = append(ids, b.ID)
	}

	got, err := svcclient.Call[listBatchesReq, listBatchesResp](ctx, p.production(),
		productionSvc+"/ListBatches", listBatchesReq{
			TenantID: p.tenant,
			From:     "2027-07-02T00:00:00Z", To: "2027-07-02T23:59:59Z",
			Limit: 100,
		}, p.opts())
	if err != nil {
		t.Fatalf("ListBatches: %v", err)
	}
	seen := map[string]bool{}
	for _, b := range got.Batches {
		seen[b.ID] = true
	}
	if !seen[ids[1]] {
		t.Error("the batch made inside the window is not in the answer")
	}
	if seen[ids[0]] || seen[ids[2]] {
		t.Errorf("a batch made outside the window came back: before=%v after=%v\n"+
			"The window is what turns this into \"what did we make on Tuesday\"; "+
			"without it the answer is the whole quarter, which still contains "+
			"Tuesday.", seen[ids[0]], seen[ids[2]])
	}
}

// A recipe is looked up for the day it applied, and an unapproved one applies
// to nothing.
//
// One code, two versions: the recipe changed in the spring. A batch made in
// February must be judged against February's recipe, and one made in June
// against June's. Answering either with the other is the retroactive problem in
// both directions — a plant found to be off-target against a recipe that did
// not exist yet, or on-target against one that has been superseded.
//
// The third version is a draft nobody signed off. It covers the same period as
// the second and must never be the answer: FormulationInForce takes only
// APPROVED rows, so signing a recipe off is what makes it govern anything.
func TestARecipeIsLookedUpForTheDayItAppliedAndADraftGovernsNothing(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	code := "REC-" + newID("f")[:10]

	declare := func(name, from, to string, yield int64) *formulationProto {
		t.Helper()
		return declareRecipe(t, p, ctx, code, name, from, to, yield)
	}

	approve := func(f *formulationProto) {
		t.Helper()
		if _, err := svcclient.Call[approveFormulationReq, formulationResp](ctx, p.production(),
			productionSvc+"/ApproveFormulation", approveFormulationReq{
				TenantID: p.tenant, ID: f.ID, Approver: "plant manager",
				At: "2026-01-01T00:00:00Z", Note: "agreed at the Tuesday meeting",
			}, p.opts()); err != nil {
			t.Fatalf("ApproveFormulation %s: %v", f.Code, err)
		}
	}

	winter := declare("winter make", "2026-01-01T00:00:00Z", "2026-05-01T00:00:00Z", 420000)
	summer := declare("summer make", "2026-05-01T00:00:00Z", "", 380000)
	approve(winter)
	approve(summer)

	// A recipe created as a draft is a recipe nobody has signed, and it governs
	// nothing.
	//
	// It gets its own code rather than sitting alongside the summer make,
	// because under one code the assertion would prove nothing.
	// FormulationInForce has no ORDER BY and no LIMIT; the schema's exclusion
	// constraint is what guarantees it a single row, and that constraint reads
	// `WHERE (deleted_at IS NULL AND status = 'APPROVED')` — only approved
	// versions are exclusive, so a draft may overlap an approved one freely.
	// Drop the query's status filter and two rows match, and which one comes
	// back is the planner's choice. Measured: with the filter removed and a
	// draft beside the summer make, three runs all returned the summer make and
	// the test passed. An assertion that relies on the planner choosing wrongly
	// is not an assertion.
	//
	// Alone under its own code the draft is the only candidate, so removing the
	// filter has exactly one possible outcome and the test can insist on it.
	draftCode := "REC-" + newID("d")[:10]
	draft := declareRecipe(t, p, ctx, draftCode, "unsigned revision",
		"2026-01-01T00:00:00Z", "", 999999)
	if draft.Status != "DRAFT" {
		t.Fatalf("a newly created recipe is %q, want DRAFT — a recipe is approved "+
			"separately so signing one off is an act with a name against it",
			draft.Status)
	}

	for _, c := range []struct {
		when   string
		wantID string
		name   string
	}{
		{"2026-02-14T00:00:00Z", winter.ID, "winter make"},
		{"2026-06-14T00:00:00Z", summer.ID, "summer make"},
	} {
		got, err := svcclient.Call[getFormulationReq, formulationResp](ctx, p.production(),
			productionSvc+"/GetFormulation", getFormulationReq{
				TenantID: p.tenant, Code: code, At: c.when,
			}, p.opts())
		if err != nil {
			t.Fatalf("GetFormulation at %s: %v", c.when, err)
		}
		if got.Formulation.ID != c.wantID {
			t.Errorf("the recipe in force at %s is %q, want %q — a batch is judged "+
				"against the recipe that was on the wall when it was made",
				c.when[:10], got.Formulation.Name, c.name)
		}
	}

	// Nothing is in force under the draft's code, because nobody signed it.
	if _, err := svcclient.Call[getFormulationReq, formulationResp](ctx, p.production(),
		productionSvc+"/GetFormulation", getFormulationReq{
			TenantID: p.tenant, Code: draftCode, At: "2026-06-14T00:00:00Z",
		}, p.opts()); err == nil {
		t.Error("a recipe nobody approved is in force\n" +
			"A recipe is created as a draft and approved separately so that " +
			"signing one off is an act with a name against it. A draft that " +
			"governs batches makes the approval ceremonial.")
	}

	// A code with no moment beside it is refused. Defaulting to now would be a
	// default that looks like a decision, and wrong exactly when it matters.
	if _, err := svcclient.Call[getFormulationReq, formulationResp](ctx, p.production(),
		productionSvc+"/GetFormulation", getFormulationReq{
			TenantID: p.tenant, Code: code,
		}, p.opts()); err == nil {
		t.Error("a recipe named by code alone was answered\n" +
			"A recipe is several versions; answering with today's when somebody " +
			"asked about March is the retroactive problem versioning exists for.")
	}

	// By id, which needs no moment: an id names one version already.
	byID, err := svcclient.Call[getFormulationReq, formulationResp](ctx, p.production(),
		productionSvc+"/GetFormulation", getFormulationReq{
			TenantID: p.tenant, ID: winter.ID,
		}, p.opts())
	if err != nil {
		t.Fatalf("GetFormulation by id: %v", err)
	}
	if byID.Formulation.ID != winter.ID {
		t.Errorf("GetFormulation asked for %s and answered with %s", winter.ID, byID.Formulation.ID)
	}
	if byID.Formulation.ExpectedYieldPPM == nil || *byID.Formulation.ExpectedYieldPPM != 420000 {
		t.Errorf("the winter recipe's expected yield reads back as %v, want 420000 ppm",
			byID.Formulation.ExpectedYieldPPM)
	}
	if byID.Formulation.ExpectationBasis == "" {
		t.Error("the recipe carries a yield target and no basis for it; a figure " +
			"from this plant's own vats and one off a supplier's leaflet are " +
			"different claims")
	}
	if len(byID.Formulation.Inputs) != 1 || byID.Formulation.Inputs[0].ProductRef != "CREAM" {
		t.Errorf("the recipe reads back with inputs %+v, want one CREAM", byID.Formulation.Inputs)
	}

	// The listing carries every version, drafts included: it is the recipe book,
	// not the answer to "what applies today".
	listed, err := svcclient.Call[listFormulationsReq, listFormulationsResp](ctx, p.production(),
		productionSvc+"/ListFormulations", listFormulationsReq{TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("ListFormulations: %v", err)
	}
	seen := map[string]bool{}
	for _, f := range listed.Formulations {
		seen[f.ID] = true
	}
	for _, want := range []*formulationProto{winter, summer, draft} {
		if !seen[want.ID] {
			t.Errorf("recipe %q is missing from the recipe book", want.Name)
		}
	}
}

// declareRecipe creates one version of a recipe. It comes back a draft: a
// recipe is approved separately, so signing one off is an act with a name
// against it rather than a field somebody filled in while typing the rest.
func declareRecipe(t *testing.T, p *platform, ctx context.Context, code, name, from, to string, yield int64) *formulationProto {
	t.Helper()
	req := createFormulationReq{
		TenantID: p.tenant, Code: code, Name: name,
		OutputProductRef: "BUTTER", OutputUnit: "KILOGRAMS",
		ExpectedYieldPPM: ppm(yield),
		ExpectationBasis: "two hundred of this plant's own vats",
		ValidFrom:        from, Actor: "e2e",
		Inputs: []formulationInputProto{
			{ProductRef: "CREAM", Required: true, ExpectedSharePPM: ppm(1000000)},
		},
	}
	if to != "" {
		req.ValidTo = to
	}
	out, err := svcclient.Call[createFormulationReq, formulationResp](ctx, p.production(),
		productionSvc+"/CreateFormulation", req, p.opts())
	if err != nil {
		t.Fatalf("CreateFormulation %s: %v", name, err)
	}
	return out.Formulation
}
