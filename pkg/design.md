# Kosha - Design Document

## Architecture Overview

### Layered Architecture

```
┌─────────────────────────────────────────────────────────┐
│                   Application Layer                      │
│              (Consumer Microservices)                    │
└─────────────────────────────────────────────────────────┘
                          ▲
                          │ imports p9e.in/samavaya/packages/*
                          │
┌─────────────────────────────────────────────────────────┐
│                    Kosha Package                         │
│  ┌───────────────────────────────────────────────────┐  │
│  │           Service Dependencies (ServiceDeps)      │  │
│  │  - Config, Pool, Logger, Metrics, Cache, Events  │  │
│  └───────────────────────────────────────────────────┘  │
│                                                          │
│  ┌─────────────┐  ┌──────────────┐  ┌───────────────┐  │
│  │  Database   │  │  Middleware  │  │   Helpers     │  │
│  │  - pgx      │  │  - tenant    │  │  - repo       │  │
│  │  - query    │  │  - auth      │  │  - service    │  │
│  │  - filter   │  │  - recovery  │  │  - utils      │  │
│  └─────────────┘  └──────────────┘  └───────────────┘  │
│                                                          │
│  ┌─────────────┐  ┌──────────────┐  ┌───────────────┐  │
│  │   Events    │  │   Errors     │  │   Config      │  │
│  │  - kafka    │  │  - structured│  │  - file       │  │
│  │  - producer │  │  - mapping   │  │  - env        │  │
│  │  - consumer │  │  - wrapping  │  │  - validation │  │
│  └─────────────┘  └──────────────┘  └───────────────┘  │
│                                                          │
│  ┌─────────────┐  ┌──────────────┐  ┌───────────────┐  │
│  │   Logging   │  │   Metrics    │  │   Tracing     │  │
│  │  - p9log    │  │  - prom      │  │  - otel       │  │
│  │  - zap      │  │  - otel      │  │  - jaeger     │  │
│  │  - fields   │  │  - datadog   │  │  - zipkin     │  │
│  └─────────────┘  └──────────────┘  └───────────────┘  │
└─────────────────────────────────────────────────────────┘
                          ▲
                          │
┌─────────────────────────────────────────────────────────┐
│              Infrastructure Layer                        │
│  PostgreSQL │ Kafka │ Redis │ Jaeger │ Prometheus       │
└─────────────────────────────────────────────────────────┘
```

## Core Components Design

### 1. Unit of Work Pattern Implementation

**Purpose**: Manage database transactions consistently across the application

**Design**:
```go
// uow/uow.go
package uow

import (
    "context"
    "github.com/jackc/pgx/v5"
    "github.com/jackc/pgx/v5/pgxpool"
)

// UnitOfWork manages a database transaction
type UnitOfWork struct {
    tx     pgx.Tx
    ctx    context.Context
    pool   *pgxpool.Pool
    closed bool
}

// NewUnitOfWork creates a new unit of work from a connection pool
func NewUnitOfWork(ctx context.Context, pool *pgxpool.Pool) (*UnitOfWork, error)

// Commit commits the transaction
func (uow *UnitOfWork) Commit() error

// Rollback rolls back the transaction
func (uow *UnitOfWork) Rollback() error

// Tx returns the underlying transaction
func (uow *UnitOfWork) Tx() pgx.Tx

// WithTransaction executes a function within a transaction
func WithTransaction(ctx context.Context, pool *pgxpool.Pool, fn func(uow *UnitOfWork) error) error
```

**Usage Pattern**:
```go
err := uow.WithTransaction(ctx, deps.Pool, func(uow *uow.UnitOfWork) error {
    // All operations use uow.Tx()
    _, err := uow.Tx().Exec(ctx, "INSERT INTO users...")
    if err != nil {
        return err // Auto rollback
    }

    _, err = uow.Tx().Exec(ctx, "INSERT INTO profiles...")
    return err // Auto commit if nil
})
```

### 2. Security Context Extraction

**Problem**: Hardcoded "admin" in 19 locations
**Solution**: Extract from JWT claims in request context

