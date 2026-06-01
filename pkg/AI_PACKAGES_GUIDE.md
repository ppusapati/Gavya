# Packages Module - AI Understanding Guide

This document explains every package in the shared packages module so that AI tools can understand the purpose, behavior, and relationships of each component.

## Module Overview

The **Packages Module** (import path: `kosha/...`) is the foundational infrastructure layer for all microservices in the UGCL platform. It provides reusable building blocks for database access, logging, metrics, tracing, configuration, caching, events, and more.

**Module Path:** `kosha` (aliased in go.mod as `kosha`)

**Key Architectural Patterns:**
- **Strategy Pattern:** Metrics, tracing, and cache providers use pluggable backends
- **Factory Pattern:** UnitOfWork, database connections, and service dependencies
- **Dependency Injection:** ServiceDeps struct aggregates all infrastructure dependencies
- **Graceful Degradation:** Noop implementations when features are disabled
- **Multi-Tenancy Support:** Tenant-aware database connections and context propagation

---

## Package Descriptions

### 1. api/v1

**Purpose:** Contains generated protobuf Go code for shared data structures used across all services.

**Sub-packages:**
| Package | Description | Key Types |
|---------|-------------|-----------|
| `blob` | Binary large object handling | Blob messages |
| `config` | Configuration protobuf definitions | Bootstrap, Server, Data, Observability |
| `data` | Common data structures | Generic data wrappers |
| `errors` | Error response structures | Status, ErrorInfo |
| `fields` | Field-level definitions | Field metadata |
| `filter` | Query filtering structures | Filter, FilterOperator |
| `geo` | Geographic data types | Coordinates, Address |
| `identifier` | Entity identification | Identifier (ID or UUID oneof) |
| `message` | Messaging structures | Message envelopes |
| `pagination` | Pagination support | PageRequest, PageResponse |
| `query` | Query building | QueryParams |
| `response` | Standard API responses | OperationResponse, ErrorReason, SuccessReason |

**Usage Pattern:**
```go
import pb "kosha/api/v1/response"

// Standard success response
return &pb.OperationResponse{
    Success: true,
    Reason:  pb.SuccessReason_CREATED_SUCCESSFULLY,
}
```

---

### 2. authz

**Purpose:** Authorization helpers for permission checking and access control.

**Key Types:**
```go
type Effect int32
const (
    Effect_UNSPECIFIED Effect = 0
    Effect_GRANT       Effect = 1
    Effect_DENY        Effect = 2
)

type Permission struct {
    Namespace string // e.g., "user", "tenant", "billing"
    Resource  string // e.g., "profile", "settings", "invoice"
    Action    string // e.g., "read", "write", "delete"
    Effect    Effect // GRANT or DENY
}

type CheckPermissionResponse struct {
    Allowed bool
    Effect  Effect
    Reason  string
}
```

**Domain Context:**
- Namespace-based permission model (namespace.resource.action)
- Supports both GRANT and DENY effects
- Integration with SpiceDB/Authzed for relationship-based access control

---

### 3. cache

**Purpose:** In-memory caching with TTL support and automatic expiry cleanup.

**Key Types:**
```go
type Cache interface {
    Set(key interface{}, value interface{})
    SetWithTTL(key interface{}, value interface{}, ttl time.Duration)
    Get(key interface{}) (interface{}, bool)
    GetJSON(key interface{}, dest interface{}) error
    SetJSON(key interface{}, value interface{}) error
    Delete(key interface{})
    Exists(key interface{}) bool
    Clear()
    Close()
}
```

**Features:**
- **TTL Support:** Automatic expiration of cache entries
- **LRU Eviction:** Removes least recently used entries when max capacity reached
- **JSON Helpers:** `GetJSON` and `SetJSON` for structured data
- **Background Cleanup:** Periodic expiry monitoring goroutine
- **Graceful Degradation:** `NoopCache` when caching is disabled

