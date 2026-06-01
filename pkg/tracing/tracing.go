// Package tracing is the production-grade OpenTelemetry tracing provider.
// It wires the global OTel tracer to Jaeger / Zipkin / OTLP exporters based
// on config and exposes StartSpan / AddSpanTags / AddSpanError / etc. used
// by helpers/service to instrument every ServiceDeps call.
//
// For the in-memory trace BROWSER (GetTrace / GetAllTraces / PruneTraces used
// by observability/api to render recent traces in a debug UI), see
// packages/observability/tracing. The two coexist intentionally — this
// package emits spans to external backends; observability/tracing keeps a
// short-window copy in process memory so operators can inspect without
// leaving the admin dashboard. Confirmed non-duplicate during the
// 2026-04-19 packages audit (roadmap task B.4).
package tracing

import (
	"context"
	"fmt"
	"os"

	"p9e.in/samavaya/packages/api/v1/config"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/zipkin"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	otelTrace "go.opentelemetry.io/otel/trace"
)

const (
	// defaultServiceName is used when no service name is provided in configuration
	defaultServiceName = "unknown-service"
	// defaultServiceVersion is used when no version is provided
	defaultServiceVersion = "v0.0.0"
)

// TracingProvider manages the lifecycle of tracing
type TracingProvider struct {
	tracer         otelTrace.Tracer
	tracerProvider *trace.TracerProvider
	cfg            *config.Observability
	serviceName    string
}

// NewProvider creates a new tracing provider
func NewProvider(cfg *config.Observability) (*TracingProvider, error) {
	return NewProviderWithContext(context.Background(), cfg)
}

// NewProviderWithContext creates a new tracing provider with the given context
func NewProviderWithContext(ctx context.Context, cfg *config.Observability) (*TracingProvider, error) {
	// If no configuration is provided, use default
	if cfg == nil {
		cfg = &config.Observability{
			Tracing: &config.Observability_Tracing{
				Provider:     config.Observability_Tracing_JAEGER,
				Endpoint:     "http://localhost:14268/api/traces",
				SamplingRate: 0.5,
				Enabled:      true,
				Verbosity:    config.Observability_Tracing_INFO,
			},
		}
	}

	// Determine service name from config, environment, or default
	svcName := resolveServiceName(cfg)

	// Create a resource with service name
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(svcName),
			semconv.ServiceVersion(defaultServiceVersion),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Check if tracing is enabled
	if !cfg.Tracing.Enabled {
		return &TracingProvider{
			cfg:         cfg,
			serviceName: svcName,
		}, nil
	}

	// Select tracing provider
	var exporter trace.SpanExporter
	switch cfg.Tracing.Provider {
	case config.Observability_Tracing_JAEGER:
		exporter, err = jaeger.New(jaeger.WithCollectorEndpoint(
			jaeger.WithEndpoint(cfg.Tracing.Endpoint),
		))
	case config.Observability_Tracing_ZIPKIN:
		exporter, err = zipkin.New(cfg.Tracing.Endpoint)
	case config.Observability_Tracing_OTLP:
		client := otlptracehttp.NewClient(
			otlptracehttp.WithEndpoint(cfg.Tracing.Endpoint),
		)
		exporter, err = otlptrace.New(ctx, client)
	default:
		return nil, fmt.Errorf("unsupported tracing provider: %v", cfg.Tracing.Provider)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create tracing exporter: %w", err)
	}

	// Create TracerProvider with sampling
	tracerProvider := trace.NewTracerProvider(
		trace.WithSampler(trace.ParentBased(trace.TraceIDRatioBased(float64(cfg.Tracing.SamplingRate)))),
		trace.WithBatcher(exporter),
		trace.WithResource(res),
	)

	// Set the global tracer provider
	otel.SetTracerProvider(tracerProvider)

	// Create tracer
	tracer := tracerProvider.Tracer(svcName)

	return &TracingProvider{
		tracer:         tracer,
		tracerProvider: tracerProvider,
		cfg:            cfg,
		serviceName:    svcName,
	}, nil
}

// resolveServiceName determines the service name from config, environment, or default
func resolveServiceName(cfg *config.Observability) string {
	// First try config
	if cfg != nil && cfg.ServiceName != nil && cfg.ServiceName.ServiceName != "" {
		return cfg.ServiceName.ServiceName
	}

	// Then try environment variable
	if envName := os.Getenv("SERVICE_NAME"); envName != "" {
		return envName
	}
	if envName := os.Getenv("OTEL_SERVICE_NAME"); envName != "" {
		return envName
	}

	// Fall back to default
	return defaultServiceName
}

// Shutdown stops the tracing provider
func (p *TracingProvider) Shutdown(ctx context.Context) error {
	if p.tracerProvider != nil {
		return p.tracerProvider.Shutdown(ctx)
	}
	return nil
}

// StartSpan starts a new span and returns the context with the span
func (p *TracingProvider) StartSpan(ctx context.Context, name string) (context.Context, otelTrace.Span) {
	tracer := p.tracer
	return tracer.Start(ctx, name)
}

// AddSpanTags adds tags to the current span
func (p *TracingProvider) AddSpanTags(ctx context.Context, tags map[string]string) {
	span := p.SpanFromContext(ctx)
	for k, v := range tags {
		span.SetAttributes(attribute.String(k, v))
	}
}

// AddSpanError adds an error to the current span
func (p *TracingProvider) AddSpanError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	span := p.SpanFromContext(ctx)
	span.RecordError(err)
}

// AddSpanEvent adds an event to the current span
func (p *TracingProvider) AddSpanEvent(ctx context.Context, name string, attributes map[string]string) {
	span := p.SpanFromContext(ctx)
	attrs := make([]attribute.KeyValue, 0, len(attributes))
	for k, v := range attributes {
		attrs = append(attrs, attribute.String(k, v))
	}
	span.AddEvent(name, otelTrace.WithAttributes(attrs...))
}

// SpanFromContext returns the current span from context
func (p *TracingProvider) SpanFromContext(ctx context.Context) otelTrace.Span {
	return otelTrace.SpanFromContext(ctx)
}

// TraceID returns the trace ID from context
func (p *TracingProvider) TraceID(ctx context.Context) string {
	span := p.SpanFromContext(ctx)
	if !span.SpanContext().HasTraceID() {
		return ""
	}
	return span.SpanContext().TraceID().String()
}

// SpanID returns the span ID from context
func (p *TracingProvider) SpanID(ctx context.Context) string {
	span := p.SpanFromContext(ctx)
	if !span.SpanContext().HasSpanID() {
		return ""
	}
	return span.SpanContext().SpanID().String()
}
