//go:build e2e

// The five ways canonical-service is read.
//
// shadow_pipeline_test and decisions_test drive the identity layer forward —
// mapping, claiming, resolving — and between them they never read a slot, a
// policy or an identity back by any route but the one that created it. These
// five are what an operator actually uses: the queue of unresolved conflicts,
// what a slot holds now, which policy was in force when the milk was collected,
// and which external identifiers a source system has registered.
//
// Every one of them filters. ListConflicts filters on two columns, GetSlot on
// three, ListIdentities on a source system and on whether an identity has been
// retired, GetEffectivePolicy on a time window. A filter that has stopped
// filtering is invisible from the outside: the row somebody wanted is still in
// the answer, with other people's rows beside it. So each test below puts
// something in the way that must not come back.
package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

type listIdentitiesReq struct {
	TenantID       string `json:"tenant_id"`
	SourceSystemID string `json:"source_system_id"`
	Limit          int32  `json:"limit"`
	Offset         int32  `json:"offset"`
}

// fullIdentityProto names the fields the handler sends. shadow_pipeline_test's
// identityProto reads five of them; a listing is worth checking more closely,
// because it is the route by which somebody audits what a source system claims.
type fullIdentityProto struct {
	ID             string `json:"id"`
	TenantID       string `json:"tenant_id"`
	SourceSystemID string `json:"source_system_id"`
	EntityKind     string `json:"entity_kind"`
	ExternalID     string `json:"external_id"`
	EntityID       string `json:"entity_id"`
	Method         string `json:"method"`
	SupersededAt   string `json:"superseded_at,omitempty"`
}

type listIdentitiesResp struct {
	Identities []*fullIdentityProto `json:"identities"`
}

type getEffectiveCanonPolicyReq struct {
	TenantID string `json:"tenant_id"`
	At       string `json:"at,omitempty"`
}

type fullPolicyProto struct {
	ID            string   `json:"id"`
	TenantID      string   `json:"tenant_id"`
	Name          string   `json:"name"`
	Dimensions    []string `json:"dimensions"`
	Resolution    string   `json:"resolution"`
	Version       int32    `json:"version"`
	EffectiveFrom string   `json:"effective_from"`
	EffectiveTo   string   `json:"effective_to,omitempty"`
}

type getEffectiveCanonPolicyResp struct {
	Policy *fullPolicyProto `json:"policy"`
}

type listPoliciesReq struct {
	TenantID string `json:"tenant_id"`
}

type listPoliciesResp struct {
	Policies []*fullPolicyProto `json:"policies"`
}

type getSlotReq struct {
	TenantID   string `json:"tenant_id"`
	SlotKey    string `json:"slot_key"`
	OriginKind string `json:"origin_kind"`
}

// fullSlotProto reads the fields that say who holds a slot and on what
// authority, not only its id and status.
type fullSlotProto struct {
	ID                  string            `json:"id"`
	TenantID            string            `json:"tenant_id"`
	SlotKey             string            `json:"slot_key"`
	OriginKind          string            `json:"origin_kind"`
	PolicyID            string            `json:"policy_id"`
	PolicyVersion       int32             `json:"policy_version"`
	AuthoritativeRef    string            `json:"authoritative_ref"`
	Status              string            `json:"status"`
	IncumbentRecordedAt string            `json:"incumbent_recorded_at"`
	Values              map[string]string `json:"values"`
	Resolution          string            `json:"resolution,omitempty"`
	ResolvedBy          string            `json:"resolved_by,omitempty"`
}

type getSlotResp struct {
	Slot *fullSlotProto `json:"slot"`
}

type listConflictsReq struct {
	TenantID string `json:"tenant_id"`
	Limit    int32  `json:"limit"`
	Offset   int32  `json:"offset"`
}

type listConflictsResp struct {
	Slots []*fullSlotProto `json:"slots"`
}

// declarePolicyIn declares one policy for a tenant over a stated window.
//
// The tenant is a parameter because a tenant may have only one policy in force
// at a time — an EXCLUDE USING gist over the tenant and the window — so a test
// that declared into the shared tenant would collide with every other test that
// claims a slot.
func declarePolicyIn(t *testing.T, p *platform, tenant, name, resolution string, version int32, from, to time.Time) *policyProto {
	t.Helper()
	req := declarePolicyReq{
		TenantID: tenant, Name: name,
		Dimensions:    []string{"PRODUCER", "COLLECTION_DATE", "SHIFT"},
		Resolution:    resolution,
		Version:       version,
		EffectiveFrom: from.Format(time.RFC3339),
		Actor:         "e2e",
	}
	if !to.IsZero() {
		req.EffectiveTo = to.Format(time.RFC3339)
	}
	out, err := svcclient.Call[declarePolicyReq, declarePolicyResp](context.Background(), p.canonical(),
		canonicalSvc+"/DeclarePolicy", req,
		actingAs(tenant, "e2e"))
	if err != nil {
		t.Fatalf("DeclarePolicy %s: %v", name, err)
	}
	return out.Policy
}