**Configuration:**
```go
cfg.Cache.Enabled        // Enable/disable caching
cfg.Cache.DefaultTtl     // Default time-to-live
cfg.Cache.MaxEntries     // Maximum cache entries
cfg.Cache.EnableExpiry   // Enable automatic expiration
cfg.Cache.ExpiryCheckInterval // Cleanup frequency
```

---

### 4. config

**Purpose:** Configuration management supporting multiple sources (files, environment variables) with merging and hot-reloading.

**Key Types:**
```go
type Reader interface {
    Merge(...*KeyValue) error  // Merge configuration from multiple sources
    Value(string) (Value, bool) // Get config value by dot-notation path
    Source() ([]byte, error)   // Get merged config as JSON
    Resolve() error            // Resolve environment variable placeholders
}

type Source interface {
    Load() ([]*KeyValue, error)  // Load configuration
    Watch() (Watcher, error)     // Watch for changes
}
```

**Sub-packages:**
| Package | Description |
|---------|-------------|
| `env` | Load config from environment variables (prefix-based) |
| `file` | Load config from YAML/TOML/JSON files |

**Usage Pattern:**
```go
source := []config.Source{
    env.NewSource("APP_"),           // Environment variables with APP_ prefix
    file.NewSource("config.yaml"),   // YAML configuration file
}

c := config.New(config.WithSource(source...))
c.Load()

var cfg conf.Bootstrap
c.Scan(&cfg)
```

**Key Features:**
- **Dot-notation Access:** `reader.Value("database.host")`
- **Deep Merging:** Later sources override earlier ones
- **Protobuf Support:** Direct scanning into protobuf messages
- **File Watching:** Hot-reload configuration changes

---

### 5. constants

**Purpose:** Shared constants used across all services.

**Categories:**
- Error codes and reasons
- Status constants
- Default values
- System-wide identifiers

---

### 6. converters

**Purpose:** Type conversion utilities for database and protobuf types.

**Key Functions:**
```go
// Convert between SQL nullable types and Go maps
func NullRawMessageToMap(nrm pqtype.NullRawMessage) map[string]interface{}
func MapToNullRawMessage(m map[string]interface{}) pqtype.NullRawMessage
func MapToNullString(m map[string]any) sql.NullString
func NullStringToMap(ns sql.NullString) map[string]any
```

**Use Cases:**
- Converting JSONB columns to Go maps
- Handling nullable database fields
- Protobuf to database model conversions

---

### 7. database

**Purpose:** Database connectivity and utilities for PostgreSQL using pgx.

**Sub-packages:**

#### 7.1 pgxpostgres

**Purpose:** PostgreSQL connection management with multi-tenancy support.

**Key Types:**
```go
type MultiTenancy struct {
    TenantId HasTenant
}

type DbProvider saas.DbProvider[*pgxpool.Pool]
type ClientProvider saas.ClientProvider[*pgxpool.Pool]
```

**Features:**
- Connection pooling with pgxpool
- Multi-tenant database resolution
- Connection wrapping with cleanup

#### 7.2 pgxpostgres/builder

**Purpose:** Dynamic SQL query building with filtering support.

#### 7.3 pgxpostgres/query

**Purpose:** Query utilities and helpers.

#### 7.4 pgxpostgres/retry

**Purpose:** Automatic retry logic for transient database failures.

#### 7.5 pgxpostgres/tenantDB

**Purpose:** Tenant-specific database management.

**Key Functions:**
```go
func CreateDatabaseIfNotExists(ctx context.Context, pool *pgxpool.Pool, databaseName string) error
```

**Use Case:** Creating dedicated databases for paid-tier tenants.

#### 7.6 redis

**Purpose:** Redis client configuration and connection management.

#### 7.7 sqlc

**Purpose:** SQLC-generated code providers and transaction helpers.

---

### 8. deps

**Purpose:** Aggregates all service dependencies into a single injectable struct.

**Key Type:**
```go
type ServiceDeps struct {
    Cfg           *config.Server
    Pool          *pgxpool.Pool
    Tp            *timeout.TimeoutProvider
    Metrics       metrics.MetricsProvider
    Log           p9log.Logger
    Cache         *cache.CacheProvider
    KafkaProducer *producer.KafkaProducer
    KafkaConsumer *consumer.KafkaConsumer
    Tracing       *tracing.TracingProvider
}
```

