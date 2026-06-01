# Event System Integration Guide

**Version:** 1.0
**Date:** February 24, 2026
**Status:** Production-Ready

---

## 📋 OVERVIEW

The event system provides in-memory event-driven communication for the samavaya monolithic application. It enables loose coupling between modules through event publishing and handling.

**Components:**
1. EventBusWrapper - Event publishing & subscription
2. BaseEventHandler - Base class for handlers
3. FX Provider - Dependency injection setup

---

## 🚀 QUICK START

### Step 1: Include EventBusModule in Application

In your main `cmd/app/main.go`:

```go
package main

import (
    "go.uber.org/fx"
    "p9e.in/samavaya/packages/events/provider"
)

func main() {
    app := fx.New(
        // Core event system
        provider.EventBusModule,

        // Your application modules
        // ... other modules
    )

    app.Run()
}
```

### Step 2: Create Event Handler

In your module (e.g., `business/finance/internal/handler/events/sales_order_handler.go`):

```go
package events

import (
    "context"
    "go.uber.org/fx"

    "p9e.in/samavaya/packages/events"
    "p9e.in/samavaya/packages/events/domain"
    "p9e.in/samavaya/packages/events/handler"
)

// SalesOrderCreatedHandler handles sales order created events
type SalesOrderCreatedHandler struct {
    *handler.BaseEventHandler
    arService *ARService
}

func NewSalesOrderCreatedHandler(
    eventBus *events.EventBusWrapper,
    arService *ARService,
) *SalesOrderCreatedHandler {
    return &SalesOrderCreatedHandler{
        BaseEventHandler: handler.NewBaseEventHandler(
            eventBus,
            "SalesOrderCreatedHandler",
            domain.EventTypeSalesOrderCreated,
        ),
        arService: arService,
    }
}

func (h *SalesOrderCreatedHandler) Handle(
    ctx context.Context,
    event *domain.DomainEvent,
) error {
    h.LogEvent(ctx, event, "Processing")

    // Extract data
    soID, err := h.ExtractString(event, "sales_order_id")
    if err != nil {
        return h.WrapError(err, "extract sales_order_id")
    }

    amount, err := h.ExtractFloat(event, "amount")
    if err != nil {
        return h.WrapError(err, "extract amount")
    }

    // Post AR with retry
    err = h.HandleWithRetry(ctx, func() error {
        return h.arService.PostAR(ctx, soID, amount)
    }, 3)

    if err != nil {
        h.LogError("Failed to post AR", err)
        return err
    }

    h.LogEvent(ctx, event, "Completed successfully")

    // Publish follow-up event
    arEvent := domain.NewDomainEvent(
        domain.EventTypeARPosted,
        soID,
        "SalesOrder",
        map[string]interface{}{"amount": amount},
    )

    return h.PublishEventWithCorrelation(ctx, arEvent, event)
}

// Module registration
var EventHandlersModule = fx.Module(
    "finance_event_handlers",
    fx.Provide(NewSalesOrderCreatedHandler),
    fx.Invoke(registerHandlers),
)

func registerHandlers(h *SalesOrderCreatedHandler) error {
    return h.RegisterHandler()
}
```

### Step 3: Publish Events from Services

In your service (e.g., `business/sales/internal/service/sales_order_service.go`):

```go
type SalesOrderService struct {
    repo     SalesOrderRepository
    eventBus *events.EventBusWrapper
}

func (s *SalesOrderService) CreateSalesOrder(
    ctx context.Context,
    input *CreateSalesOrderInput,
) (*SalesOrder, error) {
    // Validate input
    if err := input.Validate(); err != nil {
        return nil, err
    }

    // Create order
    order := &SalesOrder{...}

    // Save to database
    if err := s.repo.Create(ctx, order); err != nil {
        return nil, err
    }

    // Publish event (Finance handler will listen)
    event := domain.NewDomainEvent(
        domain.EventTypeSalesOrderCreated,
        order.ID,
        "SalesOrder",
        map[string]interface{}{
            "sales_order_id": order.ID,
            "amount":         order.Total,
            "customer_id":    order.CustomerID,
        },
    ).WithSource("sales.service")

    _ = s.eventBus.PublishEvent(ctx, event)

    return order, nil
}
```

