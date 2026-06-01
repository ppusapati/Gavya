package mesh

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"p9e.in/samavaya/packages/p9log"
	"p9e.in/samavaya/packages/circuitbreaker"
	"p9e.in/samavaya/packages/loadbalancer"
	"p9e.in/samavaya/packages/registry"
)

// ServiceMesh provides service-to-service routing with policies, retries, and circuit breaking.
//
// API drift note (B.1 sweep, 2026-04-19): the underlying
// `circuitbreaker.SimpleCircuitBreaker` was refactored to an
// `Execute(ctx, fn)` callback-style API; it no longer exposes per-key
// `Check / RecordFailure / RecordSuccess`. ServiceMesh now inspects state
// via `breaker.GetState()` during Route and lets the breaker track
// transitions itself — callers that wrap their call through
// `Execute(ctx, ...)` still get full protection. The old Record* methods
// on ServiceMesh now update only the load-balancer metrics (which are
// per-endpoint); the breaker's failure-count bookkeeping flows from
// Execute wrappers upstream instead. `logger` stored as *p9log.Helper.
type ServiceMesh struct {
	registry       registry.ServiceRegistry
	lb             loadbalancer.LoadBalancer
	breaker        *circuitbreaker.SimpleCircuitBreaker
	logger         *p9log.Helper
	opts           Options
	policies       map[string]*RoutingPolicy
	policiesMu     sync.RWMutex
	policyCache    map[string]*RoutingPolicy
	policyCacheMu  sync.RWMutex

	stopChan chan struct{}
	wg       sync.WaitGroup
}

// New creates a new service mesh
func New(
	reg registry.ServiceRegistry,
	lb loadbalancer.LoadBalancer,
	breaker *circuitbreaker.SimpleCircuitBreaker,
	logger p9log.Logger,
	opts ...Option,
) *ServiceMesh {
	options := DefaultOptions()
	for _, opt := range opts {
		opt(&options)
	}

	m := &ServiceMesh{
		registry:    reg,
		lb:          lb,
		breaker:     breaker,
		logger:      p9log.NewHelper(logger),
		opts:        options,
		policies:    make(map[string]*RoutingPolicy),
		policyCache: make(map[string]*RoutingPolicy),
		stopChan:    make(chan struct{}),
	}

	return m
}

// Route selects an endpoint for the given service and applies all policies
func (m *ServiceMesh) Route(ctx context.Context, serviceName string) (*loadbalancer.Endpoint, error) {
	if serviceName == "" {
		return nil, fmt.Errorf("service name is required")
	}

	// Get routing policy
	policy, err := m.GetPolicy(ctx, serviceName)
	if err != nil {
		m.logger.Warn("failed to get policy, using default",
			"service_name", serviceName,
			"error", err,
		)
		policy = m.opts.DefaultPolicy
	}

	// Check circuit breaker state. SimpleCircuitBreaker's Execute(ctx, fn)
	// is the canonical path; here we only need the "is it open?" read —
	// the caller wraps the actual outbound call with Execute elsewhere.
	if m.breaker != nil && m.breaker.GetState() == circuitbreaker.SimpleStateOpen {
		m.logger.Warn("circuit breaker open",
			"service_name", serviceName,
		)
		return nil, fmt.Errorf("circuit breaker open for service %s", serviceName)
	}

	// Get instances from registry. Failure here doesn't directly toggle
	// breaker state — the outbound wrapper's Execute path does that when
	// the downstream call eventually fails.
	instances, err := m.registry.GetInstances(ctx, serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to get service instances: %w", err)
	}
	if len(instances) == 0 {
		return nil, fmt.Errorf("no instances available for service: %s", serviceName)
	}

	// Filter instances by policy constraints
	filtered := m.filterInstances(instances, policy)
	if len(filtered) == 0 {
		// Fallback to unfiltered if no instances match filters
		filtered = instances
	}

	// Select endpoint using load balancer
	endpoint, err := m.lb.Select(ctx, filtered)
	if err != nil {
		return nil, fmt.Errorf("load balancer selection failed: %w", err)
	}

	return endpoint, nil
}

