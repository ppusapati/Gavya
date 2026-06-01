// Package metrics (observability/metrics) is the RAW READ layer over a
// Prometheus registry — Counter/Gauge/Histogram/Summary primitives plus
// GetSnapshot() for enumerating current state. Used by observability/alerts
// (threshold evaluation) and observability/api (HTTP snapshot endpoint).
//
// For the SEMANTIC WRITE layer (MetricsProvider with RecordDBOperation,
// RecordHTTPRequest, etc.), see packages/metrics at the top of the repo.
// The two packages intentionally coexist as complementary layers — the
// write API is backend-swappable (Prometheus / OTel / Datadog) while this
// collector is Prometheus-specific because it enables readback which the
// semantic API does not expose. Confirmed non-duplicate during the
// 2026-04-19 packages audit (roadmap task B.3).
package metrics

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"p9e.in/samavaya/packages/p9log"
	"p9e.in/samavaya/packages/observability"
)

// Collector collects and manages metrics.
//
// Type notes (B.1 sweep, 2026-04-19):
//   - `logger` holds *p9log.Helper for the level-methods (Debug/Info/...).
//     Raw Logger only defines Log(level, keyvals...).
//   - `histograms` / `summaries` store prometheus.Observer. On recent
//     client_golang versions *HistogramVec.WithLabelValues() and
//     *SummaryVec.WithLabelValues() both return the common Observer
//     supertype; storing as Observer lets us cache the per-label-set
//     instance without picking one concrete type. Callers read these
//     back as Observer; the concrete type isn't inspected anywhere.
type Collector struct {
	counters   map[string]prometheus.Counter
	gauges     map[string]prometheus.Gauge
	histograms map[string]prometheus.Observer
	summaries  map[string]prometheus.Observer
	mu         sync.RWMutex
	logger     *p9log.Helper
	registry   *prometheus.Registry
	namespace  string
	subsystem  string
}

// NewCollector creates a new metrics collector
func NewCollector(namespace string, logger p9log.Logger) *Collector {
	return &Collector{
		counters:   make(map[string]prometheus.Counter),
		gauges:     make(map[string]prometheus.Gauge),
		histograms: make(map[string]prometheus.Observer),
		summaries:  make(map[string]prometheus.Observer),
		logger:     p9log.NewHelper(logger),
		registry:   prometheus.NewRegistry(),
		namespace:  namespace,
	}
}

// Counter creates or gets a counter metric
func (c *Collector) Counter(name, help string, labels []string) prometheus.CounterVec {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := name
	if _, exists := c.counters[key]; exists {
		return c.getCounter(key).(prometheus.CounterVec)
	}

	opts := prometheus.CounterOpts{
		Namespace: c.namespace,
		Name:      name,
		Help:      help,
	}

	counter := promauto.With(c.registry).NewCounterVec(opts, labels)

	// Store for later retrieval
	c.counters[key] = counter.WithLabelValues()

	c.logger.Debug("created counter metric", "name", name)
	return *counter
}

// Gauge creates or gets a gauge metric
func (c *Collector) Gauge(name, help string, labels []string) prometheus.GaugeVec {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := name
	if _, exists := c.gauges[key]; exists {
		return c.getGauge(key).(prometheus.GaugeVec)
	}

	opts := prometheus.GaugeOpts{
		Namespace: c.namespace,
		Name:      name,
		Help:      help,
	}

	gauge := promauto.With(c.registry).NewGaugeVec(opts, labels)

	c.gauges[key] = gauge.WithLabelValues()

	c.logger.Debug("created gauge metric", "name", name)
	return *gauge
}

// Histogram creates a histogram Vec and registers a cached zero-label
// observer under `name`. Calling twice with the same name returns a fresh
// Vec (promauto is idempotent against the registry so this doesn't double-
// register); the cached Observer is kept primarily so GetSnapshot can
// enumerate without reflecting over every Vec.
func (c *Collector) Histogram(name, help string, labels []string) prometheus.HistogramVec {
	c.mu.Lock()
	defer c.mu.Unlock()

	opts := prometheus.HistogramOpts{
		Namespace: c.namespace,
		Name:      name,
		Help:      help,
		Buckets:   prometheus.DefBuckets,
	}

	histogram := promauto.With(c.registry).NewHistogramVec(opts, labels)

	// WithLabelValues() on HistogramVec returns prometheus.Observer on
	// recent client_golang versions; store the interface form.
	c.histograms[name] = histogram.WithLabelValues()

	c.logger.Debug("created histogram metric", "name", name)
	return *histogram
}

// Summary creates a summary Vec. Same semantics as Histogram above.
func (c *Collector) Summary(name, help string, labels []string) prometheus.SummaryVec {
	c.mu.Lock()
	defer c.mu.Unlock()

	opts := prometheus.SummaryOpts{
		Namespace: c.namespace,
		Name:      name,
		Help:      help,
	}

	summary := promauto.With(c.registry).NewSummaryVec(opts, labels)

	c.summaries[name] = summary.WithLabelValues()

	c.logger.Debug("created summary metric", "name", name)
	return *summary
}

// GetSnapshot returns a snapshot of all metrics
func (c *Collector) GetSnapshot(ctx context.Context) (*observability.MetricSnapshot, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	snapshot := &observability.MetricSnapshot{
		Timestamp: time.Now(),
		Metrics:   make([]observability.Metric, 0),
	}

	// Get metrics from registry
	families, err := c.registry.Gather()
	if err != nil {
		return nil, fmt.Errorf("failed to gather metrics: %w", err)
	}

	// Convert to observability metrics
	for _, family := range families {
		for _, metric := range family.Metric {
			labels := make(map[string]string)
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}

			value := 0.0
			if metric.Counter != nil {
				value = metric.Counter.GetValue()
			} else if metric.Gauge != nil {
				value = metric.Gauge.GetValue()
			} else if metric.Histogram != nil {
				value = float64(metric.Histogram.GetSampleCount())
			} else if metric.Summary != nil {
				value = float64(metric.Summary.GetSampleCount())
			}

			// Newer prometheus client_model types TimestampMs as *int64 so
			// unset-vs-zero is distinguishable. We treat unset as "now" —
			// the snapshot's outer Timestamp carries the real cutoff; the
			// per-metric timestamp is only a hint.
			tsMs := int64(0)
			if metric.TimestampMs != nil {
				tsMs = *metric.TimestampMs
			}
			snapshot.Metrics = append(snapshot.Metrics, observability.Metric{
				Name:      family.GetName(),
				Type:      observability.MetricType(family.GetType().String()),
				Value:     value,
				Labels:    labels,
				Timestamp: time.Unix(0, tsMs*1e6),
				Help:      family.GetHelp(),
			})
		}
	}

	return snapshot, nil
}

// GetRegistry returns the underlying Prometheus registry
func (c *Collector) GetRegistry() *prometheus.Registry {
	return c.registry
}

// Helper methods

func (c *Collector) getCounter(key string) interface{} {
	if counter, ok := c.counters[key]; ok {
		return counter
	}
	return nil
}

func (c *Collector) getGauge(key string) interface{} {
	if gauge, ok := c.gauges[key]; ok {
		return gauge
	}
	return nil
}

func (c *Collector) getHistogram(key string) interface{} {
	if histogram, ok := c.histograms[key]; ok {
		return histogram
	}
	return nil
}

func (c *Collector) getSummary(key string) interface{} {
	if summary, ok := c.summaries[key]; ok {
		return summary
	}
	return nil
}
