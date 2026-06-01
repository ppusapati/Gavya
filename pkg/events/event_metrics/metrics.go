package event_metrics

import (
	"sync"
	"time"
)

type EventMetrics struct {
	mu                sync.Mutex
	processingTime    time.Duration
	failedMessages    int64
	processedMessages int64
}

func (m *EventMetrics) UpdateProcessingTime(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processingTime = duration
}

func (m *EventMetrics) IncrementFailedMessages() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failedMessages++
}

func (m *EventMetrics) IncrementProcessedMessages() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processedMessages++
}
func (m *EventMetrics) GetProcessedMessageCount() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.processedMessages
}

func (m *EventMetrics) GetFailedMessageCount() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.failedMessages
}

func (m *EventMetrics) GetAverageProcessingTime() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.processingTime
}
