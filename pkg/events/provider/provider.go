package provider

import (
	"p9e.in/samavaya/packages/events/config"
	"p9e.in/samavaya/packages/events/consumer"
	"p9e.in/samavaya/packages/events/handler"
	"p9e.in/samavaya/packages/events/producer"
)

// Kafka constructors for dependency injection.
// These can be used directly or with DI frameworks like Uber FX.
var KafkaConstructors = []interface{}{
	config.LoadConfig,
	producer.NewKafkaProducer,
	consumer.NewKafkaConsumer,
	handler.NewKafkaHandler,
}
