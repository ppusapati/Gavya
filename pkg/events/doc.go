// Package events is the domain-event publishing layer. It wraps the
// in-process EventBus (packages/events/bus) with a consistent publisher
// interface plus Kafka fan-out via packages/events/producer.
//
// EventPublisher is the canonical interface services depend on:
//
//	type EventPublisher interface {
//	    Publish(ctx context.Context, event domain.Event) error
//	}
//
// The EventBusWrapper adapter lets services publish to both the in-process
// bus (for synchronous saga orchestration + test observability) and the
// Kafka producer (for cross-service fan-out) through a single call site.
// Per-subscriber failures do NOT block the publish path — the wrapper
// fans out fire-and-forget with structured logs on failure.
//
// Subpackages:
//
//   - events/bus       — in-process pub/sub event bus
//   - events/consumer  — Kafka consumer group wiring
//   - events/producer  — Sarama-backed Kafka producer (binds the kafka.Producer port)
//   - events/config    — broker / topic / consumer-group configuration
//   - events/handler   — base handler struct that services embed
//
// See ADR-0003 § Async communication for the event-publication contract.
package events
