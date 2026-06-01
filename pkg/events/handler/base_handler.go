package handler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"p9e.in/samavaya/packages/events"
	"p9e.in/samavaya/packages/events/domain"
)

// IEventHandler defines the interface for event handlers
type IEventHandler interface {
	// Handle processes the domain event
	Handle(context.Context, *domain.DomainEvent) error

	// GetHandledEventTypes returns the event types handled by this handler
	GetHandledEventTypes() []domain.EventType

	// GetHandlerName returns the name of this handler
	GetHandlerName() string
}

// BaseEventHandler provides common functionality for all event handlers
// It should be embedded in specific handler implementations
type BaseEventHandler struct {
	eventBus      *events.EventBusWrapper
	eventTypes    []domain.EventType
	handlerName   string
	mu            sync.RWMutex
	handledCount  int64
	errorCount    int64
	lastHandleTime time.Time
	recoveryFunc  func(interface{}) // For panic recovery
}

// NewBaseEventHandler creates a new base event handler
func NewBaseEventHandler(
	eventBus *events.EventBusWrapper,
	handlerName string,
	eventTypes ...domain.EventType,
) *BaseEventHandler {
	return &BaseEventHandler{
		eventBus:    eventBus,
		handlerName: handlerName,
		eventTypes:  eventTypes,
		recoveryFunc: DefaultPanicRecovery,
	}
}

// GetHandledEventTypes returns the event types handled by this handler
func (h *BaseEventHandler) GetHandledEventTypes() []domain.EventType {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.eventTypes
}

// GetHandlerName returns the name of this handler
func (h *BaseEventHandler) GetHandlerName() string {
	return h.handlerName
}

// Handle processes the event (to be implemented by subclasses)
func (h *BaseEventHandler) Handle(ctx context.Context, event *domain.DomainEvent) error {
	return fmt.Errorf("handler %s: Handle not implemented", h.handlerName)
}

// LogEvent logs event for debugging purposes
func (h *BaseEventHandler) LogEvent(ctx context.Context, event *domain.DomainEvent, action string) {
	fmt.Printf("[HANDLER] %s | %s | Type: %s | ID: %s | Aggregate: %s/%s | CorrelationID: %s\n",
		h.handlerName, action, event.Type, event.ID, event.AggregateType, event.AggregateID, event.CorrelationID)
}

// LogInfo logs informational message
func (h *BaseEventHandler) LogInfo(message string, args ...interface{}) {
	fmt.Printf("[INFO] %s: %s\n", h.handlerName, fmt.Sprintf(message, args...))
}

// LogError logs error message
func (h *BaseEventHandler) LogError(message string, err error, args ...interface{}) {
	fmt.Printf("[ERROR] %s: %s | Error: %v\n", h.handlerName, fmt.Sprintf(message, args...), err)
}

// PublishEvent publishes a new event from this handler
// Maintains correlation ID from original event if provided
func (h *BaseEventHandler) PublishEvent(ctx context.Context, event *domain.DomainEvent) error {
	if event == nil {
		return fmt.Errorf("handler %s: cannot publish nil event", h.handlerName)
	}

	h.LogEvent(ctx, event, "PUBLISHING")

	if err := h.eventBus.PublishEvent(ctx, event); err != nil {
		h.LogError("PUBLISH_FAILED", err)
		return err
	}

	return nil
}

// PublishEventWithCorrelation publishes event and maintains correlation ID from source event
func (h *BaseEventHandler) PublishEventWithCorrelation(
	ctx context.Context,
	event *domain.DomainEvent,
	sourceEvent *domain.DomainEvent,
) error {
	if event == nil {
		return fmt.Errorf("handler %s: cannot publish nil event", h.handlerName)
	}

	// Preserve correlation ID from source event
	if sourceEvent != nil && sourceEvent.CorrelationID != "" {
		event.CorrelationID = sourceEvent.CorrelationID
	}

	// Set causation ID to source event ID for tracing
	if sourceEvent != nil {
		event.CausationID = sourceEvent.ID
	}

	return h.PublishEvent(ctx, event)
}

