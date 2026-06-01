// Tests for packages/p9context. Covers the three most-used key bundles:
// DBPoolContext, RequestContext, RLSScope. Focus: round-trip (Set → Get)
// plus the Must* panic semantics and the Or-generate helpers.
//
// Deliberately ignores connection-pool creation (no pgxpool.Pool is made
// in-process); the DBPoolContext tests use a nil pool pointer — its only
// role here is to verify context plumbing, not pool wiring.
package p9context_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"p9e.in/samavaya/packages/p9context"
)

// ────────────────────────────────────────────────────────────────────────────
// DBPoolContext
// ────────────────────────────────────────────────────────────────────────────

func TestDBPoolContext_RoundTrip(t *testing.T) {
	ctx := context.Background()

	// Without a pool: From… returns (nil, false); Has… returns false.
	if got, ok := p9context.FromDBPoolContext(ctx); ok || got != nil {
		t.Fatalf("empty ctx: From returned (%v, %v); want (nil, false)", got, ok)
	}
	if p9context.HasDBPoolContext(ctx) {
		t.Fatal("empty ctx: HasDBPoolContext returned true")
	}

	// With a planted pool: From… returns (pool, true); Has… returns true.
	pool := &pgxpool.Pool{} // zero value — never used, only identity-checked
	ctx2 := p9context.NewDBPoolContext(ctx, pool)

	got, ok := p9context.FromDBPoolContext(ctx2)
	if !ok {
		t.Fatal("planted ctx: FromDBPoolContext returned ok=false")
	}
	if got != pool {
		t.Fatalf("planted ctx: got %p, want %p", got, pool)
	}
	if !p9context.HasDBPoolContext(ctx2) {
		t.Fatal("planted ctx: HasDBPoolContext returned false")
	}
	if p9context.DBPool(ctx2) != pool {
		t.Fatal("DBPool() returned different pool than From")
	}
}

func TestMustDBPoolContext_PanicsOnMissing(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("MustDBPoolContext on empty ctx did not panic")
		}
	}()
	_ = p9context.MustDBPoolContext(context.Background())
}

