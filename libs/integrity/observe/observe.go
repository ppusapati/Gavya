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
	// seconds is the total time spent, which with the count gives a mean, and is
	// the _sum of the histogram below.
	seconds map[key]float64
	// buckets counts how many requests fell within each upper bound, per
	// procedure and code.
	//
	// There was no histogram here for a long time and the reason was good: a
	// histogram needs buckets chosen in advance, and choosing them before anybody
	// had watched this platform run would have been inventing a latency profile
	// rather than measuring one. e2e/load_test.go is the watching. Recording a
	// thousand collections at ten, twenty-five and fifty booths at once put the
	// median between 3.4ms and 17.2ms and the ninety-ninth percentile between
	// 7.3ms and 27.7ms, so the resolution that matters is from a millisecond to a
	// tenth of a second — which is where these are dense. The bounds above that
	// are headroom for a deployment with a real network between the services and
	// a disk somebody else is also using.
	//
	// A mean would not have caught what this is for. A stall that hits five per
	// cent of a morning's collections moves a mean by a few milliseconds and moves
	// the ninety-ninth percentile by seconds, and it is the second number that
	// describes what a person at a booth experienced.
	buckets  map[key][]uint64
	inflight int64
	started  time.Time
}

type key struct {
	procedure string
	code      int
}

// Bounds are the histogram's upper bounds, in seconds, and they are the ones
// e2e/load_test.go reports against so that a future measurement can be compared
// with the buckets rather than only with the percentiles.
//
// Cumulative, as Prometheus histograms are: a request counted in 5ms is also
// counted in every bound above it. The +Inf bucket is implied and written last.
var Bounds = []float64{
	0.001, 0.0025, 0.005, 0.010, 0.025, 0.050,
	0.100, 0.250, 0.500, 1.0, 2.5, 5.0,
}

// A Gauge is a number a service publishes about itself.
//
// Until now this package counted requests and nothing else, so anything a
// service knows about its own work — how deep a queue is, whether a chain still
// verifies — could not be watched. The alert rules named two such blind spots
// and could do nothing about either, because there was no way to publish the
// number.
//
// Pull rather than push: the service registers a function and it is called when
// somebody scrapes. That suits what these actually are. An outbox's depth is a
// question with an answer at the moment it is asked, and a counter maintained
// alongside the rows is a second copy that drifts from them — which is the
// failure this platform keeps finding, in the form that looks most like
// diligence.
//
// The function must be cheap and must not block. It runs inside the scrape, so
// one that takes a second makes every scrape take a second, and one that hangs
// takes the metrics endpoint with it. Something expensive — verifying a hash
// chain, say — belongs on its own schedule, publishing its last result through a
// gauge that only reads a variable.
type Gauge struct {
	Name string
	Help string
	Read func() float64
}

// A Counter is a number a service publishes that only ever goes up.
//
// A separate type from Gauge rather than a field on it, because the difference
// is the whole of what a reader does with the number. `rate()` and `increase()`
// are written against counters, and Prometheus uses the declared type to know
// that a drop is a process restart rather than a value falling. A monotonic
// number declared as a gauge works by accident today and is a lie in the
// exposition, which is the kind of thing this platform does not leave lying
// about for somebody to trust later.
//
// Same rule as Gauge about the function: it runs inside the scrape, so it reads
// a variable and does not go to the database.
type Counter struct {
	Name string
	Help string
	Read func() float64
}

// A Series is one labelled sample: the label values, in the order the
// LabelledCounter that produced it names them, and the number.
type Series struct {
	Labels []string
	Value  float64
}

// A LabelledCounter is a counter broken down by something.
//
// Until now this package had one shape for a published number: a name and a
// value. That was enough for everything a service knows about itself as a single
// figure — a queue depth, a chain that still verifies — and not enough for the
// one requirement left owed from the inherited library's list: per-table,
// per-operation query metrics. A breakdown needs labels.
//
// # THE REASON THIS DID NOT EXIST SOONER
//
// Labels are how a metrics endpoint becomes an outage of its own. One label per
// identifier and a scrape returns a million lines, the scraper falls behind, and
// the first thing anybody loses is the monitoring they were relying on to notice.
// procedureOf in this same file exists entirely to prevent that for request
// counts, and it counts anything it does not recognise as "other".
//
// So this type carries a ceiling rather than trusting its callers. Read returns
// whatever it has; the handler emits MaxSeries of them in sorted order and says
// plainly how many it left out, under <name>_series_dropped. Truncating quietly
// would be the same defect in a smaller font.
//
// Same rule as Gauge and Counter about the function: it runs inside the scrape,
// so it reads a variable and does not go to the database.
type LabelledCounter struct {
	Name string
	Help string
	// Labels are the label names, in the order each Series gives its values.
	Labels []string
	Read   func() []Series
}

