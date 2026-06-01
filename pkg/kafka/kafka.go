// Package kafka is the minimal PORT that 184 service packages depend on to
// publish events without pulling the Sarama client into their go.mod
// dependency graph. It defines the Producer interface and a set of
// well-known topic constants (see topics.go).
//
// For the Sarama-backed ADAPTER that actually connects to Kafka (configured
// broker, retry policy, metrics, proto-wrapped EventMessage), see
// packages/events/producer.KafkaProducer. The composition root wires the
// concrete producer and binds it to this interface via fx so consumers
// stay decoupled from the client library.
//
// This is the ports-and-adapters (ADR-0003) layering; the two packages are
// NOT duplicates. Confirmed during the 2026-04-19 packages audit (roadmap
// task B.5).
package kafka

import "context"

// Producer is the interface for publishing messages to Kafka topics.
type Producer interface {
	Produce(ctx context.Context, topic string, data string) error
}
