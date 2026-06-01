# Kosha Refactoring Plan

**Date**: 2025-12-01
**Purpose**: Comprehensive refactoring to eliminate redundancies, minimize dependencies, and improve code quality

---

## Executive Summary

This refactoring addresses critical issues in the Kosha shared packages:
- **8% code reduction** (~1,850 lines) through deduplication
- **7 dependencies removed** (GORM, Wire, Echo, Afero, go-funk, etc.)
- **Zero test coverage** addressed (critical priority)
- **128 TODO items** requiring resolution
- **19 hardcoded security contexts** fixed
- **Query builder consolidation** from 3 packages to 1

---

## Phase 1: Module Renaming (IMMEDIATE)

### 1.1 Rename Module
- **From**: `kosha`
- **To**: `kosha` or `github.com/yourusername/kosha`
- **Files Affected**: 145+ Go files with imports

### 1.2 Update go.mod
```go
module kosha  // or github.com/yourusername/kosha
go 1.26.1
```

### 1.3 Update All Imports
- Replace all occurrences: `kosha` → `kosha`
- Estimated: 500+ import statements

### 1.4 Handle External Dependency
```go
// Current: p9e.in/Samavāya/identity/user
// Option 1: Remove dependency entirely (make optional)
// Option 2: Create interface in kosha, let consumers implement
// Option 3: Move user identity into kosha as subpackage
```

**Decision Required**: How should the identity/user dependency be handled?

---

## Phase 2: Dependency Cleanup

### 2.1 Remove GORM (PRIORITY 1)

**Files to Delete**:
- `database/gorm/gorm.go` (51 lines)

**go.mod Changes**:
```diff
- gorm.io/driver/postgres v1.5.11
- gorm.io/gorm v1.31.0
```

**Impact**: No breaking changes - GORM unused in codebase

---

### 2.2 Remove go-funk

**Current Usage**:
- `database/pgxpostgres/filter/filter.go` - Used once for array operations

**Action**:
```go
// Replace github.com/thoas/go-funk with standard library
// Before: funk.Contains(arr, val)
// After:  slices.Contains(arr, val)  // Go 1.21+
```

**go.mod Changes**:
```diff
- github.com/thoas/go-funk v0.9.3
```

---

### 2.3 Remove Echo Framework

**Current Usage**:
- `config/config.go` - Only for logging

**Action**:
```go
// Replace echo logging with p9log
// Before: echo.NewLogger()
// After:  p9log.NewHelper()
```

**go.mod Changes**:
```diff
- github.com/labstack/echo/v4 v4.13.3
- github.com/labstack/gommon v0.4.2
```

---

### 2.4 Remove Google Wire

**Current Usage**: Minimal - manual DI via ServiceDeps is prevalent

**Action**:
- Remove any wire.go files
- Keep manual dependency injection pattern

**go.mod Changes**:
```diff
- github.com/google/wire v0.6.0
```

---

### 2.5 Evaluate Afero VFS

**Current Usage**:
- `vfs/` package (7 files, minimal external usage)

**Options**:
1. **Remove entirely** - Use standard `os` package
2. **Keep as optional feature** - Only import if needed

**Decision Required**: Is VFS abstraction critical for any consumers?

**If removing**:
```diff
- github.com/spf13/afero v1.14.0
```

---

### 2.6 Optional: Simplify Observability

**Current**: 3 full implementations (Prometheus, OTEL, Datadog)

**Recommendation**:
- Keep **Prometheus** as default (most common)
- Keep **OpenTelemetry** (modern standard)
- Remove **Datadog** or make plugin-based

**go.mod Changes** (if removing Datadog):
```diff
- github.com/DataDog/datadog-go/v5 v5.6.0
```

---

## Phase 3: Code Consolidation

### 3.1 Query Builder Consolidation (CRITICAL)

**Problem**: 3 overlapping packages (~2,214 lines)
- `database/pgxpostgres/builder/` (664 lines)
- `database/pgxpostgres/filter/` (714 lines)
- `database/pgxpostgres/operations/` (599 lines)

**Solution**: Merge into single unified package

