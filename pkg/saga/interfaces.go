// Package saga provides distributed saga transaction orchestration
package saga

import (
	"context"
	"time"

	"p9e.in/samavaya/packages/saga/models"
)

// Type aliases for the domain model shapes so `saga.StepDefinition` and
// `models.StepDefinition` refer to the same underlying type. This lets
// the engine packages (compensation / executor / orchestrator) use
// either `saga.X` or `models.X` freely — historically the codebase mixed
// both which caused compile errors; making them aliases resolves that
// without changing call sites. B.8 fix 2026-04-19.
type (
	SagaExecution        = models.SagaExecution
	SagaExecutionInput   = models.SagaExecutionInput
	StepExecution        = models.StepExecution
	StepDefinition       = models.StepDefinition
	StepResult           = models.StepResult
	RetryConfiguration   = models.RetryConfiguration
	CircuitBreakerStatus = models.CircuitBreakerStatus
	TimeoutTracker       = models.TimeoutTracker
	CompensationStatus   = models.CompensationStatus
	SagaEvent            = models.SagaEvent
	SagaEventType        = models.SagaEventType
	CompensationRecord   = models.CompensationRecord
	SagaExecutionStatus  = models.SagaExecutionStatus
	StepExecutionStatus  = models.StepExecutionStatus
)

// SagaOrchestrator coordinates the execution of all saga steps
type SagaOrchestrator interface {
	// ExecuteSaga starts a new saga execution
	ExecuteSaga(ctx context.Context, sagaType string, input *SagaExecutionInput) (*SagaExecution, error)

	// ResumeSaga resumes interrupted saga from last successful step
	ResumeSaga(ctx context.Context, sagaID string) (*SagaExecution, error)

	// GetExecution retrieves current saga execution state
	GetExecution(ctx context.Context, sagaID string) (*SagaExecution, error)

	// GetExecutionTimeline retrieves all steps executed so far
	GetExecutionTimeline(ctx context.Context, sagaID string) ([]*StepExecution, error)

	// RegisterSagaHandler registers handler for specific saga type
	RegisterSagaHandler(sagaType string, handler SagaHandler) error
}

// SagaStepExecutor executes individual saga steps by invoking service handlers via RPC
type SagaStepExecutor interface {
	// ExecuteStep executes a single saga step with timeout and retry
	ExecuteStep(ctx context.Context, sagaID string, stepNum int, stepDef *StepDefinition) (*StepResult, error)

	// GetStepStatus retrieves status of executed step
	GetStepStatus(ctx context.Context, sagaID string, stepNum int) (*StepExecution, error)
}

// SagaTimeoutHandler manages step execution timeouts, retries, and circuit breaker
type SagaTimeoutHandler interface {
	// SetupStepTimeout sets up timeout for a step
	SetupStepTimeout(ctx context.Context, sagaID string, stepNum int, timeoutSeconds int32) error

	// CancelStepTimeout cancels timeout for completed step
	CancelStepTimeout(sagaID string, stepNum int) error

	// CheckExpired checks if saga/step has expired
	CheckExpired(sagaID string, stepNum int) (bool, error)

	// GetRetryConfig returns retry configuration for step
	GetRetryConfig(sagaType string, stepNum int) (*RetryConfiguration, error)
}

// SagaEventPublisher publishes saga step events to Kafka for asynchronous
// processing.
//
// Signature note (2026-04-19 B.8): the step-level methods take a
// *models.SagaExecution because the publisher needs saga_type / tenant /
// current_step context to build the event payload — passing just a sagaID
// would force the publisher to re-query state. The orchestrator already
// holds the execution when publishing.
type SagaEventPublisher interface {
	// PublishSagaStarted publishes saga started event
	PublishSagaStarted(ctx context.Context, execution *SagaExecution) error

	// PublishStepStarted publishes step started event
	PublishStepStarted(ctx context.Context, execution *SagaExecution, stepNum int32) error

	// PublishStepCompleted publishes step completed event with result
	PublishStepCompleted(ctx context.Context, execution *SagaExecution, stepNum int32, result *StepResult) error

	// PublishStepFailed publishes step failed event
	PublishStepFailed(ctx context.Context, execution *SagaExecution, stepNum int32, err error) error

	// PublishStepRetrying publishes step retrying event
	PublishStepRetrying(ctx context.Context, execution *SagaExecution, stepNum int32, err error) error

	// PublishSagaCompleted publishes saga completed event
	PublishSagaCompleted(ctx context.Context, execution *SagaExecution) error

	// PublishSagaFailed publishes saga failed event. Error details are read
	// from execution.ErrorCode / execution.ErrorMessage — the caller sets
	// those before publishing.
	PublishSagaFailed(ctx context.Context, execution *SagaExecution) error

	// PublishCompensationStarted publishes compensation started event
	PublishCompensationStarted(ctx context.Context, execution *SagaExecution) error

	// PublishCompensationCompleted publishes compensation completed event
	PublishCompensationCompleted(ctx context.Context, execution *SagaExecution) error
}

