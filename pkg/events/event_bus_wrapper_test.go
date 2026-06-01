package events

import (
	"context"
	"errors"
	"testing"

	"p9e.in/samavaya/packages/events/bus"
	"p9e.in/samavaya/packages/events/domain"
)

func TestNewEventBusWrapper(t *testing.T) {
	// Test with nil bus (should create new one)
	wrapper := NewEventBusWrapper(nil)
	if wrapper == nil {
		t.Fatal("expected non-nil wrapper")
	}
	if wrapper.bus == nil {
		t.Fatal("expected bus to be initialized")
	}

	// Test with existing bus
	existingBus := bus.New()
	wrapper = NewEventBusWrapper(existingBus)
	if wrapper.bus != existingBus {
		t.Fatal("expected wrapper to use provided bus")
	}
}

func TestPublishEvent_Success(t *testing.T) {
	wrapper := NewEventBusWrapper(nil)
	ctx := context.Background()

	event := domain.NewDomainEvent(
		domain.EventTypeSalesOrderCreated,
		"order-123",
		"SalesOrder",
		map[string]interface{}{
			"amount": 1000.0,
		},
	)

	err := wrapper.PublishEvent(ctx, event)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Check stats
	stats := wrapper.GetStats()
	if stats["published_count"].(int64) != 1 {
		t.Fatalf("expected published count of 1, got %v", stats["published_count"])
	}

	if stats["error_count"].(int64) != 0 {
		t.Fatalf("expected error count of 0, got %v", stats["error_count"])
	}
}

