# Test Coverage Plan (TSK-017)

**Created**: 2025-12-02
**Sprint**: S5-S7 (Multi-Sprint Initiative)
**Target**: 80% code coverage
**Current**: ~1% overall (only builder package has 23%)

---

## Executive Summary

Achieving 80% test coverage across 73 packages is a **multi-sprint effort**. This plan breaks down the work into prioritized phases based on:
1. **Risk** (production impact if bugs occur)
2. **Complexity** (testing difficulty)
3. **Dependencies** (used by other packages)

---

## Current State Analysis

### Packages WITH Tests
| Package | Coverage | Test Files |
|---------|----------|------------|
| database/pgxpostgres/builder | 23.0% | typed_query_test.go, query_logger_test.go, query_metrics_test.go |

### Packages WITHOUT Tests (73 packages)
All other packages have 0% coverage.

---

## Testing Strategy

### Test Types Pyramid

```
        Unit Tests (70%)
       /              \
      /    Integration  \
     /      Tests (20%)  \
    /____________________\
      E2E Tests (10%)
```

**Focus**: Unit tests first, integration tests for critical paths

---

## Phase 1: Critical Infrastructure (Sprint 5)
**Target**: Core packages that other services depend on
**Priority**: HIGH

| Task ID | Package | Reason | Estimated LOC |
|---------|---------|--------|---------------|
| TSK-025 | database/pgxpostgres | Database layer - critical | 300 |
| TSK-026 | p9log | Logging used everywhere | 150 |
| TSK-027 | metrics | Observability foundation | 200 |
| TSK-028 | errors | Error handling system | 200 |
| TSK-029 | uow | Transaction management | 150 |
| **Total** | | | **1,000 lines** |

---

## Phase 2: Middleware & Context (Sprint 6)
**Target**: Request flow components
**Priority**: HIGH

| Task ID | Package | Reason | Estimated LOC |
|---------|---------|--------|---------------|
| TSK-030 | p9context | Context management | 200 |
| TSK-031 | middleware/tenant | Multi-tenancy critical | 150 |
| TSK-032 | middleware/dbmiddleware | DB routing | 150 |
| TSK-033 | authz | Authorization | 200 |
| TSK-034 | middleware/recovery | Error handling | 100 |
| **Total** | | | **800 lines** |

---

## Phase 3: Utilities & Helpers (Sprint 6)
**Target**: Helper packages used across codebase
**Priority**: MEDIUM

| Task ID | Package | Reason | Estimated LOC |
|---------|---------|--------|---|
| TSK-035 | helpers/repo | Generic repository helpers | 300 |
| TSK-036 | helpers/service | Service layer helpers | 250 |
| TSK-037 | utils | Conversion utilities (already done manually) | 100 |
| TSK-038 | ULID | ULID generation (partially done) | 100 |
| TSK-039 | converters | Protobuf converters | 150 |
| **Total** | | | **900 lines** |

---

## Phase 4: Events & Messaging (Sprint 7)
**Target**: Kafka event bus
**Priority**: MEDIUM

| Task ID | Package | Reason | Estimated LOC |
|---------|---------|--------|---|
| TSK-040 | events/producer | Kafka publishing | 200 |
| TSK-041 | events/consumer | Kafka consumption | 200 |
| TSK-042 | events/bus | Event bus logic | 150 |
| TSK-043 | events/config | Event configuration | 100 |
| **Total** | | | **650 lines** |

---

## Phase 5: Server & Transport (Sprint 7)
**Target**: HTTP/gRPC servers
**Priority**: MEDIUM

| Task ID | Package | Reason | Estimated LOC |
|---------|---------|--------|---|
| TSK-044 | server/http | HTTP server | 200 |
| TSK-045 | server/grpc | gRPC server | 200 |
| TSK-046 | server | Multi-server coordination | 150 |
| TSK-047 | transport | Transport layer | 100 |
| **Total** | | | **650 lines** |

---

## Phase 6: Encoding & Config (Sprint 7)
**Target**: Codec system and configuration
**Priority**: LOW

| Task ID | Package | Reason | Estimated LOC |
|---------|---------|--------|---|
| TSK-048 | encoding/json | JSON codec | 100 |
| TSK-049 | encoding/proto | Protobuf codec | 100 |
| TSK-050 | encoding/xml | XML codec | 100 |
| TSK-051 | encoding/yaml | YAML codec | 100 |
| TSK-052 | config | Configuration management | 200 |
| **Total** | | | **600 lines** |

