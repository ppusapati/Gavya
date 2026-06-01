// Package helpers provides middleware and decorators to eliminate boilerplate
// in repository and service layer operations.
//
// The WithObservability decorator wraps database operations with:
//   - Timeout application via TimeoutProvider
//   - Distributed tracing spans
//   - Metrics recording (duration, success rate)
//   - Structured logging with context
//   - Error handling and logging
//
// Example usage:
//
//	result, err := helpers.WithObservability(ctx, deps, "GetUser", func(opCtx *OperationContext) (*User, error) {
//	    dm := models.DataModel[User]{
//	        TableName: "users",
//	        Where: "id = $1",
//	        WhereArgs: []any{userID},
//	    }
//	    return operations.ExecuteQuery(opCtx.Ctx, deps.Pool, &dm, operations.QueryTypeSelect)
//	})
package helpers

import (
	"context"
	"time"

	"p9e.in/samavaya/packages/deps"
	"p9e.in/samavaya/packages/p9log"

	"go.opentelemetry.io/otel/attribute"
)

// OperationContext contains common operation metadata and dependencies.
// This is passed to wrapped operations to provide access to timeout-applied
// context, dependencies, and operation metadata.
type OperationContext struct {
	// OperationName identifies the operation for logging/tracing
	OperationName string

	// EntityType is the type of entity being operated on (e.g., "User", "Product")
	EntityType string

	// StartTime records when the operation started
	StartTime time.Time

	// Ctx is the context with timeout already applied
	Ctx context.Context

	// Deps provides access to all service dependencies
	Deps *deps.ServiceDeps

	// Logger is a contextual logger with operation metadata
	Logger *p9log.Helper

	// LongQuery indicates if this is a long-running query (affects timeout)
	LongQuery bool
}

// WithObservability wraps a database operation with tracing, metrics, logging, and timeout.
//
// This eliminates repetitive boilerplate code in repository and service helpers.
// The wrapped function receives an OperationContext with timeout-applied context
// and structured logger.
//
// Type parameter T is the return type of the operation.
//
// Example:
//
//	user, err := WithObservability(ctx, deps, "GetUserByID", func(opCtx *OperationContext) (*User, error) {
//	    dm := models.DataModel[User]{
//	        TableName: "users",
//	        Where: "id = $1",
//	        WhereArgs: []any{userID},
//	    }
//	    result, err := operations.ExecuteQuery(opCtx.Ctx, opCtx.Deps.Pool, &dm, operations.QueryTypeSelect)
//	    if err != nil {
//	        return nil, err
//	    }
//	    return &result, nil
//	})
func WithObservability[T any](
	ctx context.Context,
	deps *deps.ServiceDeps,
	operationName string,
	fn func(opCtx *OperationContext) (T, error),
) (T, error) {
	return WithObservabilityLongQuery(ctx, deps, operationName, false, fn)
}

// WithObservabilityLongQuery is like WithObservability but allows specifying
// whether this is a long-running query (affects timeout duration).
func WithObservabilityLongQuery[T any](
	ctx context.Context,
	deps *deps.ServiceDeps,
	operationName string,
	longQuery bool,
	fn func(opCtx *OperationContext) (T, error),
) (T, error) {
	var zero T

	// Apply timeout based on query type
	tctx, cancel := deps.Tp.ApplyTimeout(ctx, longQuery)
	defer cancel()

	// Create operation context
	opCtx := &OperationContext{
		OperationName: operationName,
		StartTime:     time.Now(),
		Ctx:           tctx,
		Deps:          deps,
		Logger:        p9log.NewHelper(p9log.With(deps.Log, operationName)),
		LongQuery:     longQuery,
	}

	// Start tracing span
	spanCtx, span := deps.Tracing.StartSpan(opCtx.Ctx, operationName)
	opCtx.Ctx = spanCtx
	defer span.End()

	// Execute operation
	result, err := fn(opCtx)

	// Record duration
	duration := time.Since(opCtx.StartTime)

	// Record metrics
	success := err == nil
	if deps.Metrics != nil {
		deps.Metrics.RecordDBOperation(operationName, duration, success)
	}

	// Add span attributes
	span.SetAttributes(
		attribute.String("operation", operationName),
		attribute.Int64("duration_ms", duration.Milliseconds()),
		attribute.Bool("success", success),
	)

	// Handle errors
	if err != nil {
		// Log timeout specifically
		if opCtx.Ctx.Err() == context.DeadlineExceeded {
			opCtx.Logger.Errorf("%s operation timed out after %v", operationName, duration)
		} else {
			opCtx.Logger.Errorf("%s failed: %v", operationName, err)
		}

		// Add error to span
		span.RecordError(err)

		return zero, err
	}

	return result, nil
}

// WithEntityType sets the entity type on an operation context for more descriptive logging.
// This is optional and should be used when the operation name doesn't clearly indicate
// the entity type.
func (opCtx *OperationContext) WithEntityType(entityType string) *OperationContext {
	opCtx.EntityType = entityType
	// Update logger with entity type
	opCtx.Logger = p9log.NewHelper(p9log.With(opCtx.Deps.Log,
		"operation", opCtx.OperationName,
		"entity_type", entityType,
	))
	return opCtx
}
