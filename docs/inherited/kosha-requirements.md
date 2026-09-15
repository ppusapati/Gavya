# Kosha - Requirements Document

## Project Overview
Kosha is a shared packages module providing infrastructure components, utilities, and cross-cutting concerns for microservices. It serves as the foundation layer for database access, logging, metrics, tracing, configuration, event messaging, and multi-tenancy.

---

## Epics

| Epic ID   | Title                                | Priority | Status  | Description                                                                                                     | Business Value                                                           |
|-----------|--------------------------------------|----------|---------|----------------------------------------------------------------------------------------------------------------|--------------------------------------------------------------------------|
| EPIC-001  | Dependency Injection Consolidation   | High     | Planned | Remove Google Wire dependency completely and standardize on Uber FX for all DI. Plan for eventual custom DI.   | Reduces dependency count, simplifies DI patterns, prepares for custom DI |
| EPIC-002  | Query Builder Enhancements           | Medium   | Planned | Add runtime validation, query logging, and SQLC-inspired type safety to dynamic query builder                  | Improves developer experience, catches errors earlier, maintains dynamic query capabilities |

## User Stories

| Story ID | Epic      | Role      | Want                                    | So That                                      | Linked Requirements   | Status  | Acceptance Criteria                                                                                                                                                                                                   |
|----------|-----------|-----------|----------------------------------------|----------------------------------------------|-----------------------|---------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| US-001   | EPIC-001  | Developer | Wire dependency removed and replaced with FX | We have a single, consistent DI pattern across the codebase | REQ-FR11.1, REQ-FR11.2 | Completed | AC1: ✅ All Wire provider functions converted (6 files)<br>AC2: ✅ Wire dependency removed from go.mod<br>AC3: ✅ All Wire imports removed from codebase<br>AC4: ✅ Build passes without Wire<br>AC5: ✅ ServiceDeps pattern maintained and functional |
| US-002   | EPIC-001  | Developer | A plan for custom lightweight DI package | We can eventually eliminate external DI dependencies | REQ-FR11.3             | Pending | AC1: Design document created for custom DI package<br>AC2: API design matches FX patterns for easy migration<br>AC3: Performance benchmarks defined<br>AC4: Migration path documented                                  |
| US-003   | EPIC-002  | Developer | Schema-based column validation in query builder | I catch typos and invalid field names at runtime | REQ-FR1.7              | Completed | AC1: ✅ TableSchema struct with column metadata<br>AC2: ✅ ValidateFieldMask() rejects invalid columns<br>AC3: ✅ Clear error messages with valid column suggestions<br>AC4: ✅ Zero performance impact on valid queries (<1 µs overhead) |
| US-004   | EPIC-002  | Developer | Query logging for debugging | I can see the actual SQL queries being executed | REQ-NFR4.1             | Pending | AC1: Optional query logging to configured logger<br>AC2: Includes full SQL with parameter values (sanitized)<br>AC3: Query duration and result count logged<br>AC4: Configurable log level (disabled by default) |
| US-005   | EPIC-002  | Developer | Query performance metrics | I can identify slow queries and optimize them | REQ-FR3.6              | Pending | AC1: Per-table query duration metrics<br>AC2: Per-operation (SELECT/INSERT/UPDATE/DELETE) breakdowns<br>AC3: Slow query threshold alerts<br>AC4: Query pattern identification for caching |

---

## Functional Requirements

### FR1: Database Layer
- **FR1.1**: Support PostgreSQL via pgx/v5 with connection pooling
- **FR1.2**: Provide multi-tenancy support (shared and independent database pools)
- **FR1.3**: Implement transaction management via Unit of Work pattern
- **FR1.4**: Support query building with SQL injection prevention
- **FR1.5**: Provide generic repository helpers for CRUD operations
- **FR1.6**: Apply configurable timeouts to all database operations
- **FR1.7**: Validate field names against schema at runtime to prevent typos

### FR2: Logging
- **FR2.1**: Structured logging using Zap
- **FR2.2**: Support multiple log levels (debug, info, warn, error)
- **FR2.3**: Context-aware logging with request tracing
- **FR2.4**: No debug logging (fmt.Printf) in production code

### FR3: Metrics & Monitoring
- **FR3.1**: Support multiple metrics providers (Prometheus, OpenTelemetry, Datadog)
- **FR3.2**: Track database operation metrics (duration, success rate)
- **FR3.3**: Track HTTP request metrics
- **FR3.4**: Track connection pool statistics
- **FR3.5**: Configurable metrics backend selection
- **FR3.6**: Per-table and per-operation query performance metrics

### FR4: Distributed Tracing
- **FR4.1**: OpenTelemetry-based tracing
- **FR4.2**: Support Jaeger, Zipkin, and OTLP exporters
- **FR4.3**: Automatic span creation for database operations
- **FR4.4**: Context propagation across service boundaries

### FR5: Event Bus (Kafka)
- **FR5.1**: Kafka producer with retry logic
- **FR5.2**: Kafka consumer with group management
- **FR5.3**: Event serialization using Protobuf
- **FR5.4**: Graceful shutdown coordination

### FR6: Error Handling
- **FR6.1**: Structured errors with gRPC/HTTP code mapping
- **FR6.2**: Error wrapping with context preservation
- **FR6.3**: Type-safe error checking
- **FR6.4**: Consistent error response format
- **FR6.5**: Use internal error package exclusively

### FR7: Multi-Tenancy
- **FR7.1**: Tenant extraction from HTTP headers/gRPC metadata
- **FR7.2**: Tenant context propagation
- **FR7.3**: Database routing based on tenant configuration
- **FR7.4**: Tenant-specific connection pool management