**Design**:
```go
// p9context/security_context.go
package p9context

import (
    "context"
    "p9e.in/samavaya/packages/database/pgxpostgres/validator"
)

type securityContextKey struct{}

// SetSecurityContext stores security context in request context
func SetSecurityContext(ctx context.Context, userID, role string) context.Context

// GetSecurityContext retrieves security context from request context
func GetSecurityContext(ctx context.Context) *validator.SecurityContext

// GetSecurityContextOrDefault returns security context or "system" default
func GetSecurityContextOrDefault(ctx context.Context) *validator.SecurityContext
```

**Integration with authz**:
```go
// authz/interceptor.go
func enrichContext(ctx context.Context, user *InjectedUserInfo) context.Context {
    // Store security context
    ctx = p9context.SetSecurityContext(ctx, user.UserID, user.Role)
    // Also store for legacy access
    ctx = context.WithValue(ctx, "user_id", user.UserID)
    return ctx
}
```

**Query Builder Update**:
```go
// database/pgxpostgres/builder/builder.go
func BuildSelectQuery(...) {
    // Before: secCtx := validator.NewSecurityContext("admin")
    // After:
    secCtx := p9context.GetSecurityContextOrDefault(ctx)

    // Validate query with actual user context
    if err := secCtx.ValidateQuery(query); err != nil {
        return "", nil, err
    }
}
```

### 3. Consolidated Query Builder

**Current State**: 3 packages with overlap
- `database/pgxpostgres/builder/` (664 lines)
- `database/pgxpostgres/filter/` (714 lines)
- `database/pgxpostgres/operations/` (599 lines)

**Proposed Structure**:
```
database/pgxpostgres/
├── postgres.go              # Connection management
├── query/
│   ├── builder.go           # SELECT, INSERT, UPDATE, DELETE builders
│   ├── filter.go            # WHERE clause and filter operations
│   ├── executor.go          # Query execution with context
│   ├── validator.go         # SQL injection prevention
│   └── query.go             # Shared types and interfaces
├── tenantDB/
└── retry/
```

**Unified API**:
```go
// query/builder.go
type QueryBuilder struct {
    tableName  string
    columns    []string
    where      *FilterBuilder
    orderBy    []string
    limit      int
    offset     int
    validator  *validator.SecurityContext
}

func NewQueryBuilder(ctx context.Context, tableName string) *QueryBuilder
func (qb *QueryBuilder) Select(columns ...string) *QueryBuilder
func (qb *QueryBuilder) Where(filter *FilterBuilder) *QueryBuilder
func (qb *QueryBuilder) OrderBy(columns ...string) *QueryBuilder
func (qb *QueryBuilder) Limit(n int) *QueryBuilder
func (qb *QueryBuilder) Build() (string, []interface{}, error)
func (qb *QueryBuilder) Execute(ctx context.Context, pool *pgxpool.Pool) (pgx.Rows, error)
func (qb *QueryBuilder) ExecuteOne(ctx context.Context, pool *pgxpool.Pool, dest interface{}) error
```

### 4. Helper Function Deduplication

**Problem**: 70% duplication in boilerplate code
**Solution**: Middleware/decorator pattern

**Design**:
```go
// helpers/middleware.go
package helpers

// OperationContext contains common operation metadata
type OperationContext struct {
    OperationName string
    EntityType    string
    StartTime     time.Time
    Ctx           context.Context
    Deps          *deps.ServiceDeps
}

// WithObservability wraps an operation with tracing, metrics, and logging
func WithObservability[T any](
    ctx context.Context,
    deps *deps.ServiceDeps,
    operationName string,
    fn func(opCtx *OperationContext) (T, error),
) (T, error) {
    // Create operation context
    opCtx := &OperationContext{
        OperationName: operationName,
        StartTime:     time.Now(),
        Ctx:           deps.Tp.ApplyTimeout(ctx, false),
        Deps:          deps,
    }

    // Start tracing span
    ctx, span := deps.Tracing.Tracer.Start(opCtx.Ctx, operationName)
    defer span.End()
    opCtx.Ctx = ctx

    // Execute operation
    result, err := fn(opCtx)

    // Record metrics
    duration := time.Since(opCtx.StartTime)
    deps.Metrics.RecordOperation(operationName, duration, err == nil)

    // Log on error
    if err != nil {
        deps.Log.Error("Operation failed",
            p9log.String("operation", operationName),
            p9log.Error(err),
        )
    }

    return result, err
}
```

