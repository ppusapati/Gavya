# Shared Packages Module

## 1. Module Overview

The Packages module provides a comprehensive collection of shared utilities, middleware, infrastructure components, and cross-cutting concerns used across all modules in the UGCL platform. It serves as the foundation layer providing reusable building blocks for database access, logging, metrics, tracing, configuration, and more.

**Purpose:** Centralized shared utilities and infrastructure components to promote code reuse, consistency, and best practices across all microservices and modules.

**Key Features:**
- Database connectivity and SQLC integration
- Structured logging with zap
- Distributed tracing with OpenTelemetry
- Metrics collection and Prometheus integration
- Configuration management
- Error handling and middleware
- Event bus and messaging
- Caching with Redis
- API utilities and helpers
- Authorization helpers
- Virtual File System (VFS) abstraction

## 2. Architecture

### Module Structure
```
packages/
├── api/                    # API utilities and helpers
│   ├── connect/           # Connect-RPC helpers
│   ├── response/          # Standard response formatters
│   └── validation/        # Request validation
├── authz/                 # Authorization helpers
│   ├── permission.go      # Permission checking
│   └── context.go         # Auth context extraction
├── cache/                 # Caching abstraction
│   ├── redis.go           # Redis cache implementation
│   └── memory.go          # In-memory cache
├── cmd/                   # CLI utilities
│   └── flags.go           # Command-line flag helpers
├── config/                # Configuration management
│   ├── loader.go          # Config file loading
│   ├── env.go             # Environment variable parsing
│   └── validation.go      # Config validation
├── constants/             # Shared constants
│   ├── errors.go          # Error codes
│   └── status.go          # Status constants
├── converters/            # Type converters
│   ├── proto.go           # Protobuf converters
│   └── time.go            # Time format converters
├── database/              # Database utilities
│   ├── sqlc/              # SQLC providers
│   │   ├── provider.go    # Database connection provider
│   │   └── transaction.go # Transaction helpers
│   ├── migrations/        # Migration helpers
│   └── health.go          # DB health checks
├── deps/                  # Dependency injection helpers
│   └── fx.go              # Uber FX utilities
├── encoding/              # Encoding/decoding utilities
│   ├── json.go            # JSON helpers
│   └── base64.go          # Base64 encoding
├── errors/                # Error handling
│   ├── errors.go          # Error types
│   ├── codes.go           # Error codes
│   └── handler.go         # Error handlers
├── events/                # Event bus
│   ├── bus.go             # Event bus implementation
│   ├── kafka.go           # Kafka integration
│   └── types.go           # Event types
├── helpers/               # General helpers
│   ├── string.go          # String utilities
│   ├── slice.go           # Slice utilities
│   └── map.go             # Map utilities
├── merger/                # Data merging utilities
│   └── struct.go          # Struct merging
├── metrics/               # Metrics collection
│   ├── prometheus.go      # Prometheus metrics
│   └── datadog.go         # DataDog integration
├── middleware/            # HTTP/gRPC middleware
│   ├── auth.go            # Authentication middleware
│   ├── logging.go         # Request logging
│   ├── recovery.go        # Panic recovery
│   ├── cors.go            # CORS middleware
│   └── ratelimit.go       # Rate limiting
├── models/                # Shared data models
│   ├── pagination.go      # Pagination models
│   └── metadata.go        # Common metadata
├── p9context/             # Context utilities
│   ├── context.go         # Context helpers
│   └── keys.go            # Context key definitions
├── p9log/                 # Logging utilities
│   ├── logger.go          # Zap logger wrapper
│   ├── config.go          # Logging configuration
│   └── fields.go          # Structured field helpers
├── proto/                 # Shared proto utilities
│   └── helpers.go         # Protobuf helpers
├── saas/                  # Multi-tenancy utilities
│   ├── tenant.go          # Tenant context
│   └── isolation.go       # Data isolation
├── server/                # Server utilities
│   ├── http.go            # HTTP server
│   ├── grpc.go            # gRPC server
│   └── shutdown.go        # Graceful shutdown
├── timeout/               # Timeout utilities
│   └── context.go         # Context timeout helpers
├── tracing/               # Distributed tracing
│   ├── otel.go            # OpenTelemetry setup
│   ├── jaeger.go          # Jaeger exporter
│   └── zipkin.go          # Zipkin exporter
├── transport/             # Transport utilities
│   ├── http.go            # HTTP client
│   └── grpc.go            # gRPC client
├── uow/                   # Unit of Work pattern
│   ├── uow.go             # UoW implementation
│   └── transaction.go     # Transaction management
├── utils/                 # General utilities
│   ├── uuid.go            # UUID generation
│   ├── hash.go            # Hashing utilities
│   └── crypto.go          # Cryptographic helpers
├── vfs/                   # Virtual File System
│   ├── vfs.go             # VFS interface
│   ├── local.go           # Local filesystem
│   └── s3.go              # S3 implementation
└── go.mod                 # Module dependencies
```

