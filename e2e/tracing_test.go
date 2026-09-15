//go:build e2e

// One identifier, all the way through.
//
// There was no tracing, and what stood in for it made the absence hard to see.
// svcclient set an X-Request-Id on every call and every caller generated a fresh
// one, so the platform had an identifier that looked like a correlation id and
// correlated nothing: a settlement that gathers a fortnight touches procurement,
// canonical and notification, and each hop carried a different one. And
// audit.Entry had a TraceID field that nothing ever set, so every row in the
// trail — in a column that is in the schema, in audit-service's domain model,
// and on its wire format — carried an empty string.
//
// These tests run against services that are really running, because the part
// that matters is the part unit tests cannot reach: that the header survives a
// real HTTP hop, that the middleware puts it on the context, and that the audit
// write two layers down finds it there.
package e2e

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/libs/integrity/svcclient"
	"github.com/ppusapati/gavya/libs/integrity/tracing"
)

// tracesInTheTrail is every trace id milk-service has written, read from the
// database milk-service writes to.
//
// Read from the table rather than through audit-service, and that is not a
// shortcut: the audit trail is written inside the caller's own transaction, so
// the row lands in the database of the service that made the change. In this
// harness each service has its own, so asking audit-service would ask the wrong
// database and come back empty — which it did, on the first version of these
// tests, and looked exactly like a trace that was not being recorded.
func tracesInTheTrail(t *testing.T, tenant string) map[string]int {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), dsn(t, "e2e_milk"))
	if err != nil {
		t.Fatalf("connect to milk-service's database: %v", err)
	}
	defer conn.Close(context.Background())

	rows, err := conn.Query(context.Background(),
		`SELECT COALESCE(trace_id, '') FROM audit_logs WHERE tenant_id = $1`, tenant)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	defer rows.Close()

	found := map[string]int{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		found[id]++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return found
}

// The trail can be joined to the request that caused it.
//
// This is the whole point of the field being populated. Given a slow or wrong
// settlement, the question is "what did that request actually do", and until now
// the answer had to be assembled from timestamps across four services' logs.
func TestAChangeRecordsTheRequestThatCausedIt(t *testing.T) {
	p := startPlatform(t)
	trace := tracing.New()
	ctx := tracing.With(context.Background(), trace)

	cattle := newID("cow")
	sess, err := svcclient.Call[createMilkSessionReq, milkSessionResp](
		ctx, p.milk(), milkSvc+"/CreateSession",
		createMilkSessionReq{TenantID: p.tenant, CattleID: cattle,
			ShiftType: "morning", Timezone: tenantTimezone, CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := svcclient.Call[recordMilkReq, milkRecordResp](
		ctx, p.milk(), milkSvc+"/RecordMilk",
		recordMilkReq{TenantID: p.tenant, SessionID: sess.Session.ID,
			CattleID: cattle, QuantityLiters: 6.25, CreatedBy: "e2e"}, p.opts()); err != nil {
		t.Fatalf("record milk: %v", err)
	}

	found := tracesInTheTrail(t, p.tenant)
	if len(found) == 0 {
		t.Fatal("the trail is empty, so this proves nothing either way")
	}
	if found[trace.Trace()] == 0 {
		t.Errorf("nothing in the trail carries the trace %s that made it. The field "+
			"exists, the column exists, and if nothing fills them the trail cannot be "+
			"joined to the request that produced it — which is the state this was in "+
			"from the beginning.\nsaw: %v", trace.Trace(), keysOfTrace(found))
	}
}

// Two requests are two traces.
//
// Without this the test above passes against a constant: a platform that wrote
// the same trace id on every row would satisfy it and would be useless.
func TestTwoRequestsAreTwoTraces(t *testing.T) {
	p := startPlatform(t)

	seen := make([]string, 0, 2)
	for range 2 {
		trace := tracing.New()
		ctx := tracing.With(context.Background(), trace)
		cattle := newID("cow")
		sess, err := svcclient.Call[createMilkSessionReq, milkSessionResp](
			ctx, p.milk(), milkSvc+"/CreateSession",
			createMilkSessionReq{TenantID: p.tenant, CattleID: cattle,
				ShiftType: "morning", Timezone: tenantTimezone, CreatedBy: "e2e"}, p.opts())
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		if _, err := svcclient.Call[recordMilkReq, milkRecordResp](
			ctx, p.milk(), milkSvc+"/RecordMilk",
			recordMilkReq{TenantID: p.tenant, SessionID: sess.Session.ID,
				CattleID: cattle, QuantityLiters: 1.5, CreatedBy: "e2e"}, p.opts()); err != nil {
			t.Fatalf("record milk: %v", err)
		}
		seen = append(seen, trace.Trace())
	}

	found := tracesInTheTrail(t, p.tenant)
	delete(found, "")
	for _, trace := range seen {
		if found[trace] == 0 {
			t.Errorf("nothing in the trail carries %s", trace)
		}
	}
	if len(found) < 2 {
		t.Errorf("two separate requests produced %d distinct trace ids; a platform that "+
			"stamps one constant on every row would satisfy the test above and be "+
			"worthless", len(found))
	}
}

// A caller's trace is continued, not replaced.
//
// The property that makes a chain a chain. A service that started a fresh trace
// on every request would produce spans that are individually correct and join to
// nothing, and the platform would look exactly as it does now — which is why
// this asserts on the id rather than on the presence of one.
func TestAServiceContinuesTheCallersTraceRatherThanStartingItsOwn(t *testing.T) {
	p := startPlatform(t)

	// A traceparent from outside, as a gateway or an ingress would present it.
	outside := tracing.New()
	ctx := tracing.With(context.Background(), outside)

	cattle := newID("cow")
	sess, err := svcclient.Call[createMilkSessionReq, milkSessionResp](
		ctx, p.milk(), milkSvc+"/CreateSession",
		createMilkSessionReq{TenantID: p.tenant, CattleID: cattle,
			ShiftType: "morning", Timezone: tenantTimezone, CreatedBy: "e2e"}, p.opts())
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := svcclient.Call[recordMilkReq, milkRecordResp](
		ctx, p.milk(), milkSvc+"/RecordMilk",
		recordMilkReq{TenantID: p.tenant, SessionID: sess.Session.ID,
			CattleID: cattle, QuantityLiters: 3.0, CreatedBy: "e2e"}, p.opts()); err != nil {
		t.Fatalf("record milk: %v", err)
	}

	found := tracesInTheTrail(t, p.tenant)
	if found[outside.Trace()] > 0 {
		return // continued, which is the point
	}
	delete(found, "")
	t.Errorf("the caller's trace %s does not appear in the trail; the service started "+
		"its own instead, which produces spans that are individually correct and join "+
		"to nothing.\nsaw: %s", outside.Trace(), strings.Join(keysOfTrace(found), ", "))
}

func keysOfTrace(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