// MaxSeries is how many lines one LabelledCounter may contribute to a scrape.
//
// Five hundred, which is above anything this platform should produce — the
// database has a hundred and twelve tables and the statements against them use
// five verbs — and far below the point at which a scrape becomes a problem. It
// is a ceiling on a mistake, not a budget to spend.
const MaxSeries = 500

var (
	gaugesMu sync.RWMutex
	gauges   []Gauge
	counters []Counter
	labelled []LabelledCounter
)

// Publish registers a gauge. Meant to be called at boot, once per name.
//
// A second registration of the same name replaces the first rather than
// producing two lines with one name, which is a document Prometheus rejects —
// and rejecting it loses every other metric in the same scrape, so one careless
// caller would blind the whole service.
func Publish(g Gauge) {
	if g.Name == "" || g.Read == nil {
		return
	}
	gaugesMu.Lock()
	defer gaugesMu.Unlock()
	for i := range gauges {
		if gauges[i].Name == g.Name {
			gauges[i] = g
			return
		}
	}
	gauges = append(gauges, g)
}

// PublishCounter registers a counter, on the same terms as Publish.
func PublishCounter(c Counter) {
	if c.Name == "" || c.Read == nil {
		return
	}
	gaugesMu.Lock()
	defer gaugesMu.Unlock()
	for i := range counters {
		if counters[i].Name == c.Name {
			counters[i] = c
			return
		}
	}
	counters = append(counters, c)
}

// PublishLabelledCounter registers a labelled counter, on the same terms as
// Publish.
//
// A counter naming no labels is refused rather than published: it is a Counter
// written in the wrong shape, and publishing it would produce lines with an
// empty label set that read as the plain counter it should have been.
func PublishLabelledCounter(c LabelledCounter) {
	if c.Name == "" || c.Read == nil || len(c.Labels) == 0 {
		return
	}
	gaugesMu.Lock()
	defer gaugesMu.Unlock()
	for i := range labelled {
		if labelled[i].Name == c.Name {
			labelled[i] = c
			return
		}
	}
	labelled = append(labelled, c)
}

func publishedLabelled() []LabelledCounter {
	gaugesMu.RLock()
	defer gaugesMu.RUnlock()
	out := make([]LabelledCounter, len(labelled))
	copy(out, labelled)
	return out
}

// publishedGauges is a copy, so the handler is not holding the lock while it
// calls somebody else's function.
func publishedGauges() []Gauge {
	gaugesMu.RLock()
	defer gaugesMu.RUnlock()
	out := make([]Gauge, len(gauges))
	copy(out, gauges)
	return out
}

func publishedCounters() []Counter {
	gaugesMu.RLock()
	defer gaugesMu.RUnlock()
	out := make([]Counter, len(counters))
	copy(out, counters)
	return out
}

// forgetGauges exists for tests, which would otherwise see each other's.
func forgetGauges() {
	gaugesMu.Lock()
	defer gaugesMu.Unlock()
	labelled = nil
	gauges = nil
	counters = nil
}

