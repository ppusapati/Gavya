// Package observe is what somebody watching this platform can see.
//
// There was nothing. Every service served a /healthz that returned 200
// unconditionally — a liveness probe that cannot fail, which is the same shape
// as a control that reports success while doing nothing. A pod whose database
// had gone away passed it, stayed in the Service's endpoints, and was sent
// traffic it could not serve.
//
// Two things are needed and they are different questions:
//
//   - Liveness: is this process running. /healthz still answers yes
//     unconditionally, and that is now correct rather than lazy — a liveness
//     probe that fails gets the pod killed, so making it depend on the database
//     would restart every service in the platform during a database blip and
//     turn a recoverable outage into a crash loop.
//
//   - Readiness: can this process serve a request. /readyz asks the database.
//     A readiness failure takes the pod out of rotation and leaves it running,
//     which is what a temporary dependency failure deserves.
//
// The metrics are in Prometheus text exposition format, written here rather than
// pulled in as a dependency. The format is eleven lines of specification and the
// library is a supply chain; for four metrics, writing them is the smaller
// commitment. Anything that scrapes Prometheus reads this.
package observe

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Check is one dependency a service needs before it can serve.
type Check struct {
	// Name appears in the failure, so a readiness probe that goes red says which
	// dependency went away rather than only that something did.
	Name string
	// Ping reports whether it is reachable. *pgxpool.Pool satisfies this.
	Ping func(context.Context) error
}

// PingTimeout bounds a readiness check.
//
// Short, and shorter than any sensible probe interval: a readiness check that
// hangs is a pod that neither becomes ready nor reports why, which reads as the
// service being broken rather than its database being slow.
const PingTimeout = 2 * time.Second

// Ready builds the readiness handler.
//
// With no checks it answers ready, and says so in the body rather than silently:
// a service with no dependencies is a real thing, and a readiness probe that
// cannot distinguish it from one whose checks were never wired is the situation
// this package exists to end.
func Ready(checks ...Check) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), PingTimeout)
		defer cancel()

		var failed []string
		for _, c := range checks {
			if c.Ping == nil {
				continue
			}
			if err := c.Ping(ctx); err != nil {
				failed = append(failed, c.Name+": "+err.Error())
			}
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if len(failed) > 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, "not ready\n%s\n", strings.Join(failed, "\n"))
			return
		}
		if len(checks) == 0 {
			fmt.Fprint(w, "ready: nothing to check\n")
			return
		}
		fmt.Fprintf(w, "ready: %d dependency check(s) passed\n", len(checks))
	}
}

// Metrics counts what a service did.
//
// Four measurements, chosen because between them they answer the questions
// somebody actually asks during an incident: is it serving, is it failing, is it
// slow, and is it stuck.
type Metrics struct {
	mu sync.Mutex
	// requests and failures are per procedure. Per procedure and not per service:
	// "the platform is slow" is never the useful form of that sentence.
	requests map[key]uint64
	// seconds is the total time spent, which with the count gives a mean. Not a
	// histogram: a histogram needs buckets chosen in advance, and choosing them
	// before anybody has watched this run would be inventing a latency profile
	// rather than measuring one.
	seconds  map[key]float64
	inflight int64
	started  time.Time
}

type key struct {
	procedure string
	code      int
}

// NewMetrics returns a fresh set.
func NewMetrics() *Metrics {
	return &Metrics{
		requests: map[key]uint64{},
		seconds:  map[key]float64{},
		started:  time.Now(),
	}
}

// Middleware counts every request that passes through it.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The scrape endpoint does not count itself. A metric that rises because
		// something is reading metrics describes the monitoring rather than the
		// platform.
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		m.mu.Lock()
		m.inflight++
		m.mu.Unlock()

		start := time.Now()
		rec := &recorder{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(rec, r)

		m.mu.Lock()
		m.inflight--
		k := key{procedure: procedureOf(r.URL.Path), code: rec.code}
		m.requests[k]++
		m.seconds[k] += time.Since(start).Seconds()
		m.mu.Unlock()
	})
}

// Handler writes the metrics in Prometheus text exposition format.
func (m *Metrics) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		type row struct {
			k key
			n uint64
			s float64
		}
		rows := make([]row, 0, len(m.requests))
		for k, n := range m.requests {
			rows = append(rows, row{k: k, n: n, s: m.seconds[k]})
		}
		inflight := m.inflight
		up := time.Since(m.started).Seconds()
		m.mu.Unlock()

		// Sorted, so two scrapes of an unchanged process produce identical bytes.
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].k.procedure != rows[j].k.procedure {
				return rows[i].k.procedure < rows[j].k.procedure
			}
			return rows[i].k.code < rows[j].k.code
		})

		var b strings.Builder
		b.WriteString("# HELP gavya_requests_total Requests served, by procedure and status.\n")
		b.WriteString("# TYPE gavya_requests_total counter\n")
		for _, r := range rows {
			fmt.Fprintf(&b, "gavya_requests_total{procedure=%q,code=\"%d\"} %d\n",
				r.k.procedure, r.k.code, r.n)
		}

		b.WriteString("# HELP gavya_request_seconds_total Time spent serving, by procedure and status.\n")
		b.WriteString("# TYPE gavya_request_seconds_total counter\n")
		for _, r := range rows {
			fmt.Fprintf(&b, "gavya_request_seconds_total{procedure=%q,code=\"%d\"} %s\n",
				r.k.procedure, r.k.code, strconv.FormatFloat(r.s, 'f', 6, 64))
		}

		b.WriteString("# HELP gavya_requests_in_flight Requests being served right now.\n")
		b.WriteString("# TYPE gavya_requests_in_flight gauge\n")
		fmt.Fprintf(&b, "gavya_requests_in_flight %d\n", inflight)

		b.WriteString("# HELP gavya_uptime_seconds Seconds since this process started.\n")
		b.WriteString("# TYPE gavya_uptime_seconds gauge\n")
		fmt.Fprintf(&b, "gavya_uptime_seconds %s\n", strconv.FormatFloat(up, 'f', 3, 64))

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(b.String()))
	}
}

// procedureOf is the label a request is counted under.
//
// The path, which for this platform is already a bounded set: every procedure is
// registered by name, so there is no risk of one label per identifier — the
// failure that turns a metrics endpoint into an outage of its own. Anything that
// is not a procedure is counted as "other" rather than by its path, because that
// is where an unbounded set would come from.
func procedureOf(path string) string {
	trimmed := strings.TrimPrefix(path, "/")
	if trimmed == "" {
		return "other"
	}
	service, method, ok := strings.Cut(trimmed, "/")
	if !ok || !strings.Contains(service, ".") || method == "" {
		switch trimmed {
		case "healthz", "readyz", "metrics":
			return trimmed
		}
		return "other"
	}
	if strings.ContainsAny(method, "/?&=") {
		return "other"
	}
	return trimmed
}

// recorder remembers the status a handler wrote.
type recorder struct {
	http.ResponseWriter
	code    int
	written bool
}

func (r *recorder) WriteHeader(code int) {
	if !r.written {
		r.code, r.written = code, true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	r.written = true
	return r.ResponseWriter.Write(b)
}
