// Package metrics provides pluggable metrics collection for database operations,
// HTTP requests, circuit breakers, and service-level metrics.
//
// # This package vs observability/metrics
//
// There are two metrics packages in the repo. They are complementary layers,
// not duplicates (audited 2026-04-19 — roadmap task B.3):
//
//   - packages/metrics (this package) — the SEMANTIC WRITE layer.
//     Exposes a MetricsProvider interface with domain-flavoured record
//     methods (RecordDBOperation, RecordHTTPRequest, RecordCircuitBreakerState,
//     …). Infrastructure code (cache, pgxpostgres, helpers/service) records
//     events through this API. Backend strategy (Prometheus / OpenTelemetry /
//     Datadog) is swappable at runtime via config.
//
//   - packages/observability/metrics — the RAW READ layer.
//     Exposes a Collector that wraps a Prometheus Registry directly and
//     provides Counter/Gauge/Histogram/Summary primitives plus GetSnapshot()
//     for enumerating current metric state. Used by observability/alerts
//     (threshold evaluation) and observability/api (HTTP snapshot endpoint).
//     No corresponding readback exists on MetricsProvider, so removing
//     observability/metrics would leave the alert engine without a source.
//
// If you need to record a domain event, use THIS package. If you need to
// read current metric values from process memory, use observability/metrics.
//
// # Architecture
//
// This package implements a strategy pattern with three backend providers:
//   - Prometheus: Registry-based metrics with HTTP endpoint (/metrics)
//   - OpenTelemetry: Modern observability standard with OTLP exporters
//   - Datadog: StatsD client for Datadog agent integration
//
// All providers implement the MetricsProvider interface, ensuring consistent
// metrics recording across different backends.
//
// # Provider Selection
//
// The active provider is determined by configuration at runtime:
//
//	cfg := &config.Observability{
//	    Metrics: &config.Observability_Metrics{
//	        Enabled:  true,
//	        Provider: config.Observability_Metrics_PROMETHEUS,
//	        Port:     9090,
//	    },
//	}
//	provider, err := metrics.NewProvider(cfg)
//
// # Available Metrics
//
// Database Operations:
//   - db_operation_duration_seconds: Histogram of DB operation latencies
//   - db_connections_open: Gauge of open connection count
//   - db_operation_retries_total: Counter of retry attempts
//
// HTTP Requests:
//   - http_request_duration_seconds: Histogram of HTTP request latencies
//
// Circuit Breaker:
//   - circuit_breaker_state: Gauge of circuit breaker state
//   - circuit_breaker_failure_total: Counter of circuit breaker failures
//   - circuit_breaker_success_total: Counter of circuit breaker successes
//
// Service Metrics:
//   - service_request_count_total: Counter of service requests
//
// # Usage Pattern
//
// Metrics are accessed via ServiceDeps throughout the application:
//
//	func (h *Handler) Process(ctx context.Context) error {
//	    start := time.Now()
//	    defer func() {
//	        h.deps.Metrics.RecordDBOperation("GetUser", time.Since(start), err == nil)
//	    }()
//
//	    // Process request
//	    user, err := h.repo.GetUser(ctx, userID)
//	    return err
//	}
//
// # Graceful Degradation
//
// When metrics are disabled (cfg.Metrics.Enabled = false), a noopMetricsProvider
// is used, avoiding nil pointer checks throughout the codebase:
//
//	deps.Metrics.RecordDBOperation(...) // Safe even when disabled
//
// # Provider-Specific Details
//
// Prometheus:
//   - Starts HTTP server on configured port (default: 9090)
//   - Exposes /metrics endpoint for Prometheus scraping
//   - Uses promauto for automatic registration
//   - Registry-based for isolation from global metrics
//
// OpenTelemetry:
//   - Uses noop meter by default (TODO: integrate with OTLP exporter)
//   - Supports push-based metrics export
//   - Integrates with OpenTelemetry Collector
//   - Semantic conventions for metric naming
//
// Datadog:
//   - StatsD protocol over UDP
//   - Namespace prefix using service name
//   - Supports tags for multi-dimensional metrics
//   - Graceful shutdown closes StatsD client
//
// # Shutdown
//
// All providers implement Shutdown(ctx) for graceful cleanup:
//
//	defer deps.Metrics.Shutdown(context.Background())
//
// Only DatadogProvider performs actual cleanup (closing StatsD client).
// Prometheus and OpenTelemetry providers return nil immediately.
package metrics

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"p9e.in/samavaya/packages/api/v1/config"

	"github.com/DataDog/datadog-go/v5/statsd"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

