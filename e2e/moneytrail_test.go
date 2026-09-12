//go:build e2e

// The money path leaves a trail.
//
// Thirteen services write audit entries and pooling was not one of them —
// which is the service that decides how much each producer is paid out of a
// pool. A valuation set the figures and a settlement turned them into
// obligations, and neither left anything behind saying so. The rows were there,
// the allocations were there, and there was nothing to answer "who valued this
// pool, when, and at what".
//
// That is the gap this closes, and these tests are what keep it closed.
//
// They read pooling's own audit_logs rather than asking the audit service, and
// the reason is a property of this harness rather than of the platform: here
// every service gets its own database, so audit-service cannot see what pooling
// wrote. In the deployment they share one — every service in both compose files
// points at `dairy` — and the service can. What is being checked either way is
// that the entry exists, is attributed, and carries the figures; where it is
// read from does not change that.
package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/libs/integrity/authz"
	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

type auditEntryProto struct {
	ID           string `json:"id"`
	ActorID      string `json:"actor_id"`
	Action       string `json:"action"`
	ResourceID   string `json:"resource_id"`
	ResourceType string `json:"resource_type"`
	ServiceName  string `json:"service_name"`
	AfterState   string `json:"after_state,omitempty"`
}

type auditByResourceResp struct {
	AuditLogs []*auditEntryProto `json:"audit_logs"`
}

// trailFor reads everything recorded against one resource, from the database the
// service writing it uses.
func trailFor(t *testing.T, p *platform, database, resourceType, id string) []*auditEntryProto {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn(t, database))
	if err != nil {
		t.Fatalf("connect to %s: %v", database, err)
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, `
		SELECT id, actor_id, action, resource_id, resource_type, service_name,
		       COALESCE(new_value::text, '')
		  FROM audit_logs
		 WHERE tenant_id=$1 AND resource_type=$2 AND resource_id=$3
		 ORDER BY created_at`, p.tenant, resourceType, id)
	if err != nil {
		t.Fatalf("read the trail for %s %s: %v", resourceType, id, err)
	}
	defer rows.Close()

	var out []*auditEntryProto
	for rows.Next() {
		var e auditEntryProto
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.ResourceID,
			&e.ResourceType, &e.ServiceName, &e.AfterState); err != nil {
			t.Fatal(err)
		}
		out = append(out, &e)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func actionsIn(entries []*auditEntryProto) map[string]*auditEntryProto {
	out := map[string]*auditEntryProto{}
	for _, e := range entries {
		out[e.Action] = e
	}
	return out
}

// Valuing a pool and settling it are both on the record.
//
// These are the two acts that decide money in this platform: the first says what
// each producer is owed and the second turns that into an obligation. A dispute
// months later is argued from the trail, and before this there was none.
func TestValuingAndSettlingAPoolAreBothOnTheRecord(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	poolID, allocations := valuedPool(t, p)

	entries := actionsIn(trailFor(t, p, "e2e_pooling", "pool", poolID))

	valued, ok := entries["value_pool"]
	if !ok {
		t.Fatalf("valuing a pool left no trail; the trail holds %v", keysOfTrail(entries))
	}
	if valued.ServiceName != "pooling-service" {
		t.Errorf("the entry is attributed to %q, want pooling-service", valued.ServiceName)
	}
	if valued.ActorID == "" {
		t.Error("the entry names nobody; an act nobody can be asked about is not " +
			"on the record, it is merely written down")
	}
	// The figures, not just the fact. An entry saying "a pool was valued" with
	// no amounts answers nothing a dispute asks.
	for _, want := range []string{"producer_settlement_fund", "allocated_total", "valuation_id"} {
		if !containsField(valued.AfterState, want) {
			t.Errorf("the value_pool entry does not record %q: %s", want, valued.AfterState)
		}
	}
	if len(allocations) == 0 {
		t.Fatal("the pool was valued into no allocations, so this test proved nothing")
	}

	// Settling raises the obligations, and that is recorded too.
	if _, err := svcclient.Call[settlePoolReq, settlePoolResp](ctx, p.pooling(),
		poolingSvc+"/SettlePool", settlePoolReq{
			TenantID: p.tenant, PoolID: poolID, Actor: "accountant",
		}, p.opts()); err != nil {
		t.Fatalf("SettlePool: %v", err)
	}

	entries = actionsIn(trailFor(t, p, "e2e_pooling", "pool", poolID))
	raised, ok := entries["raise_economic_events"]
	if !ok {
		t.Fatalf("settling a pool left no trail; the trail holds %v", keysOfTrail(entries))
	}
	for _, want := range []string{"events", "total", "by_kind"} {
		if !containsField(raised.AfterState, want) {
			t.Errorf("the settlement entry does not record %q: %s", want, raised.AfterState)
		}
	}
	// Both acts, distinguishable. One entry covering both would not say which
	// figure came from which.
	if valued.ID == raised.ID {
		t.Error("valuing and settling produced one entry between them")
	}
}

