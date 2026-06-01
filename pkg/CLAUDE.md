# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Kosha** is a shared packages module (`p9e.in/samavaya/packages`) providing infrastructure components, utilities, and cross-cutting concerns for the UGCL microservices platform. It serves as the foundation layer for database access, logging, metrics, tracing, configuration, event messaging, multi-tenancy, and more.

## Common Development Commands

### Testing
```bash
# Test specific package
go test ./database/...
go test ./errors/...

# Test all packages
go test ./...

# Test with coverage
go test -cover ./...

# Run integration tests (if tagged)
go test -tags=integration ./...
```

### Building
```bash
# Build all packages (verify compilation)
go build ./...

# Build custom protoc plugin
go build ./cmd/protoc-gen-go-errors
```

### Code Quality
```bash
# Format code
go fmt ./...

# Run linter (if configured)
golangci-lint run

# Tidy dependencies
go mod tidy

# Update dependencies
go get -u ./...
```

### Protobuf Code Generation
```bash
# Generate Go code from proto files (requires protoc and plugins)
protoc --go_out=. --go_opt=paths=source_relative \
  --go-errors_out=. --go-errors_opt=paths=source_relative \
  proto/*.proto
```

## Architecture Overview

### Dependency Injection Pattern

The codebase uses **Google Wire** and **Uber FX** for dependency injection. The central structure is `ServiceDeps` (in `deps/deps.go`):

```go
type ServiceDeps struct {
    Cfg           *config.Server
    Pool          *pgxpool.Pool              // Database connection pool
    Tp            *timeout.TimeoutProvider
    Metrics       metrics.MetricsProvider
    Log           p9log.Logger
    Cache         *cache.CacheProvider
    KafkaProducer *producer.KafkaProducer
    KafkaConsumer *consumer.KafkaConsumer
    Tracing       *tracing.TracingProvider
}
```

**All services receive `ServiceDeps` as a dependency**, providing centralized access to infrastructure components. When working with handlers or services, always inject `ServiceDeps`.

### Middleware Chain Pattern

Middleware is composable using the `Chain` function in `middleware/middleware.go`:

```go
type Handler func(ctx context.Context, req interface{}) (interface{}, error)
type Middleware func(Handler) Handler

// Chain composes middlewares in reverse order
func Chain(m ...Middleware) Middleware
```

**Active middleware:**
- `dbmiddleware`: Tenant database resolution (shared vs independent pools)
- `tenant`: Tenant info extraction from headers (X-Tenant-ID/X-Tenant-Name)
- `localize`: Internationalization (i18n)
- `recovery`: Panic recovery
- CORS handling

When adding new middleware, follow this pattern and register it in the chain.

### Multi-Server Architecture

The `server/` package supports running multiple servers (gRPC, HTTP, custom) concurrently:

- **gRPC Server** (`server/grpc/`): With interceptor chains and tenant middleware
- **HTTP Server** (`server/http/`): gRPC-Gateway for REST APIs with CORS
- **MultiServer** (`server/server.go`): Coordinates parallel servers with graceful shutdown

All servers implement the `Server` interface:
```go
interface Server {
    Run(ctx context.Context) error
}
```

### Database Layer (pgxpostgres)

**Connection Management:**
- Uses `pgx/v5` with `pgxpool.Pool` for PostgreSQL
- Default pool: max 30 connections, 10 idle, 60s lifetime, 10s idle timeout
- Health checks on initialization via ping and query test
- Location: `database/pgxpostgres/postgres.go`

**Multi-Tenancy Support:**
- **Shared database pool**: Default for all tenants
- **Independent database pools**: Dynamically created per tenant
- Tenant resolution via `middleware/dbmiddleware/` using context info
- DB routing decision: Check if tenant has independent DB, otherwise use shared

**Transaction Support:**
- Unit of Work pattern interfaces in `uow/`
- Context-based timeout application
- Operations query builder in `database/pgxpostgres/operations/`