### Step 4: Inject Service into Handler Module

In your FX configuration:

```go
var FinanceModule = fx.Module(
    "finance",
    // Services
    fx.Provide(NewARService),
    fx.Provide(NewSalesOrderHandler),

    // Event handlers
    EventHandlersModule,
)
```

---

## 🏗️ ARCHITECTURE

```
Application Startup
    ↓
EventBusModule initializes (FX)
    ├─ Creates EventBus singleton
    ├─ Creates EventBusWrapper singleton
    └─ Registers lifecycle hooks

    ↓
Each Module initializes
    ├─ Creates handlers with injected EventBusWrapper
    ├─ Handlers register with EventBus
    └─ Services get EventBusWrapper injected

    ↓
Application Running
    ├─ Service publishes event
    │  EventBusWrapper.PublishEvent(event)
    │
    ├─ Event published to EventBus
    │
    ├─ All matching handlers called
    │  Handler.Handle(event)
    │
    ├─ Handler extracts data
    │  handler.ExtractString(event, "key")
    │
    ├─ Handler performs action
    │  service.DoSomething()
    │
    └─ Handler publishes follow-up event (optional)
       handler.PublishEventWithCorrelation(newEvent, sourceEvent)
```

---

## 📝 EVENT FLOW EXAMPLE

```
1. Sales Order Created
   Sales Service
   ├─ Create order in DB
   ├─ Publish "SalesOrderCreated" event
   └─ Return response

2. Finance Handler Listens
   Finance Handler (SalesOrderCreatedHandler)
   ├─ Receive "SalesOrderCreated" event
   ├─ Extract sales_order_id and amount
   ├─ Call arService.PostAR()
   ├─ Post AR entry to GL
   ├─ Publish "ARPosted" event
   └─ Log completion

3. Inventory Handler Listens (Optional)
   Inventory Handler
   ├─ Receive "SalesOrderCreated" event
   ├─ Allocate inventory
   └─ Publish "InventoryAllocated" event

4. All Complete
   All handlers complete within same transaction
   Response sent to client ✓
```

---

## 🔧 DEPENDENCY INJECTION PATTERNS

### Pattern 1: Handler with Service

```go
type MyHandler struct {
    *handler.BaseEventHandler
    myService *MyService
}

func NewMyHandler(
    eventBus *events.EventBusWrapper,
    myService *MyService,
) *MyHandler {
    return &MyHandler{
        BaseEventHandler: handler.NewBaseEventHandler(
            eventBus,
            "MyHandler",
            domain.EventTypeSalesOrderCreated,
        ),
        myService: myService,
    }
}
```

### Pattern 2: Service with EventBus

```go
type MyService struct {
    repo     MyRepository
    eventBus *events.EventBusWrapper
}

func NewMyService(
    repo MyRepository,
    eventBus *events.EventBusWrapper,
) *MyService {
    return &MyService{
        repo:     repo,
        eventBus: eventBus,
    }
}
```

### Pattern 3: Module Registration

```go
var MyModule = fx.Module(
    "my_module",

    // Providers
    fx.Provide(NewMyRepository),
    fx.Provide(NewMyService),
    fx.Provide(NewMyHandler),

    // Event handlers
    fx.Invoke(registerMyHandlers),
)

func registerMyHandlers(h *MyHandler) error {
    return h.RegisterHandler()
}
```

---

## 📚 API REFERENCE

### EventBusWrapper

```go
// Publish event
func (e *EventBusWrapper) PublishEvent(ctx context.Context, event *DomainEvent) error

// Subscribe to single event type
func (e *EventBusWrapper) SubscribeToEvent(
    eventType EventType,
    handler func(context.Context, *DomainEvent) error,
) error

// Subscribe to multiple event types
func (e *EventBusWrapper) SubscribeToMultipleEvents(
    eventTypes []EventType,
    handler func(context.Context, *DomainEvent) error,
) error

// Get statistics
func (e *EventBusWrapper) GetStats() map[string]interface{}
```

### BaseEventHandler

