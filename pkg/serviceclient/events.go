package serviceclient

import (
	"context"

	"p9e.in/samavaya/packages/events/bus"
)

// Publisher[E] fire-and-forgets a typed event. Monolith mode uses the
// in-process bus; split mode swaps for a Kafka publisher. Callers see one
// interface.
type Publisher[E any] interface {
	Publish(ctx context.Context, event E) error
}

// Subscriber[E] hooks a handler onto the event stream for E. Multiple
// subscribers on the same E fan out in the order they registered; a single
// subscriber error aborts fan-out to later subscribers (this matches the
// underlying bus semantics — see packages/events/bus/bus.go publish()).
//
// Subscribe returns a disposer for unregistration. Callers don't need to
// hold onto it in production (process exit cleans up); tests should call
// Dispose to keep state clean between runs.
type Subscriber[E any] interface {
	Subscribe(handler func(ctx context.Context, event E) error) (bus.IDisposable, error)
}

// ============================================================================
// In-process implementation
// ============================================================================

type inprocPublisher[E any] struct {
	publish bus.PublisherFunc[E]
}

func (p *inprocPublisher[E]) Publish(ctx context.Context, event E) error {
	return p.publish(ctx, event)
}

// NewInProcPublisher builds an in-process Publisher backed by the given bus.
func NewInProcPublisher[E any](b *bus.EventBus) Publisher[E] {
	return &inprocPublisher[E]{publish: bus.Publish[E](b)}
}

type inprocSubscriber[E any] struct {
	subscribe bus.SubscribeFunc[E]
}

func (s *inprocSubscriber[E]) Subscribe(handler func(ctx context.Context, event E) error) (bus.IDisposable, error) {
	return s.subscribe(bus.Handler[E](handler))
}

// NewInProcSubscriber builds an in-process Subscriber backed by the given bus.
func NewInProcSubscriber[E any](b *bus.EventBus) Subscriber[E] {
	return &inprocSubscriber[E]{subscribe: bus.Subscribe[E](b)}
}
