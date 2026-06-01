// Package saga provides error types for saga operations
package saga

import (
	"errors"
	"fmt"
)

// Common saga errors
var (
	ErrSagaTypeNotRegistered    = errors.New("saga type not registered")
	ErrStepFailed               = errors.New("saga step execution failed")
	ErrStepTimeout              = errors.New("saga step execution timeout")
	ErrCircuitBreakerOpen       = errors.New("circuit breaker open, request rejected")
	ErrCompensationFailed       = errors.New("compensation execution failed")
	ErrSagaNotFound             = errors.New("saga instance not found")
	ErrInvalidSagaState         = errors.New("invalid saga state transition")
	ErrUncompensatableFailure   = errors.New("failure cannot be compensated")
	ErrMaxRetriesExceeded       = errors.New("maximum retries exceeded")
	ErrSagaAlreadyRunning       = errors.New("saga is already running")
)

// SagaError wraps saga execution errors with context
type SagaError struct {
	SagaID    string // Saga instance ID
	StepNum   int    // Step number (0 if not step-specific)
	Code      string // Error code
	Message   string // Human-readable message
	Err       error  // Underlying error
	Retryable bool   // Whether error is retryable
}

// Error implements the error interface
func (e *SagaError) Error() string {
	if e.StepNum > 0 {
		return fmt.Sprintf("saga error [%s] step %d: %s", e.Code, e.StepNum, e.Message)
	}
	return fmt.Sprintf("saga error [%s]: %s", e.Code, e.Message)
}

// Unwrap returns the underlying error
func (e *SagaError) Unwrap() error {
	return e.Err
}

// NewSagaError creates a new SagaError.
//
// Accepts two arg shapes (2026-04-19 B.8):
//   - (sagaID, code, message, err)                           — saga-level
//   - (sagaID, stepNum, code, message, err, retryable)       — step-level with explicit retry policy
//
// The second form is used by the orchestrator; it lets the caller
// signal retryability explicitly instead of relying on isRetryableError's
// heuristic. Dispatched via trailing arg count.
func NewSagaError(sagaID string, args ...interface{}) *SagaError {
	switch len(args) {
	case 3:
		// (code, message, err)
		code, _ := args[0].(string)
		message, _ := args[1].(string)
		var err error
		if args[2] != nil {
			err, _ = args[2].(error)
		}
		return &SagaError{
			SagaID:    sagaID,
			Code:      code,
			Message:   message,
			Err:       err,
			Retryable: isRetryableError(err),
		}
	case 5:
		// (stepNum, code, message, err, retryable)
		stepNum, _ := asInt(args[0])
		code, _ := args[1].(string)
		message, _ := args[2].(string)
		var err error
		if args[3] != nil {
			err, _ = args[3].(error)
		}
		retryable, _ := args[4].(bool)
		return &SagaError{
			SagaID:    sagaID,
			StepNum:   stepNum,
			Code:      code,
			Message:   message,
			Err:       err,
			Retryable: retryable,
		}
	default:
		return &SagaError{
			SagaID:  sagaID,
			Code:    "BAD_NewSagaError_CALL",
			Message: fmt.Sprintf("NewSagaError: expected 3 or 5 trailing args, got %d", len(args)),
		}
	}
}

// asInt accepts int / int32 / int64 for the step-number arg.
func asInt(v interface{}) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int32:
		return int(x), true
	case int64:
		return int(x), true
	}
	return 0, false
}

// NewSagaStepError creates a new SagaError for a specific step
func NewSagaStepError(sagaID string, stepNum int, code string, message string, err error) *SagaError {
	return &SagaError{
		SagaID:    sagaID,
		StepNum:   stepNum,
		Code:      code,
		Message:   message,
		Err:       err,
		Retryable: isRetryableError(err),
	}
}

// IsRetryableError determines if error is retryable based on error code
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific retryable errors
	switch err {
	case ErrStepTimeout, ErrCircuitBreakerOpen:
		return true
	}

	// Check for retryable error codes in message
	errStr := err.Error()
	retryableCodes := []string{
		"TIMEOUT",
		"UNAVAILABLE",
		"RESOURCE_EXHAUSTED",
		"INTERNAL",
		"TEMPORARY",
		"TRANSIENT",
	}

	for _, code := range retryableCodes {
		if contains(errStr, code) {
			return true
		}
	}

	return false
}

// Non-retryable errors
func isNonRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	nonRetryableCodes := []string{
		"INVALID_ARGUMENT",
		"NOT_FOUND",
		"PERMISSION_DENIED",
		"UNAUTHENTICATED",
		"ALREADY_EXISTS",
		"FAILED_PRECONDITION",
		"ABORTED",
		"OUT_OF_RANGE",
		"UNIMPLEMENTED",
		"DATA_LOSS",
	}

	for _, code := range nonRetryableCodes {
		if contains(errStr, code) {
			return true
		}
	}

	return false
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
