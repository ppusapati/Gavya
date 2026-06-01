// Package healthcheck provides continuous health checking for services.
//
// # Overview
//
// The healthcheck package monitors the health of services, instances, and
// dependencies through multiple check types:
//
//   - HTTP: GET requests to health endpoints
//   - TCP: Port connectivity testing
//   - gRPC: gRPC Health probe protocol
//   - Database: SQL connectivity checks
//   - Custom: User-defined health logic
//
// # Health States
//
// Each check produces a HealthStatus:
//
//   - HEALTHY: Service is functioning normally
//   - UNHEALTHY: Service has detected failures
//   - DEGRADED: Service is partially functional
//   - UNKNOWN: Check has not completed yet
//
// # Usage
//
//	coordinator := coordinator.New(pool, logger)
//	coordinator.RegisterCheck("payment-service", "http", &HTTPCheck{
//	    URL: "http://localhost:8080/health",
//	    Timeout: 5 * time.Second,
//	})
//
//	// Start background health checking
//	go coordinator.Start(ctx)
//
//	// Get health status
//	status, _ := coordinator.GetStatus(ctx, "payment-service")
//	fmt.Printf("Service status: %s\n", status.Status)
//
// # Check Types
//
// ## HTTP Check
//
// Sends GET request to health endpoint and expects 2xx response:
//
//	check := &HTTPCheck{
//	    URL:     "http://localhost:8080/health",
//	    Timeout: 5 * time.Second,
//	    SuccessCodes: []int{200},
//	}
//
// ## TCP Check
//
// Attempts to connect to TCP port:
//
//	check := &TCPCheck{
//	    Host:    "localhost",
//	    Port:    5432,
//	    Timeout: 3 * time.Second,
//	}
//
// ## gRPC Check
//
// Uses gRPC Health proto for checking:
//
//	check := &GRPCCheck{
//	    Host:    "localhost",
//	    Port:    50051,
//	    Service: "myservice.v1.MyService",
//	    Timeout: 5 * time.Second,
//	}
//
// ## Database Check
//
// Tests database connectivity and queries:
//
//	check := &DatabaseCheck{
//	    DSN:     "postgres://user:pass@localhost/mydb",
//	    Query:   "SELECT 1",
//	    Timeout: 10 * time.Second,
//	}
//
// ## Custom Check
//
// Implement custom health logic:
//
//	type CustomCheck struct{}
//	func (c *CustomCheck) Check(ctx context.Context) (*CheckResult, error) {
//	    // Your logic here
//	    return &CheckResult{
//	        Status: "HEALTHY",
//	        Message: "All good",
//	    }, nil
//	}
//
// # Failure Detection
//
// Unhealthy instances are automatically:
//   - Removed from service registry
//   - Excluded from load balancing
//   - Marked in circuit breaker
//   - Notified via event stream
//
// # Integration
//
// Integrates with:
//   - Service Registry (Phase 1): Update instance health
//   - Load Balancer (Phase 2): Skip unhealthy instances
//   - Circuit Breaker: Mark dependencies unhealthy
//   - Observability: Track health metrics
//
package healthcheck
