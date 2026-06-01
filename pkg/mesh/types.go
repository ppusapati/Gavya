package mesh

import (
	"time"

	"p9e.in/samavaya/packages/loadbalancer"
)

// RoutingPolicy defines how to route requests to a service
type RoutingPolicy struct {
	// Service name this policy applies to
	ServiceName string

	// Version constraint (optional, e.g., "v1.2.x")
	VersionConstraint string

	// Region preference (optional, route to specific region)
	Region string

	// Load balancing algorithm
	LoadBalancingAlgorithm loadbalancer.Algorithm

	// Circuit breaker configuration
	CircuitBreakerConfig CircuitBreakerConfig

	// Retry policy configuration
	RetryPolicy RetryPolicy

	// Timeout policy configuration
	TimeoutPolicy TimeoutPolicy

	// Canary deployment configuration
	CanaryConfig *CanaryConfig

	// Custom metadata
	Metadata map[string]string
}

// CircuitBreakerConfig defines circuit breaker behavior
type CircuitBreakerConfig struct {
	// Number of consecutive failures before opening circuit
	FailureThreshold int

	// Number of consecutive successes before closing circuit
	SuccessThreshold int

	// How long to wait before testing recovery (moving to HALF_OPEN)
	RecoveryTimeout time.Duration

	// Maximum requests in HALF_OPEN state
	HalfOpenMaxRequests int
}

// RetryPolicy defines retry behavior
type RetryPolicy struct {
	// Maximum number of attempts (1 = no retries)
	MaxAttempts int

	// Initial backoff duration
	InitialBackoff time.Duration

	// Maximum backoff duration
	MaxBackoff time.Duration

	// Backoff multiplier (for exponential backoff)
	BackoffMultiplier float64

	// Which errors are retryable (by name, e.g., "UNAVAILABLE", "DEADLINE_EXCEEDED")
	RetryableErrors []string
}

// TimeoutPolicy defines timeout behavior
type TimeoutPolicy struct {
	// TCP connection timeout
	ConnectTimeout time.Duration

	// Request timeout (from connection established to response received)
	RequestTimeout time.Duration

	// Idle connection timeout
	IdleTimeout time.Duration

	// Maximum request body size
	MaxRequestSize int64
}

// CanaryConfig defines canary deployment behavior
type CanaryConfig struct {
	// New version to gradually roll out
	NewVersion string

	// Percentage of traffic to route to new version (0-100)
	TrafficPercentage int

	// Minimum number of requests before evaluating canary success
	MinRequests int

	// Error rate threshold (if exceeded, rollback)
	ErrorRateThreshold float64

	// Latency threshold (if exceeded, rollback)
	LatencyThresholdMs int64
}

// DefaultCircuitBreakerConfig returns sensible defaults
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold:    5,
		SuccessThreshold:    2,
		RecoveryTimeout:     30 * time.Second,
		HalfOpenMaxRequests: 5,
	}
}

// DefaultRetryPolicy returns sensible defaults
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:       3,
		InitialBackoff:    100 * time.Millisecond,
		MaxBackoff:        10 * time.Second,
		BackoffMultiplier: 2.0,
		RetryableErrors:   []string{"UNAVAILABLE", "DEADLINE_EXCEEDED", "INTERNAL"},
	}
}

// DefaultTimeoutPolicy returns sensible defaults
func DefaultTimeoutPolicy() TimeoutPolicy {
	return TimeoutPolicy{
		ConnectTimeout:  2 * time.Second,
		RequestTimeout:  5 * time.Second,
		IdleTimeout:     30 * time.Second,
		MaxRequestSize:  10 * 1024 * 1024, // 10MB
	}
}

// RequestMetadata contains metadata about a request
type RequestMetadata struct {
	// Unique request ID (for tracing)
	RequestID string

	// Service being called
	ServiceName string

	// Method being called
	Method string

	// Custom headers/context
	Headers map[string]string
}

// ResponseMetadata contains metadata about a response
type ResponseMetadata struct {
	// Selected endpoint that served the request
	Endpoint *loadbalancer.Endpoint

	// HTTP/gRPC status code
	StatusCode int

	// Request duration
	Duration time.Duration

	// Whether request was retried
	Retried bool

	// Number of retries before success
	RetryCount int

	// Circuit breaker state at time of request
	CircuitBreakerState string
}

// RequestError represents an error from a request
type RequestError struct {
	// Error code (HTTP status, gRPC code, etc.)
	Code string

	// Error message
	Message string

	// Whether this error is retryable
	Retryable bool

	// Which instance failed
	FailedEndpoint *loadbalancer.Endpoint
}

// Options for mesh configuration
type Options struct {
	// Default routing policy
	DefaultPolicy *RoutingPolicy

	// How often to sync policies from database (optional)
	PolicySyncInterval time.Duration

	// Enable automatic policy caching
	EnablePolicyCache bool

	// Cache TTL for policies
	PolicyCacheTTL time.Duration
}

// DefaultOptions returns sensible defaults
func DefaultOptions() Options {
	defaultPolicy := &RoutingPolicy{
		LoadBalancingAlgorithm: loadbalancer.AlgorithmRoundRobin,
		CircuitBreakerConfig:   DefaultCircuitBreakerConfig(),
		RetryPolicy:            DefaultRetryPolicy(),
		TimeoutPolicy:          DefaultTimeoutPolicy(),
	}

	return Options{
		DefaultPolicy:      defaultPolicy,
		PolicySyncInterval: 30 * time.Second,
		EnablePolicyCache:  true,
		PolicyCacheTTL:     5 * time.Second,
	}
}

// Option is a functional option for configuring mesh
type Option func(*Options)

// WithDefaultPolicy sets the default routing policy
func WithDefaultPolicy(policy *RoutingPolicy) Option {
	return func(o *Options) {
		o.DefaultPolicy = policy
	}
}

// WithPolicySyncInterval sets the policy sync interval
func WithPolicySyncInterval(interval time.Duration) Option {
	return func(o *Options) {
		o.PolicySyncInterval = interval
	}
}

// WithPolicyCache enables policy caching
func WithPolicyCache(enabled bool) Option {
	return func(o *Options) {
		o.EnablePolicyCache = enabled
	}
}
