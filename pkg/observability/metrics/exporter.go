package metrics

import (
	"encoding/json"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"p9e.in/samavaya/packages/p9log"
)

// PrometheusExporter exports metrics in Prometheus format.
// `logger` stores *p9log.Helper for the level-methods (B.1 sweep).
type PrometheusExporter struct {
	registry *prometheus.Registry
	logger   *p9log.Helper
}

// NewPrometheusExporter creates a new Prometheus exporter
func NewPrometheusExporter(collector *Collector, logger p9log.Logger) *PrometheusExporter {
	return &PrometheusExporter{
		registry: collector.GetRegistry(),
		logger:   p9log.NewHelper(logger),
	}
}

// ServeHTTP implements http.Handler for Prometheus metrics endpoint
func (pe *PrometheusExporter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	handler := promhttp.HandlerFor(pe.registry, promhttp.HandlerOpts{
		Registry: pe.registry,
	})
	handler.ServeHTTP(w, r)
}

// PrometheusHandler returns an HTTP handler for metrics
func PrometheusHandler(collector *Collector) http.Handler {
	return promhttp.HandlerFor(collector.GetRegistry(), promhttp.HandlerOpts{
		Registry: collector.GetRegistry(),
	})
}

// JSONExporter exports metrics in JSON format. `logger` stores *p9log.Helper.
type JSONExporter struct {
	collector *Collector
	logger    *p9log.Helper
}

// NewJSONExporter creates a new JSON exporter
func NewJSONExporter(collector *Collector, logger p9log.Logger) *JSONExporter {
	return &JSONExporter{
		collector: collector,
		logger:    p9log.NewHelper(logger),
	}
}

// ServeHTTP implements http.Handler for JSON metrics endpoint
func (je *JSONExporter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	snapshot, err := je.collector.GetSnapshot(r.Context())
	if err != nil {
		je.logger.Error("failed to get metrics snapshot", "error", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(snapshot)
}
