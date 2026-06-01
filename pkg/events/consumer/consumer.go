package consumer

import (
	"context"
	"sync/atomic"

	"p9e.in/samavaya/packages/api/v1/message"
	"p9e.in/samavaya/packages/events/config"
	"p9e.in/samavaya/packages/events/handler"
	"p9e.in/samavaya/packages/p9log"

	"github.com/IBM/sarama"
)

type KafkaConsumer struct {
	config     *config.KafkaConfig
	log        p9log.Helper
	committed  atomic.Value
	shutdown   atomic.Bool
	consumerCh chan sarama.ConsumerGroup
}

func NewKafkaConsumer(config *config.KafkaConfig, lg p9log.Logger) *KafkaConsumer {
	kc := &KafkaConsumer{
		config:     config,
		log:        *p9log.NewHelper(p9log.With(lg, "caller", "Kafka Consumer")),
		consumerCh: make(chan sarama.ConsumerGroup, 1),
	}
	kc.committed.Store(make(map[string]map[int32]int64))
	return kc
}

func (kc *KafkaConsumer) ConsumerGroup(groupName string) sarama.ConsumerGroup {
	if kc.shutdown.Load() {
		return nil
	}

	consumerConfig := kc.createConsumerConfig()
	consumer, err := sarama.NewConsumerGroup([]string{kc.config.Broker}, groupName, consumerConfig)
	if err != nil {
		kc.log.Fatalf("Error creating Kafka consumer group: %v", err)
	}

	kc.consumerCh <- consumer
	return consumer
}

func (kc *KafkaConsumer) Consume(ctx context.Context, kafkaMessages chan *message.EventMessage) {
	ctx = kc.watchSignals(ctx)

	consumer := kc.ConsumerGroup(kc.config.Group)
	defer consumer.Close()

	for _, topic := range kc.config.Topic {
		kc.ConsumingTopic(ctx, consumer, topic, kafkaMessages)
	}

	<-ctx.Done()
	kc.log.Info("Kafka consumer shutdown initiated")
}

func (kc *KafkaConsumer) ConsumingTopic(ctx context.Context, consumer sarama.ConsumerGroup, topic string, kafkaMessages chan *message.EventMessage) {
	handler := &handler.KafkaHandler{
		KafkaMessages: kafkaMessages,
		// Consumer:      kc,
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				if err := consumer.Consume(ctx, []string{topic}, handler); err != nil {
					kc.log.Errorf("Error consuming topic %s: %v", topic, err)
				}
			}
		}
	}()
}

func (kc *KafkaConsumer) updateCommittedOffset(m *message.EventMessage) {
	committed := kc.committed.Load().(map[string]map[int32]int64)

	if committed[m.Topic] == nil {
		committed[m.Topic] = make(map[int32]int64)
	}

	if committed[m.Topic][m.Partition] < m.Offset {
		committed[m.Topic][m.Partition] = m.Offset
		kc.committed.Store(committed)
	}
}

func (kc *KafkaConsumer) watchSignals(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)

	go func() {
		<-ctx.Done()
		kc.shutdown.Store(true)
		close(kc.consumerCh)
		cancel()
	}()

	return ctx
}

func (kc *KafkaConsumer) Cleanup() {
	kc.shutdown.Store(true)
}

func (kc *KafkaConsumer) createConsumerConfig() *sarama.Config {
	config := sarama.NewConfig()
	config.Version, _ = sarama.ParseKafkaVersion(kc.config.KafkaVersion)

	switch kc.config.Assignor {
	case "sticky":
		config.Consumer.Group.Rebalance.Strategy = sarama.NewBalanceStrategySticky()
	case "roundrobin":
		config.Consumer.Group.Rebalance.Strategy = sarama.NewBalanceStrategyRoundRobin()
	case "range":
		config.Consumer.Group.Rebalance.Strategy = sarama.NewBalanceStrategyRange()
	default:
		kc.log.Errorf("Unrecognized consumer group partition assignor: %s", kc.config.Assignor)
	}

	return config
}