**Updated Helpers**:
```go
// helpers/repo/getEntity.go
func GetByID[T any](ctx context.Context, deps *deps.ServiceDeps, tableName string, id int64) (*T, error) {
    return WithObservability(ctx, deps, "GetByID", func(opCtx *OperationContext) (*T, error) {
        // Core logic only
        query := fmt.Sprintf("SELECT * FROM %s WHERE id = $1", tableName)
        var result T
        err := opCtx.Deps.Pool.QueryRow(opCtx.Ctx, query, id).Scan(&result)
        return &result, err
    })
}
```

### 5. Metrics Provider Simplification

**Current**: 3 inline implementations
**Proposed**: Strategy pattern with pluggable backends

**Design**:
```go
// metrics/backend.go
package metrics

type Backend interface {
    RecordCounter(name string, value float64, tags map[string]string)
    RecordHistogram(name string, value float64, tags map[string]string)
    RecordGauge(name string, value float64, tags map[string]string)
    Close() error
}

// metrics/provider.go
type MetricsProvider struct {
    backend Backend
    enabled bool
}

func NewMetricsProvider(backendType string, config interface{}) (*MetricsProvider, error) {
    var backend Backend
    switch backendType {
    case "prometheus":
        backend = NewPrometheusBackend(config)
    case "otel":
        backend = NewOTELBackend(config)
    case "datadog":
        backend = NewDatadogBackend(config)
    default:
        backend = NewNoopBackend()
    }
    return &MetricsProvider{backend: backend, enabled: true}, nil
}
```

**Separate Implementations**:
```
metrics/
├── metrics.go           # Main provider (150 lines)
├── backend.go           # Interface (20 lines)
├── prometheus/
│   └── prometheus.go    # Prometheus backend (120 lines)
├── otel/
│   └── otel.go          # OpenTelemetry backend (120 lines)
├── datadog/
│   └── datadog.go       # Datadog backend (120 lines)
└── noop/
    └── noop.go          # No-op backend (30 lines)
```

### 6. Error Handling Standardization

**Problem**: Inconsistent return patterns
- Some: `(*T, error)`
- Others: `(T, error)`

**Standard**: Use `(T, error)` for value types, `(*T, error)` for pointers

**Design Guideline**:
```go
// For entities (database records, DTOs)
func GetUser(ctx context.Context, id int64) (*User, error)  // ✅ Pointer

// For simple values
func CalculateTotal(items []Item) (float64, error)  // ✅ Value

// For slices/maps (already reference types)
func ListUsers(ctx context.Context) ([]User, error)  // ✅ Slice

// For protobuf messages
func GetResponse(ctx context.Context) (*pb.Response, error)  // ✅ Pointer
```

**Refactoring Strategy**:
1. Identify all public functions with inconsistent patterns
2. Update signatures to follow standard
3. Update all call sites
4. Add linter rule to enforce (e.g., via golangci-lint)

### 7. Production Panic Replacement

**Current Issues**:
```go
// saas/provider.go:47,51
if err != nil {
    panic("cannot create tenant connection pool")
}
```

**Solution**: Error propagation with context

**Design**:
```go
// saas/provider.go
func (p *DbProvider) GetTenantPool(ctx context.Context, tenantID string) (*pgxpool.Pool, error) {
    // Check cache first
    if pool, ok := p.pools.Load(tenantID); ok {
        return pool.(*pgxpool.Pool), nil
    }

    // Get connection string
    connStr, err := p.getTenantConnectionString(tenantID)
    if err != nil {
        // Return error instead of panic
        return nil, errors.New(
            errors.CodeInternal,
            "TENANT_CONFIG_ERROR",
            fmt.Sprintf("Failed to get tenant connection string: %v", err),
        )
    }

    // Create pool
    pool, err := pgxpool.New(ctx, connStr)
    if err != nil {
        // Return error instead of panic
        return nil, errors.New(
            errors.CodeInternal,
            "POOL_CREATION_ERROR",
            fmt.Sprintf("Failed to create tenant pool: %v", err),
        )
    }

    // Store and return
    p.pools.Store(tenantID, pool)
    return pool, nil
}
```

