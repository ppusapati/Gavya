package tracing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// EndpointEnv is where the collector is, and empty means no tracing.
//
// The same arrangement as the ML tier's URLs, and for the same reason: the
// platform has to be complete without it. A deployment with no collector records
// nothing and loses nothing else, and the spans a service would have sent are
// simply not sent.
const EndpointEnv = "GAVYA_TRACE_ENDPOINT"

// OTLP sends spans to a collector as OTLP over HTTP with a JSON body.
//
// Hand-written, like the Prometheus exposition in libs/integrity/observe, and
// for the same reason: the format is small and specified, and the SDK that would
// write it is not small. What goes on the wire is the standard, so Jaeger or
// Tempo or an OpenTelemetry Collector reads it without knowing the difference.
//
// Three properties matter more than throughput here:
//
//   - It never blocks a request. Spans go into a buffered channel and are
//     dropped when it is full. A tracing system that slows the collection path
//     is worse than no tracing system, because the thing it is measuring is the
//     thing it is now breaking.
//   - It never fails a request. An export error is counted and logged once in a
//     while, not returned. Nothing a caller did is wrong because a collector is
//     down.
//   - It says how many it dropped. A silent drop makes a trace with a hole in it
//     look like a service that was never called, which is the wrong conclusion
//     and an expensive one.
type OTLP struct {
	endpoint string
	client   *http.Client
	spans    chan Span

	// batch is how many spans go in one request, and flush is how long a
	// partial batch waits. Small numbers: a co-operative's traffic is bursty
	// and a span held for a minute is one somebody is currently waiting to see.
	batch int
	flush time.Duration

	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once

	mu      sync.Mutex
	dropped uint64
	failed  uint64
	sent    uint64
}