**Helper Functions:**
Generic repository operations with type safety (in `helpers/repo/`):
```go
GetByID[T any](ctx, deps, tableName, id) (*T, error)
CreateEntity[T any](ctx, deps, entity) (*T, error)
UpdateEntity[T any](ctx, deps, entity) error
ListEntity[T any](ctx, deps, dataModel) ([]T, error)
```

Use these instead of writing repetitive repository code.

### Request Flow

```
HTTP/gRPC Request
    ↓
Middleware Chain (Localize → DB Resolution → Tenant Extraction → CORS/Recovery)
    ↓
Handler/Interceptor (with ServiceDeps)
    ↓
Apply Timeout (via TimeoutProvider)
    ↓
Cache Check (optional, before DB)
    ↓
Database Query (pgx with tenant-specific pool)
    ↓
Event Publishing (Kafka, optional)
    ↓
Response Encoding (JSON/Protobuf/XML/YAML via codec)
    ↓
Client Response
```

### Event Bus (Kafka)

**Producer** (`events/producer/`):
- Synchronous Kafka publishing with retry logic
- Configurable retry attempts and intervals
- Message metrics tracking (success/failure)

**Consumer** (`events/consumer/`):
- Consumer group management
- Topic subscriptions
- Graceful shutdown coordination
- Offset tracking per partition/topic

**Usage Pattern:**
```go
// Publish event
err := deps.KafkaProducer.Publish(ctx, events.Event{
    Topic:     "user.created",
    Key:       userID,
    Payload:   userData,
    Timestamp: time.Now(),
})

// Subscribe to events
err := deps.KafkaConsumer.Subscribe(ctx, "user.created", handlerFunc)
```

### Protobuf Code Generation

**Custom Plugin:** `cmd/protoc-gen-go-errors`
- Generates error type definitions from Protobuf `ErrorReason` enums
- Uses protogen plugin architecture

**Proto Definitions:** `proto/` directory
- `config.proto`: Configuration structures
- `response.proto`: Standard response/error types (Status, OperationResponse)
- `errors.proto`: Error definitions
- `query.proto`, `filter.proto`: Query/filtering helpers
- `pagination.proto`: Pagination models
- Various domain types: `data.proto`, `blob.proto`, `geo.proto`, etc.

**Generated Code:** Located in `api/v1/*/` (e.g., `api/v1/response/response.pb.go`)

### Encoding System (Content Negotiation)

The `encoding/` package provides a pluggable codec system:

**Codec Interface:**
```go
interface Codec {
    Marshal(v interface{}) ([]byte, error)
    Unmarshal(data []byte, v interface{}) error
    Name() string
}
```

**Registered Codecs:**
- `encoding/json/`: JSON codec
- `encoding/proto/`: Protobuf codec
- `encoding/xml/`: XML codec
- `encoding/yaml/`: YAML codec
- `encoding/form/`: Form data codec (with protobuf support)

Codecs are auto-registered on import. Select codec at runtime via content subtype.

## Cross-Cutting Concerns

### Logging (p9log)

Wrapper around Zap logger with structured logging:

```go
// Get logger from ServiceDeps
logger := deps.Log

// Structured logging
logger.Info("User created",
    p9log.String("user_id", userID),
    p9log.Int("tenant_count", len(tenantIDs)),
)

logger.Error("Failed to process request",
    p9log.Error(err),
    p9log.String("request_id", reqID),
)
```

**Best Practice:** Use structured fields instead of formatted strings for better log analysis.

### Metrics Collection

Pluggable architecture with multiple providers:
- **Prometheus**: Registry-based metrics with HTTP endpoint
- **OpenTelemetry**: Modern tracing/metrics standard
- **Datadog**: StatsD client integration

**Common Metrics:**
- DB operation duration & success rate
- DB connection pool stats
- HTTP request duration/status
- Service request counts

**Usage:**
```go
deps.Metrics.RecordDBOperation("SELECT", duration, success)
deps.Metrics.RecordHTTPRequest(handler, method, statusCode, duration)
```

### Distributed Tracing

Supports multiple exporters via OpenTelemetry:
- **Jaeger**: Collector endpoint
- **Zipkin**: Spans endpoint
- **OTLP**: OpenTelemetry Protocol HTTP

