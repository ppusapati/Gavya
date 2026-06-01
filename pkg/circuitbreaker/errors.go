package circuitbreaker

import (
	"fmt"
	"time"
)

// CircuitOpenError is returned when a request is blocked by an open circuit.
type CircuitOpenError struct {
	Key        string
	State      State
	RecoveryAt *time.Time
	WaitTime   time.Duration
}

// Error implements the error interface.
func (e *CircuitOpenError) Error() string {
	if e.RecoveryAt != nil {
		return fmt.Sprintf("circuit breaker '%s' is %s, recovery at %s (wait %s)",
			e.Key, e.State, e.RecoveryAt.Format(time.RFC3339), e.WaitTime)
	}
	return fmt.Sprintf("circuit breaker '%s' is %s", e.Key, e.State)
}

// IsCircuitOpenError checks if an error is a circuit open error.
func IsCircuitOpenError(err error) bool {
	_, ok := err.(*CircuitOpenError)
	return ok
}

// AsCircuitOpenError attempts to convert an error to a CircuitOpenError.
func AsCircuitOpenError(err error) (*CircuitOpenError, bool) {
	e, ok := err.(*CircuitOpenError)
	return e, ok
}
