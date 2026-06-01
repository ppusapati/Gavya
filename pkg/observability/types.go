package observability

import (
	"context"
	"time"
)

// MetricType represents the type of metric
type MetricType string

const (
	MetricTypeCounter   MetricType = "counter"
	MetricTypeGauge    MetricType = "gauge"
	MetricTypeHistogram MetricType = "histogram"
	MetricTypeSummary  MetricType = "summary"
)

// Metric represents a single metric value
type Metric struct {
	// Name of the metric
	Name string

	// Type of metric
	Type MetricType

	// Value
	Value float64

	// Labels (dimensions)
	Labels map[string]string

	// Timestamp
	Timestamp time.Time

	// Help text
	Help string
}

// MetricSnapshot represents a snapshot of all current metrics
type MetricSnapshot struct {
	// Timestamp when snapshot was taken
	Timestamp time.Time

	// All metrics
	Metrics []Metric
}

// Span represents a single trace span
type Span interface {
	// SetAttribute sets a span attribute
	SetAttribute(key string, value interface{})

	// AddEvent adds an event to the span
	AddEvent(name string, attrs ...interface{})

	// End ends the span
	End()

	// IsRecording returns whether this span is recording
	IsRecording() bool

	// SpanContext returns the span context
	SpanContext() SpanContext
}

// SpanContext represents span context information
type SpanContext struct {
	// Trace ID
	TraceID string

	// Span ID
	SpanID string

	// Trace state
	TraceState string

	// Is remote (from different service)
	IsRemote bool
}

// Trace represents a complete distributed trace
type Trace struct {
	// Trace ID
	TraceID string

	// Root span ID
	RootSpanID string

	// Service name
	Service string

	// Operation name
	Operation string

	// Start time
	StartTime time.Time

	// End time
	EndTime time.Time

	// Duration
	Duration time.Duration

	// Status (ok, error)
	Status string

	// Error message (if any)
	Error string

	// All spans in trace
	Spans []Span

	// Request metadata
	Metadata map[string]string
}

// Dependency represents a service dependency
type Dependency struct {
	// Dependent service name
	Service string

	// Number of calls
	CallCount int64

	// Successful calls
	SuccessCount int64

	// Failed calls
	ErrorCount int64

	// Success rate (0-100)
	SuccessRate int

	// Average latency (ms)
	AvgLatency int64

	// P99 latency (ms)
	P99Latency int64

	// Last call time
	LastCallTime time.Time

	// Error rate (0-100)
	ErrorRate int
}

// ServiceDependencies represents all dependencies of a service
type ServiceDependencies struct {
	// Service name
	Service string

	// All dependencies
	Dependencies []Dependency

	// Dependency graph depth
	Depth int

	// Circular dependency detected
	HasCircular bool

	// Generated at
	GeneratedAt time.Time
}

// AlertSeverity represents alert severity level
type AlertSeverity string

const (
	SeverityInfo    AlertSeverity = "info"
	SeverityWarning AlertSeverity = "warning"
	SeverityCritical AlertSeverity = "critical"
)

// AlertStatus represents alert status
type AlertStatus string

const (
	StatusFiring   AlertStatus = "firing"
	StatusResolved AlertStatus = "resolved"
)

// AlertRule defines a single alert rule
type AlertRule struct {
	// Rule name
	Name string

	// Metric to monitor
	Metric string

	// Operator (>, <, ==, >=, <=)
	Op string

	// Threshold value
	Threshold float64

	// Duration the condition must be true
	Duration time.Duration

	// Action to trigger (slack, pagerduty, email)
	Action string

	// Cooldown period (prevent alert spam)
	Cooldown time.Duration

	// Custom labels
	Labels map[string]string

	// Whether rule is enabled
	Enabled bool
}

// Alert represents a fired alert
type Alert struct {
	// Alert name
	Name string

	// Severity
	Severity AlertSeverity

	// Status (firing or resolved)
	Status AlertStatus

	// Message
	Message string

	// Service affected
	Service string

	// Metric value that triggered alert
	Value float64

	// When alert was triggered
	FiredAt time.Time

	// When alert was resolved (if resolved)
	ResolvedAt *time.Time

	// Associated labels
	Labels map[string]string

	// Rule that fired this alert
	Rule *AlertRule
}

// AlertRecipient defines where to send alerts
type AlertRecipient struct {
	// Type (slack, email, pagerduty, webhook)
	Type string

	// Address (slack channel, email, webhook URL)
	Address string

	// Minimum severity to notify
	MinSeverity AlertSeverity
}

// ObservabilityConfig defines configuration
type ObservabilityConfig struct {
	// Enable metrics collection
	EnableMetrics bool

	// Enable distributed tracing
	EnableTracing bool

	// Enable dependency tracking
	EnableDependencies bool

	// Enable alerting
	EnableAlerting bool

	// Metrics flush interval
	MetricsFlushInterval time.Duration

	// Trace batch size
	TraceBatchSize int

	// Trace export interval
	TraceExportInterval time.Duration

	// Dependency update interval
	DependencyInterval time.Duration

	// Alert check interval
	AlertInterval time.Duration

	// Custom metadata
	Metadata map[string]string
}

// DefaultObservabilityConfig returns sensible defaults
func DefaultObservabilityConfig() ObservabilityConfig {
	return ObservabilityConfig{
		EnableMetrics:        true,
		EnableTracing:        true,
		EnableDependencies:   true,
		EnableAlerting:       true,
		MetricsFlushInterval: 10 * time.Second,
		TraceBatchSize:       512,
		TraceExportInterval:  5 * time.Second,
		DependencyInterval:   30 * time.Second,
		AlertInterval:        10 * time.Second,
	}
}

// Options for observability configuration
type Options struct {
	// Configuration
	Config ObservabilityConfig

	// Callback for alerts
	OnAlert func(ctx context.Context, alert *Alert)

	// Callback for metrics
	OnMetricsSnapshot func(ctx context.Context, snapshot *MetricSnapshot)
}

// Option is a functional option for configuring observability
type Option func(*Options)

// WithEnabled enables all observability features
func WithEnabled(enabled bool) Option {
	return func(o *Options) {
		o.Config.EnableMetrics = enabled
		o.Config.EnableTracing = enabled
		o.Config.EnableDependencies = enabled
		o.Config.EnableAlerting = enabled
	}
}

// WithMetricsFlushInterval sets metrics flush interval
func WithMetricsFlushInterval(interval time.Duration) Option {
	return func(o *Options) {
		o.Config.MetricsFlushInterval = interval
	}
}

// WithOnAlert sets the alert callback
func WithOnAlert(callback func(ctx context.Context, alert *Alert)) Option {
	return func(o *Options) {
		o.OnAlert = callback
	}
}