**Usage:**
```go
// Tracing is initialized in ServiceDeps
// Create spans in handlers
ctx, span := deps.Tracing.Tracer.Start(ctx, "OperationName")
defer span.End()

span.SetAttributes(
    attribute.String("user.id", userID),
    attribute.Int("tenant.id", tenantID),
)
```

### Error Handling

Structured errors with gRPC/HTTP code mapping (`errors/errors.go`):

```go
// Define typed errors
var ErrUserNotFound = errors.New(
    errors.CodeNotFound,
    "USER_NOT_FOUND",
    "User not found",
)

// Use in handlers
if user == nil {
    return nil, ErrUserNotFound
}

// Wrap errors for context
return nil, errors.Wrap(err, "failed to create user")

// Check error types
if errors.Is(err, ErrUserNotFound) {
    // Handle not found
}
```

**Error Structure:**
```go
type Error struct {
    Status  // Code, Reason, Message, Metadata
    cause error
}
```

Errors automatically convert to appropriate gRPC status codes or HTTP status codes via `FromError()`.

### Caching

In-memory cache with TTL and LRU eviction (`cache/cache.go`):

```go
// Cache is available in ServiceDeps
cache := deps.Cache

// Set with TTL
err := cache.Set(ctx, "key", value, 5*time.Minute)

// Get value
var result User
err := cache.Get(ctx, "key", &result)

// Delete
err := cache.Delete(ctx, "key")
```

**Implementation:** Based on `sync.Map` with background expiration cleanup. Falls back to `NoopCache` when disabled in config.

### Multi-Tenancy (SaaS)

**Components:**
- `p9context/saas_context.go`: Tenant context management
- `middleware/tenant/`: Middleware to extract tenant from headers
- `middleware/dbmiddleware/`: Database pool routing based on tenant
- `saas/provider.go`: Generic DbProvider pattern

**Tenant Resolution Flow:**
1. Extract X-Tenant-ID/X-Tenant-Name from HTTP headers or gRPC metadata
2. Store in context via `p9context.NewCurrentTenant()`
3. DB middleware checks if tenant has independent database
4. Route to appropriate connection pool (shared or tenant-specific)

**Important:** Always pass context through call chains to preserve tenant info.

### Configuration Management

Multi-source configuration loading (`config/config.go`):

**Features:**
- Load from file (TOML/YAML/JSON)
- Load from environment variables
- Observer pattern for config changes
- Type-safe Scan to structs

**Usage:**
```go
type AppConfig struct {
    Server   ServerConfig   `toml:"server"`
    Database DatabaseConfig `toml:"database"`
}

var cfg AppConfig
err := config.LoadFile("config.toml", &cfg)
err := config.LoadEnv(&cfg)  // Overlay with env vars
err := config.Validate(&cfg)
```

## Key Interfaces to Know

When extending the codebase, implement these interfaces:

```go
// Server abstraction
interface Server {
    Run(ctx context.Context) error
}

// Logger
interface Logger {
    Log(level Level, keyvals ...interface{}) error
}

// Metrics
interface MetricsProvider {
    RecordDBOperation(operation string, duration time.Duration, success bool)
    RecordHTTPRequest(handler, method string, status int, duration time.Duration)
}

// Codec
interface Codec {
    Marshal(v interface{}) ([]byte, error)
    Unmarshal(data []byte, v interface{}) error
    Name() string
}

// Unit of Work
interface UnitOfWork {
    Commit(ctx context.Context) error
    Rollback(ctx context.Context) error
}

// Entity (for domain models)
interface Entity {
    GetID() int64
    GetUUID() string
}
```

## Generic Helper Usage

The codebase extensively uses Go generics for type-safe operations:

**Repository Helpers** (`helpers/repo/`):
```go
// Instead of writing repetitive CRUD code
user, err := repo.GetByID[User](ctx, deps, "users", userID)
users, err := repo.ListEntity[User](ctx, deps, dataModel)
err := repo.CreateEntity[User](ctx, deps, newUser)
err := repo.UpdateEntity[User](ctx, deps, updatedUser)
```

