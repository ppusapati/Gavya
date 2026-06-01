package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"p9e.in/samavaya/packages/events"
	"p9e.in/samavaya/packages/events/bus"
	"p9e.in/samavaya/packages/events/domain"
)

func TestNewBaseEventHandler(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	eventTypes := []domain.EventType{
		domain.EventTypeSalesOrderCreated,
		domain.EventTypeInventoryAllocated,
	}

	handler := NewBaseEventHandler(eventBus, "TestHandler", eventTypes...)

	if handler == nil {
		t.Fatal("expected non-nil handler")
	}

	if handler.GetHandlerName() != "TestHandler" {
		t.Fatalf("expected handler name TestHandler, got %s", handler.GetHandlerName())
	}

	if len(handler.GetHandledEventTypes()) != 2 {
		t.Fatalf("expected 2 event types, got %d", len(handler.GetHandledEventTypes()))
	}
}

func TestGetHandledEventTypes(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	eventTypes := []domain.EventType{
		domain.EventTypeSalesOrderCreated,
		domain.EventTypeInventoryAllocated,
	}

	handler := NewBaseEventHandler(eventBus, "TestHandler", eventTypes...)
	handledTypes := handler.GetHandledEventTypes()

	if len(handledTypes) != 2 {
		t.Fatalf("expected 2 types, got %d", len(handledTypes))
	}

	if handledTypes[0] != domain.EventTypeSalesOrderCreated {
		t.Fatalf("expected first type to be SalesOrderCreated")
	}
}

func TestGetHandlerName(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "MyHandler", domain.EventTypeSalesOrderCreated)

	if handler.GetHandlerName() != "MyHandler" {
		t.Fatalf("expected MyHandler, got %s", handler.GetHandlerName())
	}
}

func TestLogEvent(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	event := domain.NewDomainEvent(
		domain.EventTypeSalesOrderCreated,
		"order-123",
		"SalesOrder",
		map[string]interface{}{},
	)

	// Should not panic
	handler.LogEvent(context.Background(), event, "TESTING")
}

func TestPublishEvent_Success(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	event := domain.NewDomainEvent(
		domain.EventTypeSalesOrderCreated,
		"order-123",
		"SalesOrder",
		map[string]interface{}{},
	)

	err := handler.PublishEvent(context.Background(), event)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	stats := eventBus.GetStats()
	if stats["published_count"].(int64) != 1 {
		t.Fatalf("expected 1 published event")
	}
}

