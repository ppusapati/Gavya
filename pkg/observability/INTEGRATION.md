# Observability Integration Guide

## Overview

The observability package provides comprehensive monitoring for microservices with metrics, distributed tracing, dependency graphs, and alerting.

## Quick Start

```go
import (
    "p9e.in/samavaya/packages/packages/observability/metrics"
    "p9e.in/samavaya/packages/packages/observability/tracing"
    "p9e.in/samavaya/packages/packages/observability/graph"
    "p9e.in/samavaya/packages/packages/observability/alerts"
    "p9e.in/samavaya/packages/packages/observability/api"
)

// Create components
collector := metrics.NewCollector("myservice", logger)
tracer := tracing.NewTracer("payment-service", logger)
tracker := graph.NewTracker(logger)
engine := alerts.NewEngine(collector, logger)

// Create API server
server := api.New(collector, tracer, tracker, engine, logger)
http.Handle("/v1/", server)

// Start collecting metrics
httpCounter := collector.Counter("http_requests_total", "Total HTTP requests", []string{"method", "path"})
httpCounter.WithLabelValues("POST", "/api/pay").Inc()

// Start tracing requests
span := tracer.StartSpan(ctx, "process_payment")
defer span.End()
span.SetAttribute("payment_id", "pay-123")

// Track dependencies
tracker.RecordCall(ctx, "payment-service", "auth-service", "VerifyToken", 50*time.Millisecond, true)

// Add alerts
engine.AddRule(observability.AlertRule{
    Name:      "high_error_rate",
    Metric:    "http_requests_errors_total",
    Op:        ">",
    Threshold: 100,
    Duration:  5 * time.Minute,
})

go engine.Start(ctx, 10*time.Second)
```

## Metrics

Collect Prometheus-compatible metrics:

```go
collector := metrics.NewCollector("payment-service", logger)

// Counter - monotonically increasing
counter := collector.Counter("payments_processed", "Payments processed", []string{"currency"})
counter.WithLabelValues("USD").Inc()
counter.WithLabelValues("EUR").Add(5)

// Gauge - can go up or down
gauge := collector.Gauge("active_connections", "Active connections", []string{"service"})
gauge.WithLabelValues("db").Set(42)
gauge.WithLabelValues("cache").Set(15)

// Histogram - track latency distribution
histogram := collector.Histogram("request_duration_ms", "Request latency", []string{"endpoint"})
histogram.WithLabelValues("/api/pay").Observe(125.5)
histogram.WithLabelValues("/api/refund").Observe(87.2)

// Summary - percentile calculations
summary := collector.Summary("transaction_amount", "Transaction amounts", []string{})
summary.WithLabelValues().Observe(1500.00)

// Export metrics
http.Handle("/metrics", metrics.PrometheusHandler(collector))
```

### Metrics Best Practices

```go
// Use descriptive names (snake_case with _total suffix for counters)
requests_total           // Counter of all requests
requests_duration_ms     // Histogram of request latency
active_connections       // Gauge of active connections
cache_hit_ratio          // Gauge of cache hit percentage

// Use meaningful labels
counter := collector.Counter("requests_total", "...", []string{"service", "method", "status"})
counter.WithLabelValues("payment", "POST", "200").Inc()

// Track business metrics
payments_processed       // Total payments
payment_amount_total     // Total amount processed
refund_rate              // Percentage of refunds
customer_acquisition     // New customers
```

## Distributed Tracing

Track request flow across services:

```go
tracer := tracing.NewTracer("payment-service", logger)

// Start a span
span := tracer.StartSpan(ctx, "process_payment")
defer span.End()

// Set attributes
span.SetAttribute("payment_id", "pay-123")
span.SetAttribute("amount", 99.99)
span.SetAttribute("currency", "USD")

// Add events
span.AddEvent("payment_authorized",
    "auth_code", "AUTH456",
    "timestamp", time.Now(),
)

span.AddEvent("payment_captured",
    "transaction_id", "TXN789",
)

// Set span status
if err != nil {
    span.SetStatus("error")
}

// Get trace information
ctx = context.WithValue(ctx, "span", span)
traceCtx := span.SpanContext()
fmt.Printf("Trace ID: %s, Span ID: %s\n", traceCtx.TraceID, traceCtx.SpanID)

// Query traces
trace, _ := tracer.GetTrace(ctx, traceCtx.TraceID)
fmt.Printf("Operation: %s, Duration: %s\n", trace.Operation, trace.Duration)
```

### Tracing Best Practices

```go
// Use meaningful span names
"process_payment"
"verify_token"
"update_inventory"
"send_email"

// Set important attributes
span.SetAttribute("user_id", userID)
span.SetAttribute("payment_method", "credit_card")
span.SetAttribute("retry_count", 2)

// Track key events
span.AddEvent("payment_authorized")
span.AddEvent("fraud_check_passed")
span.AddEvent("settlement_initiated")

// Connect spans across services (would use W3C Trace Context in production)
```

