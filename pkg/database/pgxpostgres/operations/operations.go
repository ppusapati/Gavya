// Package operations provides high-level database operations with connection pooling,
// retry logic, and transaction management.
//
// This package orchestrates query execution using the builder package for SQL generation
// and pgxpool for connection management. It provides:
//   - ExecuteQuery[T] - Execute query returning single result
//   - ExecuteQuerySlice[T] - Execute query returning multiple results
//   - WithTransaction - Transaction wrapper with automatic rollback
//   - Retry logic with exponential backoff for transient failures
//   - Query metrics and logging
//
// Example usage:
//
//	result, err := operations.ExecuteQuery[User](
//	    ctx, pool, &dataModel,
//	    operations.WithQueryType(operations.QueryTypeSelect),
//	)
//
// See also: builder package for query construction, uow package for advanced transactions
package operations

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"p9e.in/samavaya/packages/database/pgxpostgres/builder"
	"p9e.in/samavaya/packages/database/pgxpostgres/retry"
	"p9e.in/samavaya/packages/models"
)

// QueryType defines the type of database operation
type QueryType string

const (
	QueryTypeSelect     QueryType = "SELECT"
	QueryTypeInsert     QueryType = "INSERT"
	QueryTypeUpdate     QueryType = "UPDATE"
	QueryTypeDelete     QueryType = "DELETE"
	QueryTypeBulkInsert QueryType = "BULK_INSERT"
	QueryTypeCount      QueryType = "COUNT"
	QueryTypeUpsert     QueryType = "UPSERT"
)

// QueryOptions provides configurable options for database operations
type QueryOptions struct {
	// Operation name for logging and metrics
	OperationName string
	// Enables retry mechanism
	EnableRetry bool

	// Maximum number of retry attempts
	MaxRetries uint64

	// Base retry delay
	BaseRetryDelay time.Duration

	// Query type for more specific logging and error handling
	QueryType         QueryType
	PreparedStatement bool
	CacheEnabled      bool
	BatchSize         int
	Timeout           time.Duration
}

// DefaultQueryOptions provides a set of default options
func DefaultQueryOptions() QueryOptions {
	return QueryOptions{
		OperationName:  "default_db_operation",
		EnableRetry:    true,
		MaxRetries:     3,
		BaseRetryDelay: 100 * time.Millisecond,
	}
}

// Migrator creates or migrates database using a SQL script
// It reads the script, splits it into queries, and executes them within a transaction
func Migrator(ctx context.Context, db *pgxpool.Pool, scriptName string) error {
	// Read the migration script
	file, err := os.ReadFile(scriptName)
	if err != nil {
		return fmt.Errorf("failed to read migration script %s: %w", scriptName, err)
	}

	// Begin a transaction to ensure atomic migration
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for migration: %w", err)
	}
	defer func() {
		if err != nil {
			// Rollback on any error
			if rbErr := tx.Rollback(ctx); rbErr != nil {
				log.Printf("Error rolling back migration transaction: %v", rbErr)
			}
		} else {
			// Commit if no errors
			if cmErr := tx.Commit(ctx); cmErr != nil {
				log.Printf("Error committing migration transaction: %v", cmErr)
				err = cmErr
			}
		}
	}()

	// Split queries and execute
	requests := strings.Split(string(file), ";")
	for i, query := range requests {
		query = strings.TrimSpace(query)
		if query == "" {
			continue
		}

		// Execute the query directly on the transaction
		_, err = tx.Exec(ctx, query)
		if err != nil {
			return fmt.Errorf("migration failed at query %d: %w\nQuery: %s", i+1, err, query)
		}

		// Log successful query execution
		log.Printf("Successfully executed migration query %d", i+1)
	}

	return nil
}