// MetricsProvider defines the interface for metrics providers
type MetricsProvider interface {
	RecordDBOperation(operation string, duration time.Duration, success bool)
	RecordDBRetry(operation string)
	SetDBConnections(count float64)
	RecordHTTPRequest(handler, method string, status int, duration time.Duration)
	RecordCircuitBreakerState(serviceName string, state string)
	RecordCircuitBreakerFailure(serviceName string)
	RecordCircuitBreakerSuccess(serviceName string)
	RecordServiceRequestCount(serviceName string)
	Shutdown(ctx context.Context) error
}

// PrometheusProvider implements MetricsProvider for Prometheus
type PrometheusProvider struct {
	serviceName  string
	registration *prometheus.Registry
	cfg          *config.Observability

	dbOperationDuration   *prometheus.HistogramVec
	dbConnectionsOpen     prometheus.Gauge
	dbOperationRetries    *prometheus.CounterVec
	httpRequestDuration   *prometheus.HistogramVec
	circuitBreakerState   *prometheus.GaugeVec
	circuitBreakerFailure *prometheus.CounterVec
	circuitBreakerSuccess *prometheus.CounterVec
	serviceRequestCount   *prometheus.CounterVec
}

// OpenTelemetryProvider implements MetricsProvider for OpenTelemetry
type OpenTelemetryProvider struct {
	meter metric.Meter

	dbOperationDuration   metric.Float64Histogram
	dbConnectionsOpen     metric.Float64UpDownCounter
	dbOperationRetries    metric.Int64Counter
	httpRequestDuration   metric.Float64Histogram
	circuitBreakerState   metric.Int64UpDownCounter
	circuitBreakerFailure metric.Int64Counter
	circuitBreakerSuccess metric.Int64Counter
	serviceRequestCount   metric.Int64Counter
}

// DatadogProvider implements MetricsProvider for Datadog
type DatadogProvider struct {
	client      *statsd.Client
	serviceName string
}

// NewProvider creates a new metrics provider based on configuration
func NewProvider(cfg *config.Observability) (MetricsProvider, error) {
	if !cfg.Metrics.Enabled {
		return &noopMetricsProvider{}, nil
	}

	serviceName := cfg.ServiceName.ServiceName
	if serviceName == "" {
		serviceName = "unnamed-service"
	}

	switch cfg.Metrics.Provider {
	case config.Observability_Metrics_PROMETHEUS:
		return newPrometheusProvider(serviceName, cfg)
	case config.Observability_Metrics_OPENTELEMETRY:
		return newOpenTelemetryProvider(serviceName, cfg)
	case config.Observability_Metrics_DATADOG:
		return newDatadogProvider(serviceName, cfg)
	default:
		return &noopMetricsProvider{}, fmt.Errorf("unsupported metrics provider: %v", cfg.Metrics.Provider)
	}
}

