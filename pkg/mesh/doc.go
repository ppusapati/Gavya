// Package mesh provides service-to-service communication with policies, routing, retries, and circuit breaking.
//
// # Core Concepts
//
// The mesh layer sits between services and handles:
//   - Request routing based on policies (load balancing algorithm, region/zone preference)
//   - Automatic retries with exponential backoff
//   - Circuit breaking for cascading failure prevention
//   - Timeout enforcement
//   - Request/response interception for observability
//
// # Architecture
//
//   - ServiceMesh: Main interface for routing decisions
//   - RoutingPolicy: Defines how to route requests to a service
//   - RetryPolicy: Defines retry behavior
//   - TimeoutPolicy: Defines timeout behavior
//   - LoadBalancer: Selects specific endpoint
//   - CircuitBreaker: Prevents cascading failures
//
// # Usage Example
//
//	// Initialize mesh
//	registry := registry.New(backend, logger)
//	lb := algorithms.NewRoundRobinBalancer()
//	mesh := mesh.New(registry, lb, circuitbreaker, logger)
//
//	// Get endpoint for a service (includes all policies)
//	endpoint, err := mesh.Route(ctx, "payment-service")
//	if err != nil {
//		// Fallback or error handling
//	}
//
//	// Make request to endpoint
//	call := endpoint.CallService()
//	mesh.RecordMetrics(endpoint, latency, success)
//
// # Routing Policies
//
// Each service can have a routing policy that defines:
//   - Load balancing algorithm (round-robin, least-connections, latency-aware)
//   - Circuit breaker thresholds
//   - Retry strategy
//   - Request timeouts
//   - Canary deployment settings (traffic split)
//
// # Circuit Breaker States
//
//   - CLOSED: Normal operation, requests allowed
//   - OPEN: Requests blocked, service is unhealthy
//   - HALF_OPEN: Testing recovery, limited requests allowed
//
// When a circuit is OPEN, the mesh:
//   - Fast-fails requests (return error immediately)
//   - Reduces load on failing service
//   - Allows service time to recover
//   - Tests recovery with HALF_OPEN state
//
// # Retry Logic
//
// The mesh automatically retries on transient failures:
//   - Retries failed requests on different instances
//   - Uses exponential backoff to spread retries
//   - Respects max retry count
//   - Retries only on retryable errors (UNAVAILABLE, DEADLINE_EXCEEDED)
//
// # Timeout Policy
//
// Each service has timeout settings:
//   - Connect timeout (how long to wait for TCP connection)
//   - Request timeout (how long to wait for response)
//   - Idle timeout (how long to keep connections open)
package mesh
