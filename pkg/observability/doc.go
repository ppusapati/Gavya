// Package observability provides comprehensive observability for microservices.
//
// # Overview
//
// The observability package includes:
//
//   - Metrics: Prometheus-compatible metrics collection and export
//   - Tracing: Distributed tracing with OpenTelemetry
//   - Dependency Graph: Service interaction visualization
//   - Alerting: Rules-based alerting on metrics
//
// # Metrics
//
// Collect and export metrics compatible with Prometheus:
//
//	collector := metrics.NewCollector()
//	counter := collector.Counter("requests_total", "Request count", []string{"service"})
//	counter.WithLabelValues("payment-service").Inc()
//
//	histogram := collector.Histogram("request_duration_ms", "Request latency")
//	histogram.Observe(45.5)
//
//	exporter, _ := metrics.NewPrometheusExporter(collector)
//	http.Handle("/metrics", exporter)
//
// # Distributed Tracing
//
// Track request flow across services:
//
//	tracer := tracing.NewTracer("payment-service")
//	span := tracer.StartSpan(ctx, "process_payment")
//	defer span.End()
//
//	span.SetAttribute("payment_id", paymentID)
//	span.SetAttribute("amount", amount)
//	span.AddEvent("payment_authorized")
//
// # Dependency Graph
//
// Visualize service interactions:
//
//	tracker := graph.NewTracker()
//	tracker.RecordCall(ctx, "payment-service", "auth-service", "VerifyToken", 150*time.Millisecond, true)
//
//	deps := tracker.GetDependencies(ctx, "payment-service")
//	for _, dep := range deps.Dependencies {
//	    fmt.Printf("%s → %s (%d%% success)\n",
//	        deps.Service,
//	        dep.Service,
//	        dep.SuccessRate)
//	}
//
// # Alerting
//
// Create rules-based alerts:
//
//	engine := alerts.NewEngine()
//	engine.AddRule(alerts.Rule{
//	    Name:    "high_error_rate",
//	    Metric:  "request_errors_total",
//	    Op:      alerts.OpGreaterThan,
//	    Threshold: 100,
//	    Duration: 5 * time.Minute,
//	    Action:  "notify_slack",
//	})
//
//	go engine.Start(ctx)
//
// # Integration
//
// Integrates with all previous phases:
//
//   - Service Registry: Track service metadata in metrics
//   - Load Balancer: Monitor endpoint latency
//   - Rate Limiter: Track rate limit rejections
//   - Health Checker: Emit health metrics
//
package observability
