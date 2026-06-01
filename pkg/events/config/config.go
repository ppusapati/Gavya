// config/config.go
package config

import (
	"time"

	"p9e.in/samavaya/packages/api/v1/config"
	"p9e.in/samavaya/packages/p9log"

	"github.com/IBM/sarama"
)

// KafkaConfig contains Kafka configuration
type KafkaConfig struct {
	Broker             string
	Group              string
	Topic              []string
	KafkaVersion       string
	Assignor           string
	OffsetOldest       bool
	RetryMax           int32
	RetryInterval      int32
	ConsumptionTimeout time.Duration
	Success            func(int32, int64)
	Error              func(error)
	RequiredAcks       sarama.RequiredAcks
}

// Default success handler
var DefaultSuccessHandler = func(partition int32, offset int64) {
	p9log.Infof("Default: Message sent to partition %d with offset %d", partition, offset)
}

// Default error handler
var DefaultErrorHandler = func(err error) {
	p9log.Error("Default: Failed to send/consume message: ", err)
}

func LoadConfig(data *config.Data) *KafkaConfig {
	return &KafkaConfig{
		Broker:             data.Event.Broker,
		Group:              data.Event.Group,
		Topic:              data.Event.Topic,
		KafkaVersion:       data.Event.KafkaVersion,
		Assignor:           data.Event.Assignor,
		OffsetOldest:       data.Event.OffsetOldest,
		RetryMax:           data.Event.RetryMax,
		RetryInterval:      data.Event.RetryInterval,
		ConsumptionTimeout: 0, // Initialize ConsumptionTimeout to 0
		Success:            DefaultSuccessHandler,
		Error:              DefaultErrorHandler,
		RequiredAcks:       sarama.WaitForAll,
	}
}
