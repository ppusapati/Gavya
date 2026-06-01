# Query Builder Usage Examples

This document provides comprehensive examples of how to use Kosha's dynamic query builder for protobuf-first APIs with PostgreSQL.

---

## Table of Contents
1. [Basic CRUD Operations](#basic-crud-operations)
2. [Dynamic Field Selection (FieldMask)](#dynamic-field-selection-fieldmask)
3. [Complex Filtering (Protobuf Filters)](#complex-filtering-protobuf-filters)
4. [Pagination and Sorting](#pagination-and-sorting)
5. [Transactions (Unit of Work)](#transactions-unit-of-work)
6. [Multi-Tenancy](#multi-tenancy)
7. [Full Example: User Service](#full-example-user-service)

---

## Basic CRUD Operations

### Create (INSERT)

```go
package main

import (
    "context"
    "kosha/deps"
    "kosha/helpers/repo"
    "kosha/models"
)

type User struct {
    ID        int64  `db:"id"`
    UUID      string `db:"uuid"`
    Name      string `db:"name"`
    Email     string `db:"email"`
    Status    string `db:"status"`
    CreatedAt time.Time `db:"created_at"`
}

func CreateUser(ctx context.Context, serviceDeps deps.ServiceDeps) error {
    newUser := User{
        UUID:   ULID.NewString(), // Use our enhanced ULID package
        Name:   "John Doe",
        Email:  "john@example.com",
        Status: "active",
    }

    // Helper function handles:
    // - Timeout application
    // - SQL generation
    // - Metrics recording
    // - Error handling
    // - Tracing
    createdUser, err := repo.CreateEntity[User](
        ctx,
        serviceDeps,
        "users",                                    // table name
        newUser,                                    // entity
        []string{"uuid", "name", "email", "status"}, // fields to insert
        []string{"email"},                          // conflict columns (ON CONFLICT)
    )

    if err != nil {
        return fmt.Errorf("failed to create user: %w", err)
    }

    log.Printf("Created user with ID: %d", createdUser.ID)
    return nil
}
```

**Generated SQL:**
```sql
INSERT INTO users (uuid, name, email, status, created_at, created_by)
VALUES ($1, $2, $3, $4, NOW(), $5)
ON CONFLICT (email) DO UPDATE SET
  uuid = EXCLUDED.uuid,
  name = EXCLUDED.name,
  status = EXCLUDED.status
RETURNING id, uuid, name, email, status, created_at;
```

---

### Read (SELECT)

#### Get by ID
```go
func GetUserByID(ctx context.Context, deps deps.ServiceDeps, userID int64) (*User, error) {
    user, err := repo.GetByID[User](
        ctx,
        deps,
        "users",
        userID,
    )

    if err != nil {
        return nil, fmt.Errorf("user not found: %w", err)
    }

    return user, nil
}
```

**Generated SQL:**
```sql
SELECT id, uuid, name, email, status, created_at, updated_at
FROM users
WHERE id = $1 AND deleted_at IS NULL;
```

#### Get by UUID
```go
func GetUserByUUID(ctx context.Context, deps deps.ServiceDeps, uuid string) (*User, error) {
    return repo.GetByUUID[User](ctx, deps, "users", uuid)
}
```

**Generated SQL:**
```sql
SELECT id, uuid, name, email, status, created_at
FROM users
WHERE uuid = $1 AND deleted_at IS NULL;
```

#### Get by Custom Field
```go
func GetUserByEmail(ctx context.Context, deps deps.ServiceDeps, email string) (*User, error) {
    dm := models.DataModel[User]{
        TableName:  "users",
        FieldNames: []string{"*"},
        Where:      "email = $1 AND deleted_at IS NULL",
        WhereArgs:  []any{email},
    }

    return repo.GetByField[User](ctx, deps, dm)
}
```

---

### Update (UPDATE)

```go
func UpdateUser(ctx context.Context, deps deps.ServiceDeps, userID int64, updates map[string]interface{}) error {
    // Fetch existing user
    existingUser, err := repo.GetByID[User](ctx, deps, "users", userID)
    if err != nil {
        return fmt.Errorf("user not found: %w", err)
    }

    // Apply updates
    if name, ok := updates["name"].(string); ok {
        existingUser.Name = name
    }
    if status, ok := updates["status"].(string); ok {
        existingUser.Status = status
    }

    // Get user from security context for audit
    secCtx := p9context.GetSecurityContextOrDefault(ctx)

    updateReq := helpers_utils.UpdateRequest[User]{
        Entity:    *existingUser,
        FieldMask: []string{"name", "status"}, // Only update these fields
        UpdatedBy: secCtx.Username,
        UpdatedAt: time.Now(),
    }

    _, err = repo.UpdateEntity[User](ctx, deps, updateReq)
    return err
}
```

**Generated SQL:**
```sql
UPDATE users
SET name = $1, status = $2, updated_at = $3, updated_by = $4
WHERE id = $5 AND deleted_at IS NULL
RETURNING id, uuid, name, email, status, updated_at;
```

---

### Delete (Soft Delete)

```go
func DeleteUser(ctx context.Context, deps deps.ServiceDeps, userID int64) error {
    secCtx := p9context.GetSecurityContextOrDefault(ctx)

    _, err := repo.DeleteEntity[User](
        ctx,
        deps,
        "users",
        userID,
        secCtx.Username, // deleted_by field
    )

    return err
}
```

**Generated SQL (Soft Delete):**
```sql
UPDATE users
SET deleted_at = NOW(), deleted_by = $1
WHERE id = $2 AND deleted_at IS NULL;
```

---

## Dynamic Field Selection (FieldMask)

Protobuf FieldMask allows clients to request only specific fields, reducing payload size and improving performance.

```go
import (
    "google.golang.org/protobuf/types/known/fieldmaskpb"
)

func ListUsersWithFieldMask(
    ctx context.Context,
    deps deps.ServiceDeps,
    fieldMask *fieldmaskpb.FieldMask,
) ([]*User, error) {
    // Extract function converts protobuf field paths to DB columns
    // e.g., "user.name" -> "name", "user.created_at" -> "created_at"
    extractFunc := func(paths []string) ([]string, error) {
        dbFields := make([]string, 0, len(paths))
        for _, path := range paths {
            // Strip protobuf message prefix if present
            field := strings.TrimPrefix(path, "user.")
            dbFields = append(dbFields, field)
        }
        return dbFields, nil
    }

    search := &models.SearchCriteria{
        FieldMask: fieldMask,
        PageSize:  50,
    }

    return repo.ListEntity[User](ctx, deps, "users", search, extractFunc)
}
```

**Example 1: Request only ID and name**
```protobuf
field_mask: {paths: ["id", "name"]}
```

**Generated SQL:**
```sql
SELECT id, name
FROM users
WHERE deleted_at IS NULL
LIMIT 50;
```

**Example 2: Request all fields**
```protobuf
field_mask: {}  // Empty = all fields
```

**Generated SQL:**
```sql
SELECT *
FROM users
WHERE deleted_at IS NULL
LIMIT 50;
```

---

## Complex Filtering (Protobuf Filters)

The builder supports rich filter operations from protobuf definitions.

### String Filters

```go
import (
    "kosha/api/v1/query"
    "google.golang.org/protobuf/types/known/wrapperspb"
)

func SearchUsersByName(ctx context.Context, deps deps.ServiceDeps, searchTerm string) ([]*User, error) {
    criteria := &models.SearchCriteria{
        // Contains filter (case-insensitive)
        SearchTerm: &query.StringFilterOperation{
            Contains: &wrapperspb.StringValue{Value: searchTerm},
        },
        PageSize: 20,
    }

    return repo.ListEntity[User](ctx, deps, "users", criteria, extractFieldsFunc)
}
```

**Generated SQL:**
```sql
SELECT * FROM users
WHERE name ILIKE $1 AND deleted_at IS NULL
LIMIT 20;
-- $1 = "%searchTerm%"
```

### Multiple Filters Combined

```go
func SearchUsers(ctx context.Context, deps deps.ServiceDeps, req *SearchRequest) ([]*User, error) {
    criteria := &models.SearchCriteria{
        // String filter: name contains
        SearchTerm: &query.StringFilterOperation{
            Contains: &wrapperspb.StringValue{Value: req.NameSearch},
        },

        // Enum filter: status equals
        Filters: []models.Filter{
            {
                Field:    "status",
                Operator: models.OperatorEquals,
                Value:    "active",
            },
        },

        // Date range: created in last 30 days
        CreatedAtFrom: &query.DateFilterOperators{
            Gte: timestamppb.New(time.Now().AddDate(0, 0, -30)),
        },

        // Sorting
        Sort: []string{"created_at DESC"},

        // Pagination
        PageSize:   req.PageSize,
        PageOffset: req.PageOffset,
    }

    return repo.ListEntity[User](ctx, deps, "users", criteria, extractFieldsFunc)
}
```

**Generated SQL:**
```sql
SELECT * FROM users
WHERE name ILIKE $1
  AND status = $2
  AND created_at >= $3
  AND deleted_at IS NULL
ORDER BY created_at DESC
LIMIT $4 OFFSET $5;
```

### Array Filters (IN operator)

```go
func GetUsersByIDs(ctx context.Context, deps deps.ServiceDeps, userIDs []int64) ([]*User, error) {
    criteria := &models.SearchCriteria{
        Filters: []models.Filter{
            {
                Field:    "id",
                Operator: models.OperatorIn,
                Value:    userIDs,
            },
        },
    }

    return repo.ListEntity[User](ctx, deps, "users", criteria, extractFieldsFunc)
}
```

**Generated SQL:**
```sql
SELECT * FROM users
WHERE id = ANY($1::bigint[])
  AND deleted_at IS NULL;
-- $1 = {10, 25, 42, 78}
```

---

## Pagination and Sorting

```go
func ListUsersPaginated(
    ctx context.Context,
    deps deps.ServiceDeps,
    page int32,
    pageSize int32,
    sortBy string,
) ([]*User, int64, error) {
    offset := (page - 1) * pageSize

    // Get total count
    countCriteria := &models.SearchCriteria{}
    totalCount, err := repo.CountEntity[User](ctx, deps, "users", countCriteria)
    if err != nil {
        return nil, 0, err
    }

    // Get paginated results
    criteria := &models.SearchCriteria{
        Sort:       []string{sortBy}, // e.g., "created_at DESC"
        PageSize:   pageSize,
        PageOffset: offset,
    }

    users, err := repo.ListEntity[User](ctx, deps, "users", criteria, extractFieldsFunc)
    if err != nil {
        return nil, 0, err
    }

    return users, totalCount, nil
}
```

**Generated SQL for Count:**
```sql
SELECT COUNT(*) FROM users WHERE deleted_at IS NULL;
```

**Generated SQL for List:**
```sql
SELECT * FROM users
WHERE deleted_at IS NULL
ORDER BY created_at DESC
LIMIT 20 OFFSET 40;
-- Page 3, 20 items per page
```

---

## Transactions (Unit of Work)

```go
import "kosha/uow"

func TransferCredits(
    ctx context.Context,
    deps deps.ServiceDeps,
    fromUserID int64,
    toUserID int64,
    amount int64,
) error {
    // Start transaction
    return uow.WithTransaction(ctx, deps.Pool, func(txCtx context.Context) error {
        // Deduct from source user
        fromUser, err := repo.GetByID[User](txCtx, deps, "users", fromUserID)
        if err != nil {
            return fmt.Errorf("source user not found: %w", err)
        }

        if fromUser.Credits < amount {
            return errors.New("insufficient credits")
        }

        fromUser.Credits -= amount
        updateReq1 := helpers_utils.UpdateRequest[User]{
            Entity:    *fromUser,
            FieldMask: []string{"credits"},
            UpdatedBy: "system",
            UpdatedAt: time.Now(),
        }

        _, err = repo.UpdateEntity[User](txCtx, deps, updateReq1)
        if err != nil {
            return fmt.Errorf("failed to deduct credits: %w", err)
        }

        // Add to destination user
        toUser, err := repo.GetByID[User](txCtx, deps, "users", toUserID)
        if err != nil {
            return fmt.Errorf("destination user not found: %w", err)
        }

        toUser.Credits += amount
        updateReq2 := helpers_utils.UpdateRequest[User]{
            Entity:    *toUser,
            FieldMask: []string{"credits"},
            UpdatedBy: "system",
            UpdatedAt: time.Now(),
        }

        _, err = repo.UpdateEntity[User](txCtx, deps, updateReq2)
        if err != nil {
            return fmt.Errorf("failed to add credits: %w", err)
        }

        // Transaction commits automatically if no error
        // Rolls back automatically if any error returned
        return nil
    })
}
```

**Benefit**: All operations execute within a single PostgreSQL transaction. If any operation fails, all changes are rolled back automatically.

---

## Multi-Tenancy

The builder automatically applies tenant isolation when tenant info is in context.

```go
import "kosha/p9context"

func GetTenantUsers(ctx context.Context, deps deps.ServiceDeps) ([]*User, error) {
    // Extract tenant from context (set by middleware)
    tenant := p9context.GetCurrentTenant(ctx)

    // Builder automatically adds: AND tenant_id = $X
    criteria := &models.SearchCriteria{
        PageSize: 100,
    }

    // Tenant filter is automatically applied by BuildWhereClause
    return repo.ListEntity[User](ctx, deps, "users", criteria, extractFieldsFunc)
}
```

**Generated SQL (with tenant context):**
```sql
SELECT * FROM users
WHERE tenant_id = $1
  AND deleted_at IS NULL
LIMIT 100;
-- $1 = tenant ID from context
```

**Generated SQL (without tenant context / super admin):**
```sql
SELECT * FROM users
WHERE deleted_at IS NULL
LIMIT 100;
-- No tenant filter for system-level queries
```

---

## Full Example: User Service

Here's a complete user service implementation using the query builder:

```go
package user

import (
    "context"
    "fmt"
    "time"

    "kosha/deps"
    "kosha/helpers/repo"
    "kosha/helpers/service"
    "kosha/models"
    "kosha/p9context"
    "kosha/ULID"

    pbr "kosha/api/v1/response"
    pb "myapp/api/v1/user"

    "google.golang.org/protobuf/types/known/fieldmaskpb"
)

type UserService struct {
    deps deps.ServiceDeps
}

func NewUserService(deps deps.ServiceDeps) *UserService {
    return &UserService{deps: deps}
}

// CreateUser creates a new user
func (s *UserService) CreateUser(
    ctx context.Context,
    req *pb.CreateUserRequest,
) (*pbr.OperationResponse, error) {
    // Convert protobuf to DB model
    convertFunc := func(r *pb.CreateUserRequest) User {
        return User{
            UUID:   ULID.NewString(),
            Name:   r.Name,
            Email:  r.Email,
            Status: r.Status,
        }
    }

    // Convert DB model to protobuf
    toProtoFunc := func(u User) *pb.User {
        return &pb.User{
            Id:        u.ID,
            Uuid:      u.UUID,
            Name:      u.Name,
            Email:     u.Email,
            Status:    u.Status,
            CreatedAt: timestamppb.New(u.CreatedAt),
        }
    }

    // Generic helper handles all the boilerplate
    return service.CreateEntity[User, *pb.CreateUserRequest](
        ctx,
        req,
        convertFunc,
        toProtoFunc,
        s.deps.Tracing,
        s.deps.Metrics,
        func(ctx context.Context, entity User) (*User, error) {
            return repo.CreateEntity[User](
                ctx,
                s.deps,
                "users",
                entity,
                []string{"uuid", "name", "email", "status"},
                []string{"email"},
            )
        },
    )
}

// GetUser retrieves a user by ID or UUID
func (s *UserService) GetUser(
    ctx context.Context,
    req *pb.GetUserRequest,
) (*pbr.OperationResponse, error) {
    toProtoFunc := func(u User) *pb.User {
        return &pb.User{
            Id:        u.ID,
            Uuid:      u.UUID,
            Name:      u.Name,
            Email:     u.Email,
            Status:    u.Status,
            CreatedAt: timestamppb.New(u.CreatedAt),
        }
    }

    return service.GetEntity[User, *pb.User](
        ctx,
        s.deps,
        req.Id,
        req.Uuid,
        "",  // No custom field
        nil, // No custom value
        toProtoFunc,
        s.deps.Tracing,
        s.deps.Metrics,
        func(ctx context.Context, id int64) (*User, error) {
            return repo.GetByID[User](ctx, s.deps, "users", id)
        },
        func(ctx context.Context, uuid string) (*User, error) {
            return repo.GetByUUID[User](ctx, s.deps, "users", uuid)
        },
        nil, // No GetByField
    )
}

// ListUsers retrieves users with filtering, pagination, and field selection
func (s *UserService) ListUsers(
    ctx context.Context,
    req *pb.ListUsersRequest,
) (*pb.ListUsersResponse, error) {
    // Build search criteria from request
    criteria := &models.SearchCriteria{
        FieldMask:  req.FieldMask,
        PageSize:   req.PageSize,
        PageOffset: (req.Page - 1) * req.PageSize,
        Sort:       []string{req.SortBy},
    }

    // Add filters if provided
    if req.StatusFilter != "" {
        criteria.Filters = append(criteria.Filters, models.Filter{
            Field:    "status",
            Operator: models.OperatorEquals,
            Value:    req.StatusFilter,
        })
    }

    if req.SearchTerm != "" {
        criteria.SearchTerm = &query.StringFilterOperation{
            Contains: &wrapperspb.StringValue{Value: req.SearchTerm},
        }
    }

    // Extract field mask
    extractFunc := func(paths []string) ([]string, error) {
        dbFields := make([]string, 0, len(paths))
        for _, path := range paths {
            field := strings.TrimPrefix(path, "user.")
            dbFields = append(dbFields, field)
        }
        return dbFields, nil
    }

    // Query with observability
    users, err := repo.ListEntity[User](ctx, s.deps, "users", criteria, extractFunc)
    if err != nil {
        return nil, err
    }

    // Get total count for pagination
    totalCount, err := repo.CountEntity[User](ctx, s.deps, "users", criteria)
    if err != nil {
        return nil, err
    }

    // Convert to protobuf
    pbUsers := make([]*pb.User, len(users))
    for i, user := range users {
        pbUsers[i] = &pb.User{
            Id:        user.ID,
            Uuid:      user.UUID,
            Name:      user.Name,
            Email:     user.Email,
            Status:    user.Status,
            CreatedAt: timestamppb.New(user.CreatedAt),
        }
    }

    return &pb.ListUsersResponse{
        Users:      pbUsers,
        TotalCount: totalCount,
        Page:       req.Page,
        PageSize:   req.PageSize,
    }, nil
}

// UpdateUser updates user fields
func (s *UserService) UpdateUser(
    ctx context.Context,
    req *pb.UpdateUserRequest,
) (*pbr.OperationResponse, error) {
    convertFunc := func(r *pb.UpdateUserRequest) User {
        return User{
            ID:     r.Id,
            Name:   r.User.Name,
            Email:  r.User.Email,
            Status: r.User.Status,
        }
    }

    toProtoFunc := func(u User) *pb.User {
        return &pb.User{
            Id:        u.ID,
            Uuid:      u.UUID,
            Name:      u.Name,
            Email:     u.Email,
            Status:    u.Status,
            UpdatedAt: timestamppb.New(u.UpdatedAt),
        }
    }

    return service.UpdateEntity[User, *pb.UpdateUserRequest](
        ctx,
        req,
        req.Id,
        "",  // UUID (empty if using ID)
        req.UpdateMask,
        convertFunc,
        toProtoFunc,
        s.deps.Tracing,
        s.deps.Metrics,
        func(ctx context.Context, id int64) (*User, error) {
            return repo.GetByID[User](ctx, s.deps, "users", id)
        },
        nil, // GetByUUID
        func(ctx context.Context, updateReq helpers_utils.UpdateRequest[User]) (*User, error) {
            return repo.UpdateEntity[User](ctx, s.deps, updateReq)
        },
    )
}

// DeleteUser soft-deletes a user
func (s *UserService) DeleteUser(
    ctx context.Context,
    req *pb.DeleteUserRequest,
) (*pbr.OperationResponse, error) {
    return service.DeleteEntity(
        ctx,
        s.deps,
        req.Id,
        req.Uuid,
        "User",
        s.deps.Tracing,
        s.deps.Metrics,
        func(ctx context.Context, id int64) (*User, error) {
            return repo.GetByID[User](ctx, s.deps, "users", id)
        },
        func(ctx context.Context, uuid string) (*User, error) {
            return repo.GetByUUID[User](ctx, s.deps, "users", uuid)
        },
        func(ctx context.Context, id int64, deletedBy string) error {
            _, err := repo.DeleteEntity[User](ctx, s.deps, "users", id, deletedBy)
            return err
        },
    )
}
```

---

## Key Benefits

### 1. **Type Safety with Generics**
```go
// Compile-time type checking
users, err := repo.ListEntity[User](...)  // Returns []*User
products, err := repo.ListEntity[Product](...)  // Returns []*Product
```

### 2. **Zero Boilerplate**
```go
// All this is automatic:
// - Timeout application
// - Security context extraction
// - Metrics recording
// - Distributed tracing
// - Error handling
// - Logging

user, err := repo.GetByID[User](ctx, deps, "users", 42)
// That's it!
```

### 3. **Protobuf-First Design**
```go
// FieldMask support
req.FieldMask: {paths: ["name", "email"]}
// → SELECT name, email FROM users

// Filter operations
req.Filters: {
  name: {contains: "john"},
  status: {eq: "active"},
  created_at: {gte: "2025-01-01"}
}
// → WHERE name ILIKE '%john%' AND status = 'active' AND created_at >= '2025-01-01'
```

### 4. **SQL Injection Prevention**
```go
// All values are parameterized
WHERE name = $1 AND status = $2  // ✅ Safe

// Never:
WHERE name = '" + input + "'     // ❌ Dangerous
```

### 5. **Multi-Tenancy by Default**
```go
// Tenant isolation is automatic when context has tenant info
// No need to manually add tenant_id to every query
```

### 6. **Complete Observability**
- **Metrics**: Query duration, success/failure rates
- **Tracing**: Automatic spans for all operations
- **Logging**: Structured logs with context
- **Timeouts**: Automatic application based on config

---

## Comparison with Other Approaches

| Feature | Kosha Builder | SQLC | Raw SQL | ORM (GORM) |
|---------|--------------|------|---------|------------|
| **Dynamic field selection** | ✅ | ❌ | ✅ | ⚠️ |
| **Dynamic filters** | ✅ | ❌ | ✅ | ✅ |
| **Protobuf FieldMask** | ✅ | ❌ | ❌ | ❌ |
| **Type safety** | ✅ (generics) | ✅ | ❌ | ✅ |
| **Compile-time validation** | ⚠️ | ✅ | ❌ | ❌ |
| **SQL injection prevention** | ✅ | ✅ | ⚠️ | ✅ |
| **Observability built-in** | ✅ | ❌ | ❌ | ❌ |
| **Multi-tenancy support** | ✅ | ❌ | ❌ | ⚠️ |
| **Transaction support** | ✅ | ✅ | ✅ | ✅ |
| **Learning curve** | Low | Medium | Low | High |
| **Maintenance** | Single builder | Many .sql files | Scattered | Complex configs |

---

## Next Steps

See [todo.md](todo.md) for planned enhancements:
- **TSK-021**: Schema-based column validation (catch typos at runtime)
- **TSK-022**: Optional SQL query logging for debugging
- **TSK-023**: Per-table query performance metrics

---

*Last Updated: 2025-12-01*
*For architecture details, see [database/pgxpostgres/doc.go](database/pgxpostgres/doc.go)*
