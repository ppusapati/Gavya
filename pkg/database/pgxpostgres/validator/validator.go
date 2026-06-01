package validator

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"p9e.in/samavaya/packages/models"
)

// Security context structure
type SecurityContext struct {
	Timestamp time.Time
	Username  string
}

// Validation errors
var (
	ErrInvalidQuery      = errors.New("invalid query")
	ErrUnsafeCharacters  = errors.New("query contains unsafe characters")
	ErrEmptyIdentifier   = errors.New("empty identifier")
	ErrInvalidIdentifier = errors.New("invalid identifier character")
	ErrEmptyTableName    = errors.New("empty table name")
	ErrInvalidTableName  = errors.New("invalid characters in table name")
	ErrEmptyFieldName    = errors.New("empty field name")
	ErrInvalidFieldName  = errors.New("invalid characters in field name")
	ErrMaxLengthExceeded = errors.New("maximum length exceeded")
)

// Validation constants
const (
	MaxQueryLength      = 4096
	MaxIdentifierLength = 63 // PostgreSQL's maximum identifier length
	MaxWhereLength      = 1000
)

// Regular expressions for validation
var (
	validIdentifierRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]*$`)
	sqlKeywordsRegex     = regexp.MustCompile(`(?i)\b(SELECT|INSERT|UPDATE|DELETE|DROP|TRUNCATE|ALTER|UNION|EXEC|EXECUTE)\b`)
	unsafePatterns       = []string{
		"--",                 // SQL comment
		"/*",                 // Multi-line comment start
		"*/",                 // Multi-line comment end
		"xp_",                // Extended stored procedures
		"exec",               // Execute statement
		"execute",            // Execute statement
		"sp_",                // Stored procedure
		"sysobjects",         // System objects
		"syscolumns",         // System columns
		"waitfor",            // Time delay
		"benchmark",          // Performance testing
		"sleep",              // Time delay
		"information_schema", // System schema
		"@@",                 // System variables
	}
)

// NewSecurityContext creates a new security context with current timestamp and username
func NewSecurityContext(username string) *SecurityContext {
	return &SecurityContext{
		Timestamp: time.Now().UTC(),
		Username:  username,
	}
}

// ValidateQuery checks for common SQL injection patterns with security context
func ValidateQuery(query string, ctx *SecurityContext) error {
	// Check query length
	if len(query) > MaxQueryLength {
		return fmt.Errorf("%w: query length %d exceeds maximum %d",
			ErrMaxLengthExceeded, len(query), MaxQueryLength)
	}

	query = strings.ToLower(query)

	// Check for multiple statements
	if strings.Contains(query, ";") {
		return fmt.Errorf("%w: multiple statements detected", ErrInvalidQuery)
	}

	// Check for SQL keywords in unexpected places
	if sqlKeywordsRegex.MatchString(query) {
		return fmt.Errorf("%w: unauthorized SQL keywords detected", ErrUnsafeCharacters)
	}

	// Check for unsafe patterns
	for _, pattern := range unsafePatterns {
		if strings.Contains(query, pattern) {
			return fmt.Errorf("%w: unsafe pattern detected: %s", ErrUnsafeCharacters, pattern)
		}
	}

	return nil
}

// isBalancedParentheses checks if the parentheses in the string are balanced
func isBalancedParentheses(s string) bool {
	count := 0
	for _, char := range s {
		switch char {
		case '(':
			count++
		case ')':
			count--
			if count < 0 {
				return false
			}
		}
	}
	return count == 0
}

// ValidateIdentifier checks if an identifier is safe to use with security context
func ValidateIdentifier(identifier string, ctx *SecurityContext) error {
	// Check identifier length
	if len(identifier) > MaxIdentifierLength {
		return fmt.Errorf("%w: identifier length %d exceeds maximum %d",
			ErrMaxLengthExceeded, len(identifier), MaxIdentifierLength)
	}

	// Check for empty identifier
	if identifier = strings.TrimSpace(identifier); identifier == "" {
		return ErrEmptyIdentifier
	}

	// Validate identifier format
	if !validIdentifierRegex.MatchString(identifier) {
		return fmt.Errorf("%w: identifier must start with a letter and contain only letters, numbers, and underscores",
			ErrInvalidIdentifier)
	}

	return nil
}

// ValidateTableName validates table name with security context
func ValidateTableName(tableName string, ctx *SecurityContext) error {
	if tableName == "" {
		return ErrEmptyTableName
	}

	if strings.ContainsAny(tableName, "();\"'") {
		return ErrInvalidTableName
	}

	return ValidateIdentifier(tableName, ctx)
}

// ValidateFieldNames validates field names with security context
func ValidateFieldNames(fields []string, ctx *SecurityContext) error {
	for _, field := range fields {
		if field == "" {
			return ErrEmptyFieldName
		}

		if strings.ContainsAny(field, "();\"'") {
			return ErrInvalidFieldName
		}

		if err := ValidateIdentifier(field, ctx); err != nil {
			return fmt.Errorf("invalid field '%s': %w", field, err)
		}
	}

	return nil
}

// ValidateWhereCondition validates WHERE clause with security context
func ValidateWhereCondition(where string, ctx *SecurityContext) error {
	if where == "" {
		return nil
	}

	// Trim whitespace
	where = strings.TrimSpace(where)

	// Check length
	if len(where) > MaxWhereLength {
		return fmt.Errorf("%w: where condition length %d exceeds maximum %d",
			ErrMaxLengthExceeded, len(where), MaxWhereLength)
	}

	// Validate query
	if err := ValidateQuery(where, ctx); err != nil {
		return fmt.Errorf("invalid where condition: %w", err)
	}

	// Check for balanced parentheses
	if !isBalancedParentheses(where) {
		return fmt.Errorf("unbalanced parentheses in where condition")
	}

	return nil
}

// ValidateOrderBy validates ORDER BY clause with security context
func ValidateOrderBy(orderBy string, ctx *SecurityContext) error {
	if orderBy == "" {
		return nil
	}

	columns := strings.Split(orderBy, ",")
	for _, col := range columns {
		col = strings.TrimSpace(col)
		if strings.HasPrefix(col, "-") || strings.HasPrefix(col, "+") {
			col = col[1:]
		}
		if err := ValidateIdentifier(col, ctx); err != nil {
			return fmt.Errorf("invalid order by column '%s': %w", col, err)
		}
	}

	return nil
}

// ValidateGroupBy validates GROUP BY clause with security context
func ValidateGroupBy(groupBy string, ctx *SecurityContext) error {
	if groupBy == "" {
		return nil
	}

	columns := strings.Split(groupBy, ",")
	for _, col := range columns {
		if err := ValidateIdentifier(strings.TrimSpace(col), ctx); err != nil {
			return fmt.Errorf("invalid group by column '%s': %w", col, err)
		}
	}

	return nil
}

// ValidateFieldMask validates that field mask paths exist in the table schema.
// This provides runtime column validation similar to compile-time type safety.
//
// If a schema is not registered for the table, validation is skipped (graceful degradation).
//
// Example:
//
//	err := ValidateFieldMask("users", []string{"id", "name", "invalid"})
//	// Returns: invalid column 'invalid' for table 'users'. Valid columns: created_at, email, id, name
func ValidateFieldMask(tableName string, fieldPaths []string, ctx *SecurityContext) error {
	// First validate that field names are safe identifiers
	if err := ValidateFieldNames(fieldPaths, ctx); err != nil {
		return fmt.Errorf("invalid field mask: %w", err)
	}

	// Import schema package validation (note: this will be imported at package level)
	// For now, we'll add a hook point that the schema package can use
	if schemaValidator != nil {
		if err := schemaValidator(tableName, fieldPaths); err != nil {
			return fmt.Errorf("field mask validation failed: %w", err)
		}
	}

	return nil
}

// SchemaValidatorFunc is a hook for schema-based validation
// This allows the schema package to register its validator without circular dependency
type SchemaValidatorFunc func(tableName string, columns []string) error

var schemaValidator SchemaValidatorFunc

// RegisterSchemaValidator registers a schema validation function.
// This is called by the schema package during initialization.
func RegisterSchemaValidator(validator SchemaValidatorFunc) {
	schemaValidator = validator
}

// ValidateDataModel validates the entire data model with security context
func ValidateDataModel[T any](dm models.DataModel[T], ctx *SecurityContext) error {
	if err := ValidateTableName(dm.TableName, ctx); err != nil {
		return fmt.Errorf("invalid table name: %w", err)
	}

	if err := ValidateFieldNames(dm.FieldNames, ctx); err != nil {
		return fmt.Errorf("invalid field names: %w", err)
	}

	// Schema-based field validation (US-003)
	if len(dm.FieldNames) > 0 {
		if err := ValidateFieldMask(dm.TableName, dm.FieldNames, ctx); err != nil {
			return fmt.Errorf("invalid field names: %w", err)
		}
	}

	// Validate WHERE condition if present
	if dm.Where != "" {
		if err := ValidateWhereCondition(dm.Where, ctx); err != nil {
			return fmt.Errorf("invalid where condition: %w", err)
		}
	}

	// Validate ORDER BY if present
	if dm.OrderBy != "" {
		if err := ValidateOrderBy(dm.OrderBy, ctx); err != nil {
			return fmt.Errorf("invalid order by clause: %w", err)
		}
	}

	// Validate GROUP BY if present
	if dm.GroupBy != "" {
		if err := ValidateGroupBy(dm.GroupBy, ctx); err != nil {
			return fmt.Errorf("invalid group by clause: %w", err)
		}
	}

	return nil
}
