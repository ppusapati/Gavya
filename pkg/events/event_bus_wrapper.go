package events

import (
	"context"
	"fmt"
	"sync"
	"time"

	"p9e.in/samavaya/packages/events/bus"
	"p9e.in/samavaya/packages/events/domain"
)

// EventPublisher is a simple interface for publishing events.
type EventPublisher = domain.EventPublisher

// EventBusWrapper provides application-specific event bus methods
// It wraps the generic bus.EventBus with convenience methods for domain events
type EventBusWrapper struct {
	bus              *bus.EventBus
	mu               sync.RWMutex
	publishedCount   int64
	subscriberCount  int64
	errorCount       int64
	lastPublishTime  time.Time
}

// NewEventBusWrapper creates new wrapper around event bus
func NewEventBusWrapper(eventBus *bus.EventBus) *EventBusWrapper {
	if eventBus == nil {
		eventBus = bus.New()
	}
	return &EventBusWrapper{
		bus: eventBus,
	}
}

// PublishEvent publishes domain event to the event bus
// It validates the event and logs the publish action
func (e *EventBusWrapper) PublishEvent(ctx context.Context, event *domain.DomainEvent) error {
	if event == nil {
		return fmt.Errorf("event cannot be nil")
	}

	// Validate required fields
	if err := e.validateEvent(event); err != nil {
		return fmt.Errorf("invalid event: %w", err)
	}

	e.mu.Lock()
	e.publishedCount++
	e.lastPublishTime = time.Now()
	e.mu.Unlock()

	// Log event publication
	e.logEvent("PUBLISH", event)

	// Publish to bus
	publisher := bus.Publish[*domain.DomainEvent](e.bus)
	if err := publisher(ctx, event); err != nil {
		e.mu.Lock()
		e.errorCount++
		e.mu.Unlock()
		e.logError("PUBLISH_ERROR", event, err)
		return fmt.Errorf("publish event failed: %w", err)
	}

	return nil
}

// SubscribeToEvent subscribes handler to specific event type
// Handler will only be called if event type matches
func (e *EventBusWrapper) SubscribeToEvent(
	eventType domain.EventType,
	handler func(context.Context, *domain.DomainEvent) error,
) error {
	if eventType == "" {
		return fmt.Errorf("event type cannot be empty")
	}

	if handler == nil {
		return fmt.Errorf("handler cannot be nil")
	}

	e.mu.Lock()
	e.subscriberCount++
	e.mu.Unlock()

	// Create type-filtered handler
	subscribe := bus.Subscribe[*domain.DomainEvent](e.bus)

	subscription, err := subscribe(func(ctx context.Context, event *domain.DomainEvent) error {
		// Only call handler if event type matches
		if event.Type != eventType {
			return nil
		}

		// Log handler invocation
		e.logEvent("HANDLE", event)

		// Call actual handler
		if err := handler(ctx, event); err != nil {
			e.mu.Lock()
			e.errorCount++
			e.mu.Unlock()
			e.logError("HANDLER_ERROR", event, err)
			return err
		}

		return nil
	})

	if err != nil {
		e.mu.Lock()
		e.errorCount++
		e.mu.Unlock()
		return fmt.Errorf("subscribe error: %w", err)
	}

	// Keep subscription active
	_ = subscription

	return nil
}

// SubscribeToMultipleEvents subscribes handler to multiple event types
// Handler will be called for any of the specified event types
func (e *EventBusWrapper) SubscribeToMultipleEvents(
	eventTypes []domain.EventType,
	handler func(context.Context, *domain.DomainEvent) error,
) error {
	if len(eventTypes) == 0 {
		return fmt.Errorf("at least one event type must be specified")
	}

	if handler == nil {
		return fmt.Errorf("handler cannot be nil")
	}

	// Create event type map for fast lookup
	eventTypeMap := make(map[domain.EventType]bool, len(eventTypes))
	for _, et := range eventTypes {
		if et == "" {
			return fmt.Errorf("event type cannot be empty")
		}
		eventTypeMap[et] = true
	}

	e.mu.Lock()
	e.subscriberCount++
	e.mu.Unlock()

	// Subscribe to bus with type filtering
	subscribe := bus.Subscribe[*domain.DomainEvent](e.bus)

	subscription, err := subscribe(func(ctx context.Context, event *domain.DomainEvent) error {
		// Only call handler if event type is in the map
		if !eventTypeMap[event.Type] {
			return nil
		}

		// Log handler invocation
		e.logEvent("HANDLE", event)

		// Call actual handler
		if err := handler(ctx, event); err != nil {
			e.mu.Lock()
			e.errorCount++
			e.mu.Unlock()
			e.logError("HANDLER_ERROR", event, err)
			return err
		}

		return nil
	})

	if err != nil {
		e.mu.Lock()
		e.errorCount++
		e.mu.Unlock()
		return fmt.Errorf("subscribe error: %w", err)
	}

	// Keep subscription active
	_ = subscription

	return nil
}

// validateEvent validates event has required fields
func (e *EventBusWrapper) validateEvent(event *domain.DomainEvent) error {
	if event.ID == "" {
		return fmt.Errorf("event ID cannot be empty")
	}

	if event.Type == "" {
		return fmt.Errorf("event type cannot be empty")
	}

	if event.AggregateID == "" {
		return fmt.Errorf("aggregate ID cannot be empty")
	}

	if event.AggregateType == "" {
		return fmt.Errorf("aggregate type cannot be empty")
	}

	return nil
}

// logEvent logs event for debugging
func (e *EventBusWrapper) logEvent(action string, event *domain.DomainEvent) {
	fmt.Printf("[EVENT] %s | Type: %s | ID: %s | Aggregate: %s/%s | CorrelationID: %s\n",
		action, event.Type, event.ID, event.AggregateType, event.AggregateID, event.CorrelationID)
}

// logError logs error during event processing
func (e *EventBusWrapper) logError(action string, event *domain.DomainEvent, err error) {
	fmt.Printf("[ERROR] %s | Type: %s | ID: %s | Error: %v\n",
		action, event.Type, event.ID, err)
}

// GetStats returns event bus statistics
func (e *EventBusWrapper) GetStats() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return map[string]interface{}{
		"published_count":   e.publishedCount,
		"subscriber_count":  e.subscriberCount,
		"error_count":       e.errorCount,
		"last_publish_time": e.lastPublishTime,
	}
}

// Reset clears statistics (useful for testing)
func (e *EventBusWrapper) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.publishedCount = 0
	e.subscriberCount = 0
	e.errorCount = 0
	e.lastPublishTime = time.Time{}
}
