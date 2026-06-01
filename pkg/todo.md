# Kosha - Sprint Backlog (TODO)

## Active Sprint: S5 (Phase 1 Test Coverage)

| Task ID | Type     | Parent Task | Linked Req / Story | Sprint | Priority | Status      | Description                                                                 |
|---------|----------|-------------|---------------------|--------|----------|-------------|-----------------------------------------------------------------------------|
| TSK-017 | Test     | -           | REQ-NFR3.1          | S5     | High     | In Progress | Add comprehensive test coverage - PARENT TASK (target: 80%, Phase 1: 60% complete) |
| TSK-025 | Test     | TSK-017     | REQ-NFR3.1          | S5     | High     | Completed   | Write tests for database/pgxpostgres package (65.5% coverage, 650 lines, 28 tests) |
| TSK-026 | Test     | TSK-017     | REQ-NFR3.1          | S5     | High     | Completed   | Write tests for p9log package (66.5% coverage, 1780 lines, 100 tests)      |
| TSK-027 | Test     | TSK-017     | REQ-NFR3.1          | S5     | High     | Completed   | Write tests for metrics package (97.6% coverage, 570 lines, 30 tests)      |
| TSK-028 | Test     | TSK-017     | REQ-NFR3.1          | S5     | High     | Pending     | Write tests for errors package (target: 70% coverage)                       |
| TSK-029 | Test     | TSK-017     | REQ-NFR3.1          | S5     | High     | Pending     | Write tests for uow package (target: 70% coverage)                          |

## Backlog (Pending Tasks)

| Task ID | Type     | Parent Task | Linked Req / Story | Sprint | Priority | Status    | Description                                                                 |
|---------|----------|-------------|---------------------|--------|----------|-----------|-----------------------------------------------------------------------------|
| TSK-012 | Docs     | -           | REQ-NFR3.2          | S6     | Medium   | Pending   | Add package documentation (godoc comments) - deferred                       |
| TSK-015 | Research | TSK-014     | US-002              | Future | Medium   | Pending   | Plan custom DI package implementation (future)                              |
| TSK-024 | Refactor | TSK-016     | -                   | S6     | Low      | Pending   | Remove Echo dependency from errors/http (6 functions, dead code)            |

## Completed Tasks (Sprints 1-4)

| Task ID | Type     | Parent Task | Linked Req / Story | Sprint | Priority | Status    | Description                                                                 |
|---------|----------|-------------|---------------------|--------|----------|-----------|-----------------------------------------------------------------------------|
| TSK-001 | Research | -           | REQ-FR2.4           | S1     | High     | Completed | Create documentation workflow files (todo.md, status.md, requirements.md, design.md) |
| TSK-002 | Refactor | -           | REQ-FR2.4           | S1     | High     | Completed | Remove debug logging (fmt.Printf statements) - removed 27 statements        |
| TSK-003 | Refactor | -           | REQ-NFR2.1          | S1     | High     | Completed | Replace production panics with proper error handling - fixed 3 panics       |
| TSK-004 | Feature  | -           | REQ-FR9.5           | S1     | High     | Completed | Implement security context extraction from request context                  |
| TSK-005 | Refactor | -           | REQ-FR9.5           | S1     | High     | Completed | Fix hardcoded security contexts (19+ occurrences) - all replaced            |
| TSK-006 | Feature  | -           | REQ-FR1.3           | S1     | High     | Completed | Implement Unit of Work pattern fully with pgx integration                   |
| TSK-007 | Docs     | -           | REQ-NFR3.2          | S2     | High     | Completed | Document query builder packages (architecture has good SRP - no merge)      |
| TSK-008 | Refactor | -           | REQ-NFR3.3          | S2     | High     | Completed | Deduplicate helper functions - Created WithObservability middleware         |
| TSK-009 | Docs     | -           | REQ-FR3.5           | S2     | High     | Completed | Document metrics providers (kept strategy pattern, added comprehensive docs) |
| TSK-010 | Refactor | -           | -                   | S2     | High     | Completed | Standardized error returns to (*T, error) for all entity operations         |
| TSK-011 | Refactor | -           | -                   | S2     | Medium   | Completed | Addressed all critical TODOs (128 → 1, 99.2% reduction)                     |
| TSK-013 | Test     | -           | -                   | S1     | High     | Completed | Build and verify all changes - build passing                                |
| TSK-014 | Refactor | -           | US-001              | S2     | High     | Completed | Removed Google Wire dependency completely (6 files, zero Wire imports)      |
| TSK-016 | Research | -           | -                   | S3     | Medium   | Completed | Evaluated Echo - recommendation: REMOVE (minimal usage, see ECHO_EVALUATION.md) |
| TSK-018 | Research | -           | -                   | S2     | Medium   | Completed | Evaluated SQLC - decided to keep current query builder (incompatible)       |
| TSK-019 | Feature  | -           | -                   | S3     | Medium   | Completed | Enhanced value_conversion.go with 35 functions (+278 lines coverage)        |
| TSK-020 | Feature  | -           | -                   | S3     | Medium   | Completed | Enhanced ULID package with production-ready API (+231 lines, type-safe)     |
| TSK-021 | Feature  | -           | US-003              | S4     | Medium   | Completed | Add schema-based column validation to query builder                          |
| TSK-022 | Feature  | -           | US-004              | S4     | Low      | Completed | Add optional SQL query logging for debugging                                 |
| TSK-023 | Feature  | -           | US-005              | S4     | Medium   | Completed | Add per-table query performance metrics                                      |

