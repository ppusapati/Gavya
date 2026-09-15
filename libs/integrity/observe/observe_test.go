package observe

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A readiness probe goes red when a dependency does.
//
// This is the whole point. /healthz returned 200 unconditionally, so a service
// whose database had gone away stayed in rotation being sent traffic it could
// not serve. Readiness is the probe that is allowed to fail, and it has to
// actually fail.
func TestReadinessGoesRedWhenADependencyDoes(t *testing.T) {
	gone := errors.New("connection refused")

	ready := Ready(Check{Name: "database", Ping: func(context.Context) error { return nil }})
	w := httptest.NewRecorder()
	ready(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusOK {
		t.Errorf("a service whose database answers is %d, want 200", w.Code)
	}

	notReady := Ready(Check{Name: "database", Ping: func(context.Context) error { return gone }})
	w = httptest.NewRecorder()
	notReady(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("a service whose database is gone reports %d, want 503", w.Code)
	}
	// Which dependency, and why. A probe that goes red saying only "not ready"
	// sends somebody to read logs to find out what this could have told them.
	if !strings.Contains(w.Body.String(), "database") {
		t.Errorf("the failure does not name the dependency: %q", w.Body)
	}
	if !strings.Contains(w.Body.String(), "connection refused") {
		t.Errorf("the failure does not say what went wrong: %q", w.Body)
	}
}

// One dependency failing is enough, and the others are still named.
func TestOneFailingDependencyIsEnoughAndAllAreReported(t *testing.T) {
	w := httptest.NewRecorder()
	Ready(
		Check{Name: "pooling", Ping: func(context.Context) error { return nil }},
		Check{Name: "settlement", Ping: func(context.Context) error { return errors.New("down") }},
		Check{Name: "milk", Ping: func(context.Context) error { return errors.New("also down") }},
	)(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("two of three dependencies are down and the service reports %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{"settlement", "milk"} {
		if !strings.Contains(body, want) {
			t.Errorf("the failure does not name %s: %q", want, body)
		}
	}
	if strings.Contains(body, "pooling") {
		t.Errorf("the failure names a dependency that answered: %q", body)
	}
}

// A service with nothing to check says so rather than silently passing.
func TestAServiceWithNoChecksSaysThereAreNone(t *testing.T) {
	w := httptest.NewRecorder()
	Ready()(w, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("a service with no dependencies reports %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "nothing to check") {
		t.Errorf("the body is %q and does not distinguish a service with no "+
			"dependencies from one whose checks were never wired", w.Body)
	}
}

// The metrics count what was served, per procedure and per status.
func TestMetricsCountPerProcedureAndStatus(t *testing.T) {
	m := NewMetrics()
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/ApproveCycle") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 3; i++ {
		handler.ServeHTTP(httptest.NewRecorder(),
			httptest.NewRequest(http.MethodPost, "/milk.v1.MilkService/RecordMilk", nil))
	}
	handler.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/settlement.v1.SettlementService/ApproveCycle", nil))

	w := httptest.NewRecorder()
	m.Handler()(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := w.Body.String()

	for _, want := range []string{
		`gavya_requests_total{procedure="milk.v1.MilkService/RecordMilk",code="200"} 3`,
		`gavya_requests_total{procedure="settlement.v1.SettlementService/ApproveCycle",code="403"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the metrics do not carry %s\n%s", want, body)
		}
	}
	// Time as well as count. A count alone cannot answer "is it slow", which is
	// half of what somebody asks during an incident.
	if !strings.Contains(body, "gavya_request_seconds_total{") {
		t.Error("nothing records how long anything took")
	}
	if !strings.Contains(body, "gavya_requests_in_flight") {
		t.Error("nothing records what is in flight, so a wedged service looks idle")
	}
}

// Scraping does not count itself.
func TestTheScrapeDoesNotCountItself(t *testing.T) {
	m := NewMetrics()
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for i := 0; i < 5; i++ {
		handler.ServeHTTP(httptest.NewRecorder(),
			httptest.NewRequest(http.MethodGet, "/metrics", nil))
	}

	w := httptest.NewRecorder()
	m.Handler()(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if strings.Contains(w.Body.String(), `procedure="metrics"`) {
		t.Error("the metrics count the scrapes, so the graph rises because something " +
			"is reading it rather than because anything happened")
	}
}

// A path that is not a procedure does not become its own label.
//
// One label per identifier is how a metrics endpoint becomes an outage of its
// own: the series count grows without bound and the scrape eventually kills the
// thing it is watching.
func TestAnUnboundedPathDoesNotBecomeALabel(t *testing.T) {
	m := NewMetrics()
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for _, path := range []string{
		"/files/01M2ABCDEF", "/files/01M2GHIJKL", "/some/deep/unknown/path", "/",
	} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}

	w := httptest.NewRecorder()
	m.Handler()(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := w.Body.String()
	if strings.Contains(body, "01M2ABCDEF") || strings.Contains(body, "01M2GHIJKL") {
		t.Errorf("an identifier reached a metric label:\n%s", body)
	}
	if !strings.Contains(body, `procedure="other"`) {
		t.Errorf("paths that are not procedures were not folded together:\n%s", body)
	}
}

// The health and readiness endpoints are counted under their own names.
func TestTheProbesAreCountedUnderTheirOwnNames(t *testing.T) {
	for _, c := range []struct{ path, want string }{
		{"/healthz", "healthz"},
		{"/readyz", "readyz"},
		{"/milk.v1.MilkService/RecordMilk", "milk.v1.MilkService/RecordMilk"},
		{"/nonsense", "other"},
	} {
		if got := procedureOf(c.path); got != c.want {
			t.Errorf("%s is counted as %q, want %q", c.path, got, c.want)
		}
	}
}

// Two scrapes of an unchanged process produce the same bytes.
//
// Map iteration order is random in Go, so without sorting the output differs
// every time — which makes a diff of two scrapes useless and any caching wrong.
func TestTwoScrapesOfAnUnchangedProcessAgree(t *testing.T) {
	m := NewMetrics()
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for _, p := range []string{
		"/a.v1.AService/One", "/b.v1.BService/Two", "/c.v1.CService/Three",
		"/d.v1.DService/Four", "/e.v1.EService/Five",
	} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, p, nil))
	}

	scrape := func() string {
		w := httptest.NewRecorder()
		m.Handler()(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		// The uptime gauge moves between scrapes and is meant to.
		var kept []string
		for _, line := range strings.Split(w.Body.String(), "\n") {
			if !strings.HasPrefix(line, "gavya_uptime_seconds ") &&
				!strings.HasPrefix(line, "gavya_request_seconds_total") {
				kept = append(kept, line)
			}
		}
		return strings.Join(kept, "\n")
	}
	if first, second := scrape(), scrape(); first != second {
		t.Errorf("two scrapes of an unchanged process differ:\n%s\n---\n%s", first, second)
	}
}

// The histogram is a histogram: cumulative, +Inf equal to the count, sum equal
// to the time spent.
//
// Those three are what histogram_quantile relies on, and getting any of them
// wrong produces a metric that scrapes cleanly and answers latency questions
// with nonsense — which is worse than having none, because somebody will build
// an alert on it.
func TestTheHistogramIsCumulativeAndAddsUp(t *testing.T) {
	m := NewMetrics()
	h := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Millisecond)
	}))

	const calls = 5
	for range calls {
		req := httptest.NewRequest("POST", "/milk.v1.MilkService/RecordMilk", nil)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	body := scrape(t, m)

	// Cumulative: every bound holds at least as many as the one below it.
	var last uint64
	for _, bound := range Bounds {
		le := strconv.FormatFloat(bound, 'f', -1, 64)
		n := bucketValue(t, body, le)
		if n < last {
			t.Errorf("bucket le=%s holds %d and the bound below holds %d; a histogram's "+
				"buckets are cumulative and histogram_quantile reads nonsense out of one "+
				"that is not", le, n, last)
		}
		last = n
	}

	// +Inf is every request there was.
	if n := bucketValue(t, body, "+Inf"); n != calls {
		t.Errorf("the +Inf bucket holds %d and %d requests were served", n, calls)
	}
	// And the count agrees with the counter beside it.
	if !strings.Contains(body, `gavya_request_duration_seconds_count{procedure="milk.v1.MilkService/RecordMilk",code="200"} 5`) {
		t.Errorf("the histogram's count is not 5:\n%s", body)
	}

	// Each call slept two milliseconds, so nothing can have landed in the
	// millisecond bucket and everything must be in the five.
	if n := bucketValue(t, body, "0.001"); n != 0 {
		t.Errorf("%d requests were counted under a millisecond and each one slept two", n)
	}
	if n := bucketValue(t, body, "0.005"); n != calls {
		t.Errorf("the 5ms bucket holds %d of %d requests that each slept 2ms", n, calls)
	}
}

func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler()(rec, httptest.NewRequest("GET", "/metrics", nil))
	return rec.Body.String()
}

func bucketValue(t *testing.T, body, le string) uint64 {
	t.Helper()
	want := `gavya_request_duration_seconds_bucket{procedure="milk.v1.MilkService/RecordMilk",code="200",le="` + le + `"} `
	for _, line := range strings.Split(body, "\n") {
		if rest, ok := strings.CutPrefix(line, want); ok {
			n, err := strconv.ParseUint(strings.TrimSpace(rest), 10, 64)
			if err != nil {
				t.Fatalf("bucket %s is not a number: %q", le, rest)
			}
			return n
		}
	}
	t.Fatalf("no bucket le=%q in:\n%s", le, body)
	return 0
}

// A service can publish what it knows about itself.
//
// This package counted requests and nothing else, so anything a service knew
// about its own work — how deep a queue is, whether a chain verifies — could not
// be watched at all. The alert rules named two such blind spots and could do
// nothing about either, because there was no way to publish the number.
func TestAServicePublishesWhatItKnowsAboutItself(t *testing.T) {
	forgetGauges()
	t.Cleanup(forgetGauges)

	depth := 7.0
	Publish(Gauge{
		Name: "gavya_notification_outbox_depth",
		Help: "Messages owed to somebody and not yet delivered.",
		Read: func() float64 { return depth },
	})
	Publish(Gauge{
		Name: "gavya_audit_chain_intact",
		Help: "1 when the hash chain verified at the last check.",
		Read: func() float64 { return 1 },
	})

	body := scrape(t, NewMetrics())
	for _, want := range []string{
		"# TYPE gavya_notification_outbox_depth gauge",
		"gavya_notification_outbox_depth 7",
		"# TYPE gavya_audit_chain_intact gauge",
		"gavya_audit_chain_intact 1",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the scrape does not contain %q:\n%s", want, body)
		}
	}

	// Read when asked, not when registered. An outbox's depth is a question with
	// an answer at the moment it is asked; a value captured at boot describes a
	// queue that has since drained or overflowed.
	depth = 41
	if body := scrape(t, NewMetrics()); !strings.Contains(body, "gavya_notification_outbox_depth 41") {
		t.Errorf("the gauge reported a stale value:\n%s", body)
	}
}

// One name, one line.
//
// Two lines with the same metric name is a document Prometheus rejects, and a
// rejected scrape loses every other metric in it — so one careless registration
// would blind the whole service rather than just itself.
func TestPublishingTheSameNameTwiceReplacesItRatherThanDuplicating(t *testing.T) {
	forgetGauges()
	t.Cleanup(forgetGauges)

	Publish(Gauge{Name: "gavya_queue_depth", Help: "first", Read: func() float64 { return 1 }})
	Publish(Gauge{Name: "gavya_queue_depth", Help: "second", Read: func() float64 { return 2 }})

	body := scrape(t, NewMetrics())
	if n := strings.Count(body, "\ngavya_queue_depth "); n != 1 {
		t.Errorf("the name appears on %d lines; Prometheus rejects the whole document and "+
			"every other metric in it goes with this one:\n%s", n, body)
	}
	if !strings.Contains(body, "gavya_queue_depth 2") {
		t.Errorf("the second registration did not replace the first:\n%s", body)
	}
}

// A gauge with nothing to read is ignored rather than crashing the scrape.
func TestAnIncompleteGaugeIsIgnored(t *testing.T) {
	forgetGauges()
	t.Cleanup(forgetGauges)

	Publish(Gauge{Name: "gavya_no_reader"})
	Publish(Gauge{Read: func() float64 { return 1 }})

	body := scrape(t, NewMetrics())
	if strings.Contains(body, "gavya_no_reader") {
		t.Errorf("a gauge with no reader was published:\n%s", body)
	}
}

// Two scrapes of an unchanged process produce identical bytes, gauges included.
func TestScrapesAreStable(t *testing.T) {
	forgetGauges()
	t.Cleanup(forgetGauges)

	Publish(Gauge{Name: "gavya_b", Help: "b", Read: func() float64 { return 2 }})
	Publish(Gauge{Name: "gavya_a", Help: "a", Read: func() float64 { return 1 }})

	m := NewMetrics()
	first, second := scrape(t, m), scrape(t, m)
	// Uptime moves, so compare only the gauge lines.
	pick := func(body string) []string {
		var out []string
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "gavya_a") || strings.HasPrefix(line, "gavya_b") {
				out = append(out, line)
			}
		}
		return out
	}
	a, b := pick(first), pick(second)
	if len(a) != 2 || strings.Join(a, "|") != strings.Join(b, "|") {
		t.Errorf("two scrapes differ:\n%v\n%v", a, b)
	}
	if a[0] != "gavya_a 1" {
		t.Errorf("gauges are not sorted by name: %v", a)
	}
}
