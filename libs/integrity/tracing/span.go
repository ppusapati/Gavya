package tracing

import (
	"context"
	"net/http"
	"os"
	"sync"
	"time"
)

// A Span is one hop: a service doing one thing, for one request.
//
// Deliberately small. Every field here is one somebody reads while looking at a
// slow settlement — which service, which procedure, how long, and did it fail —
// and the temptation with tracing is to record everything and produce a
// waterfall nobody can see the shape of.
type Span struct {
	Context Context
	// Parent is the span that called this one, empty at the start of a trace.
	Parent   [8]byte
	Service  string
	Name     string
	Start    time.Time
	End      time.Time
	Failed   bool
	Status   int
	TenantID string
}

// Duration is how long the hop took.
func (s Span) Duration() time.Duration { return s.End.Sub(s.Start) }

// A Recorder is given every span that finishes.
//
// An interface so that a service can be run with no tracing at all — the
// platform has to work with the tier switched off, the same way it works with
// the ML tier switched off — and so that a test can assert on what was recorded
// without standing up a collector.
type Recorder interface {
	Record(Span)
}

// Discard records nothing, and is what a service uses when no collector is
// configured.
type Discard struct{}

func (Discard) Record(Span) {}

// Collector is the recorder in use.
//
// A package-level value rather than something threaded through every handler,
// for the same reason the metrics registry is: the alternative is thirty main
// functions passing a recorder down to a middleware, and the one that forgets is
// the service missing from every trace.
var (
	collectorMu sync.RWMutex
	collector   Recorder = Discard{}
)

// UseCollector installs the recorder. Not safe to call while serving, and it is
// meant to be called once, at boot.
func UseCollector(r Recorder) {
	collectorMu.Lock()
	defer collectorMu.Unlock()
	if r == nil {
		r = Discard{}
	}
	collector = r
}

func record(s Span) {
	collectorMu.RLock()
	r := collector
	collectorMu.RUnlock()
	r.Record(s)
}

// ServiceNameEnv names this process in a trace.
//
// Read from the environment rather than passed in: threading it through would
// mean editing thirty main functions, and the one edited wrongly is the service
// that appears in every trace under somebody else's name. Every deployment
// already sets it — it is in each service's ConfigMap and in both compose files.
//
// It lives here rather than in serve because serve is no longer the only thing
// that names a span: tenantdb records one per query, and it does not import
// serve.
const ServiceNameEnv = "SERVICE_NAME"

// ServiceName is what this process calls itself in a span.
//
// "unknown-service" rather than empty, because a span with no service is one a
// collector groups with every other nameless span in the platform, which looks
// like one enormous service that does everything.
func ServiceName() string {
	if v := os.Getenv(ServiceNameEnv); v != "" {
		return v
	}
	return "unknown-service"
}

// Child records one span inside a request that is already being traced, and
// returns the function that ends it.
//
// The end function takes the error the work returned, so that a failed span is
// marked without the caller having to decide what "failed" means.
//
// # IT DOES NOTHING WHEN THERE IS NO TRACE
//
// That is the decision worth stating, because Inject does the opposite: an
// outgoing service call with no trace on its context starts one, so that the
// work still appears. This does not, and the difference is volume. A background
// sweep makes one service call and hundreds of queries. Starting a trace per
// query would fill a trace store with single-span traces nobody asked for, and
// the traces somebody did ask for would be harder to find among them.
//
// The cost is that a query made outside a request is not traced at all. That is
// the right way round: those are the sweeps and the boot checks, and they are
// watched by their own gauges rather than by traces.
func Child(ctx context.Context, service, name string) func(err error) {
	parent, ok := From(ctx)
	if !ok {
		return func(error) {}
	}
	here := parent.Child()
	start := time.Now()
	var once sync.Once
	return func(err error) {
		once.Do(func() {
			record(Span{
				Context: here,
				Parent:  parent.SpanID,
				Service: service,
				Name:    name,
				Start:   start,
				End:     time.Now(),
				Failed:  err != nil,
			})
		})
	}
}

// Middleware continues the caller's trace, or starts one, and records the span.
//
// The order this runs in matters and is set by serve: it is outside the
// authorisation check, so a refused request is still traced. A request that was
// turned away is exactly the one somebody is asking about.
func Middleware(service string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The probe endpoints are not traced. They are called every few
			// seconds by the cluster for the life of the process, and a trace
			// store full of readiness checks is one nobody opens.
			switch r.URL.Path {
			case "/healthz", "/readyz", "/metrics":
				next.ServeHTTP(w, r)
				return
			}

			var parent [8]byte
			var here Context
			if caller, ok := Parse(r.Header.Get(Header)); ok {
				parent = caller.SpanID
				here = caller.Child()
			} else {
				here = New()
			}

			ctx := With(r.Context(), here)
			rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rec, r.WithContext(ctx))

			record(Span{
				Context:  here,
				Parent:   parent,
				Service:  service,
				Name:     procedureName(r.URL.Path),
				Start:    start,
				End:      time.Now(),
				Failed:   rec.code >= 500,
				Status:   rec.code,
				TenantID: r.Header.Get("X-Gavya-Tenant"),
			})
		})
	}
}

// procedureName is the span's name: the procedure, which is already a bounded
// set because every one is registered by name.
//
// A path with an identifier in it would produce one span name per request, and a
// trace store indexes on the name.
func procedureName(path string) string {
	if path == "" || path == "/" {
		return "other"
	}
	return path[1:]
}

type statusRecorder struct {
	http.ResponseWriter
	code    int
	written bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.written {
		r.code, r.written = code, true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.written = true
	return r.ResponseWriter.Write(b)
}

// Inject puts the outgoing hop's traceparent on a request.
//
// Called by svcclient for every service-to-service call. Without it the chain
// stops at whichever service forgets, and a trace that covers four of five hops
// is one that points at the wrong service.
func Inject(ctx context.Context, h http.Header) Context {
	parent, ok := From(ctx)
	if !ok {
		// No trace on this context: a background sweep, a test, a startup task.
		// Start one rather than send nothing, so the work still appears.
		parent = New()
	}
	next := parent.Child()
	h.Set(Header, next.String())
	return next
}