func TestMustDBPoolContext_ReturnsPlantedPool(t *testing.T) {
	pool := &pgxpool.Pool{}
	ctx := p9context.NewDBPoolContext(context.Background(), pool)

	if got := p9context.MustDBPoolContext(ctx); got != pool {
		t.Fatalf("MustDBPoolContext returned different pool")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// RequestContext
// ────────────────────────────────────────────────────────────────────────────

func TestRequestContext_RoundTrip(t *testing.T) {
	ctx := context.Background()

	if _, ok := p9context.FromRequestContext(ctx); ok {
		t.Fatal("empty ctx: FromRequestContext returned ok=true")
	}
	if p9context.HasRequestContext(ctx) {
		t.Fatal("empty ctx: HasRequestContext returned true")
	}

	req := p9context.RequestContext{
		RequestID: "req-abc",
		TraceID:   "trace-123",
		SpanID:    "span-456",
		ClientIP:  "10.0.0.1",
		UserAgent: "curl/8.0",
		Method:    "GET",
		Path:      "/v1/datasets",
	}
	ctx2 := p9context.NewRequestContext(ctx, req)

	got, ok := p9context.FromRequestContext(ctx2)
	if !ok || got == nil {
		t.Fatal("planted ctx: FromRequestContext returned ok=false")
	}
	if *got != req {
		t.Fatalf("RequestContext round-trip mismatch:\n got  %+v\n want %+v", *got, req)
	}
	if !p9context.HasRequestContext(ctx2) {
		t.Fatal("HasRequestContext returned false on planted ctx")
	}
}

func TestRequestContext_AccessorShortcuts(t *testing.T) {
	req := p9context.RequestContext{
		RequestID: "r1",
		TraceID:   "t1",
		SpanID:    "s1",
		ClientIP:  "1.2.3.4",
		UserAgent: "x",
		Method:    "POST",
		Path:      "/p",
	}
	ctx := p9context.NewRequestContext(context.Background(), req)

	cases := []struct {
		name  string
		got   string
		want  string
	}{
		{"RequestID", p9context.RequestID(ctx), "r1"},
		{"TraceID", p9context.TraceID(ctx), "t1"},
		{"SpanID", p9context.SpanID(ctx), "s1"},
		{"ClientIP", p9context.ClientIP(ctx), "1.2.3.4"},
		{"UserAgent", p9context.RequestUserAgent(ctx), "x"},
		{"Method", p9context.RequestMethod(ctx), "POST"},
		{"Path", p9context.RequestPath(ctx), "/p"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

func TestRequestContext_AccessorsOnEmpty_ReturnEmpty(t *testing.T) {
	ctx := context.Background()
	// Every accessor tolerates a missing RequestContext and returns "".
	if got := p9context.RequestID(ctx); got != "" {
		t.Fatalf("RequestID(empty) = %q, want empty", got)
	}
	if got := p9context.TraceID(ctx); got != "" {
		t.Fatalf("TraceID(empty) = %q, want empty", got)
	}
	if got := p9context.ClientIP(ctx); got != "" {
		t.Fatalf("ClientIP(empty) = %q, want empty", got)
	}
}

func TestMustRequestContext_PanicsOnMissing(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("MustRequestContext on empty ctx did not panic")
		}
	}()
	_ = p9context.MustRequestContext(context.Background())
}

func TestRequestIDOrGenerate_UsesExisting(t *testing.T) {
	ctx := p9context.NewRequestContext(context.Background(), p9context.RequestContext{RequestID: "existing"})
	if got := p9context.RequestIDOrGenerate(ctx); got != "existing" {
		t.Fatalf("RequestIDOrGenerate returned %q; want existing", got)
	}
}

func TestRequestIDOrGenerate_GeneratesWhenMissing(t *testing.T) {
	// No RequestContext planted — the helper must synthesize a non-empty ULID.
	id := p9context.RequestIDOrGenerate(context.Background())
	if id == "" {
		t.Fatal("RequestIDOrGenerate returned empty string on empty ctx")
	}
	// ULIDs are 26 characters. Relax to "reasonable length" to stay
	// tolerant of implementation changes.
	if len(id) < 10 {
		t.Fatalf("generated id looks too short: %q (len %d)", id, len(id))
	}
}

func TestTraceIDOrGenerate_UsesExisting(t *testing.T) {
	ctx := p9context.NewRequestContext(context.Background(), p9context.RequestContext{TraceID: "tr-9"})
	if got := p9context.TraceIDOrGenerate(ctx); got != "tr-9" {
		t.Fatalf("TraceIDOrGenerate returned %q; want tr-9", got)
	}
}

func TestLogFields_EmitsPopulatedFieldsOnly(t *testing.T) {
	ctx := p9context.NewRequestContext(context.Background(), p9context.RequestContext{
		RequestID: "rid",
		Method:    "GET",
		Path:      "/p",
		// TraceID / SpanID / ClientIP intentionally zero — should NOT appear.
	})
	fields := p9context.LogFields(ctx)
	if fields["request_id"] != "rid" || fields["method"] != "GET" || fields["path"] != "/p" {
		t.Fatalf("missing expected fields: %v", fields)
	}
	if _, present := fields["trace_id"]; present {
		t.Fatal("trace_id should NOT appear when empty")
	}
	if _, present := fields["span_id"]; present {
		t.Fatal("span_id should NOT appear when empty")
	}
	if _, present := fields["client_ip"]; present {
		t.Fatal("client_ip should NOT appear when empty")
	}
}

func TestLogFields_EmptyCtx_ReturnsEmptyMap(t *testing.T) {
	fields := p9context.LogFields(context.Background())
	if len(fields) != 0 {
		t.Fatalf("LogFields(empty) = %v; want empty map", fields)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// RLSScope — critical for multi-tenant isolation
// ────────────────────────────────────────────────────────────────────────────

func TestRLSScope_RoundTrip(t *testing.T) {
	scope := p9context.RLSScope{TenantID: "t1", CompanyID: "c1", BranchID: "b1"}
	ctx := p9context.NewRLSScope(context.Background(), scope)

	got := p9context.FromRLSScope(ctx)
	if got == nil {
		t.Fatal("FromRLSScope returned nil on planted ctx")
	}
	if *got != scope {
		t.Fatalf("scope mismatch: got %+v, want %+v", *got, scope)
	}
}

func TestRLSScope_FromIDsConstructor(t *testing.T) {
	ctx := p9context.NewRLSScopeFromIDs(context.Background(), "t2", "c2", "b2")
	got := p9context.FromRLSScope(ctx)
	if got == nil {
		t.Fatal("FromRLSScope returned nil after NewRLSScopeFromIDs")
	}
	if got.TenantID != "t2" || got.CompanyID != "c2" || got.BranchID != "b2" {
		t.Fatalf("scope fields = %+v", *got)
	}
}

func TestFromRLSScope_EmptyCtx_ReturnsNil(t *testing.T) {
	if got := p9context.FromRLSScope(context.Background()); got != nil {
		t.Fatalf("FromRLSScope(empty) = %+v; want nil", got)
	}
}

func TestMustRLSScope_PlantedScope_ReturnsValue(t *testing.T) {
	scope := p9context.RLSScope{TenantID: "t3"}
	ctx := p9context.NewRLSScope(context.Background(), scope)

	if got := p9context.MustRLSScope(ctx); got != scope {
		t.Fatalf("MustRLSScope = %+v, want %+v", got, scope)
	}
}

func TestMustRLSScope_EmptyCtx_ReturnsZero(t *testing.T) {
	// MustRLSScope is "soft-must" — it falls back through TenantID / current
	// tenant, and ultimately returns a zero RLSScope{} rather than panicking.
	// This is contract: callers using it in repo code expect "" tenant as
	// a signal to reject the query, not a crash.
	got := p9context.MustRLSScope(context.Background())
	if got != (p9context.RLSScope{}) {
		t.Fatalf("MustRLSScope(empty) = %+v, want zero RLSScope", got)
	}
}