**Usage Pattern:**
```go
// In fx module
fx.Provide(deps.NewServiceDeps)

// In service/handler
type MyHandler struct {
    deps deps.ServiceDeps
}

func (h *MyHandler) Handle(ctx context.Context) error {
    h.deps.Log.Info("Processing request")
    h.deps.Metrics.RecordServiceRequestCount("my-service")
    // ...
}
```

---

### 9. encoding

**Purpose:** Encoding/decoding utilities.

**Sub-packages:**

#### 9.1 form

**Purpose:** Form/URL encoding for protobuf messages.

**Key Functions:**
```go
func ProtoEncode(msg proto.Message) (url.Values, error)
```

**Features:**
- Well-known types handling (Timestamp, Duration, Struct)
- Nested message encoding
- Repeated field support

---

### 10. errors

**Purpose:** Standardized error handling with gRPC and HTTP code mapping.

**Key Types:**
```go
type Error struct {
    Status
    cause error
}

func (e *Error) Error() string
func (e *Error) Unwrap() error
func (e *Error) Is(err error) bool
func (e *Error) WithCause(cause error) *Error
func (e *Error) WithMetadata(md map[string]string) *Error
func (e *Error) GRPCStatus() *status.Status
```

**Factory Functions:**
```go
func New(code int, reason, message string) *Error
func Newf(code int, reason, format string, a ...interface{}) *Error
func Errorf(code int, reason, format string, a ...interface{}) error
```

**Error Inspection:**
```go
func Code(err error) int      // Extract HTTP status code
func Reason(err error) string // Extract error reason
func FromError(err error) *Error // Convert any error to *Error
```

**Sub-packages:**
| Package | Description |
|---------|-------------|
| `database` | Database-specific errors (connection, constraint violations) |
| `grpc` | gRPC error code mappings |

**Error Code Mapping:**
- Automatic conversion between HTTP codes and gRPC codes
- Support for error metadata propagation
- Integration with errdetails.ErrorInfo

---

### 11. events

**Purpose:** Event-driven architecture with Kafka integration.

**Sub-packages:**

#### 11.1 domain

**Purpose:** Domain event definitions and builders.

**Key Types:**
```go
type EventType string

const (
    EventTypeWorkflowTransition EventType = "workflow.transition"
    EventTypeUserCreated        EventType = "identity.user.created"
    EventTypeTenantCreated      EventType = "identity.tenant.created"
    // ... many more event types
)

type Priority string

const (
    PriorityLow      Priority = "low"
    PriorityMedium   Priority = "medium"
    PriorityHigh     Priority = "high"
    PriorityCritical Priority = "critical"
)

type DomainEvent struct {
    ID            string
    Type          EventType
    AggregateID   string
    AggregateType string
    Version       int64
    Data          map[string]interface{}
    Metadata      map[string]string
    Priority      Priority
    Source        string
    Timestamp     time.Time
    CorrelationID string
    CausationID   string
}
```

**Event Builder Pattern:**
```go
event := domain.NewEventBuilder(domain.EventTypeUserCreated, userID, "User").
    WithData(userData).
    WithPriority(domain.PriorityHigh).
    WithSource("user-service").
    WithCorrelationID(requestID).
    Build()
```

**Topic Routing:** Events are automatically routed to Kafka topics based on type:
- `Samavāya.workflow.events`
- `Samavāya.identity.events`
- `Samavāya.tenant.events`
- `Samavāya.masters.events`
- etc.

#### 11.2 producer

**Purpose:** Kafka message publishing.

#### 11.3 consumer

**Purpose:** Kafka message consumption with graceful shutdown.

#### 11.4 bus

**Purpose:** Event bus abstraction for local and distributed events.

#### 11.5 handler

**Purpose:** Event handler registration and routing.

---

### 12. helpers