// A slot reads back holding the claim that won it, under the key it was
// claimed with.
//
// The slot's identity is three columns — tenant, key and origin kind — and the
// origin kind is the one worth pressing on. The same collection, the same
// producer and the same shift arriving natively and arriving in an importer's
// file are deliberately two slots, because a figure the platform measured and a
// figure somebody typed into a spreadsheet are not interchangeable evidence. A
// GetSlot that ignored the origin would hand back the native slot when asked
// for the imported one, and the caller has no way to tell.
func TestASlotReadsBackUnderTheOriginItWasClaimedFor(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	tenant := newID("ten")
	opts := actingAs(tenant, "e2e")
	policy := declarePolicyIn(t, p, tenant, newID("pol"), "FIRST_WINS", 7,
		time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{})

	values := map[string]string{
		"PRODUCER":        newID("P"),
		"COLLECTION_DATE": "2026-02-14",
		"SHIFT":           "MORNING",
	}
	claimed, err := svcclient.Call[claimSlotReq, claimSlotResp](ctx, p.canonical(),
		canonicalSvc+"/ClaimSlot", claimSlotReq{
			TenantID: tenant, SourceRef: "obs-native", Values: values,
			Origin: "NATIVE", RecordedAt: "2026-02-14T06:00:00Z",
			CollectedAt: collectedAt, Actor: "e2e",
		}, opts)
	if err != nil {
		t.Fatalf("ClaimSlot: %v", err)
	}
	if claimed.Outcome != "SLOT_ESTABLISHED" {
		t.Fatalf("claim was %s (%s)", claimed.Outcome, claimed.Reason)
	}
	key := claimed.Slot.SlotKey

	got, err := svcclient.Call[getSlotReq, getSlotResp](ctx, p.canonical(),
		canonicalSvc+"/GetSlot", getSlotReq{
			TenantID: tenant, SlotKey: key, OriginKind: "NATIVE",
		}, opts)
	if err != nil {
		t.Fatalf("GetSlot: %v", err)
	}
	slot := got.Slot
	if slot == nil {
		t.Fatal("GetSlot answered with no slot for a key that was just claimed")
	}
	if slot.AuthoritativeRef != "obs-native" {
		t.Errorf("the slot is held by %q, and obs-native claimed it", slot.AuthoritativeRef)
	}
	// SETTLED, not ESTABLISHED: the claim outcome and the slot status are
	// separate vocabularies. SLOT_ESTABLISHED is what happened to the claim,
	// SETTLED is what the slot is now.
	if slot.Status != "SETTLED" {
		t.Errorf("slot status is %q, want SETTLED", slot.Status)
	}
	if slot.PolicyID != policy.ID || slot.PolicyVersion != 7 {
		t.Errorf("the slot was adjudicated under policy %s v%d, and reads back as %s v%d\n"+
			"Which policy version decided a slot is what makes the decision "+
			"re-derivable years later.",
			policy.ID, 7, slot.PolicyID, slot.PolicyVersion)
	}
	for dim, want := range values {
		if slot.Values[dim] != want {
			t.Errorf("slot value %s is %q, want %q", dim, slot.Values[dim], want)
		}
	}

	// The same key under a different origin is a different slot, and there is
	// nothing under it.
	_, err = svcclient.Call[getSlotReq, getSlotResp](ctx, p.canonical(),
		canonicalSvc+"/GetSlot", getSlotReq{
			TenantID: tenant, SlotKey: key, OriginKind: "IMPORTED",
		}, opts)
	if err == nil {
		t.Error("the natively claimed slot came back when asked for under IMPORTED\n" +
			"A measured figure and a figure out of somebody's spreadsheet are held " +
			"in separate slots on purpose; reading one for the other silently " +
			"changes what the number is evidence of.")
	}
}

