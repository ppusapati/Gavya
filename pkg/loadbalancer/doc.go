// Package loadbalancer provides intelligent endpoint selection for service-to-service calls.
//
// # Algorithms
//
// The package supports multiple load balancing algorithms:
//
//   - RoundRobin: Cycle through endpoints sequentially
//   - LeastConnections: Select endpoint with fewest active connections
//   - WeightedRoundRobin: Cycle with weights for canary deployments
//   - LatencyAware: Select endpoint with lowest latency
//   - Random: Select random endpoint
//
// # Usage Example
//
//	// Create a load balancer
//	lb := algorithms.NewRoundRobinBalancer()
//
//	// Get instances from registry
//	instances, _ := registry.GetInstances(ctx, "payment-service")
//
//	// Select an endpoint
//	endpoint, _ := lb.Select(ctx, instances)
//
//	// Make request to selected endpoint
//	callService(endpoint.Host, endpoint.Port)
//
//	// Record metrics for latency-aware selection
//	lb.RecordMetrics(endpoint.InstanceID, latency, success)
//
// # Connection Awareness
//
// The load balancer can track active connections per endpoint for least-connections selection.
// This is useful for long-lived connections or streaming scenarios.
//
// # Metrics Integration
//
// The load balancer can record metrics for each endpoint:
//   - Latency (min, max, average)
//   - Success/failure rates
//   - Connection counts
//
// These metrics inform latency-aware and connection-aware selection.
//
// # Stateless Design
//
// All algorithms are deterministic and stateless (where possible) to support:
//   - Multiple load balancer instances
//   - Consistent selection across restarts
//   - Horizontal scaling without session affinity
package loadbalancer
