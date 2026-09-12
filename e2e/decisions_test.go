//go:build e2e

// The four decision endpoints in the integrity layer that nothing called.
//
// canonical-service and balance-service are well covered by shadow_pipeline_test
// and material_test between them — but only for the paths those two files walk.
// ReverseResolve, RetireIdentity, ResolveConflict and AcceptRun were left out,
// and they have something in common: each is where a person overrides or accepts
// what the machinery worked out.
//
// Those are exactly the ones a defect hides in longest. The happy path is walked
// every day; the endpoint somebody uses when the answer is wrong is used rarely
// and under pressure.
package e2e

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

type reverseResolveReq struct {
	TenantID   string `json:"tenant_id"`
	EntityKind string `json:"entity_kind"`
	EntityID   string `json:"entity_id"`
}

type reverseResolveResp struct {
	Identities []*identityProto `json:"identities"`
}

type retireIdentityReq struct {
	TenantID string `json:"tenant_id"`
	ID       string `json:"id"`
	Actor    string `json:"actor"`
}

type resolveConflictReq struct {
	TenantID         string `json:"tenant_id"`
	SlotID           string `json:"slot_id"`
	AuthoritativeRef string `json:"authoritative_ref"`
	Resolution       string `json:"resolution"`
	Actor            string `json:"actor"`
}

type resolveConflictResp struct {
	Slot *slotProto `json:"slot"`
}

type acceptRunReq struct {
	TenantID string `json:"tenant_id"`
	RunID    string `json:"run_id"`
	Actor    string `json:"actor"`
}

type acceptRunResp struct {
	Run *runProto `json:"run"`
}

// An entity's external identifiers can be found from the entity.
//
// Resolution goes one way in the ordinary case: a society's own code for a
// producer arrives and the platform says which producer that is. Reverse
// resolution is the other direction, and it is what somebody uses when a figure
// looks wrong and they need to know which source systems fed it.
func TestAnEntitysExternalIdentifiersCanBeFoundFromTheEntity(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()
	entity := newID("prd")

	// The same producer, known by two societies under two codes.
	for _, source := range []struct{ system, external string }{
		{"SOC-A", "A-118"},
		{"SOC-B", "B-4471"},
	} {
		if _, err := svcclient.Call[mapIdentityReq, mapIdentityResp](ctx, p.canonical(),
			canonicalSvc+"/MapIdentity", mapIdentityReq{
				TenantID: p.tenant, SourceSystemID: source.system,
				EntityKind: "PRODUCER", ExternalID: source.external,
				EntityID: entity, Method: "MANUAL",
				ValidFrom: "2020-01-01T00:00:00Z", Actor: "e2e",
			}, p.opts()); err != nil {
			t.Fatalf("map %s/%s: %v", source.system, source.external, err)
		}
	}

	back, err := svcclient.Call[reverseResolveReq, reverseResolveResp](ctx, p.canonical(),
		canonicalSvc+"/ReverseResolve", reverseResolveReq{
			TenantID: p.tenant, EntityKind: "PRODUCER", EntityID: entity,
		}, p.opts())
	if err != nil {
		t.Fatalf("ReverseResolve: %v", err)
	}
	if len(back.Identities) != 2 {
		t.Fatalf("the producer is known by %d source systems, want 2 — somebody "+
			"chasing a wrong figure needs to know every system that fed it",
			len(back.Identities))
	}
	for _, m := range back.Identities {
		if m.EntityID != entity {
			t.Errorf("a mapping for another entity (%s) came back", m.EntityID)
		}
	}

	// An entity nobody has mapped comes back empty rather than with somebody
	// else's mappings.
	none, err := svcclient.Call[reverseResolveReq, reverseResolveResp](ctx, p.canonical(),
		canonicalSvc+"/ReverseResolve", reverseResolveReq{
			TenantID: p.tenant, EntityKind: "PRODUCER", EntityID: newID("prd"),
		}, p.opts())
	if err != nil {
		t.Fatalf("ReverseResolve for an unmapped entity: %v", err)
	}
	if len(none.Identities) != 0 {
		t.Errorf("an unmapped producer came back with %d mappings", len(none.Identities))
	}
}

