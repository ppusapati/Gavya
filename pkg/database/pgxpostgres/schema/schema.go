// Package schema provides runtime schema validation for database operations.
//
// This package implements TypedQuery[T] with column metadata to catch typos
// and invalid field names at runtime, providing compile-time-like safety for
// dynamic query construction.
//
// Key Features:
//   - Schema registry with table and column metadata
//   - Runtime column validation with helpful error messages
//   - Support for aliased columns and computed fields
//   - Thread-safe schema registration and lookup
//   - Zero performance impact on valid queries
//
// Example usage:
//
//	schema := schema.NewTableSchema("users", []string{"id", "name", "email", "created_at"})
//	schema.RegisterAlias("user_id", "id")
//
//	// Register globally
//	schema.RegisterSchema("users", schema)
//
//	// Validate fields
//	err := schema.ValidateColumns([]string{"id", "name", "invalid_field"})
//	// Returns: invalid column 'invalid_field' for table 'users'. Valid columns: id, name, email, created_at
//
// See also: validator package for SQL injection protection, builder package for query construction
package schema

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"p9e.in/samavaya/packages/database/pgxpostgres/validator"
)

func init() {
	// Register schema validator with validator package
	validator.RegisterSchemaValidator(ValidateTableColumns)
}

// TableSchema holds metadata about a database table's structure.
// It provides fast column validation and supports aliased columns.
type TableSchema struct {
	TableName    string
	ValidColumns map[string]bool // Column name -> exists
	Aliases      map[string]string // Alias -> actual column name
	mu           sync.RWMutex
}

// SchemaRegistry is a global registry of table schemas.
// It provides thread-safe access to table metadata for validation.
var (
	globalRegistry = &SchemaRegistry{
		schemas: make(map[string]*TableSchema),
	}
)

// SchemaRegistry holds all registered table schemas
type SchemaRegistry struct {
	schemas map[string]*TableSchema
	mu      sync.RWMutex
}

// NewTableSchema creates a new table schema with the given table name and columns.
//
// Example:
//
//	schema := NewTableSchema("users", []string{"id", "name", "email", "created_at"})
func NewTableSchema(tableName string, columns []string) *TableSchema {
	validColumns := make(map[string]bool, len(columns))
	for _, col := range columns {
		validColumns[col] = true
	}

	return &TableSchema{
		TableName:    tableName,
		ValidColumns: validColumns,
		Aliases:      make(map[string]string),
	}
}

// RegisterAlias adds an alias for a column.
// This is useful for backward compatibility or alternate naming conventions.
//
// Example:
//
//	schema.RegisterAlias("user_id", "id")
//	schema.RegisterAlias("full_name", "name")
func (ts *TableSchema) RegisterAlias(alias, actualColumn string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Verify the actual column exists
	if !ts.ValidColumns[actualColumn] {
		return fmt.Errorf("cannot create alias '%s': column '%s' does not exist in table '%s'",
			alias, actualColumn, ts.TableName)
	}

	ts.Aliases[alias] = actualColumn
	return nil
}

// ResolveColumn resolves an alias to its actual column name.
// If the column is not an alias, it returns the original name.
func (ts *TableSchema) ResolveColumn(column string) string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	if actualCol, ok := ts.Aliases[column]; ok {
		return actualCol
	}
	return column
}

// ValidateColumns validates that all provided columns exist in the table schema.
// It returns a detailed error with suggestions if any column is invalid.
//
// Returns:
//   - nil if all columns are valid
//   - error with invalid columns and suggestions if validation fails
//
// Example:
//
//	err := schema.ValidateColumns([]string{"id", "name", "invalid"})
//	// Returns: invalid column 'invalid' for table 'users'. Valid columns: created_at, email, id, name
func (ts *TableSchema) ValidateColumns(columns []string) error {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	invalidCols := make([]string, 0)

	for _, col := range columns {
		// Skip wildcard
		if col == "*" {
			continue
		}

		// Check if column exists (either directly or as alias)
		resolvedCol := ts.ResolveColumn(col)
		if !ts.ValidColumns[resolvedCol] {
			invalidCols = append(invalidCols, col)
		}
	}

	if len(invalidCols) > 0 {
		validColsList := ts.GetValidColumnsList()
		return fmt.Errorf("invalid column(s) '%s' for table '%s'. Valid columns: %s",
			strings.Join(invalidCols, ", "),
			ts.TableName,
			strings.Join(validColsList, ", "))
	}

	return nil
}

// GetValidColumnsList returns a sorted list of valid column names for display purposes.
func (ts *TableSchema) GetValidColumnsList() []string {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	columns := make([]string, 0, len(ts.ValidColumns))
	for col := range ts.ValidColumns {
		columns = append(columns, col)
	}
	sort.Strings(columns)
	return columns
}

// AddColumn dynamically adds a column to the schema.
// This is useful for computed fields or dynamic schema updates.
func (ts *TableSchema) AddColumn(column string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.ValidColumns[column] = true
}

// RemoveColumn removes a column from the schema.
func (ts *TableSchema) RemoveColumn(column string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	delete(ts.ValidColumns, column)

	// Remove any aliases pointing to this column
	for alias, actualCol := range ts.Aliases {
		if actualCol == column {
			delete(ts.Aliases, alias)
		}
	}
}

// HasColumn checks if a column exists in the schema (including aliases).
func (ts *TableSchema) HasColumn(column string) bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	if column == "*" {
		return true
	}

	resolvedCol := ts.ResolveColumn(column)
	return ts.ValidColumns[resolvedCol]
}

// RegisterSchema registers a table schema in the global registry.
//
// Example:
//
//	schema := NewTableSchema("users", []string{"id", "name", "email"})
//	RegisterSchema("users", schema)
func RegisterSchema(tableName string, schema *TableSchema) {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	globalRegistry.schemas[tableName] = schema
}

// GetSchema retrieves a table schema from the global registry.
// Returns nil if the schema is not found.
func GetSchema(tableName string) *TableSchema {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()
	return globalRegistry.schemas[tableName]
}

// ValidateTableColumns validates columns against a registered table schema.
// If the schema is not registered, it skips validation (graceful degradation).
//
// This is the primary function used by the query builder for validation.
//
// Example:
//
//	err := ValidateTableColumns("users", []string{"id", "name", "invalid"})
func ValidateTableColumns(tableName string, columns []string) error {
	schema := GetSchema(tableName)
	if schema == nil {
		// Schema not registered - skip validation (graceful degradation)
		return nil
	}

	return schema.ValidateColumns(columns)
}

// ClearRegistry clears all registered schemas.
// This is primarily used for testing.
func ClearRegistry() {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	globalRegistry.schemas = make(map[string]*TableSchema)
}

// ListRegisteredTables returns a sorted list of all registered table names.
func ListRegisteredTables() []string {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	tables := make([]string, 0, len(globalRegistry.schemas))
	for table := range globalRegistry.schemas {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	return tables
}