## Dependency Graph

Visualize service interactions:

```go
tracker := graph.NewTracker(logger)

// Record calls between services
tracker.RecordCall(ctx,
    "payment-service",      // from
    "auth-service",         // to
    "VerifyToken",          // operation
    150 * time.Millisecond, // latency
    true,                   // success
)

tracker.RecordCall(ctx,
    "payment-service",
    "inventory-service",
    "CheckInventory",
    200 * time.Millisecond,
    true,
)

// Get dependencies for a service
deps := tracker.GetDependencies(ctx, "payment-service")

fmt.Printf("Service: %s\n", deps.Service)
fmt.Printf("Dependency count: %d\n", len(deps.Dependencies))
fmt.Printf("Max depth: %d\n", deps.Depth)
fmt.Printf("Has circular deps: %v\n", deps.HasCircular)

for _, dep := range deps.Dependencies {
    fmt.Printf("%s: %d calls, %d%% success, %dms avg latency\n",
        dep.Service,
        dep.CallCount,
        dep.SuccessRate,
        dep.AvgLatency,
    )
}

// Get full dependency graph
graph := tracker.GetDependencyGraph(ctx)
for service, deps := range graph {
    fmt.Printf("%s has %d dependencies\n", service, len(deps.Dependencies))
}
```

## Alerting

Create rules-based alerts:

```go
engine := alerts.NewEngine(collector, logger)

// Add alert rule
engine.AddRule(observability.AlertRule{
    Name:      "high_error_rate",
    Metric:    "http_requests_errors_total",
    Op:        ">",           // Operators: >, <, >=, <=, ==, !=
    Threshold: 100,
    Duration:  5 * time.Minute,
    Action:    "notify_slack",
    Cooldown:  1 * time.Minute,
    Labels: map[string]string{
        "severity": "critical",
        "team":     "platform",
    },
    Enabled: true,
})

// Add alert recipient
engine.AddRecipient(observability.AlertRecipient{
    Type:        "slack",
    Address:     "#alerts",
    MinSeverity: observability.SeverityWarning,
})

// Set alert callback
engine.SetOnAlert(func(ctx context.Context, alert *observability.Alert) {
    fmt.Printf("ALERT: %s - %s\n", alert.Name, alert.Message)
})

// Start alert checking
go engine.Start(ctx, 10*time.Second)

// Get active alerts
alerts := engine.GetActiveAlerts()
for _, alert := range alerts {
    fmt.Printf("%s: %s\n", alert.Name, alert.Status)
}
```

### Alert Rules

```go
// High error rate
observability.AlertRule{
    Name:      "high_error_rate",
    Metric:    "http_requests_errors_total",
    Op:        ">",
    Threshold: 100,
    Duration:  5 * time.Minute,
}

// High latency (P99)
observability.AlertRule{
    Name:      "high_latency",
    Metric:    "request_duration_p99_ms",
    Op:        ">",
    Threshold: 1000,
    Duration:  3 * time.Minute,
}

// Database connection pool exhausted
observability.AlertRule{
    Name:      "db_pool_exhausted",
    Metric:    "db_connections_active",
    Op:        ">=",
    Threshold: 95,
    Duration:  1 * time.Minute,
}

// Memory usage high
observability.AlertRule{
    Name:      "high_memory",
    Metric:    "memory_usage_percent",
    Op:        ">",
    Threshold: 85,
    Duration:  2 * time.Minute,
}

// Service unavailable
observability.AlertRule{
    Name:      "service_down",
    Metric:    "health_status",
    Op:        "==",
    Threshold: 0,  // 0 = down
    Duration:  30 * time.Second,
}
```

## HTTP REST API

### Get Metrics Snapshot

```bash
GET /v1/metrics/snapshot

{
  "timestamp": "2026-03-01T12:00:00Z",
  "metrics": [
    {
      "name": "http_requests_total",
      "type": "counter",
      "value": 12345,
      "labels": {"service": "payment", "method": "POST"},
      "timestamp": "2026-03-01T12:00:00Z"
    }
  ]
}
```

### Get All Traces

```bash
GET /v1/tracing/traces

{
  "traces": [
    {
      "trace_id": "abc123...",
      "root_span_id": "span1...",
      "service": "payment-service",
      "operation": "process_payment",
      "start_time": "2026-03-01T12:00:00Z",
      "duration": "150ms",
      "status": "ok",
      "span_count": 5
    }
  ],
  "count": 1
}
```

### Get Specific Trace

```bash
GET /v1/tracing/traces/abc123...

{
  "trace_id": "abc123...",
  "root_span_id": "span1...",
  "service": "payment-service",
  "operation": "process_payment",
  "start_time": "2026-03-01T12:00:00Z",
  "duration": "150ms",
  "status": "ok",
  "span_count": 5
}
```

### Get Service Dependencies

