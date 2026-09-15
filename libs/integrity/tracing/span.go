package tracing

import (
	"context"
	"net/http"
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
			here := Context{}
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
