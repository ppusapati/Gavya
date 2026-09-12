package observe

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