// ExecuteQuery executes a database query with flexible options
func ExecuteQuery[T any](
	ctx context.Context,
	db interface{},
	model *models.DataModel[T],
	queryType QueryType,
	opts ...func(*QueryOptions),
) (T, error) {
	if model == nil {
		var result T
		return result, fmt.Errorf("data model is nil")
	}

	// Log the incoming data model

	var query string
	var args []T
	var err error

	switch queryType {
	case QueryTypeSelect:
		query, args, err = builder.SelectQuery(ctx, *model)
	case QueryTypeInsert:
		query, args, err = builder.InsertQuery(ctx, *model)
	case QueryTypeUpdate:
		query, args, err = builder.UpdateQuery(ctx, *model)
	case QueryTypeDelete:
		query, args, err = builder.DeleteQuery(ctx, *model)
	case QueryTypeBulkInsert:
		var bulkArgs [][]T
		query, bulkArgs, err = builder.BulkInsertQuery(*model)
		for _, arg := range bulkArgs {
			args = append(args, arg...)
		}
	case QueryTypeCount:
		query, args, err = builder.CountQuery(ctx, *model)
	case QueryTypeUpsert:
		query, args, err = builder.UpsertQuery(*model)
	default:
		var result T
		return result, fmt.Errorf("unsupported query type: %s", queryType)
	}

	if err != nil {
		var result T
		return result, fmt.Errorf("failed to build query: %w", err)
	}

	// Log the built query and args

	// Check if args are empty when Where clause exists
	if model.Where != "" && len(args) == 0 {
		var result T
		return result, fmt.Errorf("where clause exists but no arguments provided")
	}

	// Apply default and custom options
	options := DefaultQueryOptions()
	for _, opt := range opts {
		opt(&options)
	}
	var result T
	var retryErr error

	// Retry mechanism
	if options.EnableRetry {
		retryErr = retry.WithRetry(ctx, func(ctx context.Context) error {
			// Execute query based on the type of database connection
			var execErr error
			switch conn := db.(type) {
			case *pgxpool.Pool:
				result, execErr = executePoolQuery[T](ctx, conn, query, args)
			case pgx.Tx:
				result, execErr = executeTxQuery[T](ctx, conn, query, args)
			default:
				return fmt.Errorf("unsupported database connection type")
			}

			return execErr
		}, options.MaxRetries)
	} else {
		// Execute query based on the type of database connection
		if err := ctx.Err(); err != nil {
			var result T
			return result, fmt.Errorf("context error before query execution: %w", err)
		}
		switch conn := db.(type) {
		case *pgxpool.Pool:
			result, err = executePoolQuery[T](ctx, conn, query, args)
		case pgx.Tx:
			result, err = executeTxQuery[T](ctx, conn, query, args)
		default:
			err = fmt.Errorf("unsupported database type")
		}
	}

	if retryErr != nil {
		return result, retryErr
	}

	return result, err
}

// executePoolQuery executes a query on a database pool
func executePoolQuery[T any](ctx context.Context, pool *pgxpool.Pool, query string, args []T) (T, error) {
	query = strings.Replace(query, `"`, "", -1)
	// Early context check
	if err := ctx.Err(); err != nil {
		var result T
		return result, fmt.Errorf("context error before query execution: %w", err)
	}

	// Check pool health
	if err := pool.Ping(ctx); err != nil {
		var result T
		return result, fmt.Errorf("database connection check failed: %w", err)
	}

	var result T
	switch any(result).(type) {
	case pgx.Rows:

		rows, err := pool.Query(ctx, query, TtoInterfaceArgs(args)...)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return result, fmt.Errorf("context cancelled during query: %w", ctxErr)
			}
			return result, fmt.Errorf("query error: %w", err)
		}
		defer rows.Close()
		return any(rows).(T), nil

	case pgx.Row:

		row := pool.QueryRow(ctx, query, TtoInterfaceArgs(args)...)
		err := row.Scan(any(&result).(interface{}))
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return result, fmt.Errorf("context cancelled during row scan: %w", ctxErr)
			}
			return result, fmt.Errorf("row scan error: %w", err)
		}
		result = any(row).(T)

	case pgconn.CommandTag:

		tag, err := pool.Exec(ctx, query, TtoInterfaceArgs(args)...)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return result, fmt.Errorf("context cancelled during exec: %w", ctxErr)
			}
			return result, fmt.Errorf("exec error: %w", err)
		}
		result = any(tag).(T)
	case int, *uint8: // <--- NEW CASE
		var count int
		err := pool.QueryRow(ctx, query, TtoInterfaceArgs(args)...).Scan(&count)
		if err != nil {

			return result, fmt.Errorf("count query error: %w", err)
		}
		return any(count).(T), nil
	default:
		rows, err := pool.Query(ctx, query, TtoInterfaceArgs(args)...)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return result, fmt.Errorf("context cancelled during query: %w", ctxErr)
			}
			return result, fmt.Errorf("query error: %w", err)
		}
		defer rows.Close()

		// Use a new context with timeout for row collection
		collectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		collectedRows, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[T])
		if err != nil {
			if ctxErr := collectCtx.Err(); ctxErr != nil {
				return result, fmt.Errorf("context cancelled during row collection: %w", ctxErr)
			}
			return result, fmt.Errorf("row collection error: %w", err)
		}

		if len(collectedRows) > 0 {
			result = collectedRows[0]
		}
	}

	// Final context check
	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("context cancelled after query completion: %w", err)
	}

	return result, nil
}