func TestPublishEvent_NilEvent(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	err := handler.PublishEvent(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil event")
	}

	if !contains(err.Error(), "cannot publish nil event") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPublishEventWithCorrelation(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	sourceEvent := domain.NewDomainEvent(
		domain.EventTypeSalesOrderCreated,
		"order-123",
		"SalesOrder",
		map[string]interface{}{},
	).WithCorrelationID("corr-456")

	newEvent := domain.NewDomainEvent(
		domain.EventTypeARPosted,
		"ar-789",
		"AR",
		map[string]interface{}{},
	)

	err := handler.PublishEventWithCorrelation(context.Background(), newEvent, sourceEvent)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if newEvent.CorrelationID != "corr-456" {
		t.Fatalf("expected correlation ID corr-456, got %s", newEvent.CorrelationID)
	}

	if newEvent.CausationID != sourceEvent.ID {
		t.Fatalf("expected causation ID to be source event ID")
	}
}

func TestRegisterHandler_Success(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())

	mockHandler := &MockEventHandler{
		BaseEventHandler: NewBaseEventHandler(
			eventBus,
			"MockHandler",
			domain.EventTypeSalesOrderCreated,
		),
	}

	err := mockHandler.RegisterHandler()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestRegisterHandler_NoEventTypes(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler") // No event types

	err := handler.RegisterHandler()
	if err == nil {
		t.Fatal("expected error for handler with no event types")
	}

	if !contains(err.Error(), "has no event types") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegisterHandler_NilEventBus(t *testing.T) {
	handler := &BaseEventHandler{
		handlerName: "TestHandler",
		eventTypes: []domain.EventType{
			domain.EventTypeSalesOrderCreated,
		},
	}

	err := handler.RegisterHandler()
	if err == nil {
		t.Fatal("expected error for nil event bus")
	}

	if !contains(err.Error(), "event bus not initialized") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetStats(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	stats := handler.GetStats()

	if stats["handler_name"].(string) != "TestHandler" {
		t.Fatalf("expected handler_name TestHandler")
	}

	if stats["handled_count"].(int64) != 0 {
		t.Fatalf("expected handled_count 0")
	}

	if stats["error_count"].(int64) != 0 {
		t.Fatalf("expected error_count 0")
	}

	if stats["success_rate"].(float64) != 0 {
		t.Fatalf("expected success_rate 0 initially")
	}
}

func TestReset(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	// Simulate some activity
	handler.mu.Lock()
	handler.handledCount = 10
	handler.errorCount = 2
	handler.mu.Unlock()

	handler.Reset()

	stats := handler.GetStats()
	if stats["handled_count"].(int64) != 0 {
		t.Fatalf("expected handled_count 0 after reset")
	}

	if stats["error_count"].(int64) != 0 {
		t.Fatalf("expected error_count 0 after reset")
	}
}

func TestValidateEventData_Success(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	event := domain.NewDomainEvent(
		domain.EventTypeSalesOrderCreated,
		"order-123",
		"SalesOrder",
		map[string]interface{}{
			"order_id": "order-123",
			"amount":   1000.0,
		},
	)

	err := handler.ValidateEventData(event, "order_id", "amount")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateEventData_MissingKey(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	event := domain.NewDomainEvent(
		domain.EventTypeSalesOrderCreated,
		"order-123",
		"SalesOrder",
		map[string]interface{}{
			"order_id": "order-123",
		},
	)

	err := handler.ValidateEventData(event, "order_id", "amount")
	if err == nil {
		t.Fatal("expected error for missing key")
	}

	if !contains(err.Error(), "missing required event data key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEventData_NilData(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	event := &domain.DomainEvent{
		ID:            "event-123",
		Type:          domain.EventTypeSalesOrderCreated,
		AggregateID:   "order-123",
		AggregateType: "SalesOrder",
		Data:          nil,
	}

	err := handler.ValidateEventData(event, "order_id")
	if err == nil {
		t.Fatal("expected error for nil data")
	}

	if !contains(err.Error(), "event data is nil") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractString_Success(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	event := domain.NewDomainEvent(
		domain.EventTypeSalesOrderCreated,
		"order-123",
		"SalesOrder",
		map[string]interface{}{
			"customer_id": "cust-456",
		},
	)

	val, err := handler.ExtractString(event, "customer_id")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if val != "cust-456" {
		t.Fatalf("expected cust-456, got %s", val)
	}
}

func TestExtractString_TypeMismatch(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	event := domain.NewDomainEvent(
		domain.EventTypeSalesOrderCreated,
		"order-123",
		"SalesOrder",
		map[string]interface{}{
			"amount": 1000.0,
		},
	)

	_, err := handler.ExtractString(event, "amount")
	if err == nil {
		t.Fatal("expected error for type mismatch")
	}

	if !contains(err.Error(), "is not a string") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractInt_Success(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	event := domain.NewDomainEvent(
		domain.EventTypeSalesOrderCreated,
		"order-123",
		"SalesOrder",
		map[string]interface{}{
			"quantity": int64(100),
		},
	)

	val, err := handler.ExtractInt(event, "quantity")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if val != 100 {
		t.Fatalf("expected 100, got %d", val)
	}
}

func TestExtractFloat_Success(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	event := domain.NewDomainEvent(
		domain.EventTypeSalesOrderCreated,
		"order-123",
		"SalesOrder",
		map[string]interface{}{
			"amount": 1000.50,
		},
	)

	val, err := handler.ExtractFloat(event, "amount")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if val != 1000.50 {
		t.Fatalf("expected 1000.50, got %f", val)
	}
}

func TestExtractBool_Success(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	event := domain.NewDomainEvent(
		domain.EventTypeSalesOrderCreated,
		"order-123",
		"SalesOrder",
		map[string]interface{}{
			"is_approved": true,
		},
	)

	val, err := handler.ExtractBool(event, "is_approved")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !val {
		t.Fatal("expected true")
	}
}

func TestExtractMap_Success(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	details := map[string]interface{}{
		"key": "value",
	}

	event := domain.NewDomainEvent(
		domain.EventTypeSalesOrderCreated,
		"order-123",
		"SalesOrder",
		map[string]interface{}{
			"details": details,
		},
	)

	val, err := handler.ExtractMap(event, "details")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if val["key"] != "value" {
		t.Fatal("expected map to be extracted correctly")
	}
}

func TestSafeExtractString(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	event := domain.NewDomainEvent(
		domain.EventTypeSalesOrderCreated,
		"order-123",
		"SalesOrder",
		map[string]interface{}{},
	)

	val := handler.SafeExtractString(event, "missing_key", "default")
	if val != "default" {
		t.Fatalf("expected default, got %s", val)
	}
}

func TestSafeExtractInt(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	event := domain.NewDomainEvent(
		domain.EventTypeSalesOrderCreated,
		"order-123",
		"SalesOrder",
		map[string]interface{}{},
	)

	val := handler.SafeExtractInt(event, "missing_key", 999)
	if val != 999 {
		t.Fatalf("expected 999, got %d", val)
	}
}

func TestWrapError(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	originalErr := errors.New("original error")
	wrappedErr := handler.WrapError(originalErr, "operation failed")

	if wrappedErr == nil {
		t.Fatal("expected error")
	}

	if !contains(wrappedErr.Error(), "TestHandler") {
		t.Fatalf("expected handler name in error: %v", wrappedErr)
	}

	if !contains(wrappedErr.Error(), "operation failed") {
		t.Fatalf("expected message in error: %v", wrappedErr)
	}
}

func TestWrapError_NilError(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	err := handler.WrapError(nil, "message")
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestHandleWithRetry_Success(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	callCount := 0
	err := handler.HandleWithRetry(context.Background(), func() error {
		callCount++
		return nil
	}, 3)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if callCount != 1 {
		t.Fatalf("expected 1 call, got %d", callCount)
	}
}

func TestHandleWithRetry_FailsAfterRetries(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	callCount := 0
	testErr := errors.New("test error")
	err := handler.HandleWithRetry(context.Background(), func() error {
		callCount++
		return testErr
	}, 2)

	if err == nil {
		t.Fatal("expected error")
	}

	if callCount != 3 {
		t.Fatalf("expected 3 calls (1 initial + 2 retries), got %d", callCount)
	}

	if !contains(err.Error(), "max retries exceeded") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleWithRetry_SucceedsAfterRetry(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	callCount := 0
	err := handler.HandleWithRetry(context.Background(), func() error {
		callCount++
		if callCount < 2 {
			return errors.New("temporary error")
		}
		return nil
	}, 3)

	if err != nil {
		t.Fatalf("expected no error after retry, got %v", err)
	}

	if callCount != 2 {
		t.Fatalf("expected 2 calls, got %d", callCount)
	}
}

func TestSetRecoveryFunc(t *testing.T) {
	eventBus := events.NewEventBusWrapper(bus.New())
	handler := NewBaseEventHandler(eventBus, "TestHandler", domain.EventTypeSalesOrderCreated)

	recovered := false
	handler.SetRecoveryFunc(func(r interface{}) {
		recovered = true
	})

	if !recovered {
		// Recovery func will be called by recoverFromPanic if panic occurs
		// For now just verify it was set
		if handler.recoveryFunc == nil {
			t.Fatal("expected recovery func to be set")
		}
	}
}

// MockEventHandler for testing
type MockEventHandler struct {
	*BaseEventHandler
}

func (h *MockEventHandler) Handle(ctx context.Context, event *domain.DomainEvent) error {
	h.LogEvent(ctx, event, "MOCK_HANDLING")
	return nil
}

// Helper function
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