### FR8: Configuration Management
- **FR8.1**: Load configuration from files (TOML/YAML/JSON)
- **FR8.2**: Environment variable overlay
- **FR8.3**: Configuration validation
- **FR8.4**: Hot reload support via observers

### FR9: Authentication & Authorization
- **FR9.1**: JWT token parsing and validation
- **FR9.2**: Permission-based access control
- **FR9.3**: User context extraction from requests
- **FR9.4**: gRPC interceptor for authorization
- **FR9.5**: Security context from actual user, not hardcoded

### FR10: Server Management
- **FR10.1**: Multi-server support (gRPC, HTTP)
- **FR10.2**: Graceful shutdown coordination
- **FR10.3**: Health check endpoints
- **FR10.4**: CORS handling for HTTP server

### FR11: Dependency Injection
- **FR11.1**: Use Uber FX exclusively for dependency injection
- **FR11.2**: Remove Google Wire dependency completely
- **FR11.3**: Plan for custom DI package implementation (future)
- **FR11.4**: ServiceDeps pattern maintained for service composition

## Non-Functional Requirements

### NFR1: Performance
- **NFR1.1**: Database operations timeout within configured limits
- **NFR1.2**: Connection pool efficiency (max 30 connections, 10 idle)
- **NFR1.3**: Minimal overhead from middleware chain
- **NFR1.4**: Efficient caching with TTL and LRU eviction

### NFR4: Observability
- **NFR4.1**: SQL query logging for debugging (optional, disabled by default)
- **NFR4.2**: Query execution time tracking
- **NFR4.3**: Automatic span creation for all database operations
- **NFR4.4**: Structured error logging with context

### NFR2: Reliability
- **NFR2.1**: No panics in production code (use error returns)
- **NFR2.2**: Graceful degradation when dependencies unavailable
- **NFR2.3**: Retry logic for transient failures
- **NFR2.4**: Circuit breaker for external dependencies

### NFR3: Maintainability
- **NFR3.1**: Comprehensive test coverage (target: 80%)
- **NFR3.2**: Package-level documentation (godoc)
- **NFR3.3**: Minimal code duplication (<5%)
- **NFR3.4**: Clear separation of concerns
- **NFR3.5**: Consistent error handling patterns

### NFR4: Security
- **NFR4.1**: SQL injection prevention via parameterized queries
- **NFR4.2**: Secure JWT secret management (environment variables)
- **NFR4.3**: No hardcoded credentials or user contexts
- **NFR4.4**: Security context from authenticated requests

### NFR5: Observability
- **NFR5.1**: Comprehensive logging at all layers
- **NFR5.2**: Distributed tracing for request flows
- **NFR5.3**: Metrics for all critical operations
- **NFR5.4**: Error tracking and alerting

### NFR6: Code Quality
- **NFR6.1**: Follow Go best practices and idioms
- **NFR6.2**: Consistent code formatting (gofmt)
- **NFR6.3**: Linter compliance (golangci-lint)
- **NFR6.4**: Minimal external dependencies
- **NFR6.5**: Standard library preferred over external packages

### NFR7: Compatibility
- **NFR7.1**: Go 1.25.4+ compatibility
- **NFR7.2**: PostgreSQL 12+ support
- **NFR7.3**: Kafka 2.0+ support
- **NFR7.4**: gRPC 1.71+ compatibility

## Constraints

### C1: Technology Stack
- Language: Go 1.25.4
- Database: PostgreSQL (via pgx/v5)
- Message Queue: Kafka (via sarama)
- Logging: Zap
- Tracing: OpenTelemetry
- gRPC: google.golang.org/grpc

### C2: Architecture
- Dependency injection via ServiceDeps
- Middleware chain pattern
- Generic helpers for type safety
- Protobuf for API definitions

### C3: Development
- No external dependencies unless necessary
- Standard library preferred
- Must compile without errors
- Must pass linter checks

## Quality Attributes

### Testability
- All public functions testable
- Interface-based design for mocking
- Test fixtures for common scenarios
- Integration test support

### Modularity
- Clear package boundaries
- Minimal package coupling
- Single responsibility principle
- Interface segregation

### Extensibility
- Plugin architecture for codecs
- Multiple metrics provider support
- Configurable middleware chain
- Custom error types support

## Dependencies

### Direct Dependencies (Essential)
- github.com/jackc/pgx/v5 - PostgreSQL driver
- google.golang.org/grpc - gRPC framework
- google.golang.org/protobuf - Protocol buffers
- github.com/IBM/sarama - Kafka client
- go.uber.org/zap - Logging
- go.opentelemetry.io/otel - Tracing
- github.com/prometheus/client_golang - Metrics

### Direct Dependencies (DI Framework)
- go.uber.org/fx - Dependency injection framework (primary)

### Dependencies To Remove
- ❌ github.com/google/wire - To be removed (replaced by Uber FX)
- ❌ github.com/labstack/echo/v4 - To be evaluated for removal

### Optional Dependencies
- github.com/DataDog/datadog-go - Datadog metrics

## Success Criteria
1. ✅ All packages compile without errors
2. ❌ 80%+ test coverage (currently 0%)
3. ✅ Zero debug logging in production (removed 27 statements)
4. ✅ Zero panics in production paths (fixed 3 panics)
5. ✅ Zero hardcoded security contexts (fixed 19+ occurrences)
6. ✅ <50 direct dependencies (currently 44)
7. ❌ <20 TODO comments (currently 128)
8. ✅ Consistent import paths (kosha/*)
9. ❌ Complete godoc documentation
10. ❌ Passing linter checks
11. ✅ Unit of Work pattern implemented (full pgx implementation)
