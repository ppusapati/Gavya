package circuitbreaker

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// SimpleState represents the state of the simple circuit breaker
type SimpleState int

const (
	SimpleStateClosed SimpleState = iota
	SimpleStateOpen
	SimpleStateHalfOpen
)

// SimpleConfig holds simple circuit breaker configuration
type SimpleConfig struct {
	MaxFailures      int32             // Number of failures before opening
	SuccessThreshold int32             // Number of successes to close from half-open
	Timeout          time.Duration     // Time to wait before attempting to recover
	FailureFunc      func(error) bool  // Function to determine if error should count as failure
}

// SimpleCircuitBreaker implements a simple in-memory circuit breaker pattern
type SimpleCircuitBreaker struct {
	config          SimpleConfig
	state           SimpleState
	failures        int32
	successes       int32
	lastFailureTime time.Time
	mutex           sync.RWMutex
}

// NewSimpleCircuitBreaker creates a new simple circuit breaker
func NewSimpleCircuitBreaker(config SimpleConfig) *SimpleCircuitBreaker {
	if config.MaxFailures == 0 {
		config.MaxFailures = 5
	}
	if config.SuccessThreshold == 0 {
		config.SuccessThreshold = 2
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}
	if config.FailureFunc == nil {
		config.FailureFunc = func(err error) bool {
			return err != nil
		}
	}

	return &SimpleCircuitBreaker{
		config: config,
		state:  SimpleStateClosed,
	}
}

// NewCircuitBreaker creates a new simple circuit breaker (alias for backward compat)
func NewCircuitBreaker(config SimpleConfig) *SimpleCircuitBreaker {
	return NewSimpleCircuitBreaker(config)
}

// Execute executes a function through the simple circuit breaker
func (cb *SimpleCircuitBreaker) Execute(ctx context.Context, fn func(context.Context) error) error {
	cb.mutex.Lock()
	state := cb.state
	cb.mutex.Unlock()

	// Check if we should attempt recovery
	if state == SimpleStateOpen {
		if time.Since(cb.lastFailureTime) > cb.config.Timeout {
			cb.mutex.Lock()
			cb.state = SimpleStateHalfOpen
			cb.successes = 0
			cb.mutex.Unlock()
			state = SimpleStateHalfOpen
		} else {
			return fmt.Errorf("circuit breaker is open")
		}
	}

	// Execute the function
	err := fn(ctx)

	if cb.config.FailureFunc(err) {
		// Handle failure
		cb.mutex.Lock()
		cb.failures++
		cb.lastFailureTime = time.Now()

		if cb.state == SimpleStateClosed && cb.failures >= cb.config.MaxFailures {
			cb.state = SimpleStateOpen
		} else if cb.state == SimpleStateHalfOpen {
			cb.state = SimpleStateOpen
		}
		cb.mutex.Unlock()
		return err
	}

	// Handle success
	cb.mutex.Lock()
	if cb.state == SimpleStateHalfOpen {
		cb.successes++
		if cb.successes >= cb.config.SuccessThreshold {
			cb.state = SimpleStateClosed
			cb.failures = 0
			cb.successes = 0
		}
	} else if cb.state == SimpleStateClosed {
		cb.failures = 0
	}
	cb.mutex.Unlock()

	return nil
}

// GetState returns the current state of the circuit breaker
func (cb *SimpleCircuitBreaker) GetState() SimpleState {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state
}

// GetMetrics returns current metrics
func (cb *SimpleCircuitBreaker) GetMetrics() (state SimpleState, failures, successes int32) {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state, cb.failures, cb.successes
}

// Reset resets the circuit breaker
func (cb *SimpleCircuitBreaker) Reset() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()
	cb.state = SimpleStateClosed
	cb.failures = 0
	cb.successes = 0
}

// SimpleStateString returns string representation of simple state
func SimpleStateString(state SimpleState) string {
	switch state {
	case SimpleStateClosed:
		return "CLOSED"
	case SimpleStateOpen:
		return "OPEN"
	case SimpleStateHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}