// The conflict queue holds the conflicts, and only the ones still open.
//
// The query filters on status='CONFLICT' AND resolved_at IS NULL, and a call
// that merely checks the conflict somebody just made is in the list would pass
// with either clause gone. So this puts a settled slot in the way — one claimed
// in the same moment, never contested — which must not appear, and then
// resolves the conflict, which must leave.
func TestTheConflictQueueHoldsOnlyUnresolvedConflicts(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	tenant := newID("ten")
	opts := actingAs(tenant, "e2e")
	declarePolicyIn(t, p, tenant, newID("pol"), "MANUAL", 3,
		time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{})

	claim := func(ref, producer, recordedAt string) *claimSlotResp {
		t.Helper()
		out, err := svcclient.Call[claimSlotReq, claimSlotResp](ctx, p.canonical(),
			canonicalSvc+"/ClaimSlot", claimSlotReq{
				TenantID: tenant, SourceRef: ref,
				Values: map[string]string{
					"PRODUCER": producer, "COLLECTION_DATE": "2026-02-14", "SHIFT": "MORNING",
				},
				Origin: "NATIVE", RecordedAt: recordedAt,
				CollectedAt: collectedAt, Actor: "e2e",
			}, opts)
		if err != nil {
			t.Fatalf("ClaimSlot %s: %v", ref, err)
		}
		return out
	}

	// One producer's slot is contested; another's is settled and must stay out
	// of the queue.
	contested, settled := newID("P"), newID("P")
	claim("obs-one", contested, "2026-02-14T06:00:00Z")
	conflict := claim("obs-two", contested, "2026-02-14T09:00:00Z")
	if conflict.Outcome != "COLLECTION_SLOT_CONFLICT" {
		t.Fatalf("the second claim was %s, want a conflict under MANUAL", conflict.Outcome)
	}
	quiet := claim("obs-three", settled, "2026-02-14T06:00:00Z")
	if quiet.Outcome != "SLOT_ESTABLISHED" {
		t.Fatalf("the uncontested claim was %s, want SLOT_ESTABLISHED", quiet.Outcome)
	}

	queue, err := svcclient.Call[listConflictsReq, listConflictsResp](ctx, p.canonical(),
		canonicalSvc+"/ListConflicts", listConflictsReq{TenantID: tenant, Limit: 50}, opts)
	if err != nil {
		t.Fatalf("ListConflicts: %v", err)
	}
	inQueue := map[string]bool{}
	for _, s := range queue.Slots {
		inQueue[s.ID] = true
		if s.Status != "CONFLICT" {
			t.Errorf("the conflict queue holds slot %s with status %q", s.ID, s.Status)
		}
	}
	if !inQueue[conflict.Slot.ID] {
		t.Error("the contested slot is not in the conflict queue")
	}
	if inQueue[quiet.Slot.ID] {
		t.Error("a slot that was never contested is in the conflict queue\n" +
			"The queue is what an operator works through; filling it with " +
			"settled slots is how the real conflicts stop being looked at.")
	}

	// Resolving it takes it out of the queue: a conflict somebody has already
	// dealt with must not come back the next morning.
	//
	// Which clause of the query does that was measured rather than assumed, and
	// the answer is not the obvious one. ListConflicts filters on
	// `status='CONFLICT' AND resolved_at IS NULL`, and removing the second half
	// changes nothing at all — ResolveConflict sets status='SETTLED' and
	// resolved_at=NOW() in a single UPDATE, so no slot can ever be resolved
	// while still counting as conflicted. `resolved_at IS NULL` is a second
	// guard over a door the first one already shuts.
	//
	// The status clause is the one carrying the weight: removing it puts every
	// settled slot in the queue, and the check below on the uncontested slot
	// catches that. So this assertion is worth keeping for the behaviour it
	// states, and it should not be read as pinning the resolved_at clause,
	// because nothing does.
	if _, err := svcclient.Call[resolveConflictReq, resolveConflictResp](ctx, p.canonical(),
		canonicalSvc+"/ResolveConflict", resolveConflictReq{
			TenantID: tenant, SlotID: conflict.Slot.ID,
			AuthoritativeRef: "obs-two",
			Resolution:       "the nine o'clock reading is the tanker's",
			Actor:            "supervisor",
		}, opts); err != nil {
		t.Fatalf("ResolveConflict: %v", err)
	}

	after, err := svcclient.Call[listConflictsReq, listConflictsResp](ctx, p.canonical(),
		canonicalSvc+"/ListConflicts", listConflictsReq{TenantID: tenant, Limit: 50}, opts)
	if err != nil {
		t.Fatalf("ListConflicts after resolving: %v", err)
	}
	for _, s := range after.Slots {
		if s.ID == conflict.Slot.ID {
			t.Error("a resolved conflict is still in the queue")
		}
	}

	// And the resolution is readable from the slot itself, not only from the
	// response to the call that made it.
	resolved, err := svcclient.Call[getSlotReq, getSlotResp](ctx, p.canonical(),
		canonicalSvc+"/GetSlot", getSlotReq{
			TenantID: tenant, SlotKey: conflict.Slot.SlotKey, OriginKind: "NATIVE",
		}, opts)
	if err != nil {
		t.Fatalf("GetSlot after resolving: %v", err)
	}
	if resolved.Slot.ResolvedBy != "supervisor" {
		t.Errorf("the slot says it was resolved by %q, want supervisor — a slot "+
			"that merely changed hands is indistinguishable afterwards from one "+
			"somebody overwrote", resolved.Slot.ResolvedBy)
	}
	if resolved.Slot.Resolution == "" {
		t.Error("the slot carries no reason for its resolution")
	}
}

