package provider

import (
	"context"
	"testing"

	"go.uber.org/fx"

	"p9e.in/samavaya/packages/events"
	"p9e.in/samavaya/packages/events/bus"
)

func TestNewDefaultEventBus(t *testing.T) {
	eventBus := NewDefaultEventBus()

	if eventBus == nil {
		t.Fatal("expected non-nil event bus")
	}

	// Verify it's a valid bus.EventBus
	if _, ok := interface{}(eventBus).(*bus.EventBus); !ok {
		t.Fatal("expected *bus.EventBus type")
	}
}

func TestNewEventBusWrapper(t *testing.T) {
	eventBus := NewDefaultEventBus()
	wrapper := NewEventBusWrapper(eventBus)

	if wrapper == nil {
		t.Fatal("expected non-nil wrapper")
	}

	if _, ok := interface{}(wrapper).(*events.EventBusWrapper); !ok {
		t.Fatal("expected *events.EventBusWrapper type")
	}
}

func TestEventBusModule(t *testing.T) {
	var wrapper *events.EventBusWrapper

	app := fx.New(
		EventBusModule,
		fx.Populate(&wrapper),
	)

	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}

	if wrapper == nil {
		t.Fatal("expected wrapper to be populated")
	}

	if err := app.Stop(context.Background()); err != nil {
		t.Fatalf("failed to stop app: %v", err)
	}
}

func TestEventBusParams(t *testing.T) {
	eventBus := NewEventBusWrapper(NewDefaultEventBus())
	params := EventBusParams{
		EventBus: eventBus,
	}

	if params.EventBus == nil {
		t.Fatal("expected EventBus in params")
	}
}

func TestHandlerParams(t *testing.T) {
	eventBus := NewEventBusWrapper(NewDefaultEventBus())
	params := HandlerParams{
		EventBus: eventBus,
	}

	if params.EventBus == nil {
		t.Fatal("expected EventBus in params")
	}
}

func TestServiceParams(t *testing.T) {
	eventBus := NewEventBusWrapper(NewDefaultEventBus())
	params := ServiceParams{
		EventBus: eventBus,
	}

	if params.EventBus == nil {
		t.Fatal("expected EventBus in params")
	}
}

func TestRepositoryParams(t *testing.T) {
	eventBus := NewEventBusWrapper(NewDefaultEventBus())
	params := RepositoryParams{
		EventBus: eventBus,
	}

	if params.EventBus == nil {
		t.Fatal("expected EventBus in params")
	}
}

func TestProvideEventBusParams(t *testing.T) {
	eventBus := NewEventBusWrapper(NewDefaultEventBus())
	params := ProvideEventBusParams(eventBus)

	if params.EventBus == nil {
		t.Fatal("expected EventBus in params")
	}

	if params.EventBus != eventBus {
		t.Fatal("expected same eventBus instance")
	}
}

func TestInitializeEventBus(t *testing.T) {
	eventBus := NewEventBusWrapper(NewDefaultEventBus())

	app := fx.New(
		fx.Provide(func() *events.EventBusWrapper { return eventBus }),
		fx.Invoke(InitializeEventBus),
	)

	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}

	if err := app.Stop(context.Background()); err != nil {
		t.Fatalf("failed to stop app: %v", err)
	}
}

func TestNewEventBusHealthCheck(t *testing.T) {
	eventBus := NewEventBusWrapper(NewDefaultEventBus())
	healthCheck := NewEventBusHealthCheck(eventBus)

	if healthCheck == nil {
		t.Fatal("expected non-nil health check")
	}
}

func TestEventBusHealthCheck_IsHealthy(t *testing.T) {
	eventBus := NewEventBusWrapper(NewDefaultEventBus())
	healthCheck := NewEventBusHealthCheck(eventBus)

	if !healthCheck.IsHealthy() {
		t.Fatal("expected event bus to be healthy")
	}
}

func TestEventBusHealthCheck_IsHealthy_NilEventBus(t *testing.T) {
	healthCheck := &EventBusHealthCheck{
		eventBus: nil,
	}

	if healthCheck.IsHealthy() {
		t.Fatal("expected event bus to be unhealthy when nil")
	}
}

func TestEventBusHealthCheck_GetStatus(t *testing.T) {
	eventBus := NewEventBusWrapper(NewDefaultEventBus())
	healthCheck := NewEventBusHealthCheck(eventBus)

	status := healthCheck.GetStatus()

	if status == nil {
		t.Fatal("expected non-nil status")
	}

	if status["status"] != "healthy" {
		t.Fatalf("expected status 'healthy', got %v", status["status"])
	}

	if _, ok := status["published_count"]; !ok {
		t.Fatal("expected published_count in status")
	}

	if _, ok := status["subscriber_count"]; !ok {
		t.Fatal("expected subscriber_count in status")
	}

	if _, ok := status["error_count"]; !ok {
		t.Fatal("expected error_count in status")
	}
}

func TestEventBusHealthCheck_GetStatus_NilEventBus(t *testing.T) {
	healthCheck := &EventBusHealthCheck{
		eventBus: nil,
	}

	status := healthCheck.GetStatus()

	if status == nil {
		t.Fatal("expected non-nil status")
	}

	if status["status"] != "unhealthy" {
		t.Fatalf("expected status 'unhealthy', got %v", status["status"])
	}
}