```go
// Core
func (h *BaseEventHandler) Handle(ctx context.Context, event *DomainEvent) error
func (h *BaseEventHandler) RegisterHandler() error
func (h *BaseEventHandler) PublishEvent(ctx context.Context, event *DomainEvent) error
func (h *BaseEventHandler) PublishEventWithCorrelation(ctx context.Context, event, sourceEvent *DomainEvent) error

// Logging
func (h *BaseEventHandler) LogEvent(ctx context.Context, event *DomainEvent, action string)
func (h *BaseEventHandler) LogInfo(message string, args ...interface{})
func (h *BaseEventHandler) LogError(message string, err error, args ...interface{})

// Data Extraction
func (h *BaseEventHandler) ValidateEventData(event *DomainEvent, requiredKeys ...string) error
func (h *BaseEventHandler) ExtractString(event *DomainEvent, key string) (string, error)
func (h *BaseEventHandler) ExtractInt(event *DomainEvent, key string) (int64, error)
func (h *BaseEventHandler) ExtractFloat(event *DomainEvent, key string) (float64, error)
func (h *BaseEventHandler) ExtractBool(event *DomainEvent, key string) (bool, error)
func (h *BaseEventHandler) ExtractMap(event *DomainEvent, key string) (map[string]interface{}, error)

// Safe Extraction (with defaults)
func (h *BaseEventHandler) SafeExtractString(event *DomainEvent, key, default string) string
func (h *BaseEventHandler) SafeExtractInt(event *DomainEvent, key string, default int64) int64
func (h *BaseEventHandler) SafeExtractFloat(event *DomainEvent, key string, default float64) float64

// Advanced
func (h *BaseEventHandler) HandleWithRetry(ctx context.Context, fn func() error, maxRetries int) error
func (h *BaseEventHandler) WrapError(err error, message string) error
func (h *BaseEventHandler) GetStats() map[string]interface{}
func (h *BaseEventHandler) Reset()
```

### FX Provider

```go
// Module
var EventBusModule = fx.Module(...)

// Providers
func NewDefaultEventBus() *bus.EventBus
func NewEventBusWrapper(eventBus *bus.EventBus) *events.EventBusWrapper
func InitializeEventBus(lc fx.Lifecycle, eventBus *events.EventBusWrapper)
func NewEventBusHealthCheck(eventBus *events.EventBusWrapper) *EventBusHealthCheck

// Param Types
type EventBusParams struct { EventBus *EventBusWrapper }
type HandlerParams struct { EventBus *EventBusWrapper }
type ServiceParams struct { EventBus *EventBusWrapper }
type RepositoryParams struct { EventBus *EventBusWrapper }

// Health Check
func (h *EventBusHealthCheck) IsHealthy() bool
func (h *EventBusHealthCheck) GetStatus() map[string]interface{}

// Config
type EventBusConfig struct {
    Enabled           bool
    MaxBufferSize     int
    EnableMetrics     bool
    LogPublishedEvent bool
    LogHandledEvent   bool
}
func DefaultEventBusConfig() EventBusConfig
func ProvideEventBusConfig() EventBusConfig
```

---

## 🧪 TESTING

### Testing Handlers

```go
func TestMyHandler(t *testing.T) {
    // Setup
    eventBus := events.NewEventBusWrapper(bus.New())
    myService := NewMockMyService()
    handler := NewMyHandler(eventBus, myService)

    // Create test event
    event := domain.NewDomainEvent(
        domain.EventTypeSalesOrderCreated,
        "order-123",
        "SalesOrder",
        map[string]interface{}{
            "sales_order_id": "order-123",
            "amount": 1000.0,
        },
    )

    // Test handler
    err := handler.Handle(context.Background(), event)
    if err != nil {
        t.Fatalf("expected no error: %v", err)
    }

    // Verify service was called
    if !myService.PostARCalled {
        t.Fatal("expected PostAR to be called")
    }

    // Check stats
    stats := handler.GetStats()
    if stats["handled_count"].(int64) != 1 {
        t.Fatal("expected 1 handled event")
    }
}
```

### Testing with FX

