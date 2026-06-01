package consumer

import (
	"sync/atomic"

	"p9e.in/samavaya/packages/api/v1/message"
	"p9e.in/samavaya/packages/p9log"

	"github.com/IBM/sarama"
	"google.golang.org/protobuf/proto"
)

type KafkaHandler struct {
	KafkaMessages chan *message.EventMessage
	Consumer      *KafkaConsumer
	log           p9log.Logger
	processedMsgs atomic.Uint64
}

func (h *KafkaHandler) Setup(_ sarama.ConsumerGroupSession) error {
	return nil
}

func (h *KafkaHandler) Cleanup(_ sarama.ConsumerGroupSession) error {
	return nil
}

func (h *KafkaHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for msg := range claim.Messages() {
		m := message.EventMessage{}
		errs := proto.Unmarshal(msg.Value, &m)
		if errs != nil {
			h.log.Log(p9log.LevelError, errs)
			continue
		}

		h.KafkaMessages <- &m
		session.MarkMessage(msg, "")

		if h.Consumer != nil {
			h.Consumer.updateCommittedOffset(&m)
		}

		// Track processed messages atomically
		h.processedMsgs.Add(1)
	}

	return nil
}

// GetProcessedMessageCount returns the number of processed messages
func (h *KafkaHandler) GetProcessedMessageCount() uint64 {
	return h.processedMsgs.Load()
}
