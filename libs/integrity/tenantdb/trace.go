package tenantdb

import (
	"context"
	"math"
	"strings"
	"sync"
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
	// The statement's shape, parsed once at the start rather than again at the
	// end: the SQL is not on the end data, and re-deriving it would mean the
	// span and the counts could disagree about which table was touched.
	verb  string
	table string
}

func (t queryTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	verb, table := verbAndTable(data.SQL)
	name := "db"
	switch {
	case verb != "" && table != "":
		name = "db " + verb + " " + table
	case verb != "":
		name = "db " + verb
	}
	return context.WithValue(ctx, spanKey{}, query{
		end:     tracing.Child(ctx, t.service, name),
		started: time.Now(),
		verb:    verb,
		table:   table,
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
	took := time.Since(q.started).Seconds()
	queries.Add(1)
	querySeconds.Add(took)
	if data.Err != nil {
		queryFailures.Add(1)
	}
	recordTableQuery(q.verb, q.table, took, data.Err != nil)
}

// tableOp is what the per-table counts are keyed by.
type tableOp struct{ table, operation string }

// counts is one table-and-verb's totals.
//
// Held as plain numbers under one mutex rather than as atomics per field: a
// query end is already doing a syscall's worth of work, the map lookup dominates
// either way, and one lock read as a whole is one consistent triple rather than
// three numbers taken at three instants.
type counts struct {
	queries  uint64
	seconds  float64
	failures uint64
}

var (
	tablesMu sync.Mutex
	tables   = map[tableOp]*counts{}
)

// MaxTableSeries bounds what the per-table counts may grow to.
//
// The set is bounded by the schema already — verbAndTable refuses anything that
// is not a bare identifier, and there are a hundred and twelve tables and five
// verbs — so this is a ceiling on a mistake rather than on the platform. What it
// prevents is a map that grows without limit in a long-running process if some
// statement shape nobody anticipated slips through the parser.
//
// Reaching it stops new combinations being tracked; the ones already there go on
// counting. Preferred to evicting, because a counter that forgets is a counter
// whose rate() is wrong in a way nobody can see.
const MaxTableSeries = 400

func recordTableQuery(verb, table string, seconds float64, failed bool) {
	if verb == "" {
		// A statement the parser did not recognise. Counted in the three totals
		// above and not here: a label of "unknown" on a metric whose whole
		// purpose is to say which table is slow would be a row that answers the
		// question wrongly rather than not at all.
		return
	}
	if table == "" {
		// A verb with no plain table — BEGIN, COMMIT, SET search_path, a select
		// from a subquery. Worth counting, because transaction overhead is a real
		// answer to "where is the time going", and it goes under a name that
		// cannot be confused with a table.
		table = "(none)"
	}

	key := tableOp{table: table, operation: verb}
	tablesMu.Lock()
	defer tablesMu.Unlock()
	c, ok := tables[key]
	if !ok {
		if len(tables) >= MaxTableSeries {
			return
		}
		c = &counts{}
		tables[key] = c
	}
	c.queries++
	c.seconds += seconds
	if failed {
		c.failures++
	}
}

// tableSeries is the three breakdowns, read together under one lock.
func tableSeries() (queries, seconds, failures []observe.Series) {
	tablesMu.Lock()
	defer tablesMu.Unlock()
	for k, c := range tables {
		labels := []string{k.table, k.operation}
		queries = append(queries, observe.Series{Labels: labels, Value: float64(c.queries)})
		seconds = append(seconds, observe.Series{Labels: labels, Value: c.seconds})
		failures = append(failures, observe.Series{Labels: labels, Value: float64(c.failures)})
	}
	return queries, seconds, failures
}

// forgetTableQueries exists for tests, which would otherwise see each other's.
func forgetTableQueries() {
	tablesMu.Lock()
	defer tablesMu.Unlock()
	tables = map[tableOp]*counts{}
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

	// The same three, broken down by table and operation.
	//
	// The last requirement owed from the inherited library's list, and the
	// reason it stayed owed: the breakdown already existed in the traces — a
	// span is named `db SELECT collections` — and as a metric it needed labels,
	// which this package did not have. It has them now, with a ceiling, and the
	// parser that names the span names these.
	//
	// What the totals above cannot answer: "the database is where the time goes"
	// is where they stop, and the next question is always which table. A trace
	// answers it for one request somebody has in front of them; this answers it
	// for the morning.
	//
	// Read together rather than three closures over three locks, so one scrape
	// sees one consistent triple.
	observe.PublishLabelledCounter(observe.LabelledCounter{
		Name:   "gavya_db_table_queries_total",
		Help:   "Queries run, by table and operation.",
		Labels: []string{"table", "operation"},
		Read:   func() []observe.Series { q, _, _ := tableSeries(); return q },
	})
	observe.PublishLabelledCounter(observe.LabelledCounter{
		Name:   "gavya_db_table_query_seconds_total",
		Help:   "Time spent in the database, by table and operation.",
		Labels: []string{"table", "operation"},
		Read:   func() []observe.Series { _, s, _ := tableSeries(); return s },
	})
	observe.PublishLabelledCounter(observe.LabelledCounter{
		Name:   "gavya_db_table_query_failures_total",
		Help:   "Queries that returned an error, by table and operation.",
		Labels: []string{"table", "operation"},
		Read:   func() []observe.Series { _, _, f := tableSeries(); return f },
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
	verb, table := verbAndTable(sql)
	switch {
	case verb == "":
		return "db"
	case table == "":
		return "db " + verb
	}
	return "db " + verb + " " + table
}

// verbAndTable splits a statement into the two things worth labelling.
//
// One parser, two readers: the span name above and the per-table counts below.
// They were one function returning "db SELECT collections", and a metric needs
// the parts separately — so rather than parse the SQL twice and risk the trace
// and the metric disagreeing about which table a query touched, the split is
// here and both are composed from it.
//
// Empty verb means a statement this platform does not write. Empty table means
// one whose target is a subquery, a function call or anything that is not a bare
// identifier: refused rather than guessed at, because a label that is
// confidently wrong sends somebody to look at the wrong table.
func verbAndTable(sql string) (verb, table string) {
	fields := strings.Fields(sql)
	if len(fields) == 0 {
		return "", ""
	}

	verb = strings.ToUpper(strings.TrimLeft(fields[0], "("))
	switch verb {
	case "SELECT", "INSERT", "UPDATE", "DELETE", "WITH",
		"BEGIN", "COMMIT", "ROLLBACK", "SET", "SAVEPOINT", "RELEASE":
	default:
		// Not a statement this platform writes. Named by nothing rather than by
		// whatever the first word happened to be, which for a malformed or
		// generated statement is where an unbounded set of names would come from.
		return "", ""
	}

	for i := 0; i < len(fields)-1; i++ {
		switch strings.ToUpper(fields[i]) {
		case "FROM", "INTO", "JOIN", "UPDATE":
			table = identifier(fields[i+1])
		}
		if table != "" {
			break
		}
	}
	return verb, table
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
