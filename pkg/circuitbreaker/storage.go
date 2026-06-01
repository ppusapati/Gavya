package circuitbreaker

import (
	"context"
	"time"
)

// Storage defines the interface for circuit breaker state persistence.
// Implementations can use in-memory, Redis, PostgreSQL, or any other backend.
type Storage interface {
	// Get retrieves the circuit state for the given key.
	// Returns nil if no state exists.
	Get(ctx context.Context, key string) (*CircuitState, error)

	// Save persists the circuit state for the given key.
	Save(ctx context.Context, key string, state *CircuitState) error

	// Delete removes the circuit state for the given key.
	Delete(ctx context.Context, key string) error

	// GetAll returns all circuit states (for recovery processing).
	GetAll(ctx context.Context) (map[string]*CircuitState, error)

	// GetByState returns all circuit states with the given state.
	GetByState(ctx context.Context, state State) (map[string]*CircuitState, error)
}

// StateChangeEvent represents a circuit breaker state change event.
type StateChangeEvent struct {
	Key       string
	OldState  State
	NewState  State
	Timestamp time.Time
	Reason    string
}

// EventHandler is called when circuit breaker state changes.
type EventHandler func(event StateChangeEvent)

// Options configures the circuit breaker manager behavior.
type Options struct {
	// DefaultConfig is the default configuration for new circuits.
	DefaultConfig Config
	// CleanupInterval is how often stale entries are cleaned up.
	CleanupInterval time.Duration
	// MaxAge is the maximum age for cache entries.
	MaxAge time.Duration
	// OnStateChange is called when circuit state changes.
	OnStateChange EventHandler
}

// DefaultOptions returns default circuit breaker manager options.
func DefaultOptions() Options {
	return Options{
		DefaultConfig:   DefaultConfig(),
		CleanupInterval: 5 * time.Minute,
		MaxAge:          10 * time.Minute,
	}
}

// Option is a function that configures Options.
type Option func(*Options)

// WithDefaultConfig sets the default circuit breaker configuration.
func WithDefaultConfig(cfg Config) Option {
	return func(o *Options) {
		o.DefaultConfig = cfg
	}
}

// WithCleanupInterval sets the cleanup interval.
func WithCleanupInterval(interval time.Duration) Option {
	return func(o *Options) {
		o.CleanupInterval = interval
	}
}

// WithMaxAge sets the maximum cache entry age.
func WithMaxAge(age time.Duration) Option {
	return func(o *Options) {
		o.MaxAge = age
	}
}

// WithOnStateChange sets the state change event handler.
func WithOnStateChange(handler EventHandler) Option {
	return func(o *Options) {
		o.OnStateChange = handler
	}
}