### Key Dependencies
- **Database:** `pgx/v5`, `gorm`, `sqlc`
- **Logging:** `zap`
- **Metrics:** `prometheus`, `datadog`
- **Tracing:** `opentelemetry`, `jaeger`, `zipkin`
- **Messaging:** `sarama` (Kafka)
- **Caching:** `go-redis`
- **HTTP:** `echo/v4`, `cors`
- **gRPC:** `grpc`, `grpc-gateway`
- **Config:** `toml`, `yaml`
- **Validation:** `validator`, `protovalidate`
- **DI:** `wire`, `uber/fx`

## 3. Quick Start

### Database Connection

```go
import (
    "p9e.in/samavaya/packages/database/sqlc"
)

// Provide database connection via FX
fx.Provide(
    sqlc.NewDatabaseProvider,
)

// Use in service
type MyService struct {
    db *sql.DB
}

func NewMyService(db *sql.DB) *MyService {
    return &MyService{db: db}
}
```

### Logging

```go
import (
    "p9e.in/samavaya/packages/p9log"
)

// Initialize logger
logger := p9log.NewLogger(p9log.Config{
    Level:      "info",
    Format:     "json",
    OutputPath: "stdout",
})

// Use structured logging
logger.Info("User created",
    p9log.String("user_id", userID),
    p9log.String("email", email),
    p9log.Int("tenant_count", len(tenantIDs)),
)

logger.Error("Failed to process request",
    p9log.Error(err),
    p9log.String("request_id", reqID),
)
```

### Metrics

```go
import (
    "p9e.in/samavaya/packages/metrics"
)

// Define metrics
var (
    requestCounter = metrics.NewCounter(metrics.CounterOpts{
        Name: "api_requests_total",
        Help: "Total number of API requests",
    })

    requestDuration = metrics.NewHistogram(metrics.HistogramOpts{
        Name:    "api_request_duration_seconds",
        Help:    "API request duration",
        Buckets: []float64{0.1, 0.5, 1.0, 2.0, 5.0},
    })
)

// Use metrics
requestCounter.Inc()

start := time.Now()
// ... process request ...
requestDuration.Observe(time.Since(start).Seconds())
```

### Tracing

```go
import (
    "p9e.in/samavaya/packages/tracing"
    "go.opentelemetry.io/otel"
)

// Initialize tracing
shutdown, err := tracing.InitTracing(tracing.Config{
    ServiceName: "user-service",
    Endpoint:    "http://jaeger:14268/api/traces",
})
defer shutdown()

// Create spans
tracer := otel.Tracer("user-service")
ctx, span := tracer.Start(ctx, "CreateUser")
defer span.End()

span.SetAttributes(
    attribute.String("user.email", email),
    attribute.String("user.id", userID),
)
```

## 4. Component Reference

### Database Package

**Provider:**
```go
// Get database connection from config
db, err := sqlc.NewDatabaseProvider(config.Database{
    Host:     "localhost",
    Port:     5432,
    Database: "samavaya",
    User:     "postgres",
    Password: "password",
})
```

**Transactions:**
```go
// Execute in transaction
err := sqlc.WithTransaction(ctx, db, func(tx *sql.Tx) error {
    // Execute queries in transaction
    _, err := queries.WithTx(tx).CreateUser(ctx, params)
    return err
})
```

**Health Check:**
```go
// Check database health
healthy := database.HealthCheck(db)
if !healthy {
    log.Error("Database unhealthy")
}
```

### Logging Package (p9log)