**Purpose:** Generic helper functions for common operations.

**Sub-packages:**

#### 12.1 service

**Purpose:** Generic CRUD operation helpers with tracing and metrics.

**Key Functions:**
```go
func CreateEntity[T models.Entity, P proto.Message](
    ctx context.Context,
    req P,
    convertFunc func(P) T,
    tracer *tracing.TracingProvider,
    metrics metrics.MetricsProvider,
    repoFunc func(context.Context, T) (T, error),
) (*pbr.OperationResponse, error)

func GetEntity[T models.Entity, P proto.Message](...)
func ListEntity[T models.Entity, P proto.Message](...)
func DeleteEntity[T models.Entity](...)
func FetchEntity[T models.Entity](...)
```

**Benefits:**
- Automatic tracing span creation
- Metrics recording (duration, success/failure)
- Request validation
- Consistent error responses

#### 12.2 utils

**Purpose:** General utility functions.

**Key Functions:**
```go
// Field mask application for partial updates
func ApplyFieldMask(mask *fieldmaskpb.FieldMask, source, target proto.Message)

// Get type name for generic types
func GetTypeName[T any]() string

// Validate protobuf messages
func ValidateProto(msg proto.Message) error

// Build standard responses
func SuccessResponse(ctx context.Context, msg string, entity any, reason pb.SuccessReason) (*pb.OperationResponse, error)
func ErrorResponse(ctx context.Context, msg string, entity any, reason pb.ErrorReason) (*pb.OperationResponse, error)
```

---

### 13. merger

**Purpose:** Deep struct merging with configurable behavior.

**Key Function:**
```go
func Merge(dst, src interface{}, opts ...func(*Config)) error
```

**Configuration Options:**
```go
WithOverride          // Override non-empty dst with non-empty src
WithOverwriteWithEmptyValue // Override even with empty values
WithAppendSlice       // Append slices instead of replacing
WithTypeCheck         // Validate type compatibility
WithSliceDeepCopy     // Deep merge slice elements
WithoutDereference    // Don't dereference pointers
WithTransformers      // Custom type transformers
```

**Use Cases:**
- Merging configuration from multiple sources
- Partial entity updates
- Default value application

---

### 14. metrics

**Purpose:** Pluggable metrics collection supporting multiple backends.

**Key Interface:**
```go
type MetricsProvider interface {
    RecordDBOperation(operation string, duration time.Duration, success bool)
    RecordDBRetry(operation string)
    SetDBConnections(count float64)
    RecordHTTPRequest(handler, method string, status int, duration time.Duration)
    RecordCircuitBreakerState(serviceName string, state string)
    RecordCircuitBreakerFailure(serviceName string)
    RecordCircuitBreakerSuccess(serviceName string)
    RecordServiceRequestCount(serviceName string)
    Shutdown(ctx context.Context) error
}
```

**Supported Backends:**
| Provider | Transport | Features |
|----------|-----------|----------|
| Prometheus | HTTP /metrics endpoint | Pull-based, histogram buckets |
| OpenTelemetry | OTLP exporter | Push-based, standard attributes |
| Datadog | StatsD UDP | Tags, namespace prefixing |

**Available Metrics:**
- `db_operation_duration_seconds` - Database operation latency histogram
- `db_connections_open` - Open connection gauge
- `db_operation_retries_total` - Retry counter
- `http_request_duration_seconds` - HTTP request latency
- `circuit_breaker_state` - Circuit breaker state gauge
- `circuit_breaker_failure_total` - Failure counter
- `circuit_breaker_success_total` - Success counter
- `service_request_count_total` - Request counter

**Graceful Degradation:** When disabled, uses `noopMetricsProvider` with no-op implementations.

---

### 15. middleware

**Purpose:** HTTP/gRPC middleware chain composition.

**Key Types:**
```go
type Handler func(ctx context.Context, req interface{}) (interface{}, error)

type Middleware func(Handler) Handler

func Chain(m ...Middleware) Middleware
```