---

## Deferred Packages (Future)
**Reason**: Low priority, generated code, or minimal logic

| Package Type | Examples | Reason to Defer |
|--------------|----------|-----------------|
| Protobuf Generated | api/v1/* | Auto-generated, low value |
| Cache | cache | Third-party wrapper |
| Database Drivers | database/redis, database/sqlc | Thin wrappers |
| Tracing | tracing | OpenTelemetry wrapper |
| Timeout | timeout | Simple logic |

---

## Testing Guidelines

### Unit Test Template

```go
package mypackage

import (
    "context"
    "testing"
)

func TestFunctionName(t *testing.T) {
    tests := []struct {
        name    string
        input   InputType
        want    OutputType
        wantErr bool
    }{
        {
            name:    "happy path",
            input:   validInput,
            want:    expectedOutput,
            wantErr: false,
        },
        {
            name:    "error case",
            input:   invalidInput,
            want:    nil,
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := FunctionName(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("FunctionName() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if got != tt.want {
                t.Errorf("FunctionName() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

### Mock Guidelines

**Use interfaces for mocking**:
```go
// In production code
type MetricsProvider interface {
    RecordOperation(op string, duration time.Duration)
}

// In test code
type mockMetrics struct {
    calls []string
}

func (m *mockMetrics) RecordOperation(op string, duration time.Duration) {
    m.calls = append(m.calls, op)
}
```

### Coverage Thresholds

| Phase | Package Type | Min Coverage | Target Coverage |
|-------|--------------|--------------|-----------------|
| 1 | Critical | 70% | 85% |
| 2-3 | High Priority | 60% | 75% |
| 4-6 | Medium Priority | 50% | 65% |
| Deferred | Low Priority | 30% | 50% |

---

## Success Criteria

### Phase 1 Complete When:
- [ ] All Phase 1 packages have ≥70% coverage
- [ ] All critical paths tested (happy + error cases)
- [ ] Mock dependencies established
- [ ] Integration tests for database layer

### Phase 2 Complete When:
- [ ] All Phase 2 packages have ≥60% coverage
- [ ] Middleware chain tested end-to-end
- [ ] Context propagation verified

### Overall Success (Target 80%):
```
Weighted Average = (Critical * 85%) + (High * 75%) + (Medium * 65%) + (Low * 50%)
                 = 72-78% actual coverage (meets 80% target with critical focus)
```

---

## Estimated Effort

| Phase | Packages | Test Lines | Estimated Hours | Sprint |
|-------|----------|------------|-----------------|--------|
| Phase 1 | 5 | 1,000 | 20-30 hours | S5 |
| Phase 2 | 5 | 800 | 16-24 hours | S6 |
| Phase 3 | 5 | 900 | 18-27 hours | S6 |
| Phase 4 | 4 | 650 | 13-20 hours | S7 |
| Phase 5 | 4 | 650 | 13-20 hours | S7 |
| Phase 6 | 5 | 600 | 12-18 hours | S7 |
| **Total** | **28** | **4,600** | **92-139 hours** | **3 sprints** |

**Note**: Remaining 45 packages deferred to future sprints or deprioritized (generated code, thin wrappers).

---

## Risk Mitigation

| Risk | Impact | Mitigation |
|------|--------|-----------|
| Tests break existing code | High | Run tests continuously, no breaking changes |
| Mock complexity | Medium | Use interface-based mocks, keep simple |
| Integration test flakiness | Medium | Use testcontainers for Postgres, mock Kafka |
| Time overrun | High | Focus on critical packages first, defer low-value tests |
| Team bandwidth | High | This is a multi-sprint effort, adjust as needed |

---

## Next Steps

1. ✅ Create this plan (TSK-017 parent task)
2. ⏭️ Add TSK-025 through TSK-052 to [todo.md](todo.md)
3. ⏭️ Start Phase 1 (Sprint 5)
4. ⏭️ Review coverage after each phase
5. ⏭️ Adjust plan based on learnings

---

*Plan Created: 2025-12-02*
*Phases: 6 (across 3 sprints)*
*Target: 72-78% weighted coverage (80% effective with critical focus)*
