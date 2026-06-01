package provider

import (
	"context"
	"fmt"

	"go.uber.org/fx"

	"p9e.in/samavaya/packages/events"
	"p9e.in/samavaya/packages/events/bus"
)

// EventBusModule provides the event bus and related components to all modules
// This module should be included in the main fx.App() setup
var EventBusModule = fx.Module(
	"events",
	fx.Provide(
		NewDefaultEventBus,
		NewEventBusWrapper,
	),
)

// NewDefaultEventBus creates a default in-memory event bus instance
// This is a singleton - the same instance is used throughout the application
func NewDefaultEventBus() *bus.EventBus {
	return bus.New()
}

// NewEventBusWrapper creates a wrapper around the event bus
// This wrapper provides convenience methods for domain event operations
func NewEventBusWrapper(eventBus *bus.EventBus) *events.EventBusWrapper {
	return events.NewEventBusWrapper(eventBus)
}

// EventBusParams contains the event bus dependencies
// This can be injected into handlers and services
type EventBusParams struct {
	fx.In
	EventBus *events.EventBusWrapper `name:"eventBus"`
}

// HandlerParams contains the event bus for event handler modules
// Each module that needs the event bus uses this struct for dependency injection
type HandlerParams struct {
	fx.In
	EventBus *events.EventBusWrapper
}

// ServiceParams contains the event bus for service modules
// Services that publish events use this struct for dependency injection
type ServiceParams struct {
	fx.In
	EventBus *events.EventBusWrapper
}

// RepositoryParams contains the event bus for repository modules
// Repositories that publish domain events use this struct
type RepositoryParams struct {
	fx.In
	EventBus *events.EventBusWrapper
}

// ProvideEventBusParams provides EventBusParams to any module that needs the event bus
func ProvideEventBusParams(eventBus *events.EventBusWrapper) EventBusParams {
	return EventBusParams{
		EventBus: eventBus,
	}
}

// InitializeEventBus initializes the event bus on application startup
// This is invoked as a lifecycle hook
func InitializeEventBus(lc fx.Lifecycle, eventBus *events.EventBusWrapper) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			fmt.Println("[EVENT BUS] Initializing event bus")
			// Event bus is already initialized, just log it
			stats := eventBus.GetStats()
			fmt.Printf("[EVENT BUS] Event bus initialized | Published: %d, Subscribers: %d, Errors: %d\n",
				stats["published_count"], stats["subscriber_count"], stats["error_count"])
			return nil
		},
		OnStop: func(ctx context.Context) error {
			fmt.Println("[EVENT BUS] Shutting down event bus")
			stats := eventBus.GetStats()
			fmt.Printf("[EVENT BUS] Event bus final stats | Published: %d, Errors: %d\n",
				stats["published_count"], stats["error_count"])
			return nil
		},
	})
}

// EventBusHealthCheck provides a simple health check for the event bus
type EventBusHealthCheck struct {
	eventBus *events.EventBusWrapper
}

// NewEventBusHealthCheck creates a new health check instance
func NewEventBusHealthCheck(eventBus *events.EventBusWrapper) *EventBusHealthCheck {
	return &EventBusHealthCheck{
		eventBus: eventBus,
	}
}

// IsHealthy returns true if the event bus is operational
func (h *EventBusHealthCheck) IsHealthy() bool {
	if h.eventBus == nil {
		return false
	}

	stats := h.eventBus.GetStats()
	if stats == nil {
		return false
	}

	// Event bus is healthy if it exists and can be queried
	return true
}

// GetStatus returns the current status of the event bus
func (h *EventBusHealthCheck) GetStatus() map[string]interface{} {
	if h.eventBus == nil {
		return map[string]interface{}{
			"status": "unhealthy",
			"reason": "event bus not initialized",
		}
	}

	stats := h.eventBus.GetStats()
	return map[string]interface{}{
		"status":             "healthy",
		"published_count":    stats["published_count"],
		"subscriber_count":   stats["subscriber_count"],
		"error_count":        stats["error_count"],
		"last_publish_time":  stats["last_publish_time"],
	}
}

// EventBusConfig holds configuration for the event bus
type EventBusConfig struct {
	Enabled           bool
	MaxBufferSize     int
	EnableMetrics     bool
	LogPublishedEvent bool
	LogHandledEvent   bool
}

// DefaultEventBusConfig returns the default event bus configuration
func DefaultEventBusConfig() EventBusConfig {
	return EventBusConfig{
		Enabled:           true,
		MaxBufferSize:     1000,
		EnableMetrics:     true,
		LogPublishedEvent: true,
		LogHandledEvent:   true,
	}
}

// ProvideEventBusConfig provides the event bus configuration
func ProvideEventBusConfig() EventBusConfig {
	return DefaultEventBusConfig()
}