// newPrometheusProvider creates a Prometheus metrics provider
func newPrometheusProvider(serviceName string, cfg *config.Observability) (*PrometheusProvider, error) {
	// Create Prometheus registry
	reg := prometheus.NewRegistry()

	// Define metrics
	dbOperationDuration := promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "db_operation_duration_seconds",
			Help:    "Duration of database operations",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation", "success"},
	)

	dbConnectionsOpen := promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "db_connections_open",
			Help: "Number of open database connections",
		},
	)

	dbOperationRetries := promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "db_operation_retries_total",
			Help: "Total number of database operation retries",
		},
		[]string{"operation"},
	)

	httpRequestDuration := promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Duration of HTTP requests",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"handler", "method", "status"},
	)

	circuitBreakerState := promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "circuit_breaker_state",
			Help: "State of circuit breaker",
		},
		[]string{"service_name", "state"},
	)

	circuitBreakerFailure := promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "circuit_breaker_failure_total",
			Help: "Total number of circuit breaker failures",
		},
		[]string{"service_name"},
	)

	circuitBreakerSuccess := promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "circuit_breaker_success_total",
			Help: "Total number of circuit breaker successes",
		},
		[]string{"service_name"},
	)

	serviceRequestCount := promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "service_request_count_total",
			Help: "Total number of service requests",
		},
		[]string{"service_name"},
	)

	// Register default metrics
	reg.MustRegister(
		dbOperationDuration,
		dbConnectionsOpen,
		dbOperationRetries,
		httpRequestDuration,
		circuitBreakerState,
		circuitBreakerFailure,
		circuitBreakerSuccess,
		serviceRequestCount,
	)

	// Start metrics server
	go startPrometheusMetricsServer(reg, cfg.Metrics.Port)

	return &PrometheusProvider{
		serviceName:           serviceName,
		registration:          reg,
		cfg:                   cfg,
		dbOperationDuration:   dbOperationDuration,
		dbConnectionsOpen:     dbConnectionsOpen,
		dbOperationRetries:    dbOperationRetries,
		httpRequestDuration:   httpRequestDuration,
		circuitBreakerState:   circuitBreakerState,
		circuitBreakerFailure: circuitBreakerFailure,
		circuitBreakerSuccess: circuitBreakerSuccess,
		serviceRequestCount:   serviceRequestCount,
	}, nil
}

// newOpenTelemetryProvider creates an OpenTelemetry metrics provider
func newOpenTelemetryProvider(serviceName string, cfg *config.Observability) (*OpenTelemetryProvider, error) {
	// Use noop meter if OpenTelemetry is not properly configured
	meter := noop.NewMeterProvider().Meter(serviceName)

	// Define OpenTelemetry metrics
	dbOperationDuration, _ := meter.Float64Histogram("db_operation_duration_seconds",
		metric.WithDescription("Duration of database operations"))

	dbConnectionsOpen, _ := meter.Float64UpDownCounter("db_connections_open",
		metric.WithDescription("Number of open database connections"))

	dbOperationRetries, _ := meter.Int64Counter("db_operation_retries_total",
		metric.WithDescription("Total number of database operation retries"))

	httpRequestDuration, _ := meter.Float64Histogram("http_request_duration_seconds",
		metric.WithDescription("Duration of HTTP requests"))

	circuitBreakerState, _ := meter.Int64UpDownCounter("circuit_breaker_state",
		metric.WithDescription("State of circuit breaker"))

	circuitBreakerFailure, _ := meter.Int64Counter("circuit_breaker_failure_total",
		metric.WithDescription("Total number of circuit breaker failures"))

	circuitBreakerSuccess, _ := meter.Int64Counter("circuit_breaker_success_total",
		metric.WithDescription("Total number of circuit breaker successes"))

	serviceRequestCount, _ := meter.Int64Counter("service_request_count_total",
		metric.WithDescription("Total number of service requests"))

	return &OpenTelemetryProvider{
		meter:                 meter,
		dbOperationDuration:   dbOperationDuration,
		dbConnectionsOpen:     dbConnectionsOpen,
		dbOperationRetries:    dbOperationRetries,
		httpRequestDuration:   httpRequestDuration,
		circuitBreakerState:   circuitBreakerState,
		circuitBreakerFailure: circuitBreakerFailure,
		circuitBreakerSuccess: circuitBreakerSuccess,
		serviceRequestCount:   serviceRequestCount,
	}, nil
}

// newDatadogProvider creates a Datadog metrics provider
func newDatadogProvider(serviceName string, cfg *config.Observability) (*DatadogProvider, error) {
	client, err := statsd.New(fmt.Sprintf("%s:%d", cfg.Metrics.Endpoint, cfg.Metrics.Port),
		statsd.WithNamespace(fmt.Sprintf("%s.", serviceName)))
	if err != nil {
		return nil, fmt.Errorf("failed to create Datadog statsd client: %w", err)
	}

	return &DatadogProvider{
		client:      client,
		serviceName: serviceName,
	}, nil
}

