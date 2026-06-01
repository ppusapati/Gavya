// Package deps defines ServiceDeps — the shared infrastructure bundle
// every service receives through dependency injection.
//
// ServiceDeps bundles the dependencies that most services need:
//
//   - Cfg: server configuration (*config.Server)
//   - Pool: the pgxpool.Pool for Postgres access
//   - Tp: TimeoutProvider — source of operation timeouts
//   - Metrics: MetricsProvider for recording domain metrics
//   - Log: p9log.Logger for structured logging
//   - Cache: CacheProvider for ephemeral lookups
//   - KafkaProducer / KafkaConsumer: event-bus endpoints
//   - Tracing: TracingProvider for OpenTelemetry spans
//
// Constructing ServiceDeps once at bootstrap and passing it through the fx
// graph keeps handlers and services decoupled from concrete infrastructure
// — tests swap individual fields with fakes without rewiring callers.
//
// See also: fx registration in the app composition root, which binds this
// struct's fields to the fx-provided instances.
package deps
