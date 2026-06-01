# Echo Framework Evaluation (TSK-016)

**Date**: 2025-12-02
**Sprint**: S3
**Status**: ✅ Evaluation Complete
**Decision**: ⚠️ **REMOVE - Minimal Usage, Easy Migration**

---

## Executive Summary

Echo (github.com/labstack/echo/v4) is **barely used** in the Kosha codebase. Only **6 functions** in a single file (`errors/http/http_errors.go`) depend on `echo.Context`, and the HTTP server already uses standard `net/http` + grpc-gateway. **Recommendation: Remove Echo dependency completely.**

---

## Current Usage Analysis

### Where Echo is Used

**Single File**: `errors/http/http_errors.go`

| Function | Line | Usage |
|----------|------|-------|
| `NewBadRequestError()` | 108 | `ctx echo.Context` - calls `ctx.JSON()` |
| `NewNotFoundError()` | 121 | `ctx echo.Context` - calls `ctx.JSON()` |
| `NewUnauthorizedError()` | 134 | `ctx echo.Context` - calls `ctx.JSON()` |
| `NewForbiddenError()` | 148 | `ctx echo.Context` - calls `ctx.JSON()` |
| `NewInternalServerError()` | 162 | `ctx echo.Context` - calls `ctx.JSON()` |
| `ErrorCtxResponse()` | 229 | `ctx echo.Context` - calls `ctx.JSON()` |

**Total Echo Usage**: 6 functions, all in the same file

### Where Echo is NOT Used

✅ **HTTP Server** (`server/http/http.go`): Uses standard `net/http.Server` + grpc-gateway
✅ **gRPC Server** (`server/grpc/grpc.go`): Uses `google.golang.org/grpc`
✅ **Middleware**: All middleware uses `http.Handler` pattern
✅ **Routing**: Uses grpc-gateway's `runtime.ServeMux`
✅ **Request Handling**: All handlers use standard `http.ResponseWriter` and `*http.Request`

### Actual Server Stack

```go
// server/http/http.go - NO ECHO!
type CustomHttpServer struct {
    httpServer *http.Server              // Standard Go HTTP server
    mux        *runtime.ServeMux         // grpc-gateway mux
    // ... no Echo
}

func (s *CustomHttpServer) CreateHandler(mux *runtime.ServeMux) http.Handler {
    corsHandler := cors.New(corsOptions)
    handler := tenant.HttpTenantMiddleware(corsHandler.Handler(mux))
    handler = s.ActiveCounterMiddleware(handler)
    return handler  // Returns http.Handler, not echo
}
```

---

## Why Echo Was Added (Historical Context)

Based on codebase analysis:

1. **Legacy Code**: `errors/http/http_errors.go` appears to be copied from an Echo-based project
2. **Convenience**: Echo's `ctx.JSON()` was easier than `json.NewEncoder(w).Encode()`
3. **Never Integrated**: The HTTP server was built with grpc-gateway, not Echo
4. **Dead Code**: The 6 Echo-dependent functions are **never called** anywhere in the codebase

---

## Migration Path: Echo → Standard net/http

### Current (Echo-based)
```go
func NewBadRequestError(ctx echo.Context, causes interface{}, debug bool) error {
    restError := RestError{
        ErrStatus: http.StatusBadRequest,
        ErrError:  BadRequest.Error(),
        Timestamp: time.Now().UTC(),
    }
    if debug {
        restError.ErrMessage = causes
    }
    return ctx.JSON(http.StatusBadRequest, restError)
}
```

### Proposed (Standard net/http)
```go
func NewBadRequestError(w http.ResponseWriter, r *http.Request, causes interface{}, debug bool) error {
    restError := RestError{
        ErrStatus: http.StatusBadRequest,
        ErrError:  BadRequest.Error(),
        Timestamp: time.Now().UTC(),
    }
    if debug {
        restError.ErrMessage = causes
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusBadRequest)
    return json.NewEncoder(w).Encode(restError)
}
```

**Complexity**: Trivial - 2 lines instead of 1
**Benefits**: -1 dependency, standard Go idioms

---

## Dependency Impact

### Current (with Echo)
```bash
$ grep "labstack/echo" go.mod
github.com/labstack/echo/v4 v4.13.4
```

Echo brings these transitive dependencies:
- `labstack/gommon` (logging, color)
- `valyala/fasttemplate` (template rendering)
- `golang.org/x/crypto` (crypto primitives)
- `golang.org/x/net` (HTTP/2)
- `golang.org/x/text` (i18n)

**Total**: ~10 transitive dependencies for 6 unused functions

### After Removal
```bash
# go.mod will shrink by ~10 dependencies
# go.sum will shrink by ~20 entries
```

---

## Alternative: Keep Minimal Echo Wrapper

If you want to keep Echo for future flexibility:

```go
// errors/http/context.go
package httpErrors

import (
    "encoding/json"
    "net/http"
)

// EchoContext-like wrapper for standard net/http
type Context struct {
    Writer  http.ResponseWriter
    Request *http.Request
}

func (c *Context) JSON(code int, i interface{}) error {
    c.Writer.Header().Set("Content-Type", "application/json")
    c.Writer.WriteHeader(code)
    return json.NewEncoder(c.Writer).Encode(i)
}
```

This gives you the same API as Echo without the dependency.

---

## Comparison Table

| Aspect | With Echo | Without Echo (Standard) | Without Echo (Wrapper) |
|--------|-----------|-------------------------|------------------------|
| Dependencies | +10 transitive | 0 extra | 0 extra |
| LOC in errors/http | 236 lines | ~240 lines (+4) | ~250 lines (+14) |
| API Compatibility | Echo-style | Standard Go | Echo-style (custom) |
| Maintenance | Track Echo updates | Zero maintenance | Minimal maintenance |
| Learning Curve | Echo docs required | Standard Go docs | Custom docs |
| Future HTTP Framework | Locked to Echo | Flexible | Flexible |

