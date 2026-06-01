// Package circuitbreaker provides a generic circuit breaker pattern implementation
// for building resilient distributed systems.
package circuitbreaker

import "time"

// State represents the circuit breaker state.
type State string

const (
	// StateClosed is the normal state where requests are allowed.
	StateClosed State = "CLOSED"
	// StateOpen is the state where requests are blocked.
	StateOpen State = "OPEN"
	// StateHalfOpen is the testing state where limited requests are allowed.
	StateHalfOpen State = "HALF_OPEN"
)

// String returns the string representation of the state.
func (s State) String() string {
	return string(s)
}

// IsValid checks if the state is valid.
func (s State) IsValid() bool {
	switch s {
	case StateClosed, StateOpen, StateHalfOpen:
		return true
	default:
		return false
	}
}

// Config holds the configuration for a circuit breaker.
type Config struct {
	// Name is an identifier for the circuit breaker.
	Name string
	// FailureThreshold is the number of failures before opening the circuit.
	FailureThreshold int32
	// SuccessThreshold is the number of successes in half-open state to close the circuit.
	SuccessThreshold int32
	// RecoveryTimeout is the duration to wait before transitioning from open to half-open.
	RecoveryTimeout time.Duration
	// HalfOpenMaxRequests is the maximum number of requests allowed in half-open state.
	HalfOpenMaxRequests int32
}

// DefaultConfig returns a default circuit breaker configuration.
func DefaultConfig() Config {
	return Config{
		Name:                "default",
		FailureThreshold:    5,
		SuccessThreshold:    3,
		RecoveryTimeout:     30 * time.Second,
		HalfOpenMaxRequests: 3,
	}
}

// CircuitState holds the runtime state of a circuit breaker.
type CircuitState struct {
	// Config is the circuit breaker configuration.
	Config Config
	// State is the current state.
	State State
	// FailureCount is the current consecutive failure count.
	FailureCount int32
	// SuccessCount is the current consecutive success count (in half-open).
	SuccessCount int32
	// LastFailureAt is the timestamp of the last failure.
	LastFailureAt *time.Time
	// LastSuccessAt is the timestamp of the last success.
	LastSuccessAt *time.Time
	// OpenedAt is the timestamp when the circuit was opened.
	OpenedAt *time.Time
	// RecoveryAt is the timestamp when recovery should be attempted.
	RecoveryAt *time.Time
	// HalfOpenRequests is the number of requests made in half-open state.
	HalfOpenRequests int32
}

// NewCircuitState creates a new circuit state with the given configuration.
func NewCircuitState(cfg Config) *CircuitState {
	return &CircuitState{
		Config: cfg,
		State:  StateClosed,
	}
}

// IsAllowed checks if a request is allowed through the circuit.
func (cs *CircuitState) IsAllowed() bool {
	switch cs.State {
	case StateClosed:
		return true
	case StateOpen:
		return false
	case StateHalfOpen:
		return cs.HalfOpenRequests < cs.Config.HalfOpenMaxRequests
	default:
		return true
	}
}

// ShouldTransitionToHalfOpen checks if the circuit should transition to half-open.
func (cs *CircuitState) ShouldTransitionToHalfOpen() bool {
	if cs.State != StateOpen {
		return false
	}
	if cs.RecoveryAt == nil {
		return false
	}
	return time.Now().After(*cs.RecoveryAt)
}

// GetWaitTime returns the remaining time before recovery attempt.
func (cs *CircuitState) GetWaitTime() time.Duration {
	if cs.State != StateOpen || cs.RecoveryAt == nil {
		return 0
	}
	wait := time.Until(*cs.RecoveryAt)
	if wait < 0 {
		return 0
	}
	return wait
}

// CheckResult represents the result of a circuit breaker check.
type CheckResult struct {
	// Allowed indicates if the request is allowed.
	Allowed bool
	// State is the current circuit state.
	State State
	// FailureCount is the current failure count.
	FailureCount int32
	// SuccessCount is the current success count.
	SuccessCount int32
	// RecoveryAt is when recovery will be attempted.
	RecoveryAt *time.Time
	// WaitTime is the remaining wait time if blocked.
	WaitTime time.Duration
}