**Service Helpers** (`helpers/service/`):
```go
// Higher-level service operations with validation and metrics
result, err := service.GetEntity[User](ctx, deps, entityID)
results, err := service.ListEntity[User](ctx, deps, request)
created, err := service.CreateEntity[User](ctx, deps, entity)
```

**DataModel Pattern:**
```go
type DataModel[T any] struct {
    TableName  string
    Where      string
    WhereArgs  []any
    FieldNames []string
}
```

Use `DataModel` to construct queries with the operations builder.

## Important Implementation Details

### Context Management
- **Always pass context through call chains** for timeout/deadline support and tenant info
- Use `deps.Tp.ApplyTimeout(ctx)` to apply configured timeouts before DB operations
- Context contains tenant info (via `p9context.NewCurrentTenant()`)
- Context contains SaaS metadata (via `p9context.NewSaasContext()`)

### Timeout Handling
- **TimeoutProvider** (`timeout/`) manages operation timeouts
- Default timeout: 30 seconds
- Long query timeout: 5 minutes
- Preserves existing deadlines if shorter than configured timeout

### Connection Pooling
- Single `pgxpool.Pool` for shared tenant DB
- Dynamic pools created for independent tenant DBs
- Pool settings: configurable via `config.Data.Database`
- Health checks on startup ensure connectivity

### Message Serialization
- Events use **Protobuf** for Kafka messages
- HTTP/gRPC use content negotiation (JSON/Protobuf/XML/YAML)
- Form data codec supports protobuf messages for URL-encoded forms

### Merger Utility
Deep merge for struct reconciliation (`merger/`):
- Recursively merges fields (slices, maps, pointers)
- Overwrite mode available
- Used for configuration merging
- Type checking optional

## Development Patterns

### Adding New Utilities
1. Create package under appropriate directory
2. Implement functionality with proper interfaces
3. Add unit tests (`*_test.go`)
4. Update `go.mod` if adding dependencies (`go get <dep>`)
5. Register with DI if needed (add to `ServiceDeps` or provide via FX)

### Adding New Middleware
1. Implement `Middleware` type: `func(Handler) Handler`
2. Add to middleware chain in server setup
3. Ensure it handles context properly (pass through or modify)
4. Test with mock handlers

### Adding New Error Types
1. Define in appropriate package (e.g., `errors/database/`)
2. Use structured error creation:
   ```go
   var ErrDuplicateKey = errors.New(
       errors.CodeAlreadyExists,
       "DUPLICATE_KEY",
       "Record already exists",
   )
   ```
3. Map to appropriate gRPC/HTTP codes

### Working with Tenancy
- Extract tenant info early via tenant middleware
- Use DB middleware to route to correct pool
- Never hardcode tenant IDs; always use context
- Test with both shared and independent DB scenarios

## Module Dependencies

**This module depends on:**
- `p9e.in/samavaya/identity/user` (user identity service)

**This module provides to other services:**
- Database connection management
- Logging infrastructure
- Metrics collection
- Tracing setup
- Cache abstraction
- Event bus (Kafka)
- Error handling
- Middleware implementations
- Helper functions for CRUD operations
- Configuration management
- Multi-tenancy utilities

When services import this package, they get access to all shared infrastructure.

## Testing Guidelines

### Unit Tests
```bash
# Test specific package
go test ./helpers/...

# Run with race detector
go test -race ./...

# Generate coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Using Mock Dependencies
- Mock `ServiceDeps` for testing handlers
- Use interface-based mocking for `Logger`, `MetricsProvider`, `Cache`, etc.
- Create test fixtures for `DataModel[T]` patterns

### Integration Tests
- Tag with `//go:build integration`
- Require actual Postgres/Kafka/Redis instances
- Run with `go test -tags=integration ./...`

## Important Notes

1. **Always use structured logging** with `p9log` fields instead of formatted strings
2. **Pass context through all call chains** to preserve tenant info and deadlines
3. **Use generic helpers** instead of writing repetitive repository code
4. **Inject ServiceDeps** into all services for consistent infrastructure access
5. **Handle errors properly** with typed errors and appropriate codes
6. **Apply timeouts** before database operations using `TimeoutProvider`
7. **Test with both shared and independent tenant databases** when working on multi-tenancy
8. **Register new codecs** in the encoding package if adding new serialization formats
9. **Use middleware chain** for cross-cutting concerns instead of handling in individual handlers
10. **Graceful shutdown** is coordinated via context cancellation in MultiServer

