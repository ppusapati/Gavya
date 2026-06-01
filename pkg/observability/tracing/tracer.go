// Package tracing (observability/tracing) is the in-memory trace BROWSER
// used by the observability admin UI. It keeps recent spans in process
// memory and exposes GetTrace / GetAllTraces / PruneTraces for read-side
// inspection.
//
// For the production-grade OpenTelemetry provider that emits spans to
// Jaeger / Zipkin / OTLP (what helpers/service uses to instrument every
// call), see packages/tracing at the top of the repo. The two packages
// coexist intentionally — confirmed non-duplicate during the 2026-04-19
// packages audit (roadmap task B.4).
package tracing

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"p9e.in/samavaya/packages/p9log"
	"p9e.in/samavaya/packages/observability"
)

// Tracer implements distributed tracing. `logger` is a *p9log.Helper so
// level-methods are available on the field. Roadmap task B.1.
type Tracer struct {
	serviceName string
	logger      *p9log.Helper
	spans       map[string]*SpanImpl
	mu          sync.RWMutex
	traceID     atomic.Uint64
}

// SpanImpl implements the Span interface
type SpanImpl struct {
	traceID    string
	spanID     string
	parentID   string
	name       string
	attributes map[string]interface{}
	events     []Event
	startTime  time.Time
	endTime    *time.Time
	status     string
	mu         sync.RWMutex
}

// Event represents an event in a span
type Event struct {
	Name      string
	Timestamp time.Time
	Attributes map[string]interface{}
}

// NewTracer creates a new tracer
func NewTracer(serviceName string, logger p9log.Logger) *Tracer {
	return &Tracer{
		serviceName: serviceName,
		logger:      p9log.NewHelper(logger),
		spans:       make(map[string]*SpanImpl),
	}
}

// StartSpan creates a new span
func (t *Tracer) StartSpan(ctx context.Context, name string) *SpanImpl {
	traceID := uuid.New().String()
	spanID := uuid.New().String()

	span := &SpanImpl{
		traceID:    traceID,
		spanID:     spanID,
		name:       name,
		attributes: make(map[string]interface{}),
		events:     make([]Event, 0),
		startTime:  time.Now(),
		status:     "ok",
	}

	// Extract parent span from context if present
	if parentSpan, ok := ctx.Value("span").(*SpanImpl); ok {
		span.parentID = parentSpan.spanID
	}

	t.mu.Lock()
	t.spans[spanID] = span
	t.mu.Unlock()

	t.logger.Debug("started span",
		"trace_id", traceID,
		"span_id", spanID,
		"span_name", name,
	)

	return span
}

// SetAttribute sets a span attribute
func (s *SpanImpl) SetAttribute(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.attributes[key] = value
}

// AddEvent adds an event to the span
func (s *SpanImpl) AddEvent(name string, attrs ...interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Convert attrs to map
	attrMap := make(map[string]interface{})
	for i := 0; i < len(attrs); i += 2 {
		if i+1 < len(attrs) {
			key, ok := attrs[i].(string)
			if ok {
				attrMap[key] = attrs[i+1]
			}
		}
	}

	s.events = append(s.events, Event{
		Name:       name,
		Timestamp:  time.Now(),
		Attributes: attrMap,
	})
}

// End ends the span
func (s *SpanImpl) End() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.endTime = &now
}

// IsRecording returns whether this span is recording
func (s *SpanImpl) IsRecording() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.endTime == nil
}

// SpanContext returns the span context
func (s *SpanImpl) SpanContext() observability.SpanContext {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return observability.SpanContext{
		TraceID: s.traceID,
		SpanID:  s.spanID,
	}
}

// SetStatus sets the span status
func (s *SpanImpl) SetStatus(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.status = status
}

// GetTrace returns the complete trace for this span
func (t *Tracer) GetTrace(ctx context.Context, traceID string) (*observability.Trace, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Find all spans for this trace
	var spans []observability.Span
	var rootSpan *SpanImpl
	var startTime time.Time
	var endTime time.Time

	for _, span := range t.spans {
		if span.traceID == traceID {
			spans = append(spans, span)

			if span.parentID == "" {
				rootSpan = span
			}

			if rootSpan == nil || span.startTime.Before(startTime) {
				startTime = span.startTime
			}

			if span.endTime != nil && (endTime.IsZero() || span.endTime.After(endTime)) {
				endTime = *span.endTime
			}
		}
	}

	if rootSpan == nil {
		return nil, fmt.Errorf("trace not found: %s", traceID)
	}

	duration := time.Since(startTime)
	if !endTime.IsZero() {
		duration = endTime.Sub(startTime)
	}

	return &observability.Trace{
		TraceID:    traceID,
		RootSpanID: rootSpan.spanID,
		Service:    t.serviceName,
		Operation:  rootSpan.name,
		StartTime:  startTime,
		EndTime:    endTime,
		Duration:   duration,
		Status:     rootSpan.status,
		Spans:      spans,
	}, nil
}

// GetAllTraces returns all active traces
func (t *Tracer) GetAllTraces(ctx context.Context) []*observability.Trace {
	t.mu.RLock()

	// Group spans by trace ID
	traceMap := make(map[string][]*SpanImpl)
	for _, span := range t.spans {
		traceMap[span.traceID] = append(traceMap[span.traceID], span)
	}

	t.mu.RUnlock()

	var traces []*observability.Trace

	for traceID := range traceMap {
		trace, err := t.GetTrace(ctx, traceID)
		if err == nil {
			traces = append(traces, trace)
		}
	}

	return traces
}

// PruneTraces removes old traces
func (t *Tracer) PruneTraces(maxAge time.Duration) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	pruned := 0

	for spanID, span := range t.spans {
		if span.endTime != nil && span.endTime.Before(cutoff) {
			delete(t.spans, spanID)
			pruned++
		}
	}

	return pruned
}

// SamplingDecision returns whether to sample this trace
func (t *Tracer) SamplingDecision(samplingRate float64) bool {
	if samplingRate >= 1.0 {
		return true
	}
	if samplingRate <= 0.0 {
		return false
	}

	// Simple random sampling
	return float64(t.traceID.Add(1)%100) < (samplingRate * 100)
}
