//go:build e2e

// audit-service's own endpoints.
//
// The hash chain itself is thoroughly covered by audit_test.go, which drives it
// against a real database: a sealed chain verifies, sealing twice is idempotent,
// rows written afterwards extend the same chain, and changing or removing a
// sealed row is detected. All of that goes through SQL.
//
// What had never been called is the service that publishes it. SealAuditChain
// and VerifyAuditChain are how an operator actually runs and checks the thing,
// and a chain that is sound but unreachable is a guarantee nobody can exercise.
// The four read endpoints are how somebody answers "who changed this, and when".
package e2e

import (
	"context"
	"testing"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
)

const auditSvc = "audit.v1.AuditService"

type createAuditLogReq struct {
	TenantID     string `json:"tenant_id"`
	ActorID      string `json:"actor_id"`
	ActorType    string `json:"actor_type"`
	Action       string `json:"action"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	OldValue     string `json:"old_value"`
	NewValue     string `json:"new_value"`
	ServiceName  string `json:"service_name"`
	CreatedBy    string `json:"created_by"`
}

type auditLogResp struct {
	AuditLog *struct {
		ID           string `json:"id"`
		ActorID      string `json:"actor_id"`
		Action       string `json:"action"`
		ResourceType string `json:"resource_type"`
		ResourceID   string `json:"resource_id"`
		OldValue     string `json:"old_value"`
		NewValue     string `json:"new_value"`
	} `json:"audit_log"`
}

type listAuditLogsReq struct {
	TenantID string `json:"tenant_id"`
}

type listByResourceReq struct {
	TenantID     string `json:"tenant_id"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
}

type listByActorReq struct {
	TenantID string `json:"tenant_id"`
	ActorID  string `json:"actor_id"`
}

type listAuditLogsResp struct {
	AuditLogs []*struct {
		ID           string `json:"id"`
		ActorID      string `json:"actor_id"`
		ResourceID   string `json:"resource_id"`
		ResourceType string `json:"resource_type"`
	} `json:"audit_logs"`
}

type sealChainReq struct {
	TenantID string `json:"tenant_id"`
	Limit    int    `json:"limit,omitempty"`
}

type sealChainResp struct {
	Sealed   int64  `json:"sealed"`
	LastSeq  int64  `json:"last_seq"`
	LastHash string `json:"last_hash"`
}

type verifyChainReq struct {
	TenantID string `json:"tenant_id"`
}

type verifyChainResp struct {
	Intact      bool   `json:"intact"`
	RowsChecked int64  `json:"rows_checked"`
	BrokenAtSeq int64  `json:"broken_at_seq"`
	Detail      string `json:"detail"`
}

func anEntry(t *testing.T, p *platform, tenant, actor, resourceType, resourceID string) *auditLogResp {
	t.Helper()
	out, err := svcclient.Call[createAuditLogReq, auditLogResp](
		context.Background(), p.audit(), auditSvc+"/CreateAuditLog",
		createAuditLogReq{TenantID: tenant, ActorID: actor, ActorType: "user",
			Action: "update_price", ResourceType: resourceType, ResourceID: resourceID,
			OldValue: `{"price":"42.50"}`, NewValue: `{"price":"47.75"}`,
			ServiceName: "e2e", CreatedBy: actor},
		svcclient.CallOptions{Tenant: tenant, Actor: actor})
	if err != nil {
		t.Fatalf("write an audit entry: %v", err)
	}
	return out
}