**Graceful Degradation**:
```go
// middleware/dbmiddleware/dbmiddleware.go
func (m *DBMiddleware) GetPool(ctx context.Context) (*pgxpool.Pool, error) {
    tenant := p9context.GetCurrentTenant(ctx)

    // Try tenant-specific pool
    pool, err := m.provider.GetTenantPool(ctx, tenant.ID)
    if err != nil {
        // Log error but fall back to shared pool
        m.log.Warn("Failed to get tenant pool, using shared",
            p9log.String("tenant_id", tenant.ID),
            p9log.Error(err),
        )
        return m.sharedPool, nil
    }

    return pool, nil
}
```

## Data Flow Diagrams

### Request Processing Flow
```
Client Request
    │
    ├─> HTTP/gRPC Server
    │       │
    │       ├─> Middleware Chain
    │       │     ├─> CORS (HTTP only)
    │       │     ├─> Recovery (panic → error)
    │       │     ├─> Tenant Extraction
    │       │     ├─> Auth/JWT Validation
    │       │     ├─> Security Context Setup
    │       │     └─> DB Pool Resolution
    │       │
    │       ├─> Handler/Service
    │       │     ├─> Apply Timeout
    │       │     ├─> Cache Check (optional)
    │       │     ├─> Unit of Work (if writing)
    │       │     ├─> Query Builder
    │       │     ├─> Database Execute
    │       │     └─> Event Publish (optional)
    │       │
    │       └─> Response Encoding
    │             ├─> Error Mapping
    │             ├─> Codec Selection
    │             └─> Serialization
    │
    └─> Client Response
```

### Database Operation Flow (with UoW)
```
Service Handler
    │
    ├─> WithTransaction(ctx, pool, func)
    │       │
    │       ├─> Begin Transaction
    │       │
    │       ├─> Execute Operations
    │       │     ├─> Operation 1 (uses UoW.Tx())
    │       │     ├─> Operation 2 (uses UoW.Tx())
    │       │     └─> Operation N (uses UoW.Tx())
    │       │
    │       ├─> Error Check
    │       │     ├─> If Error → Rollback
    │       │     └─> If Success → Commit
    │       │
    │       └─> Return Result
    │
    └─> Handle Result/Error
```

## Implementation Plan

### Sprint 1: Critical Fixes (Week 1)
1. Remove debug logging (fmt.Printf)
2. Replace production panics
3. Implement security context extraction
4. Update all query builders to use security context

### Sprint 2: Core Infrastructure (Week 2)
1. Implement Unit of Work pattern
2. Update all database operations to use UoW
3. Add transaction support to helpers

### Sprint 3: Consolidation (Week 3)
1. Consolidate query builders
2. Deduplicate helper functions
3. Standardize error returns

### Sprint 4: Optimization (Week 4)
1. ✅ Simplify metrics providers - Documented (kept strategy pattern)
2. ✅ Remove Wire/Echo if possible - Wire removed (6 files), Echo evaluation pending
3. ✅ Address remaining TODOs - Completed (128 → 1, 99.2% reduction)

### Sprint 5: Documentation & Testing (Week 5)
1. Add package documentation
2. Write unit tests
3. Write integration tests
4. Update CLAUDE.md

## Testing Strategy

### Unit Tests
```go
// uow/uow_test.go
func TestUnitOfWork_Commit(t *testing.T)
func TestUnitOfWork_Rollback(t *testing.T)
func TestWithTransaction_Success(t *testing.T)
func TestWithTransaction_Rollback(t *testing.T)

// p9context/security_context_test.go
func TestSetSecurityContext(t *testing.T)
func TestGetSecurityContext(t *testing.T)
func TestGetSecurityContextOrDefault(t *testing.T)

// query/builder_test.go
func TestQueryBuilder_Build(t *testing.T)
func TestQueryBuilder_SQLInjection(t *testing.T)
```