```bash
GET /v1/dependencies?service=payment-service

{
  "service": "payment-service",
  "dependencies": [
    {
      "service": "auth-service",
      "call_count": 5000,
      "success_count": 4950,
      "error_count": 50,
      "success_rate": 99,
      "error_rate": 1,
      "avg_latency_ms": 45,
      "p99_latency_ms": 120,
      "last_call_time": "2026-03-01T12:00:00Z"
    }
  ],
  "depth": 2,
  "has_circular": false,
  "generated_at": "2026-03-01T12:00:00Z"
}
```

### Get Full Dependency Graph

```bash
GET /v1/dependencies/graph

{
  "payment-service": {
    "service": "payment-service",
    "dependencies": [...],
    "depth": 2,
    "has_circular": false
  },
  "auth-service": {
    "service": "auth-service",
    "dependencies": [...],
    "depth": 1,
    "has_circular": false
  }
}
```

### Get Active Alerts

```bash
GET /v1/alerts/active

{
  "alerts": [
    {
      "name": "high_error_rate",
      "severity": "critical",
      "status": "firing",
      "message": "Alert high_error_rate fired: ...",
      "value": 250,
      "fired_at": "2026-03-01T12:00:00Z",
      "labels": {"severity": "critical"}
    }
  ],
  "count": 1
}
```

## Integration Examples

### With Service Registry

```go
// Update registry health when service health metric changes
collector := metrics.NewCollector("auth-service", logger)
health := collector.Gauge("service_health", "Service health", []string{})
health.WithLabelValues().Set(1) // 1 = healthy
```

### With Load Balancer

```go
// Track endpoint latency and success rate
tracker.RecordCall(ctx,
    "payment-service",
    "user-service",
    "GetUser",
    latency,
    success,
)

// Use dependency graph to select endpoints
deps := tracker.GetDependencies(ctx, "payment-service")
// Select endpoints with highest success rate
```

### With Health Checker

```go
// Emit health check metrics
collector.Counter("health_checks_total", "Health checks", []string{"type"}).
    WithLabelValues("http").Inc()

// Track health check duration
histogram.WithLabelValues("http").Observe(duration.Seconds() * 1000)
```

### With Circuit Breaker

```go
// Track circuit breaker state
gauge := collector.Gauge("circuit_breaker_state", "CB state", []string{"service"})
// 1 = closed, 0 = open, 0.5 = half-open
gauge.WithLabelValues("auth-service").Set(1)
```

## Monitoring Dashboard

Create Grafana dashboards with these key metrics:

```
Payment Service Dashboard:
- Requests per second (RPS)
- Error rate (%)
- P99 latency (ms)
- Success rate (%)
- Active connections
- Database pool usage
- Cache hit rate
- Payment amount (total)
```

## Performance Considerations

### Metrics

- Cardinality: Avoid unbounded labels
- Frequency: Don't record too frequently
- Retention: Implement metric pruning

### Tracing

- Sampling: Sample at appropriate rate (1-10%)
- Batch size: Default 512 spans
- Export interval: Default 5 seconds
- Memory: Prune old traces regularly

### Alerting

- Evaluation: Check every 10 seconds
- Cooldown: Prevent alert spam (default 1 minute)
- Duration: Condition must be true for duration
- Recipients: Route by severity

## Best Practices

### 1. Instrumentation

```go
// Always measure what matters
- User-facing latency
- Error rates
- Resource utilization
- Business metrics
```

### 2. Naming

```go
// Consistent naming convention
metric_type_unit        // e.g., request_duration_ms
_total suffix for counters  // e.g., payments_total
```

### 3. Labels

```go
// Don't create unbounded labels
GOOD:   service, method, status
BAD:    user_id, customer_id (unbounded)
```

### 4. Sampling

```go
// Sample traces intelligently
tracer.SamplingDecision(0.1)  // Sample 10% of traces
```

## Troubleshooting

### High Memory Usage

Problem: Observability package consuming too much memory

Solutions:
```go
// Reduce trace retention
tracer.PruneTraces(5 * time.Minute)

// Reduce metrics cardinality
// Don't use unbounded labels

// Sample traces
tracer.SamplingDecision(0.05)  // 5% sampling
```

### Missing Metrics

Problem: Expected metrics not appearing

Solutions:
```go
// Check metric names
snapshot, _ := collector.GetSnapshot(ctx)
for _, metric := range snapshot.Metrics {
    fmt.Println(metric.Name)
}

// Verify labels are set correctly
counter := collector.Counter("requests", "...", []string{"service"})
counter.WithLabelValues("payment").Inc()  // Must match labels
```

### Alert Not Firing

Problem: Alert rule not triggering

Solutions:
```go
// Check metric name matches exactly
collector.Counter("http_errors_total", "...", []string{})
// Rule metric must be "http_errors_total"

// Verify threshold and operator
rule.Op = ">"
rule.Threshold = 100
// Will fire when metric > 100

// Check duration (must be true for duration)
rule.Duration = 5 * time.Minute
// Condition must be true for 5 minutes
```