// A listing of identities answers for the source system it was asked about, and
// leaves out the ones that have been retired.
//
// Both matter to the same question. Somebody auditing a figure asks which of a
// society's codes the platform believes in now. An answer carrying another
// society's codes is confusing; one carrying a mapping that was withdrawn is
// worse, because it names a producer the platform has stopped paying.
func TestListIdentitiesAnswersForOneSourceSystemAndOmitsRetiredOnes(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	tenant := newID("ten")
	opts := actingAs(tenant, "e2e")
	mine, theirs := newID("src"), newID("src")

	mapOne := func(source, external string) *identityProto {
		t.Helper()
		out, err := svcclient.Call[mapIdentityReq, mapIdentityResp](ctx, p.canonical(),
			canonicalSvc+"/MapIdentity", mapIdentityReq{
				TenantID: tenant, SourceSystemID: source, EntityKind: "PRODUCER",
				ExternalID: external, EntityID: newID("ent"), Method: "MANUAL",
				ValidFrom: "2020-01-01T00:00:00Z", Actor: "e2e",
			}, opts)
		if err != nil {
			t.Fatalf("MapIdentity %s/%s: %v", source, external, err)
		}
		return out.Identity
	}

	kept := mapOne(mine, "P-KEPT")
	retired := mapOne(mine, "P-RETIRED")
	other := mapOne(theirs, "P-OTHER")

	if _, err := svcclient.Call[retireIdentityReq, struct{}](ctx, p.canonical(),
		canonicalSvc+"/RetireIdentity", retireIdentityReq{
			TenantID: tenant, ID: retired.ID, Actor: "e2e",
		}, opts); err != nil {
		t.Fatalf("RetireIdentity: %v", err)
	}

	got, err := svcclient.Call[listIdentitiesReq, listIdentitiesResp](ctx, p.canonical(),
		canonicalSvc+"/ListIdentities", listIdentitiesReq{
			TenantID: tenant, SourceSystemID: mine, Limit: 50,
		}, opts)
	if err != nil {
		t.Fatalf("ListIdentities: %v", err)
	}

	seen := map[string]bool{}
	for _, i := range got.Identities {
		seen[i.ID] = true
		if i.SourceSystemID != mine {
			t.Errorf("identity %s comes from source system %q, and %q was asked for",
				i.ExternalID, i.SourceSystemID, mine)
		}
	}
	if !seen[kept.ID] {
		t.Error("the live mapping is missing from the listing")
	}
	if seen[retired.ID] {
		t.Error("a retired mapping is in the listing\n" +
			"Retiring one is how a society says a code no longer means that " +
			"producer. A listing that still carries it names somebody the " +
			"platform has stopped paying.")
	}
	if seen[other.ID] {
		t.Error("another source system's mapping is in the listing")
	}

	// Asking with no source system named is the deliberate other case: the
	// filter is `$2='' OR source_system_id=$2`, so an empty one means all of
	// them rather than none.
	all, err := svcclient.Call[listIdentitiesReq, listIdentitiesResp](ctx, p.canonical(),
		canonicalSvc+"/ListIdentities", listIdentitiesReq{TenantID: tenant, Limit: 50}, opts)
	if err != nil {
		t.Fatalf("ListIdentities unfiltered: %v", err)
	}
	everything := map[string]bool{}
	for _, i := range all.Identities {
		everything[i.ID] = true
	}
	if !everything[kept.ID] || !everything[other.ID] {
		t.Errorf("asking without a source system returned %d identities and should "+
			"have held both systems' live mappings", len(all.Identities))
	}
	if everything[retired.ID] {
		t.Error("the retired mapping came back when no source system was named")
	}
}