**Logger Configuration:**
```go
logger := p9log.NewLogger(p9log.Config{
    Level:       "debug",           // debug, info, warn, error
    Format:      "json",            // json, console
    OutputPath:  "stdout",          // stdout, stderr, or file path
    ErrorOutput: "stderr",
    Sampling: &p9log.SamplingConfig{
        Initial:    100,
        Thereafter: 100,
    },
})
```

**Structured Logging:**
```go
// Field types
logger.Info("Message",
    p9log.String("key", "value"),
    p9log.Int("count", 42),
    p9log.Bool("active", true),
    p9log.Duration("elapsed", duration),
    p9log.Time("timestamp", time.Now()),
    p9log.Error(err),
    p9log.Any("data", complexObject),
)

// Context-aware logging
logger = logger.With(
    p9log.String("request_id", reqID),
    p9log.String("user_id", userID),
)
logger.Info("Processing request") // Includes request_id and user_id
```

### Metrics Package

**Counter:**
```go
counter := metrics.NewCounter(metrics.CounterOpts{
    Name:   "requests_total",
    Help:   "Total requests",
    Labels: []string{"method", "status"},
})

counter.WithLabels("GET", "200").Inc()
counter.WithLabels("POST", "201").Add(5)
```

**Gauge:**
```go
gauge := metrics.NewGauge(metrics.GaugeOpts{
    Name: "active_connections",
    Help: "Number of active connections",
})

gauge.Set(42)
gauge.Inc()
gauge.Dec()
gauge.Add(10)
```

**Histogram:**
```go
histogram := metrics.NewHistogram(metrics.HistogramOpts{
    Name:    "request_duration_seconds",
    Help:    "Request duration",
    Buckets: metrics.DefaultBuckets,
})

histogram.Observe(0.245) // Record observation
```

### Error Handling

**Define Errors:**
```go
import "p9e.in/samavaya/packages/errors"

var (
    ErrUserNotFound = errors.New(
        errors.CodeNotFound,
        "USER_NOT_FOUND",
        "User not found",
    )

    ErrInvalidCredentials = errors.New(
        errors.CodeUnauthorized,
        "INVALID_CREDENTIALS",
        "Invalid username or password",
    )
)
```

**Use Errors:**
```go
// Return error
return errors.Wrap(err, "failed to create user")

// Check error type
if errors.Is(err, ErrUserNotFound) {
    // Handle not found
}

// Get error code
code := errors.GetCode(err)
```

### Middleware

**Authentication Middleware:**
```go
import "p9e.in/samavaya/packages/middleware"

// Echo middleware
e := echo.New()
e.Use(middleware.Auth(middleware.AuthConfig{
    TokenExtractor: middleware.FromHeader("Authorization"),
    Validator:      validateToken,
}))
```

**Logging Middleware:**
```go
e.Use(middleware.RequestLogger(logger))
```

**Recovery Middleware:**
```go
e.Use(middleware.Recover(logger))
```

**CORS Middleware:**
```go
e.Use(middleware.CORS(middleware.CORSConfig{
    AllowOrigins: []string{"*"},
    AllowMethods: []string{"GET", "POST", "PUT", "DELETE"},
}))
```

### Caching

**Redis Cache:**
```go
import "p9e.in/samavaya/packages/cache"

// Create cache
redisCache := cache.NewRedis(cache.RedisConfig{
    Addr:     "localhost:6379",
    Password: "",
    DB:       0,
})

// Set value
err := redisCache.Set(ctx, "key", value, 5*time.Minute)

// Get value
var result User
err := redisCache.Get(ctx, "key", &result)

// Delete
err := redisCache.Delete(ctx, "key")
```

### Event Bus

**Publish Events:**
```go
import "p9e.in/samavaya/packages/events"

bus := events.NewKafkaEventBus(events.KafkaConfig{
    Brokers: []string{"localhost:9092"},
})

// Publish event
err := bus.Publish(ctx, events.Event{
    Topic:     "user.created",
    Key:       userID,
    Payload:   userData,
    Timestamp: time.Now(),
})
```

**Subscribe to Events:**
```go
// Subscribe to topic
err := bus.Subscribe(ctx, "user.created", func(event events.Event) error {
    // Handle event
    log.Info("User created", "user_id", event.Key)
    return nil
})
```

### Configuration

