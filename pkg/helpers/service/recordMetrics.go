package helpers_service

import (
	"time"

	"p9e.in/samavaya/packages/metrics"
)

// Utility function to record execution time in metrics
func recordMetric(metrics metrics.MetricsProvider, entityType, action string, startTime time.Time, success bool) {
	metrics.RecordDBOperation(entityType+action, time.Since(startTime), success)
}