### Integration Tests
```go
//go:build integration

// database/pgxpostgres/integration_test.go
func TestQueryBuilder_WithRealDB(t *testing.T)
func TestUnitOfWork_WithRealDB(t *testing.T)
func TestSecurityContext_WithRealDB(t *testing.T)
```

## Performance Considerations

1. **Connection Pooling**: Max 30 connections per pool
2. **Query Caching**: Cache query results where appropriate
3. **Lazy Loading**: Create tenant pools on-demand
4. **Circuit Breaker**: Prevent cascade failures
5. **Timeout Management**: Consistent timeout application

## Security Considerations

1. **SQL Injection**: Always use parameterized queries
2. **JWT Validation**: Verify signature and expiration
3. **Security Context**: Extract from authenticated requests only
4. **Connection Strings**: Store in environment, never hardcode
5. **Error Messages**: Don't leak sensitive information

## Monitoring & Observability

1. **Metrics**: Track all database operations
2. **Tracing**: Span for every significant operation
3. **Logging**: Structured logs at all layers
4. **Alerts**: Error rate, latency, pool exhaustion
5. **Dashboards**: Connection pools, query performance

## Migration Notes

### For Consumers
1. UoW pattern is opt-in initially
2. Old query methods continue to work
3. Security context is backward compatible (defaults to "system")
4. No breaking API changes

---

## Sprint 4: Query Builder Enhancements

### Goals
Enhance the dynamic query builder with SQLC-inspired features while maintaining full dynamic query capabilities.

### Design: Schema-Based Column Validation (TSK-021)

**Problem**: Typos in field names only discovered at runtime when SQL fails.

**Solution**: Runtime validation against schema metadata.

```go
// database/pgxpostgres/builder/typed_query.go
package builder

type TypedQuery[T any] struct {
    tableName  string
    columns    []string            // All valid columns
    validCols  map[string]bool     // Fast lookup
}

// NewTypedQuery creates a query builder with schema metadata
func NewTypedQuery[T any]() *TypedQuery[T] {
    var zero T
    tableName := getTableName(zero)  // From struct tags
    columns := getColumns(zero)       // From struct tags

    validCols := make(map[string]bool, len(columns))
    for _, col := range columns {
        validCols[col] = true
    }

    return &TypedQuery[T]{
        tableName: tableName,
        columns:   columns,
        validCols: validCols,
    }
}

// ValidateFieldMask ensures all field mask paths are valid
func (tq *TypedQuery[T]) ValidateFieldMask(paths []string) error {
    invalid := []string{}

    for _, path := range paths {
        if !tq.validCols[path] {
            invalid = append(invalid, path)
        }
    }

    if len(invalid) > 0 {
        return fmt.Errorf(
            "invalid fields: %v (valid columns: %v)",
            invalid,
            tq.columns,
        )
    }

    return nil
}

// BuildDataModel creates DataModel with validation
func (tq *TypedQuery[T]) BuildDataModel(
    fieldMask []string,
    where string,
    args []any,
) (models.DataModel[T], error) {
    // Validate field mask
    if len(fieldMask) > 0 {
        if err := tq.ValidateFieldMask(fieldMask); err != nil {
            return models.DataModel[T]{}, err
        }
    }

    return models.DataModel[T]{
        TableName:  tq.tableName,
        FieldNames: fieldMask,
        Where:      where,
        WhereArgs:  args,
    }, nil
}
```

**Usage**:
```go
// In service initialization
type UserService struct {
    deps      deps.ServiceDeps
    userQuery *builder.TypedQuery[User]
}

func NewUserService(deps deps.ServiceDeps) *UserService {
    return &UserService{
        deps:      deps,
        userQuery: builder.NewTypedQuery[User](),
    }
}

// In handler
func (s *UserService) ListUsers(ctx context.Context, req *pb.ListUsersRequest) error {
    // Validate field mask against schema
    if req.FieldMask != nil {
        if err := s.userQuery.ValidateFieldMask(req.FieldMask.Paths); err != nil {
            return status.Error(codes.InvalidArgument, err.Error())
            // Error: "invalid fields: [naem, emil] (valid columns: [id, uuid, name, email, status])"
        }
    }

    // Proceed with query...
}
```