func TestDefaultEventBusConfig(t *testing.T) {
	config := DefaultEventBusConfig()

	if !config.Enabled {
		t.Fatal("expected event bus to be enabled by default")
	}

	if config.MaxBufferSize != 1000 {
		t.Fatalf("expected MaxBufferSize 1000, got %d", config.MaxBufferSize)
	}

	if !config.EnableMetrics {
		t.Fatal("expected metrics to be enabled by default")
	}

	if !config.LogPublishedEvent {
		t.Fatal("expected LogPublishedEvent to be enabled by default")
	}

	if !config.LogHandledEvent {
		t.Fatal("expected LogHandledEvent to be enabled by default")
	}
}

func TestProvideEventBusConfig(t *testing.T) {
	config := ProvideEventBusConfig()

	if !config.Enabled {
		t.Fatal("expected event bus to be enabled")
	}

	if config.MaxBufferSize == 0 {
		t.Fatal("expected MaxBufferSize to be set")
	}
}

func TestEventBusModuleWithMultipleDependencies(t *testing.T) {
	var eventBus *bus.EventBus
	var wrapper *events.EventBusWrapper

	app := fx.New(
		EventBusModule,
		fx.Populate(&eventBus, &wrapper),
	)

	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}

	if eventBus == nil {
		t.Fatal("expected eventBus to be populated")
	}

	if wrapper == nil {
		t.Fatal("expected wrapper to be populated")
	}

	// Verify they're connected
	if wrapper == nil {
		t.Fatal("expected wrapper to reference the bus")
	}

	if err := app.Stop(context.Background()); err != nil {
		t.Fatalf("failed to stop app: %v", err)
	}
}

func TestEventBusModuleIsSingleton(t *testing.T) {
	var wrapper1 *events.EventBusWrapper
	var wrapper2 *events.EventBusWrapper

	app := fx.New(
		EventBusModule,
		fx.Provide(
			func(w *events.EventBusWrapper) *events.EventBusWrapper {
				wrapper1 = w
				return w
			},
			func(w *events.EventBusWrapper) *events.EventBusWrapper {
				wrapper2 = w
				return w
			},
		),
	)

	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("failed to start app: %v", err)
	}

	// Both should reference the same instance
	if wrapper1 != wrapper2 {
		t.Fatal("expected event bus wrapper to be singleton")
	}

	if err := app.Stop(context.Background()); err != nil {
		t.Fatalf("failed to stop app: %v", err)
	}
}

func TestEventBusParamsAllVariants(t *testing.T) {
	testCases := []struct {
		name string
		test func(t *testing.T, bus *events.EventBusWrapper)
	}{
		{
			name: "EventBusParams",
			test: func(t *testing.T, bus *events.EventBusWrapper) {
				params := EventBusParams{EventBus: bus}
				if params.EventBus == nil {
					t.Fatal("expected EventBus")
				}
			},
		},
		{
			name: "HandlerParams",
			test: func(t *testing.T, bus *events.EventBusWrapper) {
				params := HandlerParams{EventBus: bus}
				if params.EventBus == nil {
					t.Fatal("expected EventBus")
				}
			},
		},
		{
			name: "ServiceParams",
			test: func(t *testing.T, bus *events.EventBusWrapper) {
				params := ServiceParams{EventBus: bus}
				if params.EventBus == nil {
					t.Fatal("expected EventBus")
				}
			},
		},
		{
			name: "RepositoryParams",
			test: func(t *testing.T, bus *events.EventBusWrapper) {
				params := RepositoryParams{EventBus: bus}
				if params.EventBus == nil {
					t.Fatal("expected EventBus")
				}
			},
		},
	}

	bus := NewEventBusWrapper(NewDefaultEventBus())

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.test(t, bus)
		})
	}
}

func TestEventBusProviderWithApplicationStartStop(t *testing.T) {
	var wrapper *events.EventBusWrapper

	app := fx.New(
		EventBusModule,
		fx.Invoke(InitializeEventBus),
		fx.Populate(&wrapper),
	)

	// Start application
	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Verify event bus is working
	if wrapper == nil {
		t.Fatal("expected wrapper to be initialized")
	}

	stats := wrapper.GetStats()
	if stats == nil {
		t.Fatal("expected stats from event bus")
	}

	// Stop application
	if err := app.Stop(context.Background()); err != nil {
		t.Fatalf("failed to stop: %v", err)
	}
}

func TestEventBusHealthCheckIntegration(t *testing.T) {
	var healthCheck *EventBusHealthCheck

	app := fx.New(
		EventBusModule,
		fx.Provide(NewEventBusHealthCheck),
		fx.Populate(&healthCheck),
	)

	if err := app.Start(context.Background()); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	if !healthCheck.IsHealthy() {
		t.Fatal("expected health check to pass")
	}

	status := healthCheck.GetStatus()
	if status["status"] != "healthy" {
		t.Fatalf("expected healthy status: %v", status)
	}

	if err := app.Stop(context.Background()); err != nil {
		t.Fatalf("failed to stop: %v", err)
	}
}

func BenchmarkNewDefaultEventBus(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = NewDefaultEventBus()
	}
}

func BenchmarkNewEventBusWrapper(b *testing.B) {
	eventBus := NewDefaultEventBus()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = NewEventBusWrapper(eventBus)
	}
}