// An entry is written and read back with the values it replaced.
//
// The figure that was replaced is the whole point of the entry. One that records
// only that a change happened cannot reconcile an invoice raised before it.
func TestAnAuditEntryKeepsWhatItReplaced(t *testing.T) {
	p := startPlatform(t)
	resource := newID("sku")

	made := anEntry(t, p, p.tenant, "supervisor", "sku", resource)
	got, err := svcclient.Call[idTenantReq, auditLogResp](
		context.Background(), p.audit(), auditSvc+"/GetAuditLog",
		idTenantReq{ID: made.AuditLog.ID, TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("get audit log: %v", err)
	}
	if got.AuditLog.OldValue == "" {
		t.Error("the entry came back with no old_value; a record that a price changed " +
			"without the price it replaced cannot reconcile an invoice raised before it")
	}
	if got.AuditLog.NewValue == "" {
		t.Error("the entry came back with no new_value")
	}
	if got.AuditLog.ResourceID != resource {
		t.Errorf("the entry is against %s, not the resource it was written for",
			got.AuditLog.ResourceID)
	}
}

// The trail can be read by resource and by actor, and each filter filters.
//
// These are the two questions somebody actually asks: what happened to this
// thing, and what did this person do. A filter that ignores its argument answers
// both with the whole trail, which is the right kind of answer and the wrong one.
func TestTheTrailAnswersByResourceAndByActor(t *testing.T) {
	p := startPlatform(t)
	mine, theirs := newID("sku"), newID("sku")
	me, somebody := newID("usr"), newID("usr")

	anEntry(t, p, p.tenant, me, "sku", mine)
	anEntry(t, p, p.tenant, me, "sku", mine)
	anEntry(t, p, p.tenant, somebody, "sku", theirs)

	byResource, err := svcclient.Call[listByResourceReq, listAuditLogsResp](
		context.Background(), p.audit(), auditSvc+"/ListAuditLogsByResource",
		listByResourceReq{TenantID: p.tenant, ResourceType: "sku", ResourceID: mine},
		p.opts())
	if err != nil {
		t.Fatalf("list by resource: %v", err)
	}
	if len(byResource.AuditLogs) != 2 {
		t.Errorf("one resource's history holds %d entries, want 2 — another resource "+
			"was changed the same moment and its entry is not this one's",
			len(byResource.AuditLogs))
	}
	for _, e := range byResource.AuditLogs {
		if e.ResourceID != mine {
			t.Errorf("an entry for %s came back in %s's history", e.ResourceID, mine)
		}
	}

	byActor, err := svcclient.Call[listByActorReq, listAuditLogsResp](
		context.Background(), p.audit(), auditSvc+"/ListAuditLogsByActor",
		listByActorReq{TenantID: p.tenant, ActorID: me}, p.opts())
	if err != nil {
		t.Fatalf("list by actor: %v", err)
	}
	if len(byActor.AuditLogs) != 2 {
		t.Errorf("one actor's history holds %d entries, want 2", len(byActor.AuditLogs))
	}
	for _, e := range byActor.AuditLogs {
		if e.ActorID != me {
			t.Errorf("%s's entry came back in %s's history", e.ActorID, me)
		}
	}

	// And the unfiltered list holds at least all three, so the two above are
	// filtering rather than the table being small.
	all, err := svcclient.Call[listAuditLogsReq, listAuditLogsResp](
		context.Background(), p.audit(), auditSvc+"/ListAuditLogs",
		listAuditLogsReq{TenantID: p.tenant}, p.opts())
	if err != nil {
		t.Fatalf("list audit logs: %v", err)
	}
	if len(all.AuditLogs) < 3 {
		t.Errorf("the unfiltered trail holds %d entries and three were written",
			len(all.AuditLogs))
	}
}

// The chain seals and verifies through the service, not only through SQL.
//
// audit_test.go proves the chain is tamper-evident against a real database. This
// proves an operator can actually run and check it: a guarantee that only holds
// when somebody writes the right SQL is one nobody runs on a Tuesday.
func TestTheChainSealsAndVerifiesThroughTheService(t *testing.T) {
	p := startPlatform(t)
	tenant := newID("tnt")
	for i := 0; i < 3; i++ {
		anEntry(t, p, tenant, "supervisor", "sku", newID("sku"))
	}

	sealed, err := svcclient.Call[sealChainReq, sealChainResp](
		context.Background(), p.audit(), auditSvc+"/SealAuditChain",
		sealChainReq{TenantID: tenant},
		svcclient.CallOptions{Tenant: tenant, Actor: "e2e"})
	if err != nil {
		t.Fatalf("seal chain: %v", err)
	}
	if sealed.Sealed != 3 {
		t.Fatalf("sealing took in %d rows, want the 3 that were written", sealed.Sealed)
	}
	if sealed.LastHash == "" {
		t.Error("the seal reports no hash, so there is nothing to check the next one against")
	}

	ok, err := svcclient.Call[verifyChainReq, verifyChainResp](
		context.Background(), p.audit(), auditSvc+"/VerifyAuditChain",
		verifyChainReq{TenantID: tenant},
		svcclient.CallOptions{Tenant: tenant, Actor: "e2e"})
	if err != nil {
		t.Fatalf("verify chain: %v", err)
	}
	if !ok.Intact {
		t.Errorf("a chain sealed a moment ago does not verify: %s (broken at %d)",
			ok.Detail, ok.BrokenAtSeq)
	}
	if ok.RowsChecked != 3 {
		t.Errorf("verification checked %d rows, want 3 — a verifier that checks none "+
			"reports intact for a chain it never read", ok.RowsChecked)
	}

	// Sealing again takes in nothing and leaves the chain where it was. Two
	// operators running the sealer must not produce two chains.
	again, err := svcclient.Call[sealChainReq, sealChainResp](
		context.Background(), p.audit(), auditSvc+"/SealAuditChain",
		sealChainReq{TenantID: tenant},
		svcclient.CallOptions{Tenant: tenant, Actor: "e2e"})
	if err != nil {
		t.Fatalf("seal again: %v", err)
	}
	if again.Sealed != 0 {
		t.Errorf("a second seal took in %d rows, want 0", again.Sealed)
	}
	if again.LastHash != sealed.LastHash {
		t.Errorf("the chain head moved on a seal that took in nothing: %s then %s",
			sealed.LastHash, again.LastHash)
	}
}

// One tenant's trail is its own.
//
// The audit trail holds the old values of everything that changed. It is the
// last place a leak would be noticed and the worst place to have one.
func TestTheAuditTrailKeepsTenantsApart(t *testing.T) {
	p := startPlatform(t)
	mine, theirs := newID("tnt"), newID("tnt")
	anEntry(t, p, mine, "supervisor", "sku", newID("sku"))

	own, err := svcclient.Call[listAuditLogsReq, listAuditLogsResp](
		context.Background(), p.audit(), auditSvc+"/ListAuditLogs",
		listAuditLogsReq{TenantID: mine},
		svcclient.CallOptions{Tenant: mine, Actor: "e2e"})
	if err != nil {
		t.Fatalf("list own trail: %v", err)
	}
	if len(own.AuditLogs) == 0 {
		t.Fatal("the tenant that wrote an entry sees none, so the check below would " +
			"pass against a list that returns nothing to anybody")
	}

	other, err := svcclient.Call[listAuditLogsReq, listAuditLogsResp](
		context.Background(), p.audit(), auditSvc+"/ListAuditLogs",
		listAuditLogsReq{TenantID: theirs},
		svcclient.CallOptions{Tenant: theirs, Actor: "e2e"})
	if err == nil && len(other.AuditLogs) > 0 {
		t.Errorf("a second tenant reads %d audit entries it did not write; the trail "+
			"holds the old value of everything that changed", len(other.AuditLogs))
	}
}