**Benefits**:
- ✅ Catch typos before SQL execution
- ✅ Clear error messages with suggestions
- ✅ Zero performance impact on valid queries
- ✅ No code generation required
- ✅ Works with existing DataModel pattern

---

### Design: Query Logging (TSK-022)

**Problem**: Difficult to debug complex dynamic queries.

**Solution**: Optional SQL query logging with sanitized parameters.

```go
// database/pgxpostgres/operations/query_logger.go
package operations

type QueryLogger struct {
    logger  p9log.Logger
    enabled bool
    level   string  // "debug", "info", "warn"
}

func NewQueryLogger(cfg *config.Data, logger p9log.Logger) *QueryLogger {
    return &QueryLogger{
        logger:  logger,
        enabled: cfg.Database.LogQueries,
        level:   cfg.Database.QueryLogLevel,
    }
}

// LogQuery logs SQL query with parameters (sanitized)
func (ql *QueryLogger) LogQuery(
    ctx context.Context,
    query string,
    args []any,
    duration time.Duration,
    rowCount int,
    err error,
) {
    if !ql.enabled {
        return
    }

    // Sanitize sensitive parameters
    sanitizedArgs := sanitizeArgs(args)

    fields := []p9log.Field{
        p9log.String("sql", query),
        p9log.Any("args", sanitizedArgs),
        p9log.Int64("duration_ms", duration.Milliseconds()),
        p9log.Int("row_count", rowCount),
    }

    if err != nil {
        fields = append(fields, p9log.Error(err))
    }

    helper := p9log.NewHelper(p9log.With(ql.logger, "query"))

    switch ql.level {
    case "debug":
        helper.Debugw("SQL query executed", fields...)
    case "info":
        if duration > 100*time.Millisecond { // Slow query
            helper.Infow("Slow SQL query", fields...)
        }
    case "warn":
        if duration > 500*time.Millisecond {
            helper.Warnw("Very slow SQL query", fields...)
        }
    }
}

// sanitizeArgs redacts sensitive data
func sanitizeArgs(args []any) []any {
    sanitized := make([]any, len(args))
    for i, arg := range args {
        switch v := arg.(type) {
        case string:
            if isSensitive(v) {
                sanitized[i] = "***REDACTED***"
            } else {
                sanitized[i] = v
            }
        default:
            sanitized[i] = v
        }
    }
    return sanitized
}
```

**Configuration**:
```toml
# config.toml
[database]
log_queries = false  # Disabled by default
query_log_level = "debug"  # debug|info|warn

[database.sensitive_fields]
patterns = ["password", "token", "secret", "api_key"]
```

**Example Output**:
```json
{
  "level": "debug",
  "ts": "2025-12-01T10:30:45Z",
  "caller": "operations/query_logger.go:42",
  "msg": "SQL query executed",
  "sql": "SELECT id, uuid, name, email FROM users WHERE name ILIKE $1 AND status = $2 LIMIT $3",
  "args": ["%john%", "active", 20],
  "duration_ms": 45,
  "row_count": 12
}
```

**Benefits**:
- ✅ See actual executed SQL
- ✅ Parameter values visible (sanitized)
- ✅ Query performance tracking
- ✅ Disabled by default (zero overhead)
- ✅ Configurable log levels

---

### Design: Query Performance Metrics (TSK-023)

**Problem**: No visibility into which tables/operations are slow.

**Solution**: Per-table, per-operation metrics.

```go
// metrics/query_metrics.go
package metrics

type QueryMetrics struct {
    provider MetricsProvider
}

// RecordQuery records detailed query metrics
func (qm *QueryMetrics) RecordQuery(
    table string,
    operation string,  // SELECT, INSERT, UPDATE, DELETE
    duration time.Duration,
    rowCount int,
    success bool,
) {
    // Per-table metrics
    qm.provider.RecordHistogram(
        fmt.Sprintf("db.query.duration.%s", table),
        duration.Milliseconds(),
        map[string]string{
            "operation": operation,
            "table":     table,
        },
    )

    // Row count distribution
    qm.provider.RecordHistogram(
        fmt.Sprintf("db.query.rows.%s", table),
        float64(rowCount),
        map[string]string{
            "operation": operation,
            "table":     table,
        },
    )

    // Success rate
    qm.provider.RecordCounter(
        "db.query.total",
        1,
        map[string]string{
            "table":     table,
            "operation": operation,
            "status":    successStatus(success),
        },
    )

    // Slow query alert
    if duration > 500*time.Millisecond {
        qm.provider.RecordCounter(
            "db.query.slow",
            1,
            map[string]string{
                "table":     table,
                "operation": operation,
            },
        )
    }
}
```

