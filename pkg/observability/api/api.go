package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"p9e.in/samavaya/packages/p9log"
	"p9e.in/samavaya/packages/observability"
	"p9e.in/samavaya/packages/observability/alerts"
	"p9e.in/samavaya/packages/observability/graph"
	"p9e.in/samavaya/packages/observability/metrics"
	"p9e.in/samavaya/packages/observability/tracing"
)

// Server implements HTTP API for observability.
// `logger` is *p9log.Helper (B.1 sweep).
type Server struct {
	collector   *metrics.Collector
	tracer      *tracing.Tracer
	tracker     *graph.Tracker
	engine      *alerts.Engine
	logger      *p9log.Helper
	mux         *http.ServeMux
}

// New creates a new observability API server
func New(
	collector *metrics.Collector,
	tracer *tracing.Tracer,
	tracker *graph.Tracker,
	engine *alerts.Engine,
	logger p9log.Logger,
) *Server {
	s := &Server{
		collector: collector,
		tracer:    tracer,
		tracker:   tracker,
		engine:    engine,
		logger:    p9log.NewHelper(logger),
		mux:       http.NewServeMux(),
	}

	// Register handlers
	s.mux.HandleFunc("GET /v1/metrics/snapshot", s.handleMetricsSnapshot)
	s.mux.HandleFunc("GET /v1/tracing/traces", s.handleGetTraces)
	s.mux.HandleFunc("GET /v1/tracing/traces/{trace_id}", s.handleGetTrace)
	s.mux.HandleFunc("GET /v1/dependencies", s.handleGetDependencies)
	s.mux.HandleFunc("GET /v1/dependencies/graph", s.handleGetDependencyGraph)
	s.mux.HandleFunc("GET /v1/alerts/active", s.handleGetActiveAlerts)
	s.mux.HandleFunc("GET /v1/alerts/{alert_id}", s.handleGetAlert)

	return s
}

// ServeHTTP implements http.Handler
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// Response types

type MetricsResponse struct {
	Timestamp time.Time               `json:"timestamp"`
	Metrics   []observability.Metric  `json:"metrics"`
}

type TraceResponse struct {
	TraceID    string    `json:"trace_id"`
	RootSpanID string    `json:"root_span_id"`
	Service    string    `json:"service"`
	Operation  string    `json:"operation"`
	StartTime  string    `json:"start_time"`
	EndTime    string    `json:"end_time"`
	Duration   string    `json:"duration"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	SpanCount  int       `json:"span_count"`
}

type DependenciesResponse struct {
	Service        string                      `json:"service"`
	Dependencies   []DependencyResponse        `json:"dependencies"`
	Depth          int                         `json:"depth"`
	HasCircular    bool                        `json:"has_circular"`
	GeneratedAt    string                      `json:"generated_at"`
}

type DependencyResponse struct {
	Service        string    `json:"service"`
	CallCount      int64     `json:"call_count"`
	SuccessCount   int64     `json:"success_count"`
	ErrorCount     int64     `json:"error_count"`
	SuccessRate    int       `json:"success_rate"`
	ErrorRate      int       `json:"error_rate"`
	AvgLatency     int64     `json:"avg_latency_ms"`
	P99Latency     int64     `json:"p99_latency_ms"`
	LastCallTime   string    `json:"last_call_time"`
}

type AlertResponse struct {
	Name       string    `json:"name"`
	Severity   string    `json:"severity"`
	Status     string    `json:"status"`
	Message    string    `json:"message"`
	Service    string    `json:"service,omitempty"`
	Value      float64   `json:"value"`
	FiredAt    string    `json:"fired_at"`
	ResolvedAt *string   `json:"resolved_at,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// Handlers

func (s *Server) handleMetricsSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	snapshot, err := s.collector.GetSnapshot(ctx)
	if err != nil {
		s.logger.Error("failed to get metrics snapshot", "error", err)
		s.writeError(w, http.StatusInternalServerError, "Failed to get metrics snapshot")
		return
	}

	resp := MetricsResponse{
		Timestamp: snapshot.Timestamp,
		Metrics:   snapshot.Metrics,
	}

	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetTraces(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	traces := s.tracer.GetAllTraces(ctx)

	resp := make([]TraceResponse, len(traces))
	for i, trace := range traces {
		resp[i] = TraceResponse{
			TraceID:    trace.TraceID,
			RootSpanID: trace.RootSpanID,
			Service:    trace.Service,
			Operation:  trace.Operation,
			StartTime:  trace.StartTime.Format(time.RFC3339),
			EndTime:    trace.EndTime.Format(time.RFC3339),
			Duration:   trace.Duration.String(),
			Status:     trace.Status,
			Error:      trace.Error,
			SpanCount:  len(trace.Spans),
		}
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"traces": resp,
		"count":  len(resp),
	})
}

