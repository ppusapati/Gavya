package tenantdb

import (
	"context"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/libs/integrity/observe"
	"github.com/ppusapati/gavya/libs/integrity/tracing"
)

// A trace that stops at the database points at the wrong service.
//
// libs/integrity/tracing carries one identifier across every hop a request
// makes, and the hops it covered were service-to-service calls. So a trace of a
// slow settlement showed settlement-service taking four hundred milliseconds and
// procurement-service taking three hundred of them, and stopped there — with no
// way to tell a service that is doing too much work from one that is waiting on
// a database, which are two different problems with two different fixes.
//
// The collection path is bound by its database. That is not a guess: the load
// measurement found throughput flat from ten booths upward with latency rising
// in proportion to concurrency, which is the signature of a server at its
// service rate. The place the time goes was the one place a trace could not see.

// queryTracer records a span per query.
//
// pgx calls this for Query, QueryRow and Exec. Batches, COPY and connection
// setup have their own tracer interfaces and are deliberately not implemented:
// the platform makes no batch or COPY calls from a request path, and a span for
// every connection handshake would describe the pool rather than the work.
type queryTracer struct {
	service string
}

type spanKey struct{}

// query is what one call carries from start to end.
type query struct {
	end     func(error)
	started time.Time
}

func (t queryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, spanKey{}, query{
		end:     tracing.Child(ctx, t.service, operationOf(data.SQL)),
		started: time.Now(),
	})
}

func (t queryTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	q, ok := ctx.Value(spanKey{}).(query)
	if !ok {
		return
	}
	q.end(data.Err)

	// Counted as well as traced, and the distinction matters. A span exists only
	// when a collector is configured and only for a query inside a request; these
	// three are always there, and they cover the sweeps and the boot checks that
	// deliberately have no span. "How much of this service's time is spent in the
	// database" is a question somebody asks without having set up tracing first.
	//
	// Nothing alerts on the failure count, deliberately. A failed query is not
	// the same as a failing service: a unique violation is how this platform
	// detects a redelivered record, and it arrives here as an error every time
	// the collection bench retries after a lost reply. A threshold on that would
	// fire every morning. The counter is for somebody already looking — beside a
	// rise in gavya_requests_total{code="5.."}, it says whether the database is
	// where the failures are coming from.
	queries.Add(1)
	querySeconds.Add(time.Since(q.started).Seconds())
	if data.Err != nil {
		queryFailures.Add(1)
	}
}

var (
	queries       atomic.Int64
	queryFailures atomic.Int64
	querySeconds  atomicFloat
)

// atomicFloat is a float64 that several connections may add to at once.
//
// pgx calls the tracer from whichever goroutine ran the query, and a pool of
// eight has eight of them. A plain float64 incremented from several would lose
// increments silently, which is the kind of wrong number that is worse than no
// number: it is believable.
type atomicFloat struct{ bits atomic.Uint64 }

func (f *atomicFloat) Add(v float64) {
	for {
		old := f.bits.Load()
		next := math.Float64bits(math.Float64frombits(old) + v)
		if f.bits.CompareAndSwap(old, next) {
			return
		}
	}
}

func (f *atomicFloat) Load() float64 { return math.Float64frombits(f.bits.Load()) }

func publishQueryCounts() {
	observe.PublishCounter(observe.Counter{
		Name: "gavya_db_queries_total",
		Help: "Queries this process has sent to its database.",
		Read: func() float64 { return float64(queries.Load()) },
	})
	observe.PublishCounter(observe.Counter{
		Name: "gavya_db_query_seconds_total",
		Help: "Time spent waiting on the database, summed over every query.",
		Read: func() float64 { return querySeconds.Load() },
	})
	observe.PublishCounter(observe.Counter{
		Name: "gavya_db_query_failures_total",
		Help: "Queries that came back an error, including ones the statement timeout ended.",
		Read: func() float64 { return float64(queryFailures.Load()) },
	})
}

// operationOf names the span.
//
// The SQL itself cannot be the name. A trace store indexes on the name and the
// statements here carry no parameters inline, but they are still hundreds of
// distinct strings — and one span name per statement is how a trace store stops
// being usable.
//
// So: the verb, and the first table it plainly names. "db SELECT collections"
// is bounded by the schema, which is the property that matters, and it is enough
// to see which query in a handler is the slow one.
//
// It refuses rather than guesses. A statement whose target is a subquery, a
// function call or anything that is not a bare identifier gets the verb alone,
// because a span name that is confidently wrong sends somebody to look at the
// wrong table — and this is a naming convenience, not a place to be clever about
// parsing SQL.
func operationOf(sql string) string {
	fields := strings.Fields(sql)
	if len(fields) == 0 {
		return "db"
	}

	verb := strings.ToUpper(strings.TrimLeft(fields[0], "("))
	switch verb {
	case "SELECT", "INSERT", "UPDATE", "DELETE", "WITH",
		"BEGIN", "COMMIT", "ROLLBACK", "SET", "SAVEPOINT", "RELEASE":
	default:
		// Not a statement this platform writes. Named by nothing rather than by
		// whatever the first word happened to be, which for a malformed or
		// generated statement is where an unbounded set of names would come from.
		return "db"
	}

	table := ""
	for i := 0; i < len(fields)-1; i++ {
		switch strings.ToUpper(fields[i]) {
		case "FROM", "INTO", "JOIN", "UPDATE":
			table = identifier(fields[i+1])
		}
		if table != "" {
			break
		}
	}
	if table == "" {
		return "db " + verb
	}
	return "db " + verb + " " + table
}

// identifier accepts a bare table name and nothing else.
func identifier(word string) string {
	trimmed := strings.Trim(word, `"`)
	if trimmed == "" {
		return ""
	}
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.':
		default:
			return ""
		}
	}
	// A leading digit is not an identifier, and neither is a bare keyword that
	// arrived here because the statement was shaped unusually.
	if trimmed[0] >= '0' && trimmed[0] <= '9' {
		return ""
	}
	return strings.ToLower(trimmed)
}
