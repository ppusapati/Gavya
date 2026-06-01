package loadbalancer

import (
	"context"
	"time"

	"p9e.in/samavaya/packages/registry"
)

// Algorithm represents a load balancing algorithm type
type Algorithm string

const (
	AlgorithmRoundRobin        Algorithm = "round_robin"
	AlgorithmLeastConnections  Algorithm = "least_connections"
	AlgorithmWeightedRoundRobin Algorithm = "weighted_round_robin"
	AlgorithmLatencyAware      Algorithm = "latency_aware"
	AlgorithmRandom            Algorithm = "random"
)

// Endpoint represents a selectable service endpoint
type Endpoint struct {
	// Reference to the service instance
	Instance *registry.ServiceInstance

	// Current number of active connections (for least-conn algorithm)
	ActiveConnections int64

	// Metrics for this endpoint
	Metrics *EndpointMetrics

	// Weight for weighted algorithms (0-100, default 100)
	Weight int
}

// EndpointMetrics tracks metrics for an endpoint
type EndpointMetrics struct {
	// Request statistics
	SuccessCount    int64
	FailureCount    int64
	TotalRequests   int64

	// Latency statistics (in nanoseconds)
	MinLatency      time.Duration
	MaxLatency      time.Duration
	AvgLatency      time.Duration
	P50Latency      time.Duration
	P95Latency      time.Duration
	P99Latency      time.Duration

	// Error rate (0.0 - 1.0)
	ErrorRate       float64

	// Last update time
	LastUpdate      time.Time
}

// SelectionResult contains information about the selection
type SelectionResult struct {
	// Selected endpoint
	Endpoint *Endpoint

	// Selection algorithm used
	Algorithm Algorithm

	// Whether this was a fallback selection (all endpoints unhealthy)
	Fallback bool

	// Selection reason (for debugging)
	Reason string
}

// LoadBalancer defines the interface for load balancing algorithms
type LoadBalancer interface {
	// Select returns the next endpoint from the list
	// Endpoints should be pre-filtered by the registry (healthy only)
	Select(ctx context.Context, endpoints []*registry.ServiceInstance) (*Endpoint, error)

	// RecordMetrics records metrics for an endpoint
	// This is used by latency-aware and connection-aware algorithms
	RecordMetrics(instanceID string, latency time.Duration, success bool)

	// IncrementConnections increments the active connection count for an endpoint
	IncrementConnections(instanceID string)

	// DecrementConnections decrements the active connection count for an endpoint
	DecrementConnections(instanceID string)

	// Reset clears internal state (useful for testing)
	Reset()

	// GetMetrics returns current metrics for an endpoint
	GetMetrics(instanceID string) *EndpointMetrics
}

// Options for load balancer configuration
type Options struct {
	// Algorithm to use
	Algorithm Algorithm

	// Weights for weighted algorithms (instance_id -> weight)
	Weights map[string]int

	// Maximum samples to keep for latency calculation (default 1000)
	MaxLatencySamples int

	// Fallback behavior when all endpoints are unhealthy
	// If true, still return an endpoint (least healthy one)
	// If false, return error
	AllowUnhealthyFallback bool

	// Enable connection tracking
	TrackConnections bool
}

// DefaultOptions returns sensible defaults
func DefaultOptions() Options {
	return Options{
		Algorithm:          AlgorithmRoundRobin,
		MaxLatencySamples:  1000,
		AllowUnhealthyFallback: true,
		TrackConnections:   true,
	}
}

// Option is a functional option for configuring load balancer
type Option func(*Options)

// WithAlgorithm sets the load balancing algorithm
func WithAlgorithm(algo Algorithm) Option {
	return func(o *Options) {
		o.Algorithm = algo
	}
}

// WithWeights sets the weights for weighted algorithms
func WithWeights(weights map[string]int) Option {
	return func(o *Options) {
		o.Weights = weights
	}
}

// WithMaxLatencySamples sets the maximum number of latency samples to keep
func WithMaxLatencySamples(max int) Option {
	return func(o *Options) {
		o.MaxLatencySamples = max
	}
}

// WithAllowUnhealthyFallback enables fallback to unhealthy endpoints
func WithAllowUnhealthyFallback(allow bool) Option {
	return func(o *Options) {
		o.AllowUnhealthyFallback = allow
	}
}

// WithTrackConnections enables connection tracking
func WithTrackConnections(track bool) Option {
	return func(o *Options) {
		o.TrackConnections = track
	}
}