**Usage Pattern:**
```go
// Chain multiple middleware
handler := middleware.Chain(
    loggingMiddleware,
    authMiddleware,
    tracingMiddleware,
)(finalHandler)
```

---

### 16. models

**Purpose:** Shared data models and interfaces.

**Key Types:**
```go
// Entity interface for generic CRUD operations
type Entity interface {
    GetID() int64
    GetUUID() string
}

// BaseModel with audit fields
type BaseModel struct {
    ID        int64
    UUID      string
    IsActive  bool
    CreatedBy string
    CreatedAt time.Time
    UpdatedBy *string
    UpdatedAt *time.Time
    DeletedBy *string
    DeletedAt *time.Time
}

// Identifier for flexible entity lookup
type Identifier struct {
    Id   int64
    Uuid string
}
```

**Protobuf Conversion:**
```go
func ProtoToUserIdentifier(pi *pb.Identifier) *Identifier
func UserIdentifierToProto(mi *Identifier) *pb.Identifier
```

---

### 17. p9context

**Purpose:** Context utilities for tenant isolation and database connection management.

**Key Types:**
```go
type DBContext struct {
    DBPool            *pgxpool.Pool  // Current tenant's pool
    DBPoolShared      *pgxpool.Pool  // Shared database pool (free tier)
    DBPoolIndependent map[string]*pgxpool.Pool // Dedicated pools (paid tier)
}
```

**Context Keys (expected):**
- Tenant ID extraction from context
- User ID extraction from context
- Request ID propagation

---

### 18. p9log

**Purpose:** Structured logging with multiple output targets.

**Key Interface:**
```go
type Logger interface {
    Log(level Level, keyvals ...interface{}) error
}
```

**Features:**
- Log levels: Debug, Info, Warn, Error
- Structured key-value logging
- Filtering by level, key, value, or custom function
- File rotation with size threshold
- Timestamp and caller information

**Usage Pattern:**
```go
// Create logger
logger := p9log.NewStdLogger(os.Stdout)

// Add context fields
logger = p9log.With(logger,
    "service.name", "my-service",
    "service.version", "v1.0.0",
    "ts", p9log.DefaultTimestamp,
    "caller", p9log.DefaultCaller,
)

// Log messages
logger.Log(p9log.LevelInfo, "msg", "User created", "user_id", userID)

// Use helper for simpler API
helper := p9log.NewHelper(logger)
helper.Info("User created")
helper.Infof("User %s created", userID)
helper.Infow("user_id", userID, "action", "create")
```

---

### 19. proto

**Purpose:** Shared protobuf definitions (.proto files).

**Files:**
| File | Description |
|------|-------------|
| `blob.proto` | Binary data handling |
| `data.proto` | Generic data wrappers |
| `errors.proto` | Error response structures |
| `fields.proto` | Field metadata |
| `filter.proto` | Query filtering |
| `geo.proto` | Geographic types |
| `identifier.proto` | Entity identification |
| `message.proto` | Message envelopes |
| `pagination.proto` | Pagination support |
| `query.proto` | Query parameters |
| `response.proto` | Standard responses |

---

### 20. server

**Purpose:** Server lifecycle management with graceful shutdown.

**Key Types:**
```go
type Server interface {
    Run(ctx context.Context) error
}

type MultiServer struct {
    servers []Server
}

func NewMultiServer(servers ...Server) *MultiServer
func (s *MultiServer) Run(ctx context.Context) error
```

**Sub-packages:**
| Package | Description |
|---------|-------------|
| `http` | HTTP server (Echo-based) |
| `grpc` | gRPC server |
| `shutdown` | Graceful shutdown utilities |
| `utils` | Server utilities |

**Features:**
- Concurrent server startup
- Signal handling (SIGINT, SIGTERM)
- Graceful shutdown with WaitGroup
- Context cancellation propagation

---

### 21. timeout

**Purpose:** Context timeout management with configurable defaults.