// NewOTLP returns an exporter, or nil when no endpoint is configured.
//
// A nil recorder is usable: Record on a nil *OTLP does nothing, so a caller does
// not have to branch. The alternative is every main function writing the same
// "if tracing is on" and one of them getting it wrong.
func NewOTLP(endpoint string) *OTLP {
	if endpoint == "" {
		return nil
	}
	e := &OTLP{
		endpoint: endpoint + "/v1/traces",
		client:   &http.Client{Timeout: 10 * time.Second},
		spans:    make(chan Span, 2048),
		batch:    128,
		flush:    2 * time.Second,
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
	go e.run()
	return e
}

// FromEnv builds the exporter a deployment asked for.
func FromEnv() *OTLP { return NewOTLP(os.Getenv(EndpointEnv)) }

// Record queues a span, and drops it rather than waiting.
func (e *OTLP) Record(s Span) {
	if e == nil {
		return
	}
	select {
	case e.spans <- s:
	default:
		e.mu.Lock()
		e.dropped++
		e.mu.Unlock()
	}
}

// Stats is what this exporter has done, for the log line at shutdown and for a
// test to assert on.
func (e *OTLP) Stats() (sent, dropped, failed uint64) {
	if e == nil {
		return 0, 0, 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.sent, e.dropped, e.failed
}

// Close flushes what is queued and stops. Safe to call more than once.
func (e *OTLP) Close() {
	if e == nil {
		return
	}
	e.stopOnce.Do(func() { close(e.stop) })
	<-e.done
}

func (e *OTLP) run() {
	defer close(e.done)
	ticker := time.NewTicker(e.flush)
	defer ticker.Stop()

	pending := make([]Span, 0, e.batch)
	send := func() {
		if len(pending) == 0 {
			return
		}
		e.post(pending)
		pending = pending[:0]
	}

	for {
		select {
		case s := <-e.spans:
			pending = append(pending, s)
			if len(pending) >= e.batch {
				send()
			}
		case <-ticker.C:
			send()
		case <-e.stop:
			// Drain what is already queued before going, so the spans from the
			// request that was in flight when the process was asked to stop are
			// not the ones missing.
			for {
				select {
				case s := <-e.spans:
					pending = append(pending, s)
					if len(pending) >= e.batch {
						send()
					}
					continue
				default:
				}
				break
			}
			send()
			return
		}
	}
}

func (e *OTLP) post(spans []Span) {
	body, err := json.Marshal(payloadFor(spans))
	if err != nil {
		e.count(&e.failed, len(spans))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		e.count(&e.failed, len(spans))
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		e.count(&e.failed, len(spans))
		return
	}
	defer resp.Body.Close()
	// Anything but 2xx is a failure. Not retried: by the time a retry landed the
	// spans would be stale, and a queue that retries under a failing collector
	// fills up and starts dropping the live ones instead.
	if resp.StatusCode/100 != 2 {
		e.count(&e.failed, len(spans))
		return
	}
	e.count(&e.sent, len(spans))
}

func (e *OTLP) count(field *uint64, n int) {
	e.mu.Lock()
	*field += uint64(n)
	e.mu.Unlock()
}

// The OTLP/JSON shape. Written out rather than generated, because it is this
// small and a protobuf toolchain for one message is not a trade worth making.
//
// Two details are easy to get wrong and silently drop every span: identifiers
// are lower-case hex strings rather than bytes, and timestamps are strings of
// nanoseconds since the epoch rather than numbers — a JSON number cannot hold a
// nanosecond timestamp without losing precision, so the specification says
// string.
type otlpPayload struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	Resource   otlpResource    `json:"resource"`
	ScopeSpans []otlpScopeSpan `json:"scopeSpans"`
}

type otlpResource struct {
	Attributes []otlpAttr `json:"attributes"`
}

type otlpScopeSpan struct {
	Scope otlpScope  `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpScope struct {
	Name string `json:"name"`
}

type otlpSpan struct {
	TraceID           string     `json:"traceId"`
	SpanID            string     `json:"spanId"`
	ParentSpanID      string     `json:"parentSpanId,omitempty"`
	Name              string     `json:"name"`
	Kind              int        `json:"kind"`
	StartTimeUnixNano string     `json:"startTimeUnixNano"`
	EndTimeUnixNano   string     `json:"endTimeUnixNano"`
	Attributes        []otlpAttr `json:"attributes,omitempty"`
	Status            otlpStatus `json:"status"`
}

type otlpAttr struct {
	Key   string    `json:"key"`
	Value otlpValue `json:"value"`
}

type otlpValue struct {
	StringValue string `json:"stringValue"`
}

type otlpStatus struct {
	// 0 unset, 1 ok, 2 error. Unset rather than ok for a success, which is what
	// the specification asks for: "ok" means somebody decided it was, and a 200
	// is not a decision.
	Code int `json:"code"`
}

const (
	spanKindServer = 2
	statusUnset    = 0
	statusError    = 2
)

func payloadFor(spans []Span) otlpPayload {
	// Grouped by service, because a resource in OTLP is the thing the spans came
	// from. One process only ever emits its own, but the modulith is every
	// service in one process, so this cannot assume a single name.
	byService := map[string][]Span{}
	order := []string{}
	for _, s := range spans {
		if _, seen := byService[s.Service]; !seen {
			order = append(order, s.Service)
		}
		byService[s.Service] = append(byService[s.Service], s)
	}

	out := otlpPayload{ResourceSpans: make([]otlpResourceSpans, 0, len(order))}
	for _, service := range order {
		group := byService[service]
		converted := make([]otlpSpan, 0, len(group))
		for _, s := range group {
			span := otlpSpan{
				TraceID:           s.Context.Trace(),
				SpanID:            s.Context.Span(),
				Name:              s.Name,
				Kind:              spanKindServer,
				StartTimeUnixNano: strconv.FormatInt(s.Start.UnixNano(), 10),
				EndTimeUnixNano:   strconv.FormatInt(s.End.UnixNano(), 10),
				Status:            otlpStatus{Code: statusUnset},
				Attributes: []otlpAttr{
					{Key: "http.response.status_code", Value: otlpValue{StringValue: strconv.Itoa(s.Status)}},
				},
			}
			if s.Parent != ([8]byte{}) {
				span.ParentSpanID = fmt.Sprintf("%x", s.Parent)
			}
			if s.TenantID != "" {
				// The tenant, because "is this slow for everybody or for one
				// society" is the first question asked of a slow trace.
				span.Attributes = append(span.Attributes,
					otlpAttr{Key: "gavya.tenant", Value: otlpValue{StringValue: s.TenantID}})
			}
			if s.Failed {
				span.Status.Code = statusError
			}
			converted = append(converted, span)
		}
		out.ResourceSpans = append(out.ResourceSpans, otlpResourceSpans{
			Resource: otlpResource{Attributes: []otlpAttr{
				{Key: "service.name", Value: otlpValue{StringValue: service}},
			}},
			ScopeSpans: []otlpScopeSpan{{
				Scope: otlpScope{Name: "gavya"},
				Spans: converted,
			}},
		})
	}
	return out
}