# Code Behavior & Agile Execution Guidelines

> This document defines the rules Claude (or any coding agent) must follow when working inside this project. It ensures consistency, traceability, clean documentation discipline, and an Agile delivery workflow.

---

## 📌 Core Operating Principles

### 🔹 Markdown Files Are the Single Source of Truth

Claude must **only update** the following markdown files:

| File              | Purpose                                                             |
| ----------------- | ------------------------------------------------------------------- |
| `todo.md`         | Tracks active, planned, and completed tasks (Sprint Board)          |
| `explanation.md`  | Logs completed actions and reasoning (Work History)                 |
| `requirements.md` | Maintains requirements, epics, user stories, acceptance criteria    |
| `design.md`       | Captures architecture decisions, implementation plans, design notes |

🚫 Claude must **NOT create any new `.md` files** unless explicitly instructed.

---

## 🧭 Behaviour Execution Cycle

Claude must follow this workflow on **every execution**:

1. Read this file (`behaviour.md`) and `CLAUDE.md`.
2. Review current sprint backlog in `todo.md`.
3. Execute only planned tasks unless a new dependency emerges.
4. Update documentation in the following order:

   1. `todo.md`
   2. `explanation.md`
   3. `requirements.md` (if new understanding formed)
   4. `design.md` (only if architecture changed)
5. Confirm alignment before continuing development.

---

## 🏗 Agile Framework

This project follows a lightweight Agile execution layer while maintaining strict documentation discipline.

### 🔹 Agile Hierarchy

| Level                              | Definition                                        | Stored In                             |
| ---------------------------------- | ------------------------------------------------- | ------------------------------------- |
| **Epic**                           | Large functional scope or big feature area        | `requirements.md`                     |
| **User Story**                     | Work describing value from the user's perspective | `requirements.md`                     |
| **Acceptance Criteria**            | Testable conditions confirming done state         | Under each story in `requirements.md` |
| **Sprint Backlog**                 | Active tasks assigned to the sprint               | `todo.md`                             |
| **Tasks / Subtasks**               | Atomic executable items                           | `todo.md`                             |
| **Sprint Summary + Retrospective** | Reflection on execution                           | `explanation.md`                      |

---

## 📄 Requirement Structure

All requirements must be recorded in `requirements.md` with this format:

```
REQ-ID: REQ-001
Title: <Clear requirement>
Module: <affected area>
Priority: High | Medium | Low
Linked Design: DES-001 (when exists)
Origin: User Story US-001
Notes: Optional details
```

---

## 🎯 User Story Format

User Stories must include:

```
Story ID: US-001
Epic: EPIC-001
As a <role>
I want <capability>
So that <value>

Acceptance Criteria:
- [ ] AC1
- [ ] AC2
- [ ] AC3 (edge cases included)
```

---

## 🧠 Design Mapping Rules

Each requirement must have a corresponding entry in `design.md` when implementation details are needed.

Design entries include:

```
Design ID: DES-001
Requirement: REQ-001
Title: <Design Name>
Description: Architecture, logic, flows, constraints
Modules: service | middleware | database | testing
Links: Diagrams or external docs (optional)
Notes: tradeoffs or reasoning
```

---

## 📌 Todo & Sprint Workflow

Tasks in `todo.md` must follow this structure:

```
- [ ] <Task Title>
  ID: TSK-001
  Type: Feature | Bugfix | Refactor | Test | Research
  Parent: <TSK-ID or None>
  Linked Story: US-001
  Requirement: REQ-001
  Design Link: DES-001
  Sprint: S1
  Priority: High | Medium | Low
  Notes: optional context
```

#### Task Rules

* Each task must be **atomic** (can be completed in one execution step).
* Each task must have at least **one trace link** (Story, Requirement, or Design).
* Completed tasks must be moved to the completed section and logged in `explanation.md`.

