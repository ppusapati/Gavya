// Package serviceclient is the shared scaffold for cross-service calls in
// Samavāya.
//
// It implements the ports-and-adapters pattern mandated by ADR-0003
// (docs/adr/0003-service-communication-hybrid.md). Each service exposes a
// public Go "port" — an interface that other services depend on. Two
// adapters implement the port:
//
//  1. InProc — wraps the service's internal handler/service directly. Used
//     in the monolith (`app/`) and in tests. Zero serialisation, ~1µs
//     method-dispatch overhead.
//
//  2. Connect — wraps the generated ConnectRPC client. Used when services
//     are deployed separately. The port signature stays the same; only the
//     adapter changes. ADR-0003 calls out that the ConnectAdapter IS the
//     generated client — we typically don't write an extra wrapper.
//
// Caller code depends on the port interface, never on a specific adapter.
// The composition root (monolith's fx graph or a split cmd/main.go) picks
// which adapter to inject.
//
// # Three primitives
//
// The package exposes three things:
//
//   - Invoker[Req, Resp any] — typed request/reply for in-process sync calls.
//     Thin wrapper on packages/events/bus.Dispatch + AddProcessor.
//
//   - Publisher[E any] / Subscriber[E any] — async event fan-out. Thin
//     wrapper on packages/events/bus.Publish + Subscribe. In split mode,
//     swap for the Kafka/NATS adapter.
//
//   - Registry — holds named bus instances so services can route different
//     traffic through different buses (e.g. BI bus vs sales bus) without
//     globals. Optional; most services use the default bus.
//
// # What this package is NOT
//
//   - NOT a replacement for packages/events. This sits on top of it.
//   - NOT a service locator / DI container. Ports are injected via fx as
//     concrete types (see fx.go).
//   - NOT a saga engine. Sagas keep using packages/events/domain.DomainEventPublisher
//     directly — they care about workflow state, not request/reply.
//
// # Naming conventions (ADR-0003 § Naming conventions)
//
//   - Proto (wire):         <service>/proto/ + <service>/api/v1/
//   - Port interface:        <service>/client/client.go
//   - In-process adapter:    <service>/client/inproc.go
//   - Connect adapter:       <service>/client/connect.go
//   - Events:                <service>/events/events.go
//
// # Example port (from ADR-0003 follow-up Sprint 4.T3)
//
//	// core/bi/dataset/client/client.go
//	type Client interface {
//	    GetDataset(ctx context.Context, id string) (*Dataset, error)
//	    GetSchema(ctx context.Context, id string) (*Schema, error)
//	}
//
//	// core/bi/dataset/client/inproc.go
//	func NewInProcClient(svc service.DatasetService, preview service.DatasetPreviewService) Client {
//	    return &inprocClient{svc: svc, preview: preview}
//	}
//
// # Example event
//
//	// core/bi/dataset/events/events.go
//	type DatasetDeleted struct {
//	    DatasetID string
//	    TenantID  string
//	    DeletedBy string
//	    At        time.Time
//	}
//
//	// emitting:
//	publisher := serviceclient.NewPublisher[events.DatasetDeleted](bus)
//	_ = publisher.Publish(ctx, events.DatasetDeleted{...})
//
//	// consuming:
//	subscriber := serviceclient.NewSubscriber[events.DatasetDeleted](bus)
//	_, _ = subscriber.Subscribe(func(ctx context.Context, ev events.DatasetDeleted) error {
//	    cache.Invalidate(ev.DatasetID)
//	    return nil
//	})
package serviceclient
