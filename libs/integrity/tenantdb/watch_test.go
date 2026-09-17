package tenantdb

import (
	"context"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/observe"
)

// A pool that never connects.
//
// pgxpool does not dial until somebody asks for a connection — min_conns
// defaults to zero — so a pool pointed at nothing still answers Stat(), which is
// all these gauges read. That keeps this a unit test: what is being checked is
// that the numbers reach the metrics handler and that a second pool is added to
// the first, and neither of those needs a database.
func idlePool(t *testing.T, maxConns string) *pgxpool.Pool {
	t.Helper()
	dsn := "postgres://app@127.0.0.1:1/dairy?sslmode=require&pool_max_conns=" + maxConns
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		Forget(pool)
		pool.Close()
	})
	return pool
}

func scrape(t *testing.T) string {
	t.Helper()
	rec := httptest.NewRecorder()
	observe.NewMetrics().Handler()(rec, httptest.NewRequest("GET", "/metrics", nil))
	return rec.Body.String()
}

// The database was the one thing in this platform nothing watched. This is the
// end of that: the numbers are on the same handler as everything else, so the
// existing scrape configuration reaches them with no change.
func TestThePoolAppearsOnTheMetricsHandler(t *testing.T) {
	watch(idlePool(t, "5"))

	body := scrape(t)
	for _, name := range []string{
		"gavya_db_pool_max_conns",
		"gavya_db_pool_total_conns",
		"gavya_db_pool_acquired_conns",
		"gavya_db_pool_idle_conns",
		"gavya_db_pool_empty_acquires_total",
		"gavya_db_pool_acquire_wait_seconds_total",
		"gavya_db_pool_canceled_acquires_total",
	} {
		if !strings.Contains(body, name+" ") {
			t.Errorf("%s is not on the metrics handler, so nothing can alert on it", name)
		}
	}
}

// The three that only ever go up are declared as counters, because rate() and
// increase() are written against counters and Prometheus uses the declared type
// to tell a process restart from a value that fell.
func TestTheRisingNumbersAreDeclaredCounters(t *testing.T) {
	watch(idlePool(t, "5"))

	body := scrape(t)
	counters := []string{
		"gavya_db_pool_empty_acquires_total",
		"gavya_db_pool_acquire_wait_seconds_total",
		"gavya_db_pool_canceled_acquires_total",
	}
	for _, name := range counters {
		if !strings.Contains(body, "# TYPE "+name+" counter\n") {
			t.Errorf("%s is not declared a counter", name)
		}
	}
	if !strings.Contains(body, "# TYPE gavya_db_pool_acquired_conns gauge\n") {
		t.Error("gavya_db_pool_acquired_conns is not declared a gauge, and it is one: " +
			"connections in use goes down as well as up")
	}
}

// The modulith is twenty-eight modules in one process, each with its own pool.
// observe.Publish is keyed by name, so a gauge per pool would leave only the
// last module reported — which reads as a process holding eight connections when
// it may hold two hundred and twenty-four.
func TestEveryPoolInTheProcessIsCounted(t *testing.T) {
	watch(idlePool(t, "5"))
	before := poolMaxOnHandler(t)

	watch(idlePool(t, "7"))
	after := poolMaxOnHandler(t)

	if after != before+7 {
		t.Fatalf("after adding a second pool of 7 the handler reports %v, was %v: "+
			"a process's pools have to be summed or the modulith under-reports by "+
			"twenty-seven pools", after, before)
	}
}

func poolMaxOnHandler(t *testing.T) float64 {
	t.Helper()
	return counterOnHandler(t, "gavya_db_pool_max_conns")
}

// counterOnHandler reads a metric back off the handler, so an assertion is about
// what a scrape would see rather than about a variable the test can reach.
func counterOnHandler(t *testing.T, want string) float64 {
	t.Helper()
	for _, line := range strings.Split(scrape(t), "\n") {
		name, rest, ok := strings.Cut(line, " ")
		if !ok || name != want {
			continue
		}
		n, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
		if err != nil {
			t.Fatalf("%s rendered as %q, which is not a number", want, rest)
		}
		return n
	}
	t.Fatalf("%s is not on the handler", want)
	return 0
}
