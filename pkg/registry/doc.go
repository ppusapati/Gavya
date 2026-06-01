// Package registry provides dynamic service discovery for 125+ microservices.
//
// # Core Concepts
//
// The registry maintains a catalog of all running service instances and their health status.
// Services register themselves on startup and deregister on shutdown. Other services query
// the registry to discover available instances for their dependencies.
//
// # Architecture
//
//   - ServiceRegistry: Main interface for all operations
//   - PostgreSQL Backend: Persistent storage with in-memory cache
//   - Cache Layer: L1 cache for O(1) lookups
//   - Health Checking: Continuous health verification
//   - Event Streaming: Watchers get real-time updates
//
// # Usage Example
//
//	// Initialize registry
//	pool := pgxpool.New(ctx, connString)
//	reg := postgres.NewPostgresRegistry(pool, logger)
//	cache := cache.NewCache(reg, logger)
//	registry := registry.New(cache, logger)
//
//	// Service startup: Register
//	err := registry.Register(ctx, &registry.ServiceInstance{
//		ID:          "service-1",
//		ServiceName: "user-service",
//		Host:        "192.168.1.10",
//		Port:        50051,
//		Version:     "v1.2.0",
//		Region:      "us-east-1",
//	})
//
//	// Service discovery: Get healthy instances
//	instances, err := registry.GetInstances(ctx, "user-service")
//
//	// Watch for changes
//	updates, err := registry.Watch(ctx, "user-service")
//	for event := range updates {
//		log.Printf("Service update: %v", event)
//	}
//
// # Database Schema
//
// The registry uses PostgreSQL with the following schema:
//
//	CREATE TABLE service_registry (
//		instance_id UUID PRIMARY KEY,
//		service_name VARCHAR(255) NOT NULL,
//		hostname VARCHAR(255) NOT NULL,
//		port INT NOT NULL,
//		health_status VARCHAR(50) DEFAULT 'UP',
//		metadata JSONB,
//		is_external BOOLEAN DEFAULT FALSE,
//		external_url TEXT,
//		version VARCHAR(100),
//		region VARCHAR(100),
//		zone VARCHAR(100),
//		last_heartbeat TIMESTAMP DEFAULT NOW(),
//		registered_at TIMESTAMP DEFAULT NOW(),
//		created_at TIMESTAMP DEFAULT NOW(),
//		updated_at TIMESTAMP DEFAULT NOW()
//	);
//
// # Health Status
//
// Instances can be in one of these health states:
//   - UP: Instance is healthy and receiving traffic
//   - DOWN: Instance is unhealthy and not receiving traffic
//   - WARNING: Instance is degraded but still receiving some traffic
//   - UNKNOWN: Health check not yet completed
//
// # Guarantees
//
//   - Strong consistency: All reads see the latest data
//   - High availability: In-memory cache reduces database load
//   - Automatic cleanup: Stale instances are removed after heartbeat TTL
//   - External service support: Non-local services can register with external URLs
package registry