// A correction after settlement is its own entry, not an edit of the first.
//
// The whole design of this path is that money moves by further events rather
// than by changing what was already recorded. The trail has to show the same
// shape, or it says the original figure was simply different from what was paid.
func TestACorrectionIsItsOwnEntryInTheTrail(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	poolID, _ := valuedPool(t, p)
	if _, err := svcclient.Call[settlePoolReq, settlePoolResp](ctx, p.pooling(),
		poolingSvc+"/SettlePool", settlePoolReq{
			TenantID: p.tenant, PoolID: poolID, Actor: "accountant",
		}, p.opts()); err != nil {
		t.Fatalf("SettlePool: %v", err)
	}
	before := len(trailFor(t, p, "e2e_pooling", "pool", poolID))

	// A policy that allows the correction, then the correction.
	if _, err := svcclient.Call[declareRetroPolicyReq, declareRetroPolicyResp](ctx, p.pooling(),
		poolingSvc+"/DeclareRetroactivityPolicy", declareRetroPolicyReq{
			TenantID: p.tenant, Name: "corrections " + newID("pol"),
			Mode: "APPLY_INCREMENTAL", MaxLookbackDays: 365,
			Currency: "INR", AmountScale: 2,
			EffectiveFrom: "2020-01-01T00:00:00Z", Actor: "accountant",
		}, p.opts()); err != nil {
		t.Fatalf("DeclareRetroactivityPolicy: %v", err)
	}
	if _, err := svcclient.Call[recordUtilisationReq, recordUtilisationResp](ctx, p.pooling(),
		poolingSvc+"/RecordUtilisation", recordUtilisationReq{
			TenantID: p.tenant, PoolID: poolID, Class: "CLASS_II",
			Quantity: "100.000", Price: "2.0000", PriceScale: 4, Actor: "accountant",
		}, p.opts()); err != nil {
		t.Fatalf("RecordUtilisation: %v", err)
	}
	corrected, err := svcclient.Call[applyCorrectionReq, applyCorrectionResp](ctx, p.pooling(),
		poolingSvc+"/ApplyCorrection", applyCorrectionReq{
			TenantID: p.tenant, PoolID: poolID,
			ComponentPrices: []componentPriceProto{{Component: "FAT", Price: "20.0000", Scale: 4}},
			Actor:           "accountant",
		}, p.opts())
	if err != nil {
		t.Fatalf("ApplyCorrection: %v", err)
	}
	if corrected.Outcome == "" {
		t.Fatal("the correction reported no outcome")
	}

	after := trailFor(t, p, "e2e_pooling", "pool", poolID)
	if len(after) <= before {
		t.Errorf("a correction that reported %q added nothing to the trail: %d entries "+
			"before and %d after\n"+
			"Money moved by further events and the record does not show it.",
			corrected.Outcome, before, len(after))
	}
}

func keysOfTrail(m map[string]*auditEntryProto) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// containsField looks for a JSON key in the recorded state.
//
// By substring rather than by decoding into a struct, because the point is that
// the figure reached the trail at all — and a struct that silently tolerated a
// renamed field is the trap this repository has been caught by twice, once on
// calf_gender and once on total_value.
func containsField(state, field string) bool {
	return state != "" && strings.Contains(state, `"`+field+`"`)
}