// SagaHandler defines a saga implementation with steps and handlers
type SagaHandler interface {
	// SagaType returns the saga type identifier (e.g., "SAGA-S01")
	SagaType() string

	// GetStepDefinitions returns all steps in saga
	GetStepDefinitions() []*StepDefinition

	// GetStepDefinition returns definition for specific step
	GetStepDefinition(stepNum int) *StepDefinition

	// ValidateInput validates input for saga execution
	ValidateInput(input interface{}) error
}

// RpcConnector provides abstraction for invoking service handlers via RPC
type RpcConnector interface {
	// InvokeHandler calls a service handler via RPC
	// serviceName: e.g., "sales-order", "inventory-core"
	// method: e.g., "CreateOrder", "ReserveStock"
	// input: request message (proto format)
	// returns: response message (proto format)
	InvokeHandler(ctx context.Context, serviceName string, method string, input interface{}) (interface{}, error)

	// GetServiceEndpoint returns endpoint URL for service
	GetServiceEndpoint(serviceName string) (string, error)

	// RegisterService registers service endpoint
	RegisterService(serviceName string, endpoint string) error
}

// CircuitBreaker provides fault tolerance with state management
type CircuitBreaker interface {
	// Call executes function with circuit breaker protection
	Call(fn func() error) error

	// GetStatus returns current circuit breaker status
	GetStatus() CircuitBreakerStatus

	// Reset resets circuit breaker to closed state
	Reset()
}

// SagaRepository provides data access for saga execution records.
type SagaRepository interface {
	// GetByID retrieves saga by row ID
	GetByID(ctx context.Context, id string) (*SagaExecution, error)

	// GetExecution retrieves saga by saga_id (domain id, not row id).
	// Alias for GetBySagaID — keeps orchestrator + compensation call sites
	// readable when the context already reads "execution".
	GetExecution(ctx context.Context, sagaID string) (*SagaExecution, error)

	// GetBySagaID gets saga by saga ID (for recovery)
	GetBySagaID(ctx context.Context, sagaID string) (*SagaExecution, error)

	// CreateExecution creates new saga execution record
	CreateExecution(ctx context.Context, saga *SagaExecution) error

	// UpdateExecution updates saga execution record
	UpdateExecution(ctx context.Context, saga *SagaExecution) error
}

// SagaExecutionLogRepository provides audit trail for saga execution.
type SagaExecutionLogRepository interface {
	// GetBySagaID retrieves all execution log entries for a saga
	GetBySagaID(ctx context.Context, sagaID string) ([]*StepExecution, error)

	// GetExecutionLog retrieves the complete step trail for a saga.
	// Alias for GetBySagaID; kept because the orchestrator refers to the
	// trail as "execution log" in its method signatures.
	GetExecutionLog(ctx context.Context, sagaID string) ([]*StepExecution, error)

	// CreateExecutionLog creates new execution log entry
	CreateExecutionLog(ctx context.Context, entry *StepExecution) error

	// UpdateExecutionLog updates execution log entry
	UpdateExecutionLog(ctx context.Context, entry *StepExecution) error
}

// SagaTimeoutLogRepository provides timeout tracking
type SagaTimeoutLogRepository interface {
	// Create creates timeout tracking entry
	Create(ctx context.Context, sagaID string, stepNum int, timeoutAt time.Time) error

	// GetExpiredBefore retrieves timeouts that have expired
	GetExpiredBefore(ctx context.Context, before time.Time) ([]*TimeoutTracker, error)

	// Delete deletes timeout tracking entry
	Delete(ctx context.Context, sagaID string, stepNum int) error
}

// SagaCompensationEngine handles compensation execution.
//
// 2026-04-19 B.8: signatures aligned to the CompensationEngineImpl's
// actual implementation — the execution and step-def list are already
// loaded by the orchestrator when compensation starts, so the engine
// doesn't need to re-fetch them by saga-id.
type SagaCompensationEngine interface {
	// StartCompensation begins compensation process for a failed saga.
	StartCompensation(ctx context.Context, execution *SagaExecution, stepDefs []*StepDefinition) error

	// ExecuteCompensation executes compensation for the specific step
	// (+ the transitive compensation chain defined by
	// stepDef.CompensationSteps).
	ExecuteCompensation(ctx context.Context, execution *SagaExecution, stepNum int32, compensationSteps []*StepDefinition) error

	// GetCompensationStatus retrieves compensation status
	GetCompensationStatus(ctx context.Context, sagaID string) (CompensationStatus, error)
}
