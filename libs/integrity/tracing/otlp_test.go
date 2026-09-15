package tracing

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

// The exported document is the shape a collector reads.
//
// This is the part that fails silently. An OTLP body a collector rejects is
// answered with a 4xx that nothing here looks at, and the platform goes on
// producing spans that land nowhere — tracing that is switched on, costing
// something, and showing nothing. So the document is taken apart and checked
// field by field against the specification's JSON mapping.
//
// What this cannot check is that Jaeger in particular accepts it. There is no
// collector in this environment. It checks that the document is well formed,
// self-consistent, and uses the encodings the specification names; the first run
// against a real collector is still the first run against a real collector.
func TestTheExportedDocumentIsTheShapeACollectorReads(t *testing.T) {
	var got []byte
	var contentType string
	var wg sync.WaitGroup
	wg.Add(1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer wg.Done()
		if r.URL.Path != "/v1/traces" {
			t.Errorf("posted to %s, and the OTLP/HTTP path is /v1/traces", r.URL.Path)
		}
		contentType = r.Header.Get("Content-Type")
		got, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	e := NewOTLP(srv.URL)
	parent := New()
	child := parent.Child()
	start := time.Now()

	e.Record(Span{
		Context: child, Parent: parent.SpanID,
		Service: "milk-service", Name: "milk.v1.MilkService/RecordMilk",
		Start: start, End: start.Add(17 * time.Millisecond),
		Status: 200, TenantID: "TNT_TEST",
	})
	e.Close()
	wg.Wait()

	if contentType != "application/json" {
		t.Errorf("Content-Type is %q", contentType)
	}

	var doc struct {
		ResourceSpans []struct {
			Resource struct {
				Attributes []struct {
					Key   string `json:"key"`
					Value struct {
						StringValue string `json:"stringValue"`
					} `json:"value"`
				} `json:"attributes"`
			} `json:"resource"`
			ScopeSpans []struct {
				Spans []struct {
					TraceID           string `json:"traceId"`
					SpanID            string `json:"spanId"`
					ParentSpanID      string `json:"parentSpanId"`
					Name              string `json:"name"`
					Kind              int    `json:"kind"`
					StartTimeUnixNano string `json:"startTimeUnixNano"`
					EndTimeUnixNano   string `json:"endTimeUnixNano"`
					Status            struct {
						Code int `json:"code"`
					} `json:"status"`
					Attributes []struct {
						Key   string `json:"key"`
						Value struct {
							StringValue string `json:"stringValue"`
						} `json:"value"`
					} `json:"attributes"`
				} `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	}
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("the exported body is not the document it claims to be: %v\n%s", err, got)
	}

	if len(doc.ResourceSpans) != 1 || len(doc.ResourceSpans[0].ScopeSpans) != 1 ||
		len(doc.ResourceSpans[0].ScopeSpans[0].Spans) != 1 {
		t.Fatalf("expected one span in one scope in one resource:\n%s", got)
	}

	// The resource says which service. Without it every span in the platform
	// belongs to nobody and the waterfall has no names on it.
	res := doc.ResourceSpans[0].Resource.Attributes
	if len(res) == 0 || res[0].Key != "service.name" || res[0].Value.StringValue != "milk-service" {
		t.Errorf("the resource does not name the service: %+v", res)
	}

	span := doc.ResourceSpans[0].ScopeSpans[0].Spans[0]

	// Identifiers are lower-case hex strings of the right length. This is the
	// single easiest thing to get wrong, and a collector answers a wrong one
	// with a rejection nothing here reads.
	if len(span.TraceID) != 32 {
		t.Errorf("traceId is %q, and the JSON mapping is 32 hex characters", span.TraceID)
	}
	if len(span.SpanID) != 16 {
		t.Errorf("spanId is %q, and the JSON mapping is 16 hex characters", span.SpanID)
	}
	if span.TraceID != child.Trace() || span.SpanID != child.Span() {
		t.Errorf("the span reports trace %s span %s and it was %s / %s",
			span.TraceID, span.SpanID, child.Trace(), child.Span())
	}
	if span.ParentSpanID != parent.Span() {
		t.Errorf("parentSpanId is %q and the parent's span was %s; without it the "+
			"waterfall is flat and nothing shows what called what",
			span.ParentSpanID, parent.Span())
	}

	// Timestamps are strings of nanoseconds. A JSON number cannot hold one
	// without losing precision, which is why the specification says string.
	for _, ts := range []struct{ name, value string }{
		{"startTimeUnixNano", span.StartTimeUnixNano},
		{"endTimeUnixNano", span.EndTimeUnixNano},
	} {
		n, err := strconv.ParseInt(ts.value, 10, 64)
		if err != nil {
			t.Errorf("%s is %q and should be a string of nanoseconds", ts.name, ts.value)
			continue
		}
		if n < int64(time.Second) {
			t.Errorf("%s is %d, which is not a nanosecond timestamp — seconds or "+
				"milliseconds here puts every span at the epoch", ts.name, n)
		}
	}
	if span.EndTimeUnixNano <= span.StartTimeUnixNano {
		t.Error("the span ends before it starts")
	}

	if span.Name != "milk.v1.MilkService/RecordMilk" {
		t.Errorf("the span is named %q", span.Name)
	}
	if span.Kind != spanKindServer {
		t.Errorf("kind is %d, want %d for a server span", span.Kind, spanKindServer)
	}
	if span.Status.Code != statusUnset {
		t.Errorf("a request that returned 200 has status %d, and unset is what the "+
			"specification asks for: ok means somebody decided it was", span.Status.Code)
	}

	attrs := map[string]string{}
	for _, a := range span.Attributes {
		attrs[a.Key] = a.Value.StringValue
	}
	if attrs["gavya.tenant"] != "TNT_TEST" {
		t.Errorf("the span does not carry the tenant; 'is this slow for everybody or "+
			"for one society' is the first question asked of a slow trace: %v", attrs)
	}

	if sent, dropped, failed := e.Stats(); sent != 1 || dropped != 0 || failed != 0 {
		t.Errorf("sent=%d dropped=%d failed=%d", sent, dropped, failed)
	}
}

// A failing request is marked as one, or a trace full of errors looks healthy.
func TestAFailedSpanIsMarkedAnError(t *testing.T) {
	var got []byte
	var wg sync.WaitGroup
	wg.Add(1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer wg.Done()
		got, _ = io.ReadAll(r.Body)
	}))
	defer srv.Close()

	e := NewOTLP(srv.URL)
	c := New()
	e.Record(Span{Context: c, Service: "milk-service", Name: "x",
		Start: time.Now(), End: time.Now(), Failed: true, Status: 500})
	e.Close()
	wg.Wait()

	var doc otlpPayload
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatal(err)
	}
	span := doc.ResourceSpans[0].ScopeSpans[0].Spans[0]
	if span.Status.Code != statusError {
		t.Errorf("a 500 was exported with status %d, want %d", span.Status.Code, statusError)
	}
}

// A collector that is down, slow or missing must not reach the request path.
func TestACollectorThatIsDownCostsNothingButSpans(t *testing.T) {
	// Nothing listening at all.
	e := NewOTLP("http://127.0.0.1:1")
	for range 10 {
		e.Record(Span{Context: New(), Service: "s", Name: "n", Start: time.Now(), End: time.Now()})
	}
	e.Close()
	if sent, _, failed := e.Stats(); sent != 0 || failed != 10 {
		t.Errorf("sent=%d failed=%d; every span should have been counted as failed", sent, failed)
	}

	// And a collector that answers with a refusal is a failure, not a success.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	e2 := NewOTLP(srv.URL)
	e2.Record(Span{Context: New(), Service: "s", Name: "n", Start: time.Now(), End: time.Now()})
	e2.Close()
	if sent, _, failed := e2.Stats(); sent != 0 || failed != 1 {
		t.Errorf("a 400 was counted as sent=%d failed=%d; a collector rejecting the "+
			"document is exactly the failure this has to notice", sent, failed)
	}
}

// No endpoint configured is no exporter, and recording to it is safe.
func TestNoEndpointMeansNoTracingAndNoCrash(t *testing.T) {
	var e *OTLP = NewOTLP("")
	if e != nil {
		t.Fatal("an empty endpoint produced an exporter")
	}
	// The nil receiver has to work, or every main function needs its own branch
	// and one of them gets it wrong.
	e.Record(Span{Context: New()})
	e.Close()
	if sent, dropped, failed := e.Stats(); sent|dropped|failed != 0 {
		t.Error("a nil exporter counted something")
	}
}

// Dropping is counted, because a silent drop makes a trace with a hole in it
// look like a service that was never called.
func TestSpansAreDroppedRatherThanBlockingAndTheDropIsCounted(t *testing.T) {
	// A collector that never answers, so the sender is stuck and the queue
	// fills.
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)

	e := NewOTLP(srv.URL)
	// Well past the buffer. If Record blocked, this would never return and the
	// test would time out — which is the failure being guarded against.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 10000 {
			e.Record(Span{Context: New(), Service: "s", Name: "n",
				Start: time.Now(), End: time.Now()})
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Record blocked on a collector that never answers, which would have " +
			"stopped the request path")
	}

	if _, dropped, _ := e.Stats(); dropped == 0 {
		t.Error("the queue filled and nothing was counted as dropped, so a trace with a " +
			"hole in it would look like a service that was never called")
	}
}