// executeTxQuery executes a query on a database transaction
func executeTxQuery[T any](ctx context.Context, tx pgx.Tx, query string, args []T) (T, error) {
	var result T
	switch any(result).(type) {
	case pgx.Rows:
		rows, err := tx.Query(ctx, query, TtoInterfaceArgs(args)...)
		if err != nil {
			return result, err
		}
		defer rows.Close()
		result = any(rows).(T)
	case pgx.Row:
		row := tx.QueryRow(ctx, query, TtoInterfaceArgs(args)...)
		err := row.Scan(any(&result).(interface{}))
		if err != nil {
			return result, err
		}
		result = any(row).(T)
	case pgconn.CommandTag:
		tag, err := tx.Exec(ctx, query, TtoInterfaceArgs(args)...)
		if err != nil {
			return result, err
		}
		result = any(tag).(T)
	default:
		rows, err := tx.Query(ctx, query, TtoInterfaceArgs(args)...)
		if err != nil {
			return result, err
		}
		defer rows.Close()
		collectedRows, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[T])
		if err != nil {
			return result, err
		}
		if len(collectedRows) > 0 {
			result = collectedRows[0]
		} else {
			result = *new(T) // or provide a default value based on your requirements
		}
	}
	return result, nil
}

func ExecuteQuerySlice[T any](
	ctx context.Context,
	db interface{},
	model *models.DataModel[T],
	queryType QueryType,
	opts ...func(*QueryOptions),
) ([]T, error) {
	if model == nil {
		var result []T
		return result, fmt.Errorf("data model is nil")
	}

	// Log the incoming data model
	var query string
	var arg []T
	var err error

	switch queryType {
	case QueryTypeSelect:
		query, arg, err = builder.SelectQuery(ctx, *model)
	case QueryTypeInsert:
		query, arg, err = builder.InsertQuery(ctx, *model)
	case QueryTypeUpdate:
		query, arg, err = builder.UpdateQuery(ctx, *model)
	case QueryTypeDelete:
		query, arg, err = builder.DeleteQuery(ctx, *model)
	case QueryTypeBulkInsert:
		var bulkArgs [][]T
		query, bulkArgs, err = builder.BulkInsertQuery(*model)
		for _, a := range bulkArgs {
			arg = append(arg, a...)
		}
	case QueryTypeCount:
		query, arg, err = builder.CountQuery(ctx, *model)
	case QueryTypeUpsert:
		query, arg, err = builder.UpsertQuery(*model)
	default:
		var result []T
		return result, fmt.Errorf("unsupported query type: %s", queryType)
	}

	if err != nil {
		var result []T
		return result, fmt.Errorf("failed to build query: %w", err)
	}

	// Check if args are empty when Where clause exists
	if model.Where != "" && len(arg) == 0 {
		var result []T
		return result, fmt.Errorf("where clause exists but no arguments provided")
	}

	// Apply default and custom options
	options := DefaultQueryOptions()
	for _, opt := range opts {
		opt(&options)
	}
	var result []T
	var retryErr error

	// Retry mechanism
	if options.EnableRetry {
		retryErr = retry.WithRetry(ctx, func(ctx context.Context) error {
			// Execute query based on the type of database connection
			var execErr error
			switch conn := db.(type) {
			case *pgxpool.Pool:
				result, execErr = executePoolQuerySlice[T](ctx, conn, query, arg)
			case pgx.Tx:
				result, execErr = executeTxQuerySlice[T](ctx, conn, query, arg)
			default:
				return fmt.Errorf("unsupported database connection type")
			}

			return execErr
		}, options.MaxRetries)
	} else {
		// Execute query based on the type of database connection
		switch conn := db.(type) {
		case *pgxpool.Pool:
			result, err = executePoolQuerySlice[T](ctx, conn, query, arg)
		case pgx.Tx:
			result, err = executeTxQuerySlice[T](ctx, conn, query, arg)
		default:
			err = fmt.Errorf("unsupported database type")
		}
	}

	if retryErr != nil {
		return result, retryErr
	}

	return result, err

}

