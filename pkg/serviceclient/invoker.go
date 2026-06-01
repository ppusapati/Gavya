package serviceclient

import (
	"context"
	"errors"

	"p9e.in/samavaya/packages/events/bus"
)

// ErrNoHandler is returned when no handler is registered for the requested
// type. Wraps bus.ErrNotProcessor so callers can errors.Is against a stable
// sentinel regardless of underlying transport.
var ErrNoHandler = errors.New("serviceclient: no handler registered for request type")

// Invoker[Req, Resp] is the sync request/reply primitive.
//
// In monolith mode, backed by an in-process bus — Req is dispatched to the
// registered processor, which returns Resp synchronously. In split mode, a
// different Invoker implementation (the Connect adapter in the service's
// client/connect.go) hits the network. Callers see the same interface.
//
// Typed at construction time so callers get compile-time type safety. No
// interface{} shenanigans at the call site.
type Invoker[Req any, Resp any] interface {
	Invoke(ctx context.Context, req Req) (Resp, error)
}

// InvokeHandler[Req, Resp] is what a service registers to handle inbound
// requests. The signature matches bus.Processor[Req, Resp] exactly — this
// is deliberate so the bus can dispatch without an extra adapter layer.
type InvokeHandler[Req any, Resp any] func(ctx context.Context, req Req) (Resp, error)

// ============================================================================
// In-process implementation
// ============================================================================

// inprocInvoker is the default bus-backed Invoker. Constructed via
// NewInProcInvoker; callers should type it as Invoker[Req, Resp].
type inprocInvoker[Req any, Resp any] struct {
	dispatch bus.DispatcherFunc[Req, Resp]
}

// Invoke implements Invoker[Req, Resp].
func (i *inprocInvoker[Req, Resp]) Invoke(ctx context.Context, req Req) (Resp, error) {
	resp, err := i.dispatch(ctx, req)
	if err != nil {
		if errors.Is(err, bus.ErrNotProcessor) {
			var zero Resp
			return zero, ErrNoHandler
		}
		return resp, err
	}
	return resp, nil
}

// NewInProcInvoker builds an in-process Invoker backed by the given bus.
// The bus must have a matching processor registered via RegisterHandler
// before Invoke is called.
func NewInProcInvoker[Req any, Resp any](b *bus.EventBus) Invoker[Req, Resp] {
	return &inprocInvoker[Req, Resp]{
		dispatch: bus.Dispatch[Req, Resp](b),
	}
}

// RegisterHandler wires a handler into the bus so subsequent Invoke calls
// can find it. Returns a disposer the caller should invoke at shutdown to
// remove the registration (useful in tests; harmless in production if the
// process exits).
func RegisterHandler[Req any, Resp any](b *bus.EventBus, handler InvokeHandler[Req, Resp]) (bus.IDisposable, error) {
	processable := bus.AddProcessor[Req, Resp](b)
	return processable(bus.Processor[Req, Resp](handler))
}
