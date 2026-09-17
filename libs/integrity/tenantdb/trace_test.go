package tenantdb

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/libs/integrity/tracing"
)

// collected is a recorder that keeps what it was given.
type collected struct{ spans []tracing.Span }

func (c *collected) Record(s tracing.Span) { c.spans = append(c.spans, s) }

func recording(t *testing.T) *collected {
	t.Helper()
	c := &collected{}
	tracing.UseCollector(c)
	t.Cleanup(func() { tracing.UseCollector(nil) })
	return c
}

// The gap this closes: a trace showed settlement-service taking four hundred
// milliseconds and stopped, with no way to tell a service doing too much work
// from one waiting on a database.
func TestAQueryInsideATracedRequestBecomesASpan(t *testing.T) {
	c := recording(t)
	tracer := queryTracer{service: "settlement-service"}

	request := tracing.With(context.Background(), tracing.New())
	ctx := tracer.TraceQueryStart(request, nil,
		pgx.TraceQueryStartData{SQL: "SELECT id FROM payables WHERE tenant_id = $1"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	if len(c.spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(c.spans))
	}
	span := c.spans[0]
	if span.Service != "settlement-service" {
		t.Errorf("service = %q, want settlement-service", span.Service)
	}
	if span.Name != "db SELECT payables" {
		t.Errorf("name = %q, want \"db SELECT payables\"", span.Name)
	}
	if span.Failed {
		t.Error("a query that returned no error was recorded as failed")
	}

	// The span has to sit under the request's trace, or it is a second trace
	// nobody can find from the first.
	request4, _ := tracing.From(request)
	if span.Context.Trace() != request4.Trace() {
		t.Errorf("the query's span is on trace %s and the request is on %s",
			span.Context.Trace(), request4.Trace())
	}
	if span.Parent != request4.SpanID {
		t.Error("the query's span does not name the request's span as its parent, " +
			"so a viewer would show it beside the request rather than inside it")
	}
}

// A failed query is the one somebody is looking for.
func TestAFailedQueryIsRecordedAsFailed(t *testing.T) {
	c := recording(t)
	tracer := queryTracer{service: "milk-service"}

	ctx := tracer.TraceQueryStart(tracing.With(context.Background(), tracing.New()), nil,
		pgx.TraceQueryStartData{SQL: "INSERT INTO collections (id) VALUES ($1)"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: errors.New("deadlock detected")})

	if len(c.spans) != 1 {
		t.Fatalf("recorded %d spans, want 1", len(c.spans))
	}
	if !c.spans[0].Failed {
		t.Error("a query that returned an error was not recorded as failed")
	}
}

// Deliberately nothing, and the opposite of what Inject does for an outgoing
// service call. A background sweep makes one service call and hundreds of
// queries; starting a trace for each would fill a trace store with single-span
// traces and bury the ones somebody asked for.
func TestAQueryOutsideARequestIsNotTraced(t *testing.T) {
	c := recording(t)
	tracer := queryTracer{service: "settlement-service"}

	ctx := tracer.TraceQueryStart(context.Background(), nil,
		pgx.TraceQueryStartData{SQL: "SELECT count(*) FROM notification_outbox"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	if len(c.spans) != 0 {
		t.Fatalf("recorded %d spans for a query with no request behind it, want none",
			len(c.spans))
	}
}

// The counters exist whether or not anybody is collecting traces, and they cover
// the queries that deliberately have no span — the sweeps and the boot checks.
// "How much of this service's time goes to the database" is a question somebody
// asks without having set up tracing first.
func TestEveryQueryIsCountedEvenWithNothingTracing(t *testing.T) {
	tracing.UseCollector(nil)
	watch(idlePool(t, "5"))

	before := counterOnHandler(t, "gavya_db_queries_total")
	failuresBefore := counterOnHandler(t, "gavya_db_query_failures_total")

	tracer := queryTracer{service: "milk-service"}
	// No trace on this context at all, which is the case that records no span.
	ctx := tracer.TraceQueryStart(context.Background(), nil,
		pgx.TraceQueryStartData{SQL: "SELECT 1 FROM tenants"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	failed := tracer.TraceQueryStart(context.Background(), nil,
		pgx.TraceQueryStartData{SQL: "SELECT 1 FROM tenants"})
	tracer.TraceQueryEnd(failed, nil, pgx.TraceQueryEndData{Err: errors.New("canceling statement due to statement timeout")})

	if got := counterOnHandler(t, "gavya_db_queries_total"); got != before+2 {
		t.Errorf("gavya_db_queries_total went from %v to %v over two queries", before, got)
	}
	if got := counterOnHandler(t, "gavya_db_query_failures_total"); got != failuresBefore+1 {
		t.Errorf("gavya_db_query_failures_total went from %v to %v over one failure",
			failuresBefore, got)
	}
	if counterOnHandler(t, "gavya_db_query_seconds_total") <= 0 {
		t.Error("gavya_db_query_seconds_total is not above zero after two queries")
	}
}

// A trace store indexes on the span name. One name per statement is how it stops
// being usable, so the name has to come from a bounded set — and where it cannot
// be sure, it says less rather than guessing.
func TestSpanNamesAreBoundedAndRefuseToGuess(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want string
	}{
		{"select", "SELECT a, b FROM collections WHERE id = $1", "db SELECT collections"},
		{"insert", "INSERT INTO payables (id) VALUES ($1)", "db INSERT payables"},
		{"update", "UPDATE rate_cards SET x = 1", "db UPDATE rate_cards"},
		{"delete", "DELETE FROM sessions WHERE id = $1", "db DELETE sessions"},
		{"quoted", `SELECT * FROM "audit_logs"`, "db SELECT audit_logs"},
		{"schema qualified", "SELECT 1 FROM public.tenants", "db SELECT public.tenants"},
		{"lower case", "select 1 from tenants", "db SELECT tenants"},
		{"newlines and padding", "\n\t\tSELECT 1\n\t\t  FROM tenants\n", "db SELECT tenants"},
		{"join names the first table", "SELECT 1 FROM a JOIN b ON true", "db SELECT a"},
		{"transaction control", "BEGIN", "db BEGIN"},
		{"commit", "COMMIT", "db COMMIT"},
		{"set_config, which every acquire runs", "SELECT set_config($1, $2, false)", "db SELECT"},

		// The refusals. Each of these could be given a name by guessing, and a
		// span name that is confidently wrong sends somebody to the wrong table.
		{"subquery target", "SELECT 1 FROM (SELECT 2) AS x", "db SELECT"},
		// A set-returning function is a bounded name and could be reported, but
		// not as "gavya_verify_audit_chain($1)" — and stripping the arguments is
		// the beginning of parsing SQL properly, which this is not.
		{"function target", "SELECT * FROM gavya_verify_audit_chain($1)", "db SELECT"},
		{"not a statement we write", "EXPLAIN ANALYZE SELECT 1", "db"},
		{"empty", "", "db"},
		{"whitespace only", "   \n\t ", "db"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := operationOf(c.sql); got != c.want {
				t.Errorf("operationOf(%q) = %q, want %q", c.sql, got, c.want)
			}
		})
	}
}

// The property the table above is really asserting, stated so it cannot be lost
// by somebody adding a case: whatever the statement, the name is short and comes
// from a small vocabulary.
func TestASpanNameIsNeverTheStatement(t *testing.T) {
	long := "SELECT " + strings.Repeat("column_name, ", 200) + "x FROM collections"
	name := operationOf(long)
	if len(name) > 64 {
		t.Fatalf("name is %d characters: %q", len(name), name)
	}
	if strings.Contains(name, "column_name") {
		t.Fatalf("the statement's text reached the span name: %q", name)
	}
}
