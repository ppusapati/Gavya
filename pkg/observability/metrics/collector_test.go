package metrics

import (
	"context"
	"testing"

	"p9e.in/samavaya/packages/p9log"
)

func TestCollectorCounter(t *testing.T) {
	logger := p9log.NoOp()
	collector := NewCollector("test", logger)

	counter := collector.Counter("requests_total", "Total requests", []string{"service"})
	counter.WithLabelValues("payment").Inc()
	counter.WithLabelValues("auth").Add(5)

	snapshot, _ := collector.GetSnapshot(context.Background())
	if len(snapshot.Metrics) == 0 {
		t.Fatal("Expected metrics in snapshot")
	}
}

func TestCollectorGauge(t *testing.T) {
	logger := p9log.NoOp()
	collector := NewCollector("test", logger)

	gauge := collector.Gauge("connections", "Active connections", []string{"service"})
	gauge.WithLabelValues("payment").Set(42)

	snapshot, _ := collector.GetSnapshot(context.Background())
	if len(snapshot.Metrics) == 0 {
		t.Fatal("Expected metrics in snapshot")
	}
}

func TestCollectorHistogram(t *testing.T) {
	logger := p9log.NoOp()
	collector := NewCollector("test", logger)

	histogram := collector.Histogram("request_duration_ms", "Request latency", []string{"service"})
	histogram.WithLabelValues("payment").Observe(150.5)
	histogram.WithLabelValues("auth").Observe(50.2)

	snapshot, _ := collector.GetSnapshot(context.Background())
	if len(snapshot.Metrics) == 0 {
		t.Fatal("Expected metrics in snapshot")
	}
}

func BenchmarkCollectorCounter(b *testing.B) {
	logger := p9log.NoOp()
	collector := NewCollector("bench", logger)

	counter := collector.Counter("requests", "Total requests", []string{})
	counterVec := counter.WithLabelValues()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		counterVec.Inc()
	}
}

func BenchmarkCollectorHistogram(b *testing.B) {
	logger := p9log.NoOp()
	collector := NewCollector("bench", logger)

	histogram := collector.Histogram("latency", "Request latency", []string{})
	histogramVec := histogram.WithLabelValues()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		histogramVec.Observe(float64(i % 1000))
	}
}