func (s *Server) handleGetTrace(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	traceID := r.PathValue("trace_id")
	if traceID == "" {
		s.writeError(w, http.StatusBadRequest, "trace_id is required")
		return
	}

	trace, err := s.tracer.GetTrace(ctx, traceID)
	if err != nil {
		s.logger.Error("failed to get trace",
			"trace_id", traceID,
			"error", err,
		)
		s.writeError(w, http.StatusNotFound, "Trace not found")
		return
	}

	resp := TraceResponse{
		TraceID:    trace.TraceID,
		RootSpanID: trace.RootSpanID,
		Service:    trace.Service,
		Operation:  trace.Operation,
		StartTime:  trace.StartTime.Format(time.RFC3339),
		EndTime:    trace.EndTime.Format(time.RFC3339),
		Duration:   trace.Duration.String(),
		Status:     trace.Status,
		Error:      trace.Error,
		SpanCount:  len(trace.Spans),
	}

	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetDependencies(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	service := r.URL.Query().Get("service")
	if service == "" {
		s.writeError(w, http.StatusBadRequest, "service parameter is required")
		return
	}

	deps := s.tracker.GetDependencies(ctx, service)

	respDeps := make([]DependencyResponse, len(deps.Dependencies))
	for i, dep := range deps.Dependencies {
		respDeps[i] = DependencyResponse{
			Service:      dep.Service,
			CallCount:    dep.CallCount,
			SuccessCount: dep.SuccessCount,
			ErrorCount:   dep.ErrorCount,
			SuccessRate:  dep.SuccessRate,
			ErrorRate:    dep.ErrorRate,
			AvgLatency:   dep.AvgLatency,
			P99Latency:   dep.P99Latency,
			LastCallTime: dep.LastCallTime.Format(time.RFC3339),
		}
	}

	resp := DependenciesResponse{
		Service:      deps.Service,
		Dependencies: respDeps,
		Depth:        deps.Depth,
		HasCircular:  deps.HasCircular,
		GeneratedAt:  deps.GeneratedAt.Format(time.RFC3339),
	}

	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetDependencyGraph(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	graph := s.tracker.GetDependencyGraph(ctx)

	resp := make(map[string]DependenciesResponse)
	for service, deps := range graph {
		respDeps := make([]DependencyResponse, len(deps.Dependencies))
		for i, dep := range deps.Dependencies {
			respDeps[i] = DependencyResponse{
				Service:      dep.Service,
				CallCount:    dep.CallCount,
				SuccessCount: dep.SuccessCount,
				ErrorCount:   dep.ErrorCount,
				SuccessRate:  dep.SuccessRate,
				ErrorRate:    dep.ErrorRate,
				AvgLatency:   dep.AvgLatency,
				P99Latency:   dep.P99Latency,
				LastCallTime: dep.LastCallTime.Format(time.RFC3339),
			}
		}

		resp[service] = DependenciesResponse{
			Service:      deps.Service,
			Dependencies: respDeps,
			Depth:        deps.Depth,
			HasCircular:  deps.HasCircular,
			GeneratedAt:  deps.GeneratedAt.Format(time.RFC3339),
		}
	}

	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetActiveAlerts(w http.ResponseWriter, r *http.Request) {
	// Bound the handler so a slow engine call can't tie up a request thread.
	// The alert engine's GetActiveAlerts is in-memory and doesn't accept a
	// ctx, so we use `_` here; the deadline still applies via cancel().
	_, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	activeAlerts := s.engine.GetActiveAlerts()

	resp := make([]AlertResponse, len(activeAlerts))
	for i, alert := range activeAlerts {
		resolvedAt := (*string)(nil)
		if alert.ResolvedAt != nil {
			t := alert.ResolvedAt.Format(time.RFC3339)
			resolvedAt = &t
		}

		resp[i] = AlertResponse{
			Name:       alert.Name,
			Severity:   string(alert.Severity),
			Status:     string(alert.Status),
			Message:    alert.Message,
			Service:    alert.Service,
			Value:      alert.Value,
			FiredAt:    alert.FiredAt.Format(time.RFC3339),
			ResolvedAt: resolvedAt,
			Labels:     alert.Labels,
		}
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"alerts": resp,
		"count":  len(resp),
	})
}

func (s *Server) handleGetAlert(w http.ResponseWriter, r *http.Request) {
	// See handleGetActiveAlerts — ctx is discarded because the in-memory
	// engine helpers don't accept one; the deadline still bounds the request.
	_, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	alertID := r.PathValue("alert_id")
	if alertID == "" {
		s.writeError(w, http.StatusBadRequest, "alert_id is required")
		return
	}

	alert := s.engine.GetAlert(alertID)
	if alert == nil {
		s.writeError(w, http.StatusNotFound, "Alert not found")
		return
	}

	resolvedAt := (*string)(nil)
	if alert.ResolvedAt != nil {
		t := alert.ResolvedAt.Format(time.RFC3339)
		resolvedAt = &t
	}

	resp := AlertResponse{
		Name:       alert.Name,
		Severity:   string(alert.Severity),
		Status:     string(alert.Status),
		Message:    alert.Message,
		Service:    alert.Service,
		Value:      alert.Value,
		FiredAt:    alert.FiredAt.Format(time.RFC3339),
		ResolvedAt: resolvedAt,
		Labels:     alert.Labels,
	}

	s.writeJSON(w, http.StatusOK, resp)
}

// Helper methods

func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error:   "error",
		Message: message,
	})
}