// The policy in force is the one whose window covers the moment, and the
// listing shows the whole history.
//
// A slot claimed today is adjudicated under the policy in force when the milk
// was collected, not the one in force now — that is what CollectedAt on a claim
// is for. So asking "which policy applies" is always asking about a moment, and
// an implementation that returned the newest policy would be right every day
// except the ones that matter.
func TestTheCanonicalPolicyInForceIsTheOneCoveringTheMoment(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	tenant := newID("ten")
	opts := actingAs(tenant, "e2e")

	firstFrom := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)
	secondFrom := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

	// The two differ in resolution, not just in name: under FIRST_WINS the
	// platform decides a collision itself and under MANUAL it refuses to. Which
	// one applied to a disputed collection is the whole question.
	old := declarePolicyIn(t, p, tenant, "early", "FIRST_WINS", 1, firstFrom, secondFrom)
	current := declarePolicyIn(t, p, tenant, "current", "MANUAL", 2, secondFrom, time.Time{})

	for _, c := range []struct {
		when       time.Time
		wantID     string
		wantName   string
		wantResolv string
	}{
		{time.Date(2022, 6, 1, 0, 0, 0, 0, time.UTC), old.ID, "early", "FIRST_WINS"},
		{time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC), current.ID, "current", "MANUAL"},
	} {
		got, err := svcclient.Call[getEffectiveCanonPolicyReq, getEffectiveCanonPolicyResp](ctx, p.canonical(),
			canonicalSvc+"/GetEffectivePolicy", getEffectiveCanonPolicyReq{
				TenantID: tenant, At: c.when.Format(time.RFC3339),
			}, opts)
		if err != nil {
			t.Fatalf("GetEffectivePolicy at %s: %v", c.when.Format(time.DateOnly), err)
		}
		if got.Policy == nil {
			t.Fatalf("no policy in force at %s, and one was declared covering it",
				c.when.Format(time.DateOnly))
		}
		if got.Policy.ID != c.wantID {
			t.Errorf("the policy in force at %s is %q (%s), want %q (%s) — the "+
				"answer is the window covering the moment, not the newest declaration",
				c.when.Format(time.DateOnly), got.Policy.Name, got.Policy.ID,
				c.wantName, c.wantID)
		}
		if got.Policy.Resolution != c.wantResolv {
			t.Errorf("the resolution in force at %s is %q, want %q — this decides "+
				"whether the platform adjudicates a collision or refers it to a person",
				c.when.Format(time.DateOnly), got.Policy.Resolution, c.wantResolv)
		}
	}

	// Nothing was in force before the first policy began, and a claim collected
	// then has no policy to be judged under.
	if _, err := svcclient.Call[getEffectiveCanonPolicyReq, getEffectiveCanonPolicyResp](ctx, p.canonical(),
		canonicalSvc+"/GetEffectivePolicy", getEffectiveCanonPolicyReq{
			TenantID: tenant, At: "2019-06-01T00:00:00Z",
		}, opts); err == nil {
		t.Error("a policy is in force two years before any was declared")
	}

	// The listing carries the history, not just what applies today. A superseded
	// policy is how a decision made in 2022 is explained in 2026.
	listed, err := svcclient.Call[listPoliciesReq, listPoliciesResp](ctx, p.canonical(),
		canonicalSvc+"/ListPolicies", listPoliciesReq{TenantID: tenant}, opts)
	if err != nil {
		t.Fatalf("ListPolicies: %v", err)
	}
	if len(listed.Policies) != 2 {
		t.Fatalf("the tenant has declared 2 policies and %d are listed", len(listed.Policies))
	}
	// Newest first, which is what `ORDER BY effective_from DESC` promises.
	if listed.Policies[0].ID != current.ID || listed.Policies[1].ID != old.ID {
		t.Errorf("policies are listed %q then %q, want the newest window first",
			listed.Policies[0].Name, listed.Policies[1].Name)
	}
	if listed.Policies[1].EffectiveTo == "" {
		t.Error("the superseded policy is listed with no end to its window, so " +
			"nothing says when it stopped applying")
	}
}