**Integration**:
```go
// In operations/operations.go
func ExecuteQuery[T any](
    ctx context.Context,
    pool *pgxpool.Pool,
    dm *models.DataModel[T],
    queryType QueryType,
) (T, error) {
    start := time.Now()

    // Execute query
    result, rowCount, err := executeQueryInternal(ctx, pool, dm, queryType)

    // Record metrics
    queryMetrics.RecordQuery(
        dm.TableName,
        string(queryType),
        time.Since(start),
        rowCount,
        err == nil,
    )

    return result, err
}
```

**Prometheus Metrics**:
```promql
# Query duration by table and operation
db_query_duration_ms{table="users", operation="SELECT"} histogram

# Slow queries count
rate(db_query_slow{table="users"}[5m])

# Success rate
rate(db_query_total{status="success"}[5m]) /
rate(db_query_total[5m])

# Top 5 slowest tables
topk(5, avg(db_query_duration_ms) by (table))
```

**Benefits**:
- ✅ Identify slow tables/operations
- ✅ Track query performance trends
- ✅ Alert on slow queries
- ✅ Optimize based on real data
- ✅ Per-operation breakdowns

---

### Implementation Plan

**Phase 1: Schema Validation (TSK-021)**
1. Create TypedQuery[T] struct
2. Add getTableName() and getColumns() reflection helpers
3. Implement ValidateFieldMask()
4. Add validation to helpers/repo functions
5. Write tests with invalid field names

**Phase 2: Query Logging (TSK-022)**
1. Create QueryLogger with config support
2. Add sanitizeArgs() for sensitive data
3. Integrate into operations.ExecuteQuery()
4. Add configuration options
5. Write tests with various log levels

**Phase 3: Query Metrics (TSK-023)**
1. Extend MetricsProvider interface
2. Create QueryMetrics wrapper
3. Integrate into all query execution paths
4. Add Prometheus exporters
5. Create Grafana dashboard templates

---

### Testing Strategy

```go
// Test schema validation
func TestTypedQueryValidation(t *testing.T) {
    tq := builder.NewTypedQuery[User]()

    // Valid fields
    err := tq.ValidateFieldMask([]string{"id", "name", "email"})
    assert.NoError(t, err)

    // Invalid fields
    err = tq.ValidateFieldMask([]string{"naem", "emil"})  // Typos
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "invalid fields: [naem, emil]")
    assert.Contains(t, err.Error(), "valid columns:")
}

// Test query logging
func TestQueryLogger(t *testing.T) {
    logger := &mockLogger{}
    ql := NewQueryLogger(enabledConfig, logger)

    ql.LogQuery(ctx, "SELECT * FROM users WHERE email = $1", []string{"user@example.com"}, 50*time.Millisecond, 1, nil)

    assert.Equal(t, 1, logger.CallCount)
    assert.Contains(t, logger.LastMessage, "SQL query executed")
}

// Test query metrics
func TestQueryMetrics(t *testing.T) {
    metrics := &mockMetrics{}
    qm := &QueryMetrics{provider: metrics}

    qm.RecordQuery("users", "SELECT", 100*time.Millisecond, 50, true)

    assert.Equal(t, 3, metrics.RecordCount)  // histogram, counter, success
}
```

---

### Documentation Updates

1. Update [BUILDER_EXAMPLES.md](BUILDER_EXAMPLES.md) with validation examples
2. Add query logging configuration to README
3. Document metrics in [metrics/metrics.go](metrics/metrics.go)
4. Add Grafana dashboard JSON to docs/
5. Update [CLAUDE.md](CLAUDE.md) with new patterns

---

*Design Status: Planned*
*Target Sprint: S4*
*Dependencies: None*