**New Structure**:
```
database/pgxpostgres/
├── postgres.go              (connection management)
├── query/
│   ├── builder.go           (SELECT, INSERT, UPDATE, DELETE)
│   ├── filter.go            (WHERE clause building)
│   ├── executor.go          (query execution - consolidate from operations/)
│   └── validator.go         (SQL injection prevention)
├── tenantDB/
└── retry/
```

**Consolidation Strategy**:
1. Keep filter operations from `filter/filter.go` (most comprehensive)
2. Merge builder functions from `builder/builder.go`
3. Move execution logic from `operations/operations.go` to `executor.go`
4. Delete redundant implementations

**Expected Savings**: ~800 lines

**Files to Modify**:
- `helpers/repo/*.go` - Update imports
- `helpers/service/*.go` - Update imports
- Any services using operations directly

---

### 3.2 Helper Function Deduplication

**Problem**: 70% code duplication across helpers

**Current Pattern** (repeated in every helper):
```go
func HelperFunction[T any](ctx context.Context, deps *deps.ServiceDeps, ...) {
    // Duplicate boilerplate:
    ctx, span := deps.Tracing.Tracer.Start(ctx, "Operation")
    defer span.End()
    startTime := time.Now()

    // ... actual logic ...

    recordMetric(deps.Metrics, "entity", "operation", startTime, err == nil)
}
```

**Solution**: Create middleware/decorator pattern

**New Structure**:
```go
// helpers/middleware.go
type HelperFunc[T any] func(context.Context, *deps.ServiceDeps, ...interface{}) (T, error)

func WithObservability[T any](name string, fn HelperFunc[T]) HelperFunc[T] {
    return func(ctx context.Context, deps *deps.ServiceDeps, args ...interface{}) (T, error) {
        ctx, span := deps.Tracing.Tracer.Start(ctx, name)
        defer span.End()
        startTime := time.Now()

        result, err := fn(ctx, deps, args...)

        recordMetric(deps.Metrics, name, startTime, err == nil)
        return result, err
    }
}

// Usage:
var GetByID = WithObservability("GetByID", getByIDCore)
```

**Expected Savings**: ~500 lines

**Files Affected**:
- `helpers/repo/*.go` (7 files)
- `helpers/service/*.go` (7 files)

---

### 3.3 Metrics Provider Simplification

**Problem**: 3 full implementations (~400 lines duplicate logic)

**Current**:
```go
type MetricsProvider struct {
    provider string
    // Prometheus, OTEL, Datadog all inline
}
```

**Solution**: Strategy pattern with single active provider

**New Structure**:
```go
// metrics/metrics.go
type MetricsBackend interface {
    RecordCounter(name string, value float64, tags map[string]string)
    RecordHistogram(name string, value float64, tags map[string]string)
    RecordGauge(name string, value float64, tags map[string]string)
}

// metrics/prometheus/prometheus.go - separate file
type PrometheusBackend struct { ... }

// metrics/otel/otel.go - separate file
type OTELBackend struct { ... }

// metrics/datadog/datadog.go - separate file (optional)
type DatadogBackend struct { ... }
```

**Benefits**:
- Clear separation of concerns
- Easy to test each backend independently
- Can load backends dynamically based on config
- Reduce main metrics.go from 470 lines to ~100 lines

**Expected Savings**: ~300 lines in main file

---

### 3.4 Remove Dead Code

**Files to Delete**:
1. `database/gorm/gorm.go` (51 lines)
2. `database/sqlc/provider.go` (interface stub)
3. `events/handler/handler.go` (unused interface)
4. Evaluate `vfs/` directory (7 files) based on usage

**Commented Code to Remove**:
- `config/config.go:120-126, 162-168` (watcher code)
- `database/pgxpostgres/pgxprovider.go:71` (logging)

**Expected Savings**: ~200 lines

---

## Phase 4: Critical Fixes

### 4.1 Fix Hardcoded Security Contexts

**Problem**: 19 occurrences of `validator.NewSecurityContext("admin")`

