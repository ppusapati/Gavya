// Package pgxpostgres provides PostgreSQL database access using pgx/v5.
//
// # Architecture
//
// This package is organized into specialized subpackages with clear responsibilities:
//
//	pgxpostgres/          - Core connection management and health checks
//	├── builder/          - SQL query construction from DataModel[T]
//	├── filter/           - Complex WHERE clause filters (protobuf operations)
//	├── operations/       - Query execution with retry logic and pooling
//	├── validator/        - SQL injection prevention and security validation
//	├── retry/            - Exponential backoff retry logic for transient failures
//	└── tenantDB/         - Multi-tenant database pool management
//
// # Query Flow
//
// The typical query flow follows this pattern:
//
//	1. Application creates models.DataModel[T] with table name and conditions
//	2. builder package generates parameterized SQL query
//	3. validator package checks query for SQL injection
//	4. operations package executes query via pgxpool with retry logic
//	5. Results are scanned into generic type T
//
// Example:
//
//	// Create data model
//	dm := models.DataModel[User]{
//	    TableName: "users",
//	    Where:     "status = $1",
//	    WhereArgs: []any{"active"},
//	}
//
//	// Execute query
//	result, err := operations.ExecuteQuery[User](
//	    ctx, pool, &dm,
//	    operations.WithQueryType(operations.QueryTypeSelect),
//	)
//
// # Multi-Tenancy
//
// The package supports both shared and tenant-specific database pools:
//
//   - Shared pool: Default pool used by all tenants
//   - Independent pools: Created dynamically per tenant via tenantDB package
//   - Pool routing: Determined by middleware/dbmiddleware based on tenant config
//
// # Security
//
// All queries are validated for SQL injection via the validator package.
// Security context is extracted from request context (p9context.GetSecurityContext)
// rather than hardcoded values.
//
// # Transaction Support
//
// Two transaction patterns are supported:
//
//  1. operations.WithTransaction - Simple transaction wrapper
//  2. uow.WithTransaction - Advanced Unit of Work pattern
//
// Example transaction:
//
//	err := uow.WithTransaction(ctx, pool, func(uow uow.UnitOfWork) error {
//	    _, err := uow.Tx().Exec(ctx, "INSERT INTO users...")
//	    if err != nil {
//	        return err // Auto-rollback
//	    }
//	    _, err = uow.Tx().Exec(ctx, "INSERT INTO profiles...")
//	    return err // Auto-commit if nil
//	})
//
// # Package Dependencies
//
//	builder     → filter, validator, models
//	operations  → builder, retry, pgxpool
//	filter      → api/v1/query (protobuf definitions)
//	validator   → (no internal dependencies)
//
// See individual package documentation for detailed API information.
package pgxpostgres
