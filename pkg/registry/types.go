package registry

import (
	"context"
	"time"
)

// HealthStatus represents the health state of a service instance
type HealthStatus string

const (
	HealthUp      HealthStatus = "UP"
	HealthDown    HealthStatus = "DOWN"
	HealthWarning HealthStatus = "WARNING"
	HealthUnknown HealthStatus = "UNKNOWN"
)

// ServiceInstance represents a single running service instance
type ServiceInstance struct {
	// Unique identifier for this instance
	ID string

	// Service name (e.g., "user-service", "payment-service")
	ServiceName string

	// Host/IP address of the instance
	Host string

	// Port number where service is listening
	Port int

	// Custom metadata (region, zone, version, etc.)
	Metadata map[string]string

	// Current health status
	Health HealthStatus

	// Service version
	Version string

	// Region (e.g., "us-east-1", "eu-west-1")
	Region string

	// Zone/Availability zone (e.g., "us-east-1a")
	Zone string

	// Whether this is an external service (partner API, 3rd-party service)
	IsExternal bool

	// External URL (only populated if IsExternal is true)
	ExternalURL string

	// Last heartbeat timestamp
	LastHeartbeat time.Time

	// When the instance was registered
	RegisteredAt time.Time

	// When the instance was created in the registry
	CreatedAt time.Time

	// When the instance was last updated
	UpdatedAt time.Time
}

// RegistryEvent represents a change in the service registry
type RegistryEvent struct {
	// Event type: "registered", "deregistered", "health_changed", "metadata_updated"
	Type string

	// The instance that changed
	Instance *ServiceInstance

	// When this event occurred
	Timestamp time.Time
}

// RegistryBackend defines the underlying storage interface
type RegistryBackend interface {
	// Register stores a service instance
	Register(ctx context.Context, instance *ServiceInstance) error

	// Deregister removes a service instance
	Deregister(ctx context.Context, instanceID string) error

	// GetInstance retrieves a specific instance by ID
	GetInstance(ctx context.Context, instanceID string) (*ServiceInstance, error)

	// GetInstances retrieves all healthy instances of a service
	GetInstances(ctx context.Context, serviceName string) ([]*ServiceInstance, error)

	// GetInstancesByHealth retrieves instances filtered by health status
	GetInstancesByHealth(ctx context.Context, serviceName string, health HealthStatus) ([]*ServiceInstance, error)

	// GetAllInstances retrieves all instances regardless of service
	GetAllInstances(ctx context.Context) ([]*ServiceInstance, error)

	// UpdateHealth updates the health status of an instance
	UpdateHealth(ctx context.Context, instanceID string, health HealthStatus) error

	// UpdateMetadata updates the metadata of an instance
	UpdateMetadata(ctx context.Context, instanceID string, metadata map[string]string) error

	// Heartbeat records a heartbeat for an instance
	Heartbeat(ctx context.Context, instanceID string) error

	// CleanupStale removes instances that haven't heartbeated within TTL
	CleanupStale(ctx context.Context, ttl time.Duration) (int, error)

	// Close closes any connections
	Close() error
}

// ServiceRegistry is the main interface for service discovery operations
type ServiceRegistry interface {
	// Register registers a new service instance
	Register(ctx context.Context, instance *ServiceInstance) error

	// Deregister deregisters a service instance
	Deregister(ctx context.Context, instanceID string) error

	// GetInstance gets a specific instance by ID
	GetInstance(ctx context.Context, instanceID string) (*ServiceInstance, error)

	// GetInstances gets all healthy instances of a service
	GetInstances(ctx context.Context, serviceName string) ([]*ServiceInstance, error)

	// GetInstancesByHealth gets instances filtered by health status
	GetInstancesByHealth(ctx context.Context, serviceName string, health HealthStatus) ([]*ServiceInstance, error)

	// GetAllInstances gets all registered instances across all services
	GetAllInstances(ctx context.Context) ([]*ServiceInstance, error)

	// UpdateHealth updates an instance's health status
	UpdateHealth(ctx context.Context, instanceID string, health HealthStatus) error

	// UpdateMetadata updates an instance's metadata
	UpdateMetadata(ctx context.Context, instanceID string, metadata map[string]string) error

	// Heartbeat records a heartbeat from an instance (keeps it alive)
	Heartbeat(ctx context.Context, instanceID string) error

	// Watch watches for changes to a service
	// Returns a channel that receives events when instances change
	Watch(ctx context.Context, serviceName string) (<-chan RegistryEvent, error)

	// Close closes the registry and releases resources
	Close() error
}

// Options for registry configuration
type Options struct {
	// TTL for inactive instances (default 60 seconds)
	HeartbeatTTL time.Duration

	// Cleanup interval for stale instances (default 30 seconds)
	CleanupInterval time.Duration

	// Cache TTL (default 5 seconds)
	CacheTTL time.Duration

	// Maximum number of watchers per service (default 100)
	MaxWatchers int

	// Enable automatic cleanup of stale instances (default true)
	EnableAutoCleanup bool
}

// DefaultOptions returns sensible defaults
func DefaultOptions() Options {
	return Options{
		HeartbeatTTL:     60 * time.Second,
		CleanupInterval:  30 * time.Second,
		CacheTTL:         5 * time.Second,
		MaxWatchers:      100,
		EnableAutoCleanup: true,
	}
}