func TestPublishEvent_NilEvent(t *testing.T) {
	wrapper := NewEventBusWrapper(nil)
	ctx := context.Background()

	err := wrapper.PublishEvent(ctx, nil)
	if err == nil {
		t.Fatal("expected error for nil event")
	}

	if err.Error() != "event cannot be nil" {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestPublishEvent_InvalidEvent(t *testing.T) {
	wrapper := NewEventBusWrapper(nil)
	ctx := context.Background()

	testCases := []struct {
		name          string
		event         *domain.DomainEvent
		expectedError string
	}{
		{
			name: "missing ID",
			event: &domain.DomainEvent{
				Type:          domain.EventTypeSalesOrderCreated,
				AggregateID:   "order-123",
				AggregateType: "SalesOrder",
			},
			expectedError: "event ID cannot be empty",
		},
		{
			name: "missing Type",
			event: &domain.DomainEvent{
				ID:            "event-123",
				AggregateID:   "order-123",
				AggregateType: "SalesOrder",
			},
			expectedError: "event type cannot be empty",
		},
		{
			name: "missing AggregateID",
			event: &domain.DomainEvent{
				ID:            "event-123",
				Type:          domain.EventTypeSalesOrderCreated,
				AggregateType: "SalesOrder",
			},
			expectedError: "aggregate ID cannot be empty",
		},
		{
			name: "missing AggregateType",
			event: &domain.DomainEvent{
				ID:          "event-123",
				Type:        domain.EventTypeSalesOrderCreated,
				AggregateID: "order-123",
			},
			expectedError: "aggregate type cannot be empty",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := wrapper.PublishEvent(ctx, tc.event)
			if err == nil {
				t.Fatal("expected error")
			}
			if !contains(err.Error(), tc.expectedError) {
				t.Fatalf("expected error containing %q, got %v", tc.expectedError, err)
			}
		})
	}
}

func TestSubscribeToEvent_Success(t *testing.T) {
	wrapper := NewEventBusWrapper(nil)
	ctx := context.Background()

	handlerCalled := false
	var capturedEvent *domain.DomainEvent

	// Subscribe to event
	err := wrapper.SubscribeToEvent(
		domain.EventTypeSalesOrderCreated,
		func(ctx context.Context, event *domain.DomainEvent) error {
			handlerCalled = true
			capturedEvent = event
			return nil
		},
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Publish matching event
	event := domain.NewDomainEvent(
		domain.EventTypeSalesOrderCreated,
		"order-123",
		"SalesOrder",
		map[string]interface{}{},
	)

	err = wrapper.PublishEvent(ctx, event)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !handlerCalled {
		t.Fatal("expected handler to be called")
	}

	if capturedEvent.ID != event.ID {
		t.Fatalf("expected event ID %s, got %s", event.ID, capturedEvent.ID)
	}
}

func TestSubscribeToEvent_TypeFiltering(t *testing.T) {
	wrapper := NewEventBusWrapper(nil)
	ctx := context.Background()

	handlerCalled := false

	// Subscribe to SalesOrderCreated
	err := wrapper.SubscribeToEvent(
		domain.EventTypeSalesOrderCreated,
		func(ctx context.Context, event *domain.DomainEvent) error {
			handlerCalled = true
			return nil
		},
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Publish different event type
	event := domain.NewDomainEvent(
		domain.EventTypeInventoryAllocated,
		"inv-123",
		"Inventory",
		map[string]interface{}{},
	)

	err = wrapper.PublishEvent(ctx, event)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if handlerCalled {
		t.Fatal("expected handler NOT to be called for different event type")
	}
}

func TestSubscribeToEvent_NilEventType(t *testing.T) {
	wrapper := NewEventBusWrapper(nil)

	err := wrapper.SubscribeToEvent("", func(ctx context.Context, event *domain.DomainEvent) error {
		return nil
	})

	if err == nil {
		t.Fatal("expected error for empty event type")
	}
}

func TestSubscribeToEvent_NilHandler(t *testing.T) {
	wrapper := NewEventBusWrapper(nil)

	err := wrapper.SubscribeToEvent(domain.EventTypeSalesOrderCreated, nil)

	if err == nil {
		t.Fatal("expected error for nil handler")
	}
}

func TestSubscribeToMultipleEvents_Success(t *testing.T) {
	wrapper := NewEventBusWrapper(nil)
	ctx := context.Background()

	var capturedEvents []*domain.DomainEvent

	// Subscribe to multiple events
	err := wrapper.SubscribeToMultipleEvents(
		[]domain.EventType{
			domain.EventTypeSalesOrderCreated,
			domain.EventTypeInventoryAllocated,
		},
		func(ctx context.Context, event *domain.DomainEvent) error {
			capturedEvents = append(capturedEvents, event)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Publish first event type
	event1 := domain.NewDomainEvent(
		domain.EventTypeSalesOrderCreated,
		"order-123",
		"SalesOrder",
		map[string]interface{}{},
	)
	err = wrapper.PublishEvent(ctx, event1)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Publish second event type
	event2 := domain.NewDomainEvent(
		domain.EventTypeInventoryAllocated,
		"inv-456",
		"Inventory",
		map[string]interface{}{},
	)
	err = wrapper.PublishEvent(ctx, event2)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(capturedEvents) != 2 {
		t.Fatalf("expected 2 events, got %d", len(capturedEvents))
	}
}

func TestSubscribeToMultipleEvents_EmptyList(t *testing.T) {
	wrapper := NewEventBusWrapper(nil)

	err := wrapper.SubscribeToMultipleEvents([]domain.EventType{}, func(ctx context.Context, event *domain.DomainEvent) error {
		return nil
	})

	if err == nil {
		t.Fatal("expected error for empty event type list")
	}
}

func TestSubscribeToMultipleEvents_InvalidEventType(t *testing.T) {
	wrapper := NewEventBusWrapper(nil)

	err := wrapper.SubscribeToMultipleEvents(
		[]domain.EventType{"valid", ""},
		func(ctx context.Context, event *domain.DomainEvent) error {
			return nil
		},
	)

	if err == nil {
		t.Fatal("expected error for empty event type")
	}
}

func TestHandlerError(t *testing.T) {
	wrapper := NewEventBusWrapper(nil)
	ctx := context.Background()

	expectedErr := errors.New("handler error")
	handlerCalled := false

	// Subscribe with error-returning handler
	err := wrapper.SubscribeToEvent(
		domain.EventTypeSalesOrderCreated,
		func(ctx context.Context, event *domain.DomainEvent) error {
			handlerCalled = true
			return expectedErr
		},
	)
	if err != nil {
		t.Fatalf("expected no error on subscribe, got %v", err)
	}

	// Publish event
	event := domain.NewDomainEvent(
		domain.EventTypeSalesOrderCreated,
		"order-123",
		"SalesOrder",
		map[string]interface{}{},
	)

	err = wrapper.PublishEvent(ctx, event)
	if err != nil {
		t.Fatalf("expected no error on publish, got %v", err)
	}

	if !handlerCalled {
		t.Fatal("expected handler to be called")
	}

	// Check error stats
	stats := wrapper.GetStats()
	if stats["error_count"].(int64) != 1 {
		t.Fatalf("expected error count 1, got %v", stats["error_count"])
	}
}

func TestGetStats(t *testing.T) {
	wrapper := NewEventBusWrapper(nil)

	stats := wrapper.GetStats()

	if stats["published_count"].(int64) != 0 {
		t.Fatalf("expected initial published count 0")
	}

	if stats["subscriber_count"].(int64) != 0 {
		t.Fatalf("expected initial subscriber count 0")
	}

	if stats["error_count"].(int64) != 0 {
		t.Fatalf("expected initial error count 0")
	}
}

func TestReset(t *testing.T) {
	wrapper := NewEventBusWrapper(nil)
	ctx := context.Background()

	// Publish an event to increment counter
	event := domain.NewDomainEvent(
		domain.EventTypeSalesOrderCreated,
		"order-123",
		"SalesOrder",
		map[string]interface{}{},
	)
	wrapper.PublishEvent(ctx, event)

	stats := wrapper.GetStats()
	if stats["published_count"].(int64) != 1 {
		t.Fatalf("expected published count 1")
	}

	// Reset
	wrapper.Reset()

	stats = wrapper.GetStats()
	if stats["published_count"].(int64) != 0 {
		t.Fatalf("expected published count 0 after reset")
	}

	if stats["error_count"].(int64) != 0 {
		t.Fatalf("expected error count 0 after reset")
	}
}

func TestCorrelationID(t *testing.T) {
	wrapper := NewEventBusWrapper(nil)
	ctx := context.Background()

	var capturedEvent *domain.DomainEvent

	// Subscribe
	wrapper.SubscribeToEvent(
		domain.EventTypeSalesOrderCreated,
		func(ctx context.Context, event *domain.DomainEvent) error {
			capturedEvent = event
			return nil
		},
	)

	// Create event with correlation ID
	event := domain.NewDomainEvent(
		domain.EventTypeSalesOrderCreated,
		"order-123",
		"SalesOrder",
		map[string]interface{}{},
	).WithCorrelationID("corr-456")

	wrapper.PublishEvent(ctx, event)

	if capturedEvent.CorrelationID != "corr-456" {
		t.Fatalf("expected correlation ID corr-456, got %s", capturedEvent.CorrelationID)
	}
}

func TestConcurrency(t *testing.T) {
	wrapper := NewEventBusWrapper(nil)
	ctx := context.Background()

	eventCount := 0
	var mu sync.Mutex

	// Subscribe
	wrapper.SubscribeToEvent(
		domain.EventTypeSalesOrderCreated,
		func(ctx context.Context, event *domain.DomainEvent) error {
			mu.Lock()
			eventCount++
			mu.Unlock()
			return nil
		},
	)

	// Publish events concurrently
	for i := 0; i < 10; i++ {
		event := domain.NewDomainEvent(
			domain.EventTypeSalesOrderCreated,
			"order-"+string(rune(48+i)),
			"SalesOrder",
			map[string]interface{}{},
		)
		wrapper.PublishEvent(ctx, event)
	}

	if eventCount != 10 {
		t.Fatalf("expected 10 events, got %d", eventCount)
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr || len(s) > len(substr) && contains(s[1:], substr)
}

// For concurrency test
import "sync"