```go
func TestEventBusIntegration(t *testing.T) {
    var handler *MyHandler
    var eventBus *events.EventBusWrapper

    app := fx.New(
        provider.EventBusModule,
        fx.Provide(NewMockMyService),
        fx.Provide(NewMyHandler),
        fx.Invoke(registerHandlers),
        fx.Populate(&handler, &eventBus),
    )

    if err := app.Start(context.Background()); err != nil {
        t.Fatalf("failed to start: %v", err)
    }

    // Test event publishing
    event := domain.NewDomainEvent(...)
    eventBus.PublishEvent(context.Background(), event)

    if err := app.Stop(context.Background()); err != nil {
        t.Fatalf("failed to stop: %v", err)
    }
}
```

---

## 🔍 DEBUGGING & MONITORING

### Getting Handler Stats

```go
stats := handler.GetStats()
fmt.Printf("Handler: %s\n", stats["handler_name"])
fmt.Printf("Handled: %d\n", stats["handled_count"])
fmt.Printf("Errors: %d\n", stats["error_count"])
fmt.Printf("Success Rate: %.2f%%\n", stats["success_rate"])
```

### Getting Event Bus Stats

```go
stats := eventBus.GetStats()
fmt.Printf("Published: %d\n", stats["published_count"])
fmt.Printf("Subscribers: %d\n", stats["subscriber_count"])
fmt.Printf("Errors: %d\n", stats["error_count"])
```

### Health Check

```go
healthCheck := NewEventBusHealthCheck(eventBus)
if healthCheck.IsHealthy() {
    fmt.Println("Event bus is healthy")
}

status := healthCheck.GetStatus()
fmt.Printf("Status: %v\n", status)
```

### Logging

Handlers log automatically:
```
[HANDLER] MyHandler | PUBLISHING | Type: sales.order.created | ID: evt-123 | Aggregate: SalesOrder/so-456 | CorrelationID: corr-789
[HANDLER] MyHandler | HANDLING | Type: sales.order.created | ID: evt-123 | Aggregate: SalesOrder/so-456 | CorrelationID: corr-789
[HANDLER] MyHandler | HANDLED (1.23ms) | Type: sales.order.created | ID: evt-123 | Aggregate: SalesOrder/so-456 | CorrelationID: corr-789
```

---

## ⚠️ ERROR HANDLING

### Publishing Errors

```go
event := domain.NewDomainEvent(...)
if err := eventBus.PublishEvent(ctx, event); err != nil {
    log.Printf("Failed to publish event: %v", err)
    // Implement retry or fallback logic
}
```

### Handler Errors

```go
func (h *MyHandler) Handle(ctx context.Context, event *DomainEvent) error {
    // Extract with error handling
    value, err := h.ExtractString(event, "required_field")
    if err != nil {
        return h.WrapError(err, "extract required_field")
    }

    // Retry on failure
    err = h.HandleWithRetry(ctx, func() error {
        return h.myService.DoSomething(ctx, value)
    }, 3)

    if err != nil {
        h.LogError("Operation failed", err)
        return err
    }

    return nil
}
```

---

## 🚀 BEST PRACTICES

1. **Always validate event data** before extraction
2. **Use correlation IDs** for tracing across events
3. **Implement retry logic** for external service calls
4. **Log important operations** for debugging
5. **Keep handlers focused** on single responsibility
6. **Publish follow-up events** for audit trails
7. **Use safe extraction** when values might be missing
8. **Handle panics** gracefully with recovery functions
9. **Monitor handler stats** for performance tracking
10. **Test handlers** in isolation and with FX

---

## 📋 CHECKLIST FOR ADDING NEW HANDLER

- [ ] Create handler file in `business/{module}/internal/handler/events/`
- [ ] Extend `BaseEventHandler`
- [ ] Implement `Handle()` method
- [ ] Validate event data
- [ ] Extract required fields
- [ ] Call service method with retry logic
- [ ] Handle errors
- [ ] Log operations
- [ ] Publish follow-up events (if needed)
- [ ] Create FX module for registration
- [ ] Write unit tests
- [ ] Test with FX integration tests
- [ ] Update module imports

---

**Status:** Production-Ready
**Last Updated:** February 24, 2026