func executePoolQuerySlice[T any](ctx context.Context, pool *pgxpool.Pool, query string, args []T) ([]T, error) {
	query = strings.Replace(query, `"`, "", -1)
	rows, err := pool.Query(ctx, query, TtoInterfaceArgs(args)...)
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}
	defer rows.Close()

	collectedRows, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[T])
	if err != nil {
		return nil, fmt.Errorf("row collection error: %w", err)
	}
	return collectedRows, nil
}

// executeTxQuery executes a query on a database transaction
func executeTxQuerySlice[T any](ctx context.Context, tx pgx.Tx, query string, args []T) ([]T, error) {
	var result []T
	switch any(result).(type) {
	case pgx.Rows:
		rows, err := tx.Query(ctx, query, TtoInterfaceArgs(args)...)
		if err != nil {
			return result, err
		}
		defer rows.Close()
		result = any(rows).([]T)
	case pgx.Row:
		row := tx.QueryRow(ctx, query, TtoInterfaceArgs(args)...)
		err := row.Scan(any(&result).(interface{}))
		if err != nil {
			return result, err
		}
		result = any(row).([]T)
	case pgconn.CommandTag:
		tag, err := tx.Exec(ctx, query, TtoInterfaceArgs(args)...)
		if err != nil {
			return result, err
		}
		result = any(tag).([]T)
	default:
		rows, err := tx.Query(ctx, query, TtoInterfaceArgs(args)...)
		if err != nil {
			return result, err
		}
		defer rows.Close()
		collectedRows, err := pgx.CollectRows(rows, pgx.RowToStructByNameLax[T])
		if err != nil {
			return result, err
		}
		if len(collectedRows) > 0 {
			result = collectedRows
		} else {
			result = *new([]T) // or provide a default value based on your requirements
		}
	}
	return result, nil
}

// WithTransaction wraps a transaction function with optional configurations
func WithTransaction(ctx context.Context, fn func(tx pgx.Tx) error, opts ...func(*QueryOptions)) error {
	// Apply default and custom options
	options := DefaultQueryOptions()
	for _, opt := range opts {
		opt(&options)
	}

	// Get database pool from context
	pool, ok := ctx.Value("dbPool").(*pgxpool.Pool)
	if !ok {
		return fmt.Errorf("database pool not found in context")
	}

	// Start transaction
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("transaction start failed: %w", err)
	}
	defer func() {
		if err != nil {
			// Rollback on any error
			if rbErr := tx.Rollback(ctx); rbErr != nil {
				log.Printf("Error rolling back migration transaction: %v", rbErr)
			}
		} else {
			// Commit if no errors
			if cmErr := tx.Commit(ctx); cmErr != nil {
				log.Printf("Error committing migration transaction: %v", cmErr)
				err = cmErr
			}
		}
	}()

	// Execute transaction function
	err = fn(tx)
	if err != nil {
		return fmt.Errorf("transaction execution failed: %w", err)
	}
	return nil
}

func EnableRetry(enabled bool) func(*QueryOptions) {
	return func(opts *QueryOptions) {
		opts.EnableRetry = enabled
	}
}

func WithQueryType(queryType QueryType) func(*QueryOptions) {
	return func(opts *QueryOptions) {
		opts.OperationName = string(queryType)
	}
}

func TtoInterfaceArgs[T any](args []T) []interface{} {
	interfaceArgs := make([]interface{}, len(args))
	for i, v := range args {
		interfaceArgs[i] = v
	}
	return interfaceArgs
}