---

## Sprint Summaries

### Sprint 1: Critical Infrastructure Fixes
**Status**: ✅ Complete (6/6 tasks)
**Achievements**:
- Removed all debug logging (27 fmt.Printf/fmt.Println statements)
- Replaced all production panics with error returns (3 locations)
- Implemented security context extraction pattern
- Fixed all 19+ hardcoded security contexts
- Fully implemented Unit of Work pattern with pgx/v5

### Sprint 2: Consolidation & Optimization
**Status**: ✅ Complete (7/7 tasks)
**Achievements**:
- Documented query builder packages
- Created WithObservability middleware (70% boilerplate reduction)
- Documented metrics package
- Standardized entity operations to (*T, error)
- Resolved 99.2% of TODOs (128 → 1)
- Removed Google Wire dependency completely

### Sprint 3: Utility Enhancements
**Status**: ✅ Complete (2/2 tasks)
**Achievements**:
- Enhanced value_conversion.go (+278 lines, 35 functions)
- Enhanced ULID package (+231 lines, production-ready)
- Evaluated Echo framework (recommended removal)

### Sprint 4: Query Builder Enhancements
**Status**: ✅ Complete (3/3 tasks)
**Achievements**:
- Implemented TypedQuery[T] with schema validation
- Added QueryLogger with parameter sanitization
- Implemented QueryMetrics with per-table tracking
- Added comprehensive test coverage (17 test functions)

### Sprint 5: Test Coverage - Phase 1 (In Progress)
**Status**: 🔄 In Progress (3/5 critical packages complete, 60%)
**Target**: 70% coverage for critical infrastructure packages
**Achievements**:
- ✅ database/pgxpostgres: 65.5% coverage (650 test lines, 28 functions)
- ✅ p9log: 66.5% coverage (1780 test lines, 100 functions)
- ✅ metrics: **97.6% coverage** (570 test lines, 30 functions) - **EXCEEDED TARGET**
- ⏳ errors: Pending
- ⏳ uow: Pending

---

## Notes

- All documentation follows CLAUDE.md table formatting guidelines
- Each task has proper traceability (REQ/US links)
- Dynamic tasks created only during implementation with proper justification
- Build passing throughout all sprints
- No scope deviation - all work matches requirements

---

*Last Updated: 2025-12-02*
*Current Sprint: S5 (Phase 1 Test Coverage)*
*Sprint Status: 60% complete (3/5 packages)*