// RegisterHandler registers this handler with the event bus
// This should be called during application initialization
func (h *BaseEventHandler) RegisterHandler() error {
	if len(h.eventTypes) == 0 {
		return fmt.Errorf("handler %s: has no event types to handle", h.handlerName)
	}

	if h.eventBus == nil {
		return fmt.Errorf("handler %s: event bus not initialized", h.handlerName)
	}

	h.LogInfo("Registering for %d event types", len(h.eventTypes))

	err := h.eventBus.SubscribeToMultipleEvents(h.eventTypes, h.handle)
	if err != nil {
		return fmt.Errorf("handler %s: failed to register: %w", h.handlerName, err)
	}

	h.LogInfo("Registered successfully")
	return nil
}

// handle is the internal event handler that wraps Handle with error handling and metrics
func (h *BaseEventHandler) handle(ctx context.Context, event *domain.DomainEvent) error {
	defer h.recoverFromPanic()

	h.LogEvent(ctx, event, "HANDLING")

	// Record start time
	startTime := time.Now()

	// Call the actual handler
	err := h.Handle(ctx, event)

	// Record metrics
	h.mu.Lock()
	h.lastHandleTime = time.Now()
	h.handledCount++
	if err != nil {
		h.errorCount++
	}
	h.mu.Unlock()

	// Log duration
	duration := time.Since(startTime)
	if err != nil {
		h.LogError("HANDLE_FAILED", err, "Duration: %v", duration)
		return err
	}

	h.LogEvent(ctx, event, fmt.Sprintf("HANDLED (%.2fms)", duration.Seconds()*1000))
	return nil
}

// RecoverPanic is the exported version for use in handler implementations.
func (h *BaseEventHandler) RecoverPanic() {
	h.recoverFromPanic()
}

// recoverFromPanic recovers from panic in handler
func (h *BaseEventHandler) recoverFromPanic() {
	if r := recover(); r != nil {
		h.LogError("PANIC_RECOVERED", fmt.Errorf("panic: %v", r))
		if h.recoveryFunc != nil {
			h.recoveryFunc(r)
		}
	}
}

// SetRecoveryFunc sets the panic recovery function
func (h *BaseEventHandler) SetRecoveryFunc(fn func(interface{})) {
	h.recoveryFunc = fn
}

// DefaultPanicRecovery is the default panic recovery function
func DefaultPanicRecovery(r interface{}) {
	fmt.Printf("[PANIC] %v\n", r)
}

// GetStats returns handler statistics
func (h *BaseEventHandler) GetStats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return map[string]interface{}{
		"handler_name":      h.handlerName,
		"event_types":       len(h.eventTypes),
		"handled_count":     h.handledCount,
		"error_count":       h.errorCount,
		"last_handle_time":  h.lastHandleTime,
		"success_rate":      h.getSuccessRate(),
	}
}

// getSuccessRate calculates success rate
func (h *BaseEventHandler) getSuccessRate() float64 {
	if h.handledCount == 0 {
		return 0
	}
	return float64(h.handledCount-h.errorCount) / float64(h.handledCount) * 100
}

// Reset clears statistics (useful for testing)
func (h *BaseEventHandler) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.handledCount = 0
	h.errorCount = 0
	h.lastHandleTime = time.Time{}
}

// ValidateEventData extracts and validates data from event
// Returns error if required key is missing
func (h *BaseEventHandler) ValidateEventData(event *domain.DomainEvent, requiredKeys ...string) error {
	if event.Data == nil {
		return fmt.Errorf("handler %s: event data is nil", h.handlerName)
	}

	for _, key := range requiredKeys {
		if _, ok := event.Data[key]; !ok {
			return fmt.Errorf("handler %s: missing required event data key: %s", h.handlerName, key)
		}
	}

	return nil
}