// NewMetrics returns a fresh set.
func NewMetrics() *Metrics {
	return &Metrics{
		requests: map[key]uint64{},
		seconds:  map[key]float64{},
		buckets:  map[key][]uint64{},
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
		took := time.Since(start).Seconds()
		m.requests[k]++
		m.seconds[k] += took
		if m.buckets[k] == nil {
			m.buckets[k] = make([]uint64, len(Bounds))
		}
		// Cumulative: a request counted in one bound is counted in every bound
		// above it, which is what makes histogram_quantile able to interpolate.
		for i, bound := range Bounds {
			if took <= bound {
				m.buckets[k][i]++
			}
		}
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
			b []uint64
		}
		rows := make([]row, 0, len(m.requests))
		for k, n := range m.requests {
			b := make([]uint64, len(m.buckets[k]))
			copy(b, m.buckets[k])
			rows = append(rows, row{k: k, n: n, s: m.seconds[k], b: b})
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

		// The histogram. Its _count repeats gavya_requests_total and its _sum
		// repeats gavya_request_seconds_total, which is how a Prometheus
		// histogram is shaped rather than an oversight: the two counters are the
		// names an error-rate query is written against, and the histogram is what
		// a latency one needs. Dropping either would make one of the two
		// questions awkward to ask.
		b.WriteString("# HELP gavya_request_duration_seconds Time spent serving, as a histogram.\n")
		b.WriteString("# TYPE gavya_request_duration_seconds histogram\n")
		for _, r := range rows {
			for i, bound := range Bounds {
				var n uint64
				if i < len(r.b) {
					n = r.b[i]
				}
				fmt.Fprintf(&b, "gavya_request_duration_seconds_bucket{procedure=%q,code=\"%d\",le=%q} %d\n",
					r.k.procedure, r.k.code, strconv.FormatFloat(bound, 'f', -1, 64), n)
			}
			// +Inf is every request, by definition, and Prometheus requires it.
			fmt.Fprintf(&b, "gavya_request_duration_seconds_bucket{procedure=%q,code=\"%d\",le=\"+Inf\"} %d\n",
				r.k.procedure, r.k.code, r.n)
			fmt.Fprintf(&b, "gavya_request_duration_seconds_sum{procedure=%q,code=\"%d\"} %s\n",
				r.k.procedure, r.k.code, strconv.FormatFloat(r.s, 'f', 6, 64))
			fmt.Fprintf(&b, "gavya_request_duration_seconds_count{procedure=%q,code=\"%d\"} %d\n",
				r.k.procedure, r.k.code, r.n)
		}

		b.WriteString("# HELP gavya_requests_in_flight Requests being served right now.\n")
		b.WriteString("# TYPE gavya_requests_in_flight gauge\n")
		fmt.Fprintf(&b, "gavya_requests_in_flight %d\n", inflight)

		b.WriteString("# HELP gavya_uptime_seconds Seconds since this process started.\n")
		b.WriteString("# TYPE gavya_uptime_seconds gauge\n")
		fmt.Fprintf(&b, "gavya_uptime_seconds %s\n", strconv.FormatFloat(up, 'f', 3, 64))

		// What the service says about itself. Sorted by name for the same reason
		// the rows above are: two scrapes of an unchanged process should produce
		// identical bytes.
		published := publishedGauges()
		sort.Slice(published, func(i, j int) bool { return published[i].Name < published[j].Name })
		for _, g := range published {
			fmt.Fprintf(&b, "# HELP %s %s\n", g.Name, g.Help)
			fmt.Fprintf(&b, "# TYPE %s gauge\n", g.Name)
			fmt.Fprintf(&b, "%s %s\n", g.Name, strconv.FormatFloat(g.Read(), 'f', -1, 64))
		}

		publishedCounts := publishedCounters()
		sort.Slice(publishedCounts, func(i, j int) bool {
			return publishedCounts[i].Name < publishedCounts[j].Name
		})
		for _, c := range publishedCounts {
			fmt.Fprintf(&b, "# HELP %s %s\n", c.Name, c.Help)
			fmt.Fprintf(&b, "# TYPE %s counter\n", c.Name)
			fmt.Fprintf(&b, "%s %s\n", c.Name, strconv.FormatFloat(c.Read(), 'f', -1, 64))
		}

		for _, c := range publishedLabelled() {
			writeLabelled(&b, c)
		}

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(b.String()))
	}
}

// writeLabelled renders one labelled counter, bounded and sorted.
//
// Sorted so two scrapes of an unchanged process produce identical bytes, which
// is the property every other block in this handler has. Bounded because the
// alternative is the failure described on LabelledCounter, and the count of what
// was left out is emitted rather than implied — a truncated metric that does not
// say it was truncated is a number somebody will read as the whole picture.
func writeLabelled(b *strings.Builder, c LabelledCounter) {
	series := c.Read()

	// A series whose label count does not match the names is dropped rather than
	// rendered short: Prometheus rejects the whole document on a malformed line,
	// and rejecting it loses every other metric in the same scrape.
	kept := make([]Series, 0, len(series))
	malformed := 0
	for _, s := range series {
		if len(s.Labels) != len(c.Labels) {
			malformed++
			continue
		}
		kept = append(kept, s)
	}

	sort.Slice(kept, func(i, j int) bool {
		for n := range kept[i].Labels {
			if kept[i].Labels[n] != kept[j].Labels[n] {
				return kept[i].Labels[n] < kept[j].Labels[n]
			}
		}
		return false
	})

	dropped := malformed
	if len(kept) > MaxSeries {
		dropped += len(kept) - MaxSeries
		kept = kept[:MaxSeries]
	}

	fmt.Fprintf(b, "# HELP %s %s\n", c.Name, c.Help)
	fmt.Fprintf(b, "# TYPE %s counter\n", c.Name)
	for _, s := range kept {
		b.WriteString(c.Name)
		b.WriteString("{")
		for i, name := range c.Labels {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(b, "%s=%q", name, s.Labels[i])
		}
		fmt.Fprintf(b, "} %s\n", strconv.FormatFloat(s.Value, 'f', -1, 64))
	}

	// Always emitted, including as zero. A line that appears only when something
	// is wrong is a line nobody has a graph of when it does.
	fmt.Fprintf(b, "# HELP %s_series_dropped Series left out of %s: above %d, or malformed.\n",
		c.Name, c.Name, MaxSeries)
	fmt.Fprintf(b, "# TYPE %s_series_dropped gauge\n", c.Name)
	fmt.Fprintf(b, "%s_series_dropped %d\n", c.Name, dropped)
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
