package middleware

import (
	"context"
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// MonitoringMiddleware tracks metrics for RPC calls
type MonitoringMiddleware struct {
	requestCounter   prometheus.Counter
	requestDuration  prometheus.Histogram
	errorCounter     prometheus.Counter
	activeConnections prometheus.Gauge
}

// NewMonitoringMiddleware creates a new monitoring middleware
func NewMonitoringMiddleware(serviceName string) *MonitoringMiddleware {
	return &MonitoringMiddleware{
		requestCounter: promauto.NewCounter(prometheus.CounterOpts{
			Name: "rpc_requests_total",
			Help: "Total number of RPC requests",
			ConstLabels: prometheus.Labels{
				"service": serviceName,
			},
		}),
		requestDuration: promauto.NewHistogram(prometheus.HistogramOpts{
			Name: "rpc_request_duration_seconds",
			Help: "RPC request duration in seconds",
			ConstLabels: prometheus.Labels{
				"service": serviceName,
			},
			Buckets: prometheus.DefBuckets,
		}),
		errorCounter: promauto.NewCounter(prometheus.CounterOpts{
			Name: "rpc_errors_total",
			Help: "Total number of RPC errors",
			ConstLabels: prometheus.Labels{
				"service": serviceName,
			},
		}),
		activeConnections: promauto.NewGauge(prometheus.GaugeOpts{
			Name: "rpc_active_connections",
			Help: "Number of active RPC connections",
			ConstLabels: prometheus.Labels{
				"service": serviceName,
			},
		}),
	}
}

// UnaryInterceptor returns a unary RPC interceptor for monitoring
func (m *MonitoringMiddleware) UnaryInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (interface{}, error) {

		// Track active connections
		m.activeConnections.Inc()
		defer m.activeConnections.Dec()

		// Increment request counter
		m.requestCounter.Inc()

		// Measure duration
		start := time.Now()
		defer func() {
			duration := time.Since(start).Seconds()
			m.requestDuration.Observe(duration)
		}()

		// Call handler
		resp, err := handler(ctx, req)

		// Track errors
		if err != nil {
			m.errorCounter.Inc()

			st, _ := status.FromError(err)
			logger.Error("RPC error",
				slog.String("method", info.FullMethod),
				slog.String("code", st.Code().String()),
				slog.String("message", st.Message()),
				slog.String("duration", time.Since(start).String()),
			)
		} else {
			logger.Info("RPC success",
				slog.String("method", info.FullMethod),
				slog.String("duration", time.Since(start).String()),
			)
		}

		return resp, err
	}
}

// StreamInterceptor returns a stream RPC interceptor for monitoring
func (m *MonitoringMiddleware) StreamInterceptor(logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo,
		handler grpc.StreamHandler) error {

		m.activeConnections.Inc()
		defer m.activeConnections.Dec()

		m.requestCounter.Inc()

		start := time.Now()
		err := handler(srv, ss)

		duration := time.Since(start).Seconds()
		m.requestDuration.Observe(duration)

		if err != nil {
			m.errorCounter.Inc()
			st, _ := status.FromError(err)
			logger.Error("Stream RPC error",
				slog.String("method", info.FullMethod),
				slog.String("code", st.Code().String()),
				slog.String("duration", time.Since(start).String()),
			)
		}

		return err
	}
}

// LoggingInterceptor provides structured logging for RPC calls
func LoggingInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler) (interface{}, error) {

		logger.Info("RPC request",
			slog.String("method", info.FullMethod),
			slog.String("timestamp", time.Now().Format(time.RFC3339)),
		)

		resp, err := handler(ctx, req)

		if err != nil {
			st, _ := status.FromError(err)
			logger.Error("RPC failed",
				slog.String("method", info.FullMethod),
				slog.String("code", st.Code().String()),
				slog.String("message", st.Message()),
			)
		}

		return resp, err
	}
}

// HealthCheckMiddleware performs health checks
type HealthCheckMiddleware struct {
	checks map[string]HealthCheck
}

// HealthCheck is a health check function
type HealthCheck func(ctx context.Context) error

// NewHealthCheckMiddleware creates a new health check middleware
func NewHealthCheckMiddleware() *HealthCheckMiddleware {
	return &HealthCheckMiddleware{
		checks: make(map[string]HealthCheck),
	}
}

// RegisterCheck registers a health check
func (h *HealthCheckMiddleware) RegisterCheck(name string, check HealthCheck) {
	h.checks[name] = check
}

// RunChecks runs all health checks
func (h *HealthCheckMiddleware) RunChecks(ctx context.Context) map[string]error {
	results := make(map[string]error)
	for name, check := range h.checks {
		results[name] = check(ctx)
	}
	return results
}