// RecordSuccess records a successful request. Only updates load-balancer
// metrics; breaker state is managed by SimpleCircuitBreaker.Execute at the
// call site. Kept as a compatibility shim for existing callers.
func (m *ServiceMesh) RecordSuccess(ctx context.Context, serviceName string, endpoint *loadbalancer.Endpoint, duration time.Duration) {
	_ = ctx
	m.lb.RecordMetrics(endpoint.Instance.ID, duration, true)

	m.logger.Debug("request successful",
		"service_name", serviceName,
		"instance_id", endpoint.Instance.ID,
		"duration_ms", duration.Milliseconds(),
	)
}

// RecordFailure records a failed request. Same contract change as
// RecordSuccess — breaker bookkeeping happens inside Execute at the call site.
func (m *ServiceMesh) RecordFailure(ctx context.Context, serviceName string, endpoint *loadbalancer.Endpoint, duration time.Duration) {
	_ = ctx
	m.lb.RecordMetrics(endpoint.Instance.ID, duration, false)

	m.logger.Debug("request failed",
		"service_name", serviceName,
		"instance_id", endpoint.Instance.ID,
	)
}

// GetPolicy gets the routing policy for a service
func (m *ServiceMesh) GetPolicy(ctx context.Context, serviceName string) (*RoutingPolicy, error) {
	// Check cache first
	if m.opts.EnablePolicyCache {
		m.policyCacheMu.RLock()
		if policy, ok := m.policyCache[serviceName]; ok {
			m.policyCacheMu.RUnlock()
			return policy, nil
		}
		m.policyCacheMu.RUnlock()
	}

	// Get from policies map
	m.policiesMu.RLock()
	policy := m.policies[serviceName]
	m.policiesMu.RUnlock()

	// Use default if not found
	if policy == nil {
		policy = m.opts.DefaultPolicy
		if policy.ServiceName == "" {
			policy.ServiceName = serviceName
		}
	}

	// Cache it
	if m.opts.EnablePolicyCache {
		m.policyCacheMu.Lock()
		m.policyCache[serviceName] = policy
		m.policyCacheMu.Unlock()
	}

	return policy, nil
}

// SetPolicy sets the routing policy for a service
func (m *ServiceMesh) SetPolicy(ctx context.Context, policy *RoutingPolicy) error {
	if policy.ServiceName == "" {
		return fmt.Errorf("service name is required in policy")
	}

	m.policiesMu.Lock()
	m.policies[policy.ServiceName] = policy
	m.policiesMu.Unlock()

	// Invalidate cache
	m.policyCacheMu.Lock()
	delete(m.policyCache, policy.ServiceName)
	m.policyCacheMu.Unlock()

	m.logger.Info("policy updated",
		"service_name", policy.ServiceName,
		"algorithm", policy.LoadBalancingAlgorithm,
	)

	return nil
}

// Close closes the mesh and releases resources
func (m *ServiceMesh) Close() error {
	close(m.stopChan)
	m.wg.Wait()
	return nil
}

// Helper methods

// filterInstances filters instances based on policy constraints
func (m *ServiceMesh) filterInstances(instances []*registry.ServiceInstance, policy *RoutingPolicy) []*registry.ServiceInstance {
	var filtered []*registry.ServiceInstance

	for _, inst := range instances {
		// Check version constraint
		if policy.VersionConstraint != "" && inst.Version != policy.VersionConstraint {
			continue
		}

		// Check region preference
		if policy.Region != "" && inst.Region != policy.Region {
			continue
		}

		filtered = append(filtered, inst)
	}

	return filtered
}

// calculateBackoff calculates exponential backoff with jitter
func calculateBackoff(attempt int, policy RetryPolicy) time.Duration {
	// exponential backoff: initial * multiplier ^ (attempt - 1)
	backoff := float64(policy.InitialBackoff)
	for i := 0; i < attempt-1; i++ {
		backoff = backoff * policy.BackoffMultiplier
		if backoff > float64(policy.MaxBackoff) {
			backoff = float64(policy.MaxBackoff)
			break
		}
	}

	// Add jitter (random between 0 and backoff). math/rand/v2 is goroutine-safe
	// without a seed step (Go 1.22+), so there's no global lock contention.
	jitter := time.Duration(rand.Float64() * backoff)
	return time.Duration(backoff) + jitter
}

// isRetryable checks if an error should be retried
func isRetryable(errorCode string, policy RetryPolicy) bool {
	for _, retryableCode := range policy.RetryableErrors {
		if retryableCode == errorCode {
			return true
		}
	}
	return false
}
