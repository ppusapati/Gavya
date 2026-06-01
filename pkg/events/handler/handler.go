package handler

import (
	msgp "p9e.in/samavaya/packages/api/v1/message"
	"p9e.in/samavaya/packages/p9log"

	"github.com/IBM/sarama"
	"google.golang.org/protobuf/proto"
)

type KafkaHandler struct {
	KafkaMessages chan *msgp.EventMessage
	committed     map[string]map[int32]int64
	log           p9log.Helper
}

// NewKafkaHandler creates a new KafkaHandler instance.
func NewKafkaHandler(lg p9log.Logger) *KafkaHandler {
	return &KafkaHandler{
		KafkaMessages: make(chan *msgp.EventMessage),
		committed:     make(map[string]map[int32]int64),
		log:           *p9log.NewHelper(p9log.With(lg, "caller", "Kafka Consumer")),
	}
}

// Setup is called when a new session is established.
func (h *KafkaHandler) Setup(_ sarama.ConsumerGroupSession) error {
	return nil
}

// Cleanup is called when a session is closed.
func (h *KafkaHandler) Cleanup(_ sarama.ConsumerGroupSession) error {
	return nil
}

// ConsumeClaim handles the consumption of Kafka messages.
func (h *KafkaHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		// De-serialize the message
		m := msgp.EventMessage{}
		errs := proto.Unmarshal(msg.Value, &m)
		if errs != nil {
			h.log.Log(p9log.LevelError, errs)
		}
		h.KafkaMessages <- &m
		session.MarkMessage(msg, "")

		// Initialize the outer map if it's nil
		if h.committed == nil {
			h.committed = make(map[string]map[int32]int64)
		}

		// Initialize the inner map if it's nil
		if h.committed[m.Topic] == nil {
			h.committed[m.Topic] = make(map[int32]int64)
		}

		// Use a lock-free check to create or update the committed offset
		if h.committed[m.Topic][m.Partition] < m.Offset {
			h.committed[m.Topic][m.Partition] = m.Offset
		}
	}

	return nil
}

// IsOffsetCommitted checks if an offset is committed for a specific topic and partition.
func (h *KafkaHandler) IsOffsetCommitted(topic string, partition int32, offset int64) bool {
	if offsets, found := h.committed[topic]; found {
		return offsets[partition] >= offset
	}
	return false
}