---

## 🧩 Dynamic Task Creation Rules

Dynamic tasks may be created **only during implementation** when:

* A requirement reveals missing dependent work
* A task cannot be completed without a prerequisite
* Refactoring, testing, or architectural needs emerge during development

### Rules

1. Dynamic tasks MUST be added to `todo.md` only — never create new files.
2. Each dynamic task must reference:

   * Parent Task ID (if applicable)
   * Requirement ID or Story ID
   * Sprint assignment (if applicable)
3. Dynamic tasks should be categorized as:

| Type             | Meaning                      | Allowed?                                 |
| ---------------- | ---------------------------- | ---------------------------------------- |
| Subtask          | Part of an existing task     | ✔️ Allowed                               |
| Dependency Task  | Required before continuation | ✔️ Allowed                               |
| Improvement Task | Optimization or refactor     | ✔️ Allowed (next sprint unless blocking) |
| New Feature      | Not previously documented    | ❌ Needs approval                         |

### Dynamic Task Template

```
- [ ] <Task Name>
  Type: Subtask / Dependency / Improvement
  Parent Task: <TSK-ID>
  Requirement / Story: <REQ-ID or US-ID>
  Sprint: <S1/S2/...>
  Reason: Why dynamically created
```

### Logging Dynamic Tasks

Whenever a dynamic task is created Claude must update `explanation.md` explaining:

* Why it was generated
* Its priority and dependencies
* Whether it belongs to this sprint or a future one

---
## ✔️ Definition of Done (DoD)

A task is officially considered complete only when all the following conditions are met:

| Completion Requirement                 | Status Criteria                                                                      |
| -------------------------------------- | ------------------------------------------------------------------------------------ |
| Code written or updated                | Functionality exists, compiles successfully, and aligns with design decisions        |
| Task entry updated in `todo.md`        | Status changed to **Completed** and correctly linked to Story / Requirement / Design |
| Explanation added to `explanation.md`  | Includes: what was done, files touched, reasoning, impact, and verification method   |
| Requirement and design links validated | All relevant table rows updated to reflect new state or learning                     |
| Acceptance criteria fully satisfied    | All criteria under the linked Story table checked off                                |
| No scope deviation                     | Work matches sprint scope and requirement — no feature creep                         |
| Testing verified                       | Manual or automated tests executed and documented if applicable                      |
| Follow-up items captured               | Any new dependent or future work is logged as dynamic tasks                          |

Only when **all rows above are satisfied** may Claude update the task status to **DONE**.

---

🚫 Prohibited Behaviour

* No undocumented file changes
* No new markdown files unless requested
* No silent edits — all work must be traceable
* No invented or implied scope without confirmation

---

---

## 🧱 Table Formatting Enforcement Policy

All documentation across `requirements.md`, `todo.md`, `design.md`, and `explanation.md` must be maintained in **wide, human‑readable table format (Style 1)**.

### Formatting Rules:

* Tables must prioritize **clarity and readability**.
* Column widths may vary as needed; uniform alignment is **not required**.
* Rows must remain structured and aligned so future updates remain readable.
* Cells may contain multi‑line descriptive information when necessary.
* Markdown table syntax must remain valid.

### Enforcement Principles:

* No block paragraphs for items meant to be documented in structured form.
* Claude must always append new entries as new table rows instead of creating new formatting styles.
* If a column is missing for a new type of data, Claude may **add the column globally to the table** — never create a separate table for the same entity.

### Example Reference Format (for tasks):

```
| Task ID | Type       | Parent Task | Linked Req / Story | Sprint | Priority | Status      | Description                                           |
|---------|------------|-------------|---------------------|--------|----------|-------------|-------------------------------------------------------|
| TSK-001 | Feature    | -           | REQ-001 / US-001    | S1     | High     | In Progress | Implement authentication service with JWT and RBAC.   |
```

Claude must follow this formatting style across all structured content.

---

### ✅ Summary

This file governs execution, documentation discipline, task structure, decision flow, Agile layering, and change control. Claude must follow it **every time work is performed**.