// PrometheusProvider methods
func (p *PrometheusProvider) RecordDBOperation(operation string, duration time.Duration, success bool) {
	p.dbOperationDuration.WithLabelValues(
		operation,
		boolToString(success),
	).Observe(duration.Seconds())
}

func (p *PrometheusProvider) RecordDBRetry(operation string) {
	p.dbOperationRetries.WithLabelValues(operation).Inc()
}

func (p *PrometheusProvider) SetDBConnections(count float64) {
	p.dbConnectionsOpen.Set(count)
}

func (p *PrometheusProvider) RecordHTTPRequest(handler, method string, status int, duration time.Duration) {
	p.httpRequestDuration.WithLabelValues(
		handler,
		method,
		strconv.Itoa(status),
	).Observe(duration.Seconds())
}

func (p *PrometheusProvider) RecordCircuitBreakerState(serviceName string, state string) {
	p.circuitBreakerState.WithLabelValues(serviceName, state).Inc()
}

func (p *PrometheusProvider) RecordCircuitBreakerFailure(serviceName string) {
	p.circuitBreakerFailure.WithLabelValues(serviceName).Inc()
}

func (p *PrometheusProvider) RecordCircuitBreakerSuccess(serviceName string) {
	p.circuitBreakerSuccess.WithLabelValues(serviceName).Inc()
}

func (p *PrometheusProvider) RecordServiceRequestCount(serviceName string) {
	p.serviceRequestCount.WithLabelValues(serviceName).Inc()
}

func (p *PrometheusProvider) Shutdown(ctx context.Context) error {
	return nil
}

// OpenTelemetryProvider methods
func (o *OpenTelemetryProvider) RecordDBOperation(operation string, duration time.Duration, success bool) {
	o.dbOperationDuration.Record(context.Background(), duration.Seconds(),
		metric.WithAttributes(
			attribute.String("operation", operation),
			attribute.String("success", boolToString(success)),
		))
}

func (o *OpenTelemetryProvider) RecordDBRetry(operation string) {
	o.dbOperationRetries.Add(context.Background(), 1,
		metric.WithAttributes(attribute.String("operation", operation)))
}

func (o *OpenTelemetryProvider) SetDBConnections(count float64) {
	o.dbConnectionsOpen.Add(context.Background(), count)
}

func (o *OpenTelemetryProvider) RecordHTTPRequest(handler, method string, status int, duration time.Duration) {
	o.httpRequestDuration.Record(context.Background(), duration.Seconds(),
		metric.WithAttributes(
			attribute.String("handler", handler),
			attribute.String("method", method),
			attribute.String("status", strconv.Itoa(status)),
		))
}

func (o *OpenTelemetryProvider) RecordCircuitBreakerState(serviceName string, state string) {
	o.circuitBreakerState.Add(context.Background(), 1,
		metric.WithAttributes(
			attribute.String("service_name", serviceName),
			attribute.String("state", state),
		))
}

func (o *OpenTelemetryProvider) RecordCircuitBreakerFailure(serviceName string) {
	o.circuitBreakerFailure.Add(context.Background(), 1,
		metric.WithAttributes(attribute.String("service_name", serviceName)))
}

func (o *OpenTelemetryProvider) RecordCircuitBreakerSuccess(serviceName string) {
	o.circuitBreakerSuccess.Add(context.Background(), 1,
		metric.WithAttributes(attribute.String("service_name", serviceName)))
}

func (o *OpenTelemetryProvider) RecordServiceRequestCount(serviceName string) {
	o.serviceRequestCount.Add(context.Background(), 1,
		metric.WithAttributes(attribute.String("service_name", serviceName)))
}

func (o *OpenTelemetryProvider) Shutdown(ctx context.Context) error {
	return nil
}