**Load Config:**
```go
import "p9e.in/samavaya/packages/config"

type AppConfig struct {
    Server   ServerConfig   `toml:"server"`
    Database DatabaseConfig `toml:"database"`
    Redis    RedisConfig    `toml:"redis"`
}

// Load from file
var cfg AppConfig
err := config.LoadFile("config.toml", &cfg)

// Load from environment
err := config.LoadEnv(&cfg)

// Validate
err := config.Validate(&cfg)
```

## 5. Integration

### With Modules

All application modules depend on the packages module for:
- **Database access:** SQLC provider and transaction management
- **Logging:** Structured logging with zap
- **Metrics:** Prometheus metrics collection
- **Tracing:** Distributed tracing
- **Errors:** Standardized error handling
- **Middleware:** Common HTTP/gRPC middleware

### FX Dependency Injection

```go
import (
    "p9e.in/samavaya/packages/database/sqlc"
    "p9e.in/samavaya/packages/p9log"
    "p9e.in/samavaya/packages/server"
    "go.uber.org/fx"
)

fx.New(
    // Provide shared components
    fx.Provide(
        p9log.NewLogger,
        sqlc.NewDatabaseProvider,
        server.NewHTTPServer,
    ),
    // Invoke startup
    fx.Invoke(func(srv *server.HTTPServer) {
        srv.Start()
    }),
).Run()
```

## 6. Development

### Adding New Utilities

**1. Create new package:**
```bash
mkdir packages/newutil
```

**2. Implement functionality:**
```go
// packages/newutil/helper.go
package newutil

func DoSomething() {
    // Implementation
}
```

**3. Add tests:**
```go
// packages/newutil/helper_test.go
package newutil

func TestDoSomething(t *testing.T) {
    // Tests
}
```

**4. Update go.mod if needed:**
```bash
go get new-dependency
go mod tidy
```

### Testing Guidelines

**Unit Tests:**
```bash
# Test specific package
go test ./packages/database/...

# Test all packages
go test ./packages/...

# With coverage
go test -cover ./packages/...
```

**Integration Tests:**
```bash
# Run with integration tag
go test -tags=integration ./packages/...
```

## 7. Configuration

### Environment Variables

```env
# Logging
LOG_LEVEL=info
LOG_FORMAT=json
LOG_OUTPUT=stdout

# Database
DB_HOST=localhost
DB_PORT=5432
DB_NAME=samavaya
DB_USER=postgres
DB_PASSWORD=password
DB_MAX_CONNECTIONS=25
DB_MAX_IDLE_CONNECTIONS=5

# Redis
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0

# Tracing
TRACING_ENABLED=true
TRACING_ENDPOINT=http://jaeger:14268/api/traces
TRACING_SAMPLE_RATE=0.1

# Metrics
METRICS_ENABLED=true
METRICS_PORT=9090

# Kafka
KAFKA_BROKERS=localhost:9092
KAFKA_GROUP_ID=samavaya-consumer-group
```

## 8. Best Practices

### Logging
- Use structured logging with fields
- Include request IDs for tracing
- Log at appropriate levels
- Don't log sensitive data

### Metrics
- Use descriptive metric names
- Include relevant labels
- Use appropriate metric types
- Document metric meanings

### Errors
- Use typed errors
- Include context in errors
- Don't expose internal details
- Log errors appropriately

### Database
- Use connection pooling
- Handle transactions properly
- Use prepared statements
- Close resources

### Caching
- Set appropriate TTLs
- Handle cache misses
- Invalidate on updates
- Monitor cache hit rates

## 9. Troubleshooting

### Common Issues

**Issue: Database connection pool exhausted**
- **Cause:** Too many concurrent connections
- **Solution:** Increase pool size or optimize queries

**Issue: Memory leak in logger**
- **Cause:** Not closing log files
- **Solution:** Use defer logger.Sync()

**Issue: Metrics not appearing**
- **Cause:** Prometheus not configured
- **Solution:** Check metrics endpoint and scrape config

**Issue: Tracing spans missing**
- **Cause:** Context not propagated
- **Solution:** Pass context through call chain

## Additional Resources

- [Uber FX Documentation](https://uber-go.github.io/fx/)
- [Zap Logging](https://pkg.go.dev/go.uber.org/zap)
- [OpenTelemetry Go](https://opentelemetry.io/docs/instrumentation/go/)
- [Prometheus Client](https://prometheus.io/docs/guides/go-application/)
- [SQLC Documentation](https://docs.sqlc.dev/)
