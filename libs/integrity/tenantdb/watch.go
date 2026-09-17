package tenantdb

import (
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/observe"
)

// Watching the pool.
//
// The database is the one thing in this platform nothing watched. Every service
// published request counts, a latency histogram and an in-flight gauge, and none
// of them said anything about the resource every one of those requests contends
// for. The foot of deploy/monitoring/alerts.yml wrote that gap down and gave a
// reason not to close it from there: the check beside those rules verifies that
// every metric an alert names is one a live handler actually emits, and it
// cannot do that for a PostgreSQL exporter no deployment runs.
//
// That reason covers the exporter and does not cover this. These numbers come
// off a pool the service is already holding, so a live handler does emit them
// and the check can see them — which is why they are here rather than in an
// exporter nobody has deployed.
//
// # WHY THE NUMBERS ARE SUMMED
//
// A process can hold more than one pool. Twenty-eight services hold one each,
// but the modulith is all of them in a single process and holds twenty-eight —
// it does not merge the databases, so it does not merge the pools either.
//
// Registering a gauge per pool is not available: observe.Publish is keyed by
// name and a second registration replaces the first, so in the modulith the
// twenty-eighth module would silently be the only one reported. Summing is the
// honest reading of the question actually being asked, which is "how many
// connections is this process holding" — the number that has to fit under the
// server's limit.

var (
	poolsMu   sync.Mutex
	pools     []*pgxpool.Pool
	published sync.Once
)

// watch adds a pool to what the gauges below report, and registers them the
// first time it is called.
//
// Registered lazily rather than in an init, so that a process holding no pool at
// all — the gateway proxies and never opens one — does not publish four numbers
// that are all zero. A zero that means "no pool" and a zero that means "no
// connections" are the same number, and this platform has been bitten by that
// often enough to take it seriously.
func watch(p *pgxpool.Pool) {
	if p == nil {
		return
	}
	poolsMu.Lock()
	pools = append(pools, p)
	poolsMu.Unlock()

	published.Do(func() {
		publishPoolStats()
		publishQueryCounts()
	})
}

// Forget stops reporting a pool.
//
// For tests, which open and close pools in the same process and would otherwise
// accumulate closed ones. A service closes its pool when the process is ending
// and has no use for this.
func Forget(p *pgxpool.Pool) {
	poolsMu.Lock()
	defer poolsMu.Unlock()
	for i := range pools {
		if pools[i] == p {
			pools = append(pools[:i], pools[i+1:]...)
			return
		}
	}
}

// eachPool sums one number across every pool this process holds.
func eachPool(read func(*pgxpool.Stat) float64) float64 {
	poolsMu.Lock()
	held := make([]*pgxpool.Pool, len(pools))
	copy(held, pools)
	poolsMu.Unlock()

	total := 0.0
	for _, p := range held {
		total += read(p.Stat())
	}
	return total
}

func publishPoolStats() {
	observe.Publish(observe.Gauge{
		Name: "gavya_db_pool_max_conns",
		Help: "Connections this process may hold at once, summed over its pools.",
		Read: func() float64 {
			return eachPool(func(s *pgxpool.Stat) float64 { return float64(s.MaxConns()) })
		},
	})
	observe.Publish(observe.Gauge{
		Name: "gavya_db_pool_total_conns",
		Help: "Connections this process is holding, in use or idle.",
		Read: func() float64 {
			return eachPool(func(s *pgxpool.Stat) float64 { return float64(s.TotalConns()) })
		},
	})
	observe.Publish(observe.Gauge{
		Name: "gavya_db_pool_acquired_conns",
		Help: "Connections checked out of the pools right now, serving a query.",
		Read: func() float64 {
			return eachPool(func(s *pgxpool.Stat) float64 { return float64(s.AcquiredConns()) })
		},
	})
	observe.Publish(observe.Gauge{
		Name: "gavya_db_pool_idle_conns",
		Help: "Connections held open and not in use.",
		Read: func() float64 {
			return eachPool(func(s *pgxpool.Stat) float64 { return float64(s.IdleConns()) })
		},
	})

	// Counters, not gauges, and the distinction is what a reader does with them:
	// the question is the rate at which callers are waiting, not the total since
	// boot, and rate() is written against a counter.
	observe.PublishCounter(observe.Counter{
		Name: "gavya_db_pool_empty_acquires_total",
		Help: "Times a caller wanted a connection and the pool had none free.",
		Read: func() float64 {
			return eachPool(func(s *pgxpool.Stat) float64 { return float64(s.EmptyAcquireCount()) })
		},
	})
	observe.PublishCounter(observe.Counter{
		Name: "gavya_db_pool_acquire_wait_seconds_total",
		Help: "Time spent waiting for a connection, summed over every acquire.",
		Read: func() float64 {
			return eachPool(func(s *pgxpool.Stat) float64 { return s.AcquireDuration().Seconds() })
		},
	})
	observe.PublishCounter(observe.Counter{
		Name: "gavya_db_pool_canceled_acquires_total",
		Help: "Acquires abandoned before a connection came free, because the caller gave up.",
		Read: func() float64 {
			return eachPool(func(s *pgxpool.Stat) float64 { return float64(s.CanceledAcquireCount()) })
		},
	})
}