**Files**:
- `database/pgxpostgres/builder/builder.go`
- `database/pgxpostgres/builder/delete.go`
- `database/pgxpostgres/builder/insert.go`
- `database/pgxpostgres/builder/select.go`
- `database/pgxpostgres/builder/update.go`
- `database/pgxpostgres/builder/where.go`

**Solution**:
```go
// Add to p9context package
func GetSecurityContext(ctx context.Context) *validator.SecurityContext {
    // Extract from JWT claims or auth context
    user := GetCurrentUser(ctx)
    if user == nil {
        return validator.NewSecurityContext("system")
    }
    return validator.NewSecurityContext(user.ID)
}

// Update all builder calls
- secCtx := validator.NewSecurityContext("admin")
+ secCtx := p9context.GetSecurityContext(ctx)
```

**TODOs to Address**: 19 security-related TODOs

---

### 4.2 Remove Production Debug Logging

**Problem**: `fmt.Printf` statements throughout codebase

**Files**:
- `database/pgxpostgres/operations/operations.go:128,165,224`
- Others identified during sweep

**Solution**:
```go
- fmt.Printf("Debug: %v\n", value)
+ deps.Log.Debug("operation details", p9log.Any("value", value))
```

**Action**: Global search and replace

---

### 4.3 Replace Panics with Error Returns

**Problem**: Production panics in critical paths

**Files**:
- `saas/provider.go:47,51`

**Solution**:
```go
- panic("cannot create tenant connection pool")
+ return nil, errors.New(errors.CodeInternal, "DB_POOL_ERROR",
    "Failed to create tenant connection pool")
```

---

### 4.4 Implement or Remove Unit of Work

**Problem**: `uow/uow.go` has interfaces but no implementation

**Options**:
1. **Implement fully**: Add transaction management
2. **Remove interfaces**: Use manual transaction handling

**If implementing**:
```go
// uow/uow.go
type UnitOfWork struct {
    tx pgx.Tx
}

func (u *UnitOfWork) Commit(ctx context.Context) error {
    return u.tx.Commit(ctx)
}

func (u *UnitOfWork) Rollback(ctx context.Context) error {
    return u.tx.Rollback(ctx)
}

// Add to ServiceDeps
func (d *ServiceDeps) BeginUnitOfWork(ctx context.Context) (*UnitOfWork, error) {
    tx, err := d.Pool.Begin(ctx)
    if err != nil {
        return nil, err
    }
    return &UnitOfWork{tx: tx}, nil
}
```

**Decision Required**: Implement or remove?

---

## Phase 5: Documentation & Testing

### 5.1 Add Test Coverage (CRITICAL)

**Current**: 0 tests
**Target**: 80%+ coverage for critical paths

**Priority Tests**:
1. **Query builder tests** (`database/pgxpostgres/query/`)
   - SQL injection prevention
   - Filter building correctness
   - Query validation

2. **Helper function tests** (`helpers/`)
   - Generic type safety
   - Error handling
   - Timeout application

3. **Error handling tests** (`errors/`)
   - Code mapping (gRPC/HTTP)
   - Error wrapping
   - Metadata handling

4. **Middleware tests** (`middleware/`)
   - Tenant extraction
   - DB routing
   - CORS handling

5. **Metrics tests** (`metrics/`)
   - Recording accuracy
   - Backend switching
   - Label handling

**Test Structure**:
```
database/pgxpostgres/query/
├── builder.go
├── builder_test.go
├── filter.go
├── filter_test.go
├── executor.go
└── executor_test.go
```

---

### 5.2 Update Documentation

**Files to Update**:
1. **CLAUDE.md**:
   - Update module path from `kosha` → `kosha`
   - Document new query builder structure
   - Update dependency list

2. **README.md**:
   - Synchronize with CLAUDE.md
   - Add installation instructions
   - Add usage examples

3. **Package Documentation**:
   - Add godoc comments to all packages
   - Example: `// Package query provides SQL query building with type safety and injection prevention.`

4. **Proto Documentation**:
   - Add comments to proto messages
   - Document field purposes

---

### 5.3 Resolve TODO Items

**Total**: 128 TODOs

