package healthcheck

import (
	"context"
	"time"
)

// HealthStatus represents the current health state of a service
type HealthStatus string

const (
	StatusHealthy   HealthStatus = "HEALTHY"
	StatusUnhealthy HealthStatus = "UNHEALTHY"
	StatusDegraded  HealthStatus = "DEGRADED"
	StatusUnknown   HealthStatus = "UNKNOWN"
)

// CheckType represents the type of health check
type CheckType string

const (
	CheckTypeHTTP     CheckType = "http"
	CheckTypeGRPC     CheckType = "grpc"
	CheckTypeTCP      CheckType = "tcp"
	CheckTypeDatabase CheckType = "database"
	CheckTypeCustom   CheckType = "custom"
)

// Checker is the interface that all health checks must implement
type Checker interface {
	// Check performs the health check
	// Returns CheckResult with status and details
	Check(ctx context.Context) (*CheckResult, error)

	// Type returns the check type
	Type() CheckType

	// Name returns a human-readable name for this check
	Name() string
}

// CheckResult contains the result of a health check
type CheckResult struct {
	// Current health status
	Status HealthStatus

	// Human-readable message
	Message string

	// Check duration
	Duration time.Duration

	// Timestamp of check
	CheckedAt time.Time

	// Sequential number of this check
	Sequence int64

	// Additional details
	Details map[string]interface{}

	// Error from check (if any)
	Error string
}

// InstanceHealth tracks health of a single service instance
type InstanceHealth struct {
	// Instance identifier
	InstanceID string

	// Service name
	ServiceName string

	// Host and port
	Host string
	Port int

	// Current health status
	Status HealthStatus

	// Last successful check
	LastSuccessfulCheck time.Time

	// Last failed check
	LastFailedCheck time.Time

	// Consecutive failures
	FailureCount int

	// Consecutive successes
	SuccessCount int

	// Last error message
	LastError string

	// Details from last check
	Details map[string]interface{}

	// Timestamp
	UpdatedAt time.Time
}

// ServiceHealth tracks health of an entire service
type ServiceHealth struct {
	// Service name
	ServiceName string

	// Overall health status
	Status HealthStatus

	// Healthy instance count
	HealthyInstances int

	// Unhealthy instance count
	UnhealthyInstances int

	// Total instances
	TotalInstances int

	// Per-instance health
	Instances map[string]*InstanceHealth

	// Health percentage (0-100)
	HealthPercent int

	// Updated at
	UpdatedAt time.Time
}

// HealthCheckConfig defines configuration for health checking
type HealthCheckConfig struct {
	// Check interval (how often to run check)
	Interval time.Duration

	// Check timeout
	Timeout time.Duration

	// Consecutive failures before marking unhealthy
	UnhealthyThreshold int

	// Consecutive successes before marking healthy
	HealthyThreshold int

	// Jitter added to interval (prevent thundering herd)
	IntervalJitter time.Duration

	// Custom metadata
	Metadata map[string]string
}

// DefaultHealthCheckConfig returns sensible defaults
func DefaultHealthCheckConfig() HealthCheckConfig {
	return HealthCheckConfig{
		Interval:            10 * time.Second,
		Timeout:             5 * time.Second,
		UnhealthyThreshold:  3,
		HealthyThreshold:    2,
		IntervalJitter:      1 * time.Second,
	}
}

// Options for health checker configuration
type Options struct {
	// Default configuration
	Config HealthCheckConfig

	// Enable/disable health checking
	Enabled bool

	// Callback when status changes
	OnStatusChange func(ctx context.Context, service string, instance string, oldStatus, newStatus HealthStatus)

	// Callback on check failure (for logging/alerting)
	OnCheckFailure func(ctx context.Context, service string, instance string, err error)
}

// DefaultOptions returns sensible defaults
func DefaultOptions() Options {
	return Options{
		Config:  DefaultHealthCheckConfig(),
		Enabled: true,
	}
}

// Option is a functional option for configuring health checker
type Option func(*Options)

// WithInterval sets the check interval
func WithInterval(interval time.Duration) Option {
	return func(o *Options) {
		o.Config.Interval = interval
	}
}

// WithTimeout sets the check timeout
func WithTimeout(timeout time.Duration) Option {
	return func(o *Options) {
		o.Config.Timeout = timeout
	}
}

// WithUnhealthyThreshold sets the unhealthy threshold
func WithUnhealthyThreshold(threshold int) Option {
	return func(o *Options) {
		o.Config.UnhealthyThreshold = threshold
	}
}

// WithEnabled enables or disables health checking
func WithEnabled(enabled bool) Option {
	return func(o *Options) {
		o.Enabled = enabled
	}
}

// WithOnStatusChange sets the status change callback
func WithOnStatusChange(callback func(ctx context.Context, service string, instance string, oldStatus, newStatus HealthStatus)) Option {
	return func(o *Options) {
		o.OnStatusChange = callback
	}
}

// HTTPCheckConfig configuration for HTTP health checks
type HTTPCheckConfig struct {
	URL          string
	Method       string
	Headers      map[string]string
	SuccessCodes []int
	Timeout      time.Duration
}

// TCPCheckConfig configuration for TCP health checks
type TCPCheckConfig struct {
	Host    string
	Port    int
	Timeout time.Duration
}

// GRPCCheckConfig configuration for gRPC health checks
type GRPCCheckConfig struct {
	Host    string
	Port    int
	Service string
	Timeout time.Duration
}

// DatabaseCheckConfig configuration for database health checks
type DatabaseCheckConfig struct {
	DSN     string
	Query   string
	Timeout time.Duration
}

// Event represents a health check event
type Event struct {
	// Event type
	Type string // "check_started", "check_passed", "check_failed", "status_changed"

	// Service name
	ServiceName string

	// Instance ID
	InstanceID string

	// Check type
	CheckType CheckType

	// Result
	Result *CheckResult

	// Timestamp
	Timestamp time.Time
}

// Summary contains overall health check summary
type Summary struct {
	// Total services
	TotalServices int

	// Healthy services
	HealthyServices int

	// Unhealthy services
	UnhealthyServices int

	// Total instances
	TotalInstances int

	// Healthy instances
	HealthyInstances int

	// Unhealthy instances
	UnhealthyInstances int

	// Health percentage (0-100)
	HealthPercent int

	// Generated at
	GeneratedAt time.Time
}