// ExtractString safely extracts string value from event data
func (h *BaseEventHandler) ExtractString(event *domain.DomainEvent, key string) (string, error) {
	val, ok := event.Data[key]
	if !ok {
		return "", fmt.Errorf("handler %s: missing key: %s", h.handlerName, key)
	}

	str, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("handler %s: key %s is not a string", h.handlerName, key)
	}

	return str, nil
}

// ExtractInt safely extracts int value from event data
func (h *BaseEventHandler) ExtractInt(event *domain.DomainEvent, key string) (int64, error) {
	val, ok := event.Data[key]
	if !ok {
		return 0, fmt.Errorf("handler %s: missing key: %s", h.handlerName, key)
	}

	// Try int64 first
	if intVal, ok := val.(int64); ok {
		return intVal, nil
	}

	// Try int
	if intVal, ok := val.(int); ok {
		return int64(intVal), nil
	}

	// Try float64 (JSON numbers)
	if floatVal, ok := val.(float64); ok {
		return int64(floatVal), nil
	}

	return 0, fmt.Errorf("handler %s: key %s cannot be converted to int", h.handlerName, key)
}

// ExtractFloat safely extracts float value from event data
func (h *BaseEventHandler) ExtractFloat(event *domain.DomainEvent, key string) (float64, error) {
	val, ok := event.Data[key]
	if !ok {
		return 0, fmt.Errorf("handler %s: missing key: %s", h.handlerName, key)
	}

	floatVal, ok := val.(float64)
	if !ok {
		return 0, fmt.Errorf("handler %s: key %s is not a float", h.handlerName, key)
	}

	return floatVal, nil
}

// ExtractBool safely extracts bool value from event data
func (h *BaseEventHandler) ExtractBool(event *domain.DomainEvent, key string) (bool, error) {
	val, ok := event.Data[key]
	if !ok {
		return false, fmt.Errorf("handler %s: missing key: %s", h.handlerName, key)
	}

	boolVal, ok := val.(bool)
	if !ok {
		return false, fmt.Errorf("handler %s: key %s is not a bool", h.handlerName, key)
	}

	return boolVal, nil
}

// ExtractMap safely extracts map value from event data
func (h *BaseEventHandler) ExtractMap(event *domain.DomainEvent, key string) (map[string]interface{}, error) {
	val, ok := event.Data[key]
	if !ok {
		return nil, fmt.Errorf("handler %s: missing key: %s", h.handlerName, key)
	}

	mapVal, ok := val.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("handler %s: key %s is not a map", h.handlerName, key)
	}

	return mapVal, nil
}

// SafeExtractString extracts string with default value if missing
func (h *BaseEventHandler) SafeExtractString(event *domain.DomainEvent, key, defaultValue string) string {
	val, err := h.ExtractString(event, key)
	if err != nil {
		return defaultValue
	}
	return val
}

// SafeExtractInt extracts int with default value if missing
func (h *BaseEventHandler) SafeExtractInt(event *domain.DomainEvent, key string, defaultValue int64) int64 {
	val, err := h.ExtractInt(event, key)
	if err != nil {
		return defaultValue
	}
	return val
}

// SafeExtractFloat extracts float with default value if missing
func (h *BaseEventHandler) SafeExtractFloat(event *domain.DomainEvent, key string, defaultValue float64) float64 {
	val, err := h.ExtractFloat(event, key)
	if err != nil {
		return defaultValue
	}
	return val
}

// WrapError wraps an error with handler context
func (h *BaseEventHandler) WrapError(err error, message string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("handler %s: %s: %w", h.handlerName, message, err)
}

// HandleWithRetry executes a function with retry logic
// Returns error if all retries fail
func (h *BaseEventHandler) HandleWithRetry(
	ctx context.Context,
	fn func() error,
	maxRetries int,
) error {
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		if attempt < maxRetries {
			h.LogInfo("Retrying (attempt %d/%d): %v", attempt+1, maxRetries, err)
			time.Sleep(time.Duration(attempt) * time.Second) // Exponential backoff
		}
	}

	return h.WrapError(lastErr, "max retries exceeded")
}