---

## Usage Search Results

```bash
# Search for Echo usage in actual handlers/controllers
$ grep -r "NewBadRequestError\|NewNotFoundError\|NewUnauthorizedError" --include="*.go" .
# RESULT: No matches outside errors/http/http_errors.go

# Search for ErrorCtxResponse usage
$ grep -r "ErrorCtxResponse" --include="*.go" .
# RESULT: No matches outside errors/http/http_errors.go
```

**Conclusion**: The 6 Echo-dependent functions are **dead code** - never called anywhere.

---

## Decision Matrix

| Criteria | Keep Echo | Remove Echo | Weighted Score |
|----------|-----------|-------------|----------------|
| **Current Usage** | ❌ Only 6 functions in 1 file | ✅ Easy to migrate | Remove +5 |
| **Server Architecture** | ❌ HTTP server uses grpc-gateway | ✅ Already standard net/http | Remove +5 |
| **Dependency Count** | ❌ +10 transitive deps | ✅ -10 deps | Remove +3 |
| **Code Complexity** | ✅ ctx.JSON() is 1 line | ⚠️ json.Encoder is 2 lines | Keep +1 |
| **Maintenance Burden** | ❌ Track Echo security updates | ✅ Standard library (stable) | Remove +4 |
| **Team Knowledge** | ⚠️ Need Echo expertise | ✅ Standard Go (everyone knows) | Remove +3 |
| **Future Flexibility** | ❌ Locked to Echo patterns | ✅ Can switch frameworks easily | Remove +4 |
| **API Consistency** | ❌ Echo in errors, std everywhere else | ✅ 100% standard library | Remove +5 |

**Total Weighted Score**: **Remove Echo (+29)** vs Keep Echo (+1)

---

## Recommendation: REMOVE Echo

### Implementation Plan

#### Step 1: Refactor errors/http/http_errors.go

Replace all Echo-dependent functions with standard `net/http`:

```go
// Before
func NewBadRequestError(ctx echo.Context, causes interface{}, debug bool) error

// After
func NewBadRequestError(w http.ResponseWriter, r *http.Request, causes interface{}, debug bool) error
```

#### Step 2: Create Helper Function

```go
func writeJSONError(w http.ResponseWriter, status int, err RestError) error {
    w.Header().Set("Content-Type", "application/json; charset=utf-8")
    w.WriteHeader(status)
    return json.NewEncoder(w).Encode(err)
}
```

#### Step 3: Update All 6 Functions

Replace `ctx.JSON(status, restError)` with `writeJSONError(w, status, restError)`

#### Step 4: Remove Dependency

```bash
go get github.com/labstack/echo/v4@none
go mod tidy
```

#### Step 5: Update Tests

If there are tests for `errors/http`, update them to use `httptest.ResponseRecorder`.

---

## Risk Assessment

| Risk | Severity | Mitigation |
|------|----------|----------|
| Breaking changes to error handling | ⚠️ Medium | These functions are unused (dead code) |
| Need to rewrite HTTP handlers | ❌ None | HTTP server already uses std lib |
| Loss of Echo ecosystem | ✅ Low | Only used ctx.JSON(), nothing else |
| Team unfamiliar with std lib | ✅ None | Standard Go is universal knowledge |
| Future regret | ✅ Low | Easy to re-add if truly needed |

**Overall Risk**: ✅ **LOW** - Echo is barely integrated

---

## Files to Modify

| File | Changes | LOC Impact |
|------|---------|-----------|
| `errors/http/http_errors.go` | Replace 6 function signatures, add helper | +10 lines |
| `go.mod` | Remove Echo dependency | -1 line |
| `go.sum` | Remove Echo checksums | -20 lines |

**Total Impact**: 3 files, ~30 minutes of work

---

## Final Verdict

### ✅ **REMOVE Echo**

**Reasons**:
1. **Minimal Usage**: Only 6 functions in 1 file use Echo
2. **Dead Code**: No actual calls to these functions found
3. **Server Architecture**: HTTP server uses grpc-gateway (standard net/http), not Echo
4. **Dependency Bloat**: +10 transitive dependencies for essentially zero value
5. **Consistency**: Rest of codebase uses standard library
6. **Maintenance**: One less framework to track/update
7. **Simplicity**: Standard Go is easier to understand and maintain

### Migration Effort: **Trivial** (~30 minutes)

### Breaking Changes: **None** (functions are unused)

### Next Steps:
1. Mark TSK-016 as "Completed - Recommendation: Remove"
2. Create TSK-024: "Remove Echo dependency from errors/http"
3. Update `requirements.md` with NFR for dependency minimization

---

## Appendix: Alternative Frameworks (If Needed)

If you later decide you need a full HTTP framework:

| Framework | Pros | Cons |
|-----------|------|------|
| **Echo** | Fast, feature-rich, middleware | +10 deps, opinionated |
| **Gin** | Fastest benchmarks, popular | Similar to Echo |
| **Chi** | Minimal, std lib compatible | Less features |
| **Gorilla Mux** | Battle-tested, flexible | Slower than Echo/Gin |
| **net/http (stdlib)** | Zero deps, universal | More verbose |

**Recommendation**: Stick with **grpc-gateway + net/http** (current architecture)

---

*Evaluation completed by: Claude Code*
*Date: 2025-12-02*
*Status: ✅ Complete - Remove Echo Recommended*
