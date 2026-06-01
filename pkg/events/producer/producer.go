// Package producer is the Sarama-backed Kafka ADAPTER. It owns the
// concrete client connection, the retry policy, and the
// produced/failed-message counters. Consumers inject the interface from
// packages/kafka (the PORT) and only see a `kafka.Producer` — never this
// type — keeping 184 service packages free of a Sarama dependency.
//
// This layering follows ADR-0003 (ports-and-adapters). The relationship
// with packages/kafka was confirmed non-duplicate during the 2026-04-19
// packages audit (roadmap task B.5).
package producer

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	msgp "p9e.in/samavaya/packages/api/v1/message"
	"p9e.in/samavaya/packages/events/config"
	"p9e.in/samavaya/packages/p9log"

	"github.com/IBM/sarama"
	"google.golang.org/protobuf/proto"
)

// Producer represents a Kafka message producer
type KafkaProducer struct {
	config       *config.KafkaConfig
	producer     sarama.SyncProducer
	log          p9log.Helper
	shutdown     atomic.Bool
	producedMsgs atomic.Uint64
	failedMsgs   atomic.Uint64
}

func NewKafkaProducer(config *config.KafkaConfig, log p9log.Logger) *KafkaProducer {
	config.RequiredAcks = sarama.WaitForAll // Adjust as needed
	producer, err := sarama.NewSyncProducer([]string{config.Broker}, nil)
	if err != nil {
		return nil
	}
	return &KafkaProducer{
		config:   config,
		producer: producer,
		log:      *p9log.NewHelper(p9log.With(log, "Kafka Producer")),
	}
}

func (p *KafkaProducer) Close() {
	if p.shutdown.CompareAndSwap(false, true) {
		if err := p.producer.Close(); err != nil {
			p.log.Errorf("Error closing Kafka producer: %v", err)
		}
	}
}

func (p *KafkaProducer) ProduceMessage(ctx context.Context, message *msgp.EventMessage) error {
	if p.shutdown.Load() {
		return fmt.Errorf("producer is shutting down")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		retryInterval := time.Duration(p.config.RetryInterval) * time.Second
		mesg, err := proto.Marshal(message)
		if err != nil {
			p.failedMsgs.Add(1)
			return err
		}
		msg := &sarama.ProducerMessage{
			Topic:     message.Topic,
			Partition: 0,
			Key:       sarama.ByteEncoder(message.Key),
			Value:     sarama.ByteEncoder(mesg),
		}
		for attempt := 1; attempt <= int(p.config.RetryMax); attempt++ {
			// Send the message
			partition, offset, err := p.producer.SendMessage(msg)
			if err != nil {
				p.log.Errorf("Error sending message (attempt %d): %v", attempt, err)
				p.failedMsgs.Add(1)

				// Check if shutdown is in progress
				if p.shutdown.Load() {
					return fmt.Errorf("producer shutdown during retry")
				}

				// Retry the message after a delay
				if attempt < int(p.config.RetryMax) {
					time.Sleep(retryInterval)
				} else {
					return err // Exceeded max retries
				}
			} else {
				// Handle success
				p.log.Infof("Message sent successfully to partition %d at offset %d", partition, offset)
				p.producedMsgs.Add(1)
				return nil
			}
		}
		return nil
	}
}

// GetProducedMessageCount returns the number of successfully produced messages
func (p *KafkaProducer) GetProducedMessageCount() uint64 {
	return p.producedMsgs.Load()
}

// GetFailedMessageCount returns the number of failed message productions
func (p *KafkaProducer) GetFailedMessageCount() uint64 {
	return p.failedMsgs.Load()
}