**Key Types:**
```go
type TimeoutProvider struct {
    cfg *config.Data
}

func NewTimeoutProvider(cfg *config.Data) *TimeoutProvider
func (c *TimeoutProvider) ApplyTimeout(ctx context.Context, isLongQuery bool) (context.Context, context.CancelFunc)
```

**Default Timeouts:**
- Standard queries: 30 seconds
- Long queries: 5 minutes

**Features:**
- Respects existing context deadlines
- Configurable via protobuf Duration
- Differentiated timeouts for long-running operations

---

### 22. tracing

**Purpose:** Distributed tracing with OpenTelemetry.

**Key Types:**
```go
type TracingProvider struct {
    tracer         otelTrace.Tracer
    tracerProvider *trace.TracerProvider
    cfg            *config.Observability
    serviceName    string
}

func NewProvider(cfg *config.Observability) (*TracingProvider, error)
func (p *TracingProvider) StartSpan(ctx context.Context, name string) (context.Context, otelTrace.Span)
func (p *TracingProvider) AddSpanTags(ctx context.Context, tags map[string]string)
func (p *TracingProvider) AddSpanError(ctx context.Context, err error)
func (p *TracingProvider) AddSpanEvent(ctx context.Context, name string, attributes map[string]string)
func (p *TracingProvider) Shutdown(ctx context.Context) error
```

**Supported Exporters:**
| Provider | Endpoint Format |
|----------|-----------------|
| Jaeger | `http://localhost:14268/api/traces` |
| Zipkin | `http://localhost:9411/api/v2/spans` |
| OTLP | `localhost:4318` |

**Features:**
- Parent-based sampling with configurable rate
- Service name resolution (config > env > default)
- Span context propagation
- Error and event recording

---

### 23. uow (Unit of Work)

**Purpose:** Transaction management pattern for database operations.

**Key Types:**
```go
type UnitOfWork interface {
    Commit(ctx context.Context) error
    Rollback(ctx context.Context) error
    Tx() pgx.Tx
}

type Factory interface {
    Begin(ctx context.Context) (UnitOfWork, error)
}
```

**Usage Pattern:**
```go
// Start transaction
uow, err := factory.Begin(ctx)
if err != nil {
    return err
}
defer uow.Rollback(ctx) // Safe - no-op if already committed

// Execute queries
queries := db.New(uow.Tx())
_, err = queries.CreateUser(ctx, params)
if err != nil {
    return err
}

// Commit on success
return uow.Commit(ctx)
```

**Helper Function:**
```go
func WithTransaction(ctx context.Context, pool *pgxpool.Pool, fn func(uow UnitOfWork) error) error
```

---

## Cross-Cutting Concerns

### Multi-Tenancy

The packages support hybrid multi-tenancy:
- **Free Tier:** Shared database with `tenant_id` column isolation
- **Paid Tier:** Dedicated database per tenant via `DBPoolIndependent`

Tenant resolution happens via:
1. Context extraction (`p9context`)
2. Database pool selection (`saas`, `pgxpostgres`)
3. Query scoping (automatic `tenant_id` WHERE clauses)

### Dependency Injection

All packages integrate with Uber's fx framework:
```go
fx.New(
    fx.Provide(
        deps.NewServiceDeps,
        metrics.NewProvider,
        tracing.NewProvider,
        cache.NewCache,
        // ...
    ),
)
```

### Error Handling

Standardized error flow:
1. Database errors → `errors/database` → domain errors
2. Domain errors → `errors.Error` with code/reason
3. Handler errors → gRPC status or HTTP response
4. Client receives structured error with metadata

### Observability Stack

```
Request → Tracing Span → Handler → Metrics → Logger
              ↓                       ↓         ↓
           Jaeger/OTLP         Prometheus    stdout/file
```

---

## Package Dependencies Graph