// A retired mapping stops resolving, and the row is superseded rather than
// deleted.
//
// A society renumbers its producers and the old code has to stop pointing at
// somebody. The row survives with a superseded_at stamp, which is the platform's
// supersession rule: every collection recorded under the old code was resolved
// through it, and a record whose resolution had been deleted could not be
// explained afterwards.
//
// What is checked against the database rather than the API is deliberate. No
// endpoint returns a superseded mapping — ReverseResolve and ListIdentities both
// filter them out and neither takes an as-of — so the history is in the table and
// not reachable through the service. That is a real gap and docs/roadmap.md
// carries it; asserting it here against SQL is what keeps this test honest about
// which of the two it is proving.
func TestARetiredMappingStopsResolvingWithoutDisappearing(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()
	entity, external := newID("prd"), newID("ext")

	made, err := svcclient.Call[mapIdentityReq, mapIdentityResp](ctx, p.canonical(),
		canonicalSvc+"/MapIdentity", mapIdentityReq{
			TenantID: p.tenant, SourceSystemID: "SOC-A", EntityKind: "PRODUCER",
			ExternalID: external, EntityID: entity, Method: "MANUAL",
			ValidFrom: "2020-01-01T00:00:00Z", Actor: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("MapIdentity: %v", err)
	}

	if _, err := svcclient.Call[retireIdentityReq, struct{}](ctx, p.canonical(),
		canonicalSvc+"/RetireIdentity", retireIdentityReq{
			TenantID: p.tenant, ID: made.Identity.ID, Actor: "e2e",
		}, p.opts()); err != nil {
		t.Fatalf("RetireIdentity: %v", err)
	}

	// It no longer resolves.
	if _, err := svcclient.Call[resolveIdentityReq, resolveIdentityResp](ctx, p.canonical(),
		canonicalSvc+"/ResolveIdentity", resolveIdentityReq{
			TenantID: p.tenant, SourceSystemID: "SOC-A", EntityKind: "PRODUCER",
			ExternalID: external, AsOf: "2026-02-14T06:00:00Z",
		}, p.opts()); err == nil {
		t.Error("a retired mapping still resolves; the code was retired because it " +
			"stopped meaning that producer")
	}

	// It is gone from the entity's mappings, because every read path filters
	// superseded rows.
	back, err := svcclient.Call[reverseResolveReq, reverseResolveResp](ctx, p.canonical(),
		canonicalSvc+"/ReverseResolve", reverseResolveReq{
			TenantID: p.tenant, EntityKind: "PRODUCER", EntityID: entity,
		}, p.opts())
	if err != nil {
		t.Fatalf("ReverseResolve: %v", err)
	}
	for _, m := range back.Identities {
		if m.ID == made.Identity.ID {
			t.Error("a retired mapping is still returned by ReverseResolve")
		}
	}

	// But the row is there, stamped with when it stopped and who stopped it.
	// Superseding is not deleting, and this is the only place that can be seen.
	conn, err := pgx.Connect(ctx, dsn(t, "e2e_canonical"))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(context.Background())
	var supersededBy string
	if err := conn.QueryRow(ctx,
		`SELECT COALESCE(superseded_by,'') FROM external_identities
		  WHERE id=$1 AND tenant_id=$2 AND superseded_at IS NOT NULL`,
		made.Identity.ID, p.tenant).Scan(&supersededBy); err != nil {
		t.Fatalf("the retired mapping is not in the table with a superseded_at: %v\n"+
			"Retiring deleted it, and every collection resolved through that code can "+
			"no longer be explained", err)
	}
	if supersededBy != "e2e" {
		t.Errorf("the mapping was superseded by %q, want the actor who retired it",
			supersededBy)
	}
}

// A slot in conflict is decided by a person, and the decision says who and why.
//
// Under MANUAL resolution the platform declines to choose between two claims on
// one collection, which is the honest answer when the policy says a person
// decides. What matters is that resolving it records the reason: a slot that
// simply changed hands is indistinguishable afterwards from one that was
// overwritten by mistake.
func TestASlotInConflictIsDecidedByAPersonWithAReason(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	if _, err := svcclient.Call[declarePolicyReq, declarePolicyResp](ctx, p.canonical(),
		canonicalSvc+"/DeclarePolicy", declarePolicyReq{
			TenantID: p.tenant, Name: newID("pol"),
			Dimensions: []string{"PRODUCER", "COLLECTION_DATE", "SHIFT"},
			Resolution: "MANUAL", Version: 99,
			EffectiveFrom: "2020-01-01T00:00:00Z", Actor: "e2e",
		}, p.opts()); err != nil {
		t.Fatalf("DeclarePolicy: %v", err)
	}

	values := map[string]string{
		"PRODUCER":        newID("P"),
		"COLLECTION_DATE": "2026-02-14",
		"SHIFT":           "MORNING",
	}
	first, err := svcclient.Call[claimSlotReq, claimSlotResp](ctx, p.canonical(),
		canonicalSvc+"/ClaimSlot", claimSlotReq{
			TenantID: p.tenant, SourceRef: "obs-one", Values: values,
			Origin: "NATIVE", RecordedAt: "2026-02-14T06:00:00Z",
			CollectedAt: collectedAt, Actor: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("first ClaimSlot: %v", err)
	}
	if first.Outcome != "SLOT_ESTABLISHED" {
		t.Fatalf("first claim: %s (%s)", first.Outcome, first.Reason)
	}

	second, err := svcclient.Call[claimSlotReq, claimSlotResp](ctx, p.canonical(),
		canonicalSvc+"/ClaimSlot", claimSlotReq{
			TenantID: p.tenant, SourceRef: "obs-two", Values: values,
			Origin: "NATIVE", RecordedAt: "2026-02-14T09:00:00Z",
			CollectedAt: collectedAt, Actor: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("second ClaimSlot: %v", err)
	}
	if second.Outcome != "COLLECTION_SLOT_CONFLICT" {
		t.Fatalf("under MANUAL resolution the second claim was %s, want a conflict — "+
			"a policy that says a person decides must not decide", second.Outcome)
	}

	// A resolution with no note is refused: a decision nobody wrote a reason for
	// is one nobody can defend later.
	if _, err := svcclient.Call[resolveConflictReq, resolveConflictResp](ctx, p.canonical(),
		canonicalSvc+"/ResolveConflict", resolveConflictReq{
			TenantID: p.tenant, SlotID: second.Slot.ID,
			AuthoritativeRef: "obs-two", Resolution: "", Actor: "e2e",
		}, p.opts()); err == nil {
		t.Error("a conflict was resolved with no stated reason")
	}

	// And one that names no claim is refused too: the slot has to end up held
	// by something.
	if _, err := svcclient.Call[resolveConflictReq, resolveConflictResp](ctx, p.canonical(),
		canonicalSvc+"/ResolveConflict", resolveConflictReq{
			TenantID: p.tenant, SlotID: second.Slot.ID,
			AuthoritativeRef: "", Resolution: "the later reading is the tanker's",
			Actor: "e2e",
		}, p.opts()); err == nil {
		t.Error("a conflict was resolved without naming which claim holds the slot")
	}

	resolved, err := svcclient.Call[resolveConflictReq, resolveConflictResp](ctx, p.canonical(),
		canonicalSvc+"/ResolveConflict", resolveConflictReq{
			TenantID: p.tenant, SlotID: second.Slot.ID,
			AuthoritativeRef: "obs-two",
			Resolution:       "the six o'clock reading was the can, the nine o'clock the tanker",
			Actor:            "supervisor",
		}, p.opts())
	if err != nil {
		t.Fatalf("ResolveConflict: %v", err)
	}
	if resolved.Slot.AuthoritativeRef != "obs-two" {
		t.Errorf("the slot is held by %q after being resolved to obs-two",
			resolved.Slot.AuthoritativeRef)
	}
	if resolved.Slot.Status == "CONFLICT" {
		t.Error("the slot is still in conflict after being resolved")
	}
}

// A reconciliation run is accepted, and accepting it is a decision.
//
// Reconciling proposes adjustments to measured flows. Accepting one is what
// makes those adjustments the figures a plant works from, and it is the step
// somebody signs. An accept that reports success without recording who accepted
// it would leave a fortnight's reconciled figures with no owner.
func TestAReconciliationRunIsAcceptedOnce(t *testing.T) {
	// The ML platform, because accepting a run needs one that converged and
	// converging needs the reconciler. Without the tier balance-service records
	// the imbalance locally and reports that it did not converge — which is the
	// honest answer and not one this test can accept.
	p := startMLPlatform(t)
	ctx := context.Background()

	window, err := svcclient.Call[createWindowReq, createWindowResp](ctx, p.balance(),
		balanceSvc+"/CreateWindow", createWindowReq{
			TenantID: p.tenant, RouteRef: newID("rt"),
			PeriodStart: "2026-04-01T00:00:00Z", PeriodEnd: "2026-04-02T00:00:00Z",
			Unit: "LITRES", Actor: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("CreateWindow: %v", err)
	}

	// Milk in from the boundary, out to a tanker, and out of the tanker again.
	// The two legs disagree by five litres, which is what there is to reconcile.
	tanker := newID("tnk")
	for _, f := range []addFlowReq{
		{FlowID: "in", FromNode: "", ToNode: tanker, ToNodeKind: "TANKER",
			Measured: "1000.000", StandardUncertainty: "5.000"},
		{FlowID: "out", FromNode: tanker, FromNodeKind: "TANKER", ToNode: "",
			Measured: "995.000", StandardUncertainty: "5.000"},
	} {
		f.TenantID, f.WindowID, f.Actor = p.tenant, window.Window.ID, "e2e"
		if _, err := svcclient.Call[addFlowReq, addFlowResp](ctx, p.balance(),
			balanceSvc+"/AddFlow", f, p.opts()); err != nil {
			t.Fatalf("AddFlow %s: %v", f.FlowID, err)
		}
	}

	run, err := svcclient.Call[reconcileReq, reconcileResp](ctx, p.balance(),
		balanceSvc+"/Reconcile", reconcileReq{
			TenantID: p.tenant, WindowID: window.Window.ID, Actor: "e2e",
		}, p.opts())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if !run.Run.Converged {
		t.Fatalf("the reconciliation did not converge (%s); there is nothing to accept",
			run.Run.Reason)
	}

	accepted, err := svcclient.Call[acceptRunReq, acceptRunResp](ctx, p.balance(),
		balanceSvc+"/AcceptRun", acceptRunReq{
			TenantID: p.tenant, RunID: run.Run.ID, Actor: "supervisor",
		}, p.opts())
	if err != nil {
		t.Fatalf("AcceptRun: %v", err)
	}
	if accepted.Run.ID != run.Run.ID {
		t.Errorf("accepting run %s returned run %s", run.Run.ID, accepted.Run.ID)
	}

	// Once, and not again. Accepting is what makes a run's adjustments the
	// figures a plant works from, and a second acceptance would restate a
	// fortnight somebody has already signed off — with the second signature
	// overwriting the first, so the record would name the wrong person.
	//
	// This assertion was missing from the first version of this test, which was
	// called AcceptedOnce and never accepted twice.
	//
	// It takes removing two guards to reach it: the service refuses an already
	// accepted run, and the repository's UPDATE carries `AND accepted_at IS
	// NULL`. Either alone is enough, which is why mutating one at a time showed
	// nothing and looked for a while like the test was still empty.
	if _, err := svcclient.Call[acceptRunReq, acceptRunResp](ctx, p.balance(),
		balanceSvc+"/AcceptRun", acceptRunReq{
			TenantID: p.tenant, RunID: run.Run.ID, Actor: "someone-else",
		}, p.opts()); err == nil {
		t.Error("the same run was accepted twice; the second acceptance restates a " +
			"period already signed off and records a different person as having done it")
	}

	// A run nobody produced cannot be accepted. Accepting an id that is not
	// there must say so rather than report success over nothing.
	if _, err := svcclient.Call[acceptRunReq, acceptRunResp](ctx, p.balance(),
		balanceSvc+"/AcceptRun", acceptRunReq{
			TenantID: p.tenant, RunID: newID("run"), Actor: "supervisor",
		}, p.opts()); err == nil {
		t.Error("a run that does not exist was accepted")
	}

	// And another tenant cannot accept this one.
	stranger := newID("tnt")
	if _, err := svcclient.Call[acceptRunReq, acceptRunResp](ctx, p.balance(),
		balanceSvc+"/AcceptRun", acceptRunReq{
			TenantID: stranger, RunID: run.Run.ID, Actor: "supervisor",
		}, actingAs(stranger, "e2e")); err == nil {
		t.Error("a second tenant accepted a reconciliation run it did not produce")
	}
}