// DatadogProvider methods
func (d *DatadogProvider) RecordDBOperation(operation string, duration time.Duration, success bool) {
	tags := []string{
		fmt.Sprintf("operation:%s", operation),
		fmt.Sprintf("success:%v", success),
	}

	// Record operation duration as histogram
	_ = d.client.Histogram("db.operation.duration", duration.Seconds(), tags, 1)

	// Increment operation count (stateless - no memory growth)
	_ = d.client.Incr("db.operation.count", tags, 1)
}

func (d *DatadogProvider) RecordDBRetry(operation string) {
	tags := []string{fmt.Sprintf("operation:%s", operation)}
	_ = d.client.Incr("db.operation.retries", tags, 1)
}

func (d *DatadogProvider) SetDBConnections(count float64) {
	_ = d.client.Gauge("db.connections.open", count, []string{}, 1)
}

func (d *DatadogProvider) RecordHTTPRequest(handler, method string, status int, duration time.Duration) {
	tags := []string{
		fmt.Sprintf("handler:%s", handler),
		fmt.Sprintf("method:%s", method),
		fmt.Sprintf("status:%d", status),
	}
	_ = d.client.Histogram("http.request.duration", duration.Seconds(), tags, 1)
}

func (d *DatadogProvider) RecordCircuitBreakerState(serviceName string, state string) {
	tags := []string{
		fmt.Sprintf("service_name:%s", serviceName),
		fmt.Sprintf("state:%s", state),
	}
	// Use gauge with state value (0=closed, 1=half-open, 2=open)
	stateValue := float64(0)
	switch state {
	case "half-open":
		stateValue = 1
	case "open":
		stateValue = 2
	}
	_ = d.client.Gauge("circuit_breaker.state", stateValue, tags, 1)
}

func (d *DatadogProvider) RecordCircuitBreakerFailure(serviceName string) {
	tags := []string{fmt.Sprintf("service_name:%s", serviceName)}
	_ = d.client.Incr("circuit_breaker.failure", tags, 1)
}

func (d *DatadogProvider) RecordCircuitBreakerSuccess(serviceName string) {
	tags := []string{fmt.Sprintf("service_name:%s", serviceName)}
	_ = d.client.Incr("circuit_breaker.success", tags, 1)
}

func (d *DatadogProvider) RecordServiceRequestCount(serviceName string) {
	tags := []string{fmt.Sprintf("service_name:%s", serviceName)}
	_ = d.client.Incr("service.request.count", tags, 1)
}

func (d *DatadogProvider) Shutdown(ctx context.Context) error {
	return d.client.Close()
}

// startPrometheusMetricsServer starts the Prometheus metrics HTTP server
func startPrometheusMetricsServer(reg *prometheus.Registry, port int32) {
	// Use default port if not specified
	if port == 0 {
		port = 9090
	}

	// Create HTTP handler for Prometheus metrics
	handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})

	// Start metrics server
	http.Handle("/metrics", handler)
	server := &http.Server{
		Addr: fmt.Sprintf(":%d", port),
	}

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
	}
}

// Utility function
func boolToString(b bool) string {
	return strconv.FormatBool(b)
}

// noopMetricsProvider is a fallback provider that does nothing
type noopMetricsProvider struct{}

func (n *noopMetricsProvider) RecordDBOperation(operation string, duration time.Duration, success bool) {
}
func (n *noopMetricsProvider) RecordDBRetry(operation string) {}
func (n *noopMetricsProvider) SetDBConnections(count float64) {}
func (n *noopMetricsProvider) RecordHTTPRequest(handler, method string, status int, duration time.Duration) {
}
func (n *noopMetricsProvider) RecordCircuitBreakerState(serviceName string, state string) {}
func (n *noopMetricsProvider) RecordCircuitBreakerFailure(serviceName string)             {}
func (n *noopMetricsProvider) RecordCircuitBreakerSuccess(serviceName string)             {}
func (n *noopMetricsProvider) RecordServiceRequestCount(serviceName string)               {}
func (n *noopMetricsProvider) Shutdown(ctx context.Context) error                         { return nil }