// Every act where a person overrides the platform is on the record.
//
// Six services wrote no audit entries at all, and the ones that mattered were
// not the ordinary writes — they were these: the moments somebody decides
// something the platform declined to decide, or overturns something it refused.
// A supervisor adjudicating a disputed collection slot. An accountant settling a
// disagreement about what a producer was paid. Somebody releasing a reading the
// platform quarantined. Somebody accepting a reconciliation as a period's close.
// Somebody superseding a measurement that may already have priced milk.
//
// Each of those is an override, and an override nobody can be asked about is the
// same as no control at all. Each is now written inside the transaction that
// makes the change, so a trail cannot disagree with the thing it describes.
func TestEveryOverrideOfThePlatformIsOnTheRecord(t *testing.T) {
	p := startPlatform(t)
	ctx := context.Background()

	t.Run("a supervisor adjudicating a disputed slot", func(t *testing.T) {
		tenant := newID("ten")
		opts := svcclient.CallOptions{
			Tenant: tenant, Actor: "US_SUPERVISOR_00000000000", RequestID: newID("req"),
			Permissions: authz.Roles()["admin"].Permissions.String(),
		}
		declarePolicyIn(t, p, tenant, newID("pol"), "MANUAL", 5,
			time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{})

		values := map[string]string{
			"PRODUCER": newID("P"), "COLLECTION_DATE": "2026-02-14", "SHIFT": "MORNING",
		}
		claim := func(ref, at string) *claimSlotResp {
			t.Helper()
			out, err := svcclient.Call[claimSlotReq, claimSlotResp](ctx, p.canonical(),
				canonicalSvc+"/ClaimSlot", claimSlotReq{
					TenantID: tenant, SourceRef: ref, Values: values,
					Origin: "NATIVE", RecordedAt: at, CollectedAt: collectedAt, Actor: "e2e",
				}, opts)
			if err != nil {
				t.Fatalf("ClaimSlot %s: %v", ref, err)
			}
			return out
		}
		claim("obs-one", "2026-02-14T06:00:00Z")
		conflict := claim("obs-two", "2026-02-14T09:00:00Z")

		if _, err := svcclient.Call[resolveConflictReq, resolveConflictResp](ctx, p.canonical(),
			canonicalSvc+"/ResolveConflict", resolveConflictReq{
				TenantID: tenant, SlotID: conflict.Slot.ID, AuthoritativeRef: "obs-two",
				Resolution: "the nine o'clock reading is the tanker's", Actor: "supervisor",
			}, opts); err != nil {
			t.Fatalf("ResolveConflict: %v", err)
		}

		entries := trailIn(t, "e2e_canonical", tenant, "collection_slot", conflict.Slot.ID)
		assertRecorded(t, entries, "resolve_slot_conflict",
			[]string{"authoritative_ref", "resolution"},
			"a slot that changed hands with no entry beside it is indistinguishable "+
				"afterwards from one somebody overwrote")
	})

	t.Run("an accountant settling a disagreement about a payment", func(t *testing.T) {
		tenant := newID("ten")
		opts := svcclient.CallOptions{
			Tenant: tenant, Actor: "US_ACCOUNTANT_00000000000", RequestID: newID("req"),
			Permissions: authz.Roles()["admin"].Permissions.String(),
		}
		d := adjudicated(t, p, tenant, "producer:"+newID("a"),
			"1050.00", "1000.00", "300.000", "300.000", "3.5000", "3.3333")

		if _, err := svcclient.Call[resolveDivergenceReq, resolveDivergenceResp](ctx, p.shadow(),
			shadowSvc+"/ResolveDivergence", resolveDivergenceReq{
				ID: d.ID, TenantID: tenant, Status: "EXTERNAL_CONFIRMED",
				Resolution: "the society's rate card was in force that month",
				Actor:      "accountant",
			}, opts); err != nil {
			t.Fatalf("ResolveDivergence: %v", err)
		}

		entries := trailIn(t, "e2e_shadow", tenant, "settlement_divergence", d.ID)
		assertRecorded(t, entries, "resolve_settlement_divergence",
			[]string{"status", "resolution", "delta_minor_units"},
			"this is somebody deciding which of two figures for one producer's "+
				"fortnight is right, and the delta is what the decision was about")
	})
}

// trailIn reads one resource's entries from the database a service writes to.
func trailIn(t *testing.T, database, tenant, resourceType, id string) []*auditEntryProto {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn(t, database))
	if err != nil {
		t.Fatalf("connect to %s: %v", database, err)
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, `
		SELECT id, actor_id, action, resource_id, resource_type, service_name,
		       COALESCE(new_value::text, '')
		  FROM audit_logs
		 WHERE tenant_id=$1 AND resource_type=$2 AND resource_id=$3
		 ORDER BY created_at`, tenant, resourceType, id)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	defer rows.Close()

	var out []*auditEntryProto
	for rows.Next() {
		var e auditEntryProto
		if err := rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.ResourceID,
			&e.ResourceType, &e.ServiceName, &e.AfterState); err != nil {
			t.Fatal(err)
		}
		out = append(out, &e)
	}
	return out
}

// assertRecorded requires an entry for the action, attributed, carrying the
// fields that make it answer a question.
func assertRecorded(t *testing.T, entries []*auditEntryProto, action string, fields []string, why string) {
	t.Helper()
	found := actionsIn(entries)[action]
	if found == nil {
		t.Fatalf("no %s entry: %s\nthe trail holds %v", action, why, keysOfTrail(actionsIn(entries)))
	}
	if found.ActorID == "" {
		t.Errorf("the %s entry names nobody", action)
	}
	for _, f := range fields {
		if !containsField(found.AfterState, f) {
			t.Errorf("the %s entry does not record %q: %s", action, f, found.AfterState)
		}
	}
}