**Priority TODOs**:
1. Security context extraction (19 occurrences)
2. User ID from context (helpers/service/)
3. Transaction handling improvements
4. Cache invalidation strategies
5. Error message internationalization

**Action**: Create GitHub issues for remaining TODOs

---

## Phase 6: Build & Validation

### 6.1 Update All Import Paths

**Tool**: Use `gofmt` or `goimports` with find-replace

```bash
# Find all files with old import
find . -name "*.go" -exec grep -l "kosha" {} \;

# Replace module path
find . -name "*.go" -exec sed -i 's|kosha|kosha|g' {} \;
```

**Estimated**: 500+ import statements

---

### 6.2 Build Verification

```bash
# Verify all packages compile
go build ./...

# Check for unused imports
goimports -w .

# Tidy dependencies
go mod tidy

# Verify no missing dependencies
go mod verify
```

---

### 6.3 Run Tests

```bash
# Run all tests
go test ./...

# Run with race detector
go test -race ./...

# Generate coverage report
go test -cover ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

**Target**: 80%+ coverage

---

### 6.4 Linting

```bash
# Run linter
golangci-lint run

# Fix auto-fixable issues
golangci-lint run --fix
```

---

## Estimated Impact

### Code Reduction:
- Remove GORM: **-51 lines**
- Consolidate query builders: **-800 lines**
- Deduplicate helpers: **-500 lines**
- Simplify metrics: **-300 lines**
- Remove dead code: **-200 lines**
- **Total**: **~1,850 lines** (~8% reduction)

### Dependency Reduction:
- Remove: GORM (2), go-funk (1), Echo (2), Wire (1), potentially Afero (1)
- **Total**: **~7 dependencies**

### Quality Improvements:
- Add: **80%+ test coverage**
- Fix: **19 hardcoded security contexts**
- Fix: **128 TODO items**
- Remove: **All fmt.Printf debug statements**
- Replace: **All production panics with error returns**

---

## Risk Assessment

### High Risk:
- **Import path changes**: May break consumer services
  - **Mitigation**: Provide migration guide, use go mod replace temporarily

### Medium Risk:
- **Query builder consolidation**: Core database functionality
  - **Mitigation**: Extensive testing, gradual rollout

### Low Risk:
- **Dependency removal**: GORM, go-funk unused
- **Helper deduplication**: Behavioral equivalence maintained

---

## Timeline Estimate

| Phase | Duration | Complexity |
|-------|----------|------------|
| 1. Module Renaming | 1-2 hours | Low |
| 2. Dependency Cleanup | 2-3 hours | Low |
| 3. Code Consolidation | 8-12 hours | High |
| 4. Critical Fixes | 4-6 hours | Medium |
| 5. Documentation & Testing | 16-24 hours | High |
| 6. Build & Validation | 2-4 hours | Low |
| **Total** | **33-51 hours** | **~1 week** |

---

## Next Steps

1. **Review this plan** with team/stakeholders
2. **Make decisions** on:
   - Module name (`kosha` vs `github.com/user/kosha`)
   - Identity/user dependency handling
   - VFS package (keep or remove)
   - Unit of Work (implement or remove)
   - Datadog metrics backend (keep or remove)
3. **Create feature branch**: `refactor/modernize-kosha`
4. **Execute phases sequentially** with testing between each
5. **Create migration guide** for consumers

---

## Questions for Stakeholder

1. What should the new module path be?
   - `kosha`
   - `github.com/yourusername/kosha`
   - Other?

2. How should we handle `p9e.in/Samavāya/identity/user` dependency?
   - Remove and make optional
   - Create interface in kosha
   - Move into kosha as subpackage

3. Should VFS abstraction be kept?
   - Remove if not critical
   - Keep as optional feature

4. Should we implement Unit of Work or remove interfaces?
   - Implement transaction management
   - Remove unused interfaces

5. Keep Datadog metrics backend?
   - Yes (full implementation)
   - Make it optional/plugin
   - Remove entirely

6. Any critical consumers that need migration support?
   - List of services using this package
   - Breaking change communication plan