```
                    ┌─────────────────────────────────────────┐
                    │              Application                │
                    └─────────────────────────────────────────┘
                                        │
                    ┌───────────────────┼───────────────────┐
                    ▼                   ▼                   ▼
              ┌─────────┐         ┌─────────┐         ┌─────────┐
              │  deps   │         │ helpers │         │ server  │
              └────┬────┘         └────┬────┘         └────┬────┘
                   │                   │                   │
    ┌──────────────┼──────────────┬────┴────┬──────────────┤
    ▼              ▼              ▼         ▼              ▼
┌───────┐    ┌─────────┐    ┌─────────┐ ┌───────┐    ┌──────────┐
│metrics│    │ tracing │    │  cache  │ │ p9log │    │middleware│
└───┬───┘    └────┬────┘    └────┬────┘ └───┬───┘    └────┬─────┘
    │             │              │          │             │
    └─────────────┼──────────────┼──────────┼─────────────┘
                  ▼              ▼          ▼
              ┌──────────────────────────────────┐
              │           api/v1/config          │
              └──────────────────────────────────┘
                               │
    ┌──────────────────────────┼──────────────────────────┐
    ▼                          ▼                          ▼
┌───────┐                ┌──────────┐                ┌─────────┐
│errors │                │ database │                │ events  │
└───┬───┘                └────┬─────┘                └────┬────┘
    │                         │                          │
    └─────────────────────────┼──────────────────────────┘
                              ▼
                    ┌──────────────────┐
                    │  External Deps   │
                    │ (pgx, kafka, etc)│
                    └──────────────────┘
```

---

## Best Practices

### Using ServiceDeps

Always inject `ServiceDeps` rather than individual dependencies:
```go
type Handler struct {
    deps deps.ServiceDeps
}

func (h *Handler) CreateUser(ctx context.Context, req *pb.CreateUserRequest) (*pb.CreateUserResponse, error) {
    // Logging
    h.deps.Log.Info("Creating user")

    // Metrics
    start := time.Now()
    defer func() {
        h.deps.Metrics.RecordDBOperation("CreateUser", time.Since(start), err == nil)
    }()

    // Tracing
    ctx, span := h.deps.Tracing.StartSpan(ctx, "CreateUser")
    defer span.End()

    // Caching (if applicable)
    if cached, ok := h.deps.Cache.Get(cacheKey); ok {
        return cached.(*pb.CreateUserResponse), nil
    }

    // Business logic...
}
```

### Error Handling

```go
import "kosha/errors"

// Create domain errors
var ErrUserNotFound = errors.New(404, "USER_NOT_FOUND", "User not found")

// Use in handlers
if user == nil {
    return nil, ErrUserNotFound.WithMetadata(map[string]string{
        "user_id": userID,
    })
}

// Wrap underlying errors
return nil, errors.InternalServer("DB_ERROR", "Failed to query database").WithCause(err)
```

### Event Publishing

```go
event := domain.NewEventBuilder(domain.EventTypeUserCreated, userID, "User").
    WithData(map[string]interface{}{
        "email": user.Email,
        "name":  user.Name,
    }).
    WithPriority(domain.PriorityMedium).
    WithSource("user-service").
    WithCorrelationID(p9context.RequestID(ctx)).
    Build()

if err := h.deps.KafkaProducer.Publish(ctx, event); err != nil {
    h.deps.Log.Error("Failed to publish event", "error", err)
}
```

---

## Configuration Reference

### Environment Variables

```env
# Logging
LOG_LEVEL=info
LOG_FORMAT=json

# Database
DB_HOST=localhost
DB_PORT=5432
DB_NAME=Samavāya
DB_USER=postgres
DB_PASSWORD=password

# Metrics
METRICS_ENABLED=true
METRICS_PROVIDER=prometheus
METRICS_PORT=9090

# Tracing
TRACING_ENABLED=true
TRACING_PROVIDER=jaeger
TRACING_ENDPOINT=http://localhost:14268/api/traces
TRACING_SAMPLING_RATE=0.5

# Cache
CACHE_ENABLED=true
CACHE_DEFAULT_TTL=5m
CACHE_MAX_ENTRIES=10000

# Kafka
KAFKA_BROKERS=localhost:9092
KAFKA_GROUP_ID=Samavāya-consumer

# Service
SERVICE_NAME=my-service
```
