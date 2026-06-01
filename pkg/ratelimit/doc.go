// Package ratelimit provides rate limiting for 125+ microservices using multiple algorithms.
//
// # Algorithms
//
// The package supports multiple rate limiting algorithms:
//
//   - TokenBucket: Classic token bucket with refill rate
//   - BBR: Google's Bottleneck Bandwidth and RTT congestion control
//   - Adaptive: Dynamic limits based on system load
//   - Distributed: PostgreSQL-backed for multi-instance coordination
//
// # Token Bucket
//
// Simple and effective rate limiting:
//   - Capacity: Maximum tokens in bucket
//   - Refill rate: Tokens added per second
//   - Each request consumes 1 token
//   - Allows burst traffic up to capacity
//
// # BBR Algorithm
//
// Advanced congestion control based on:
//   - Bandwidth estimation (BDP - Bandwidth Delay Product)
//   - RTT (Round Trip Time) tracking
//   - Congestion window (CWND) management
//   - Four states: STARTUP, DRAIN, PROBE_BW, PROBE_RTT
//
// BBR is more efficient than token bucket for high-throughput, variable-latency workloads.
//
// # Usage Example
//
//	// Token bucket
//	limiter := algorithms.NewTokenBucketLimiter(100, 50.0) // 100 capacity, 50 req/sec
//	if limiter.Allow() {
//	    processRequest()
//	}
//
//	// BBR
//	bbrLimiter := algorithms.NewBBRLimiter()
//	allowed := bbrLimiter.Allow(requestDuration)
//	if allowed {
//	    processRequest()
//	}
//
//	// Distributed (multi-instance)
//	pool := pgxpool.New(ctx, connString)
//	distributed := backend.NewPostgresRateLimiter(pool, logger)
//
//	allowed, err := distributed.Allow(ctx, "service-name", "client-id")
//	if allowed {
//	    processRequest()
//	}
//
// # Per-Service vs Per-Client Limits
//
// The rate limiter supports multiple scopes:
//
//   - Global limit: All requests to a service
//   - Per-client limit: Per API key / user ID
//   - Per-endpoint limit: Different limits for different operations
//
// # Distributed Rate Limiting
//
// For multi-instance deployments:
//   - Shared state in PostgreSQL
//   - Advisory locks for coordination
//   - Fast local caching with sync-back
//   - Configurable sync interval (default 10 seconds)
//
// # Circuit Breaker Integration
//
// When rate limiter rejects requests:
//   - Record in metrics
//   - Update dependency tracking
//   - Consider circuit breaker state
//   - Return 429 Too Many Requests (HTTP) or RESOURCE_EXHAUSTED (gRPC)
package ratelimit
