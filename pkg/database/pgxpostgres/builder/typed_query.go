package builder

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"p9e.in/samavaya/packages/models"
)

// TypedQuery provides schema-based column validation for dynamic queries.
// It uses reflection to extract valid column names from the entity type
// and validates FieldMask paths and dynamic field selections at runtime.
type TypedQuery[T any] struct {
	tableName string
	columns   []string            // All valid columns (sorted for error messages)
	validCols map[string]bool     // Fast lookup for validation
	mu        sync.RWMutex        // Thread-safe validation
}

// NewTypedQuery creates a TypedQuery with schema information extracted
// from the entity type T using reflection.
//
// Example:
//
//	type User struct {
//	    ID        int64  `db:"id"`
//	    Name      string `db:"name"`
//	    Email     string `db:"email"`
//	}
//	tq := NewTypedQuery[User]("users")
func NewTypedQuery[T any](tableName string) *TypedQuery[T] {
	tq := &TypedQuery[T]{
		tableName: tableName,
		validCols: make(map[string]bool),
	}
	tq.extractColumns()
	return tq
}

// extractColumns uses reflection to extract column names from the entity type.
// It looks for `db` struct tags and falls back to lowercase field names.
func (tq *TypedQuery[T]) extractColumns() {
	var zero T
	t := reflect.TypeOf(zero)

	// Handle pointer types
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Only process struct types
	if t.Kind() != reflect.Struct {
		return
	}

	columns := make([]string, 0, t.NumField())

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Get column name from `db` tag or use lowercase field name
		colName := field.Tag.Get("db")
		if colName == "" {
			colName = strings.ToLower(field.Name)
		}

		// Skip "-" tagged fields (explicitly ignored)
		if colName == "-" {
			continue
		}

		columns = append(columns, colName)
		tq.validCols[colName] = true
	}

	// Sort columns for consistent error messages
	sort.Strings(columns)
	tq.columns = columns
}

// ValidateFieldMask checks if all paths in the FieldMask are valid columns.
// Returns an error with suggestions if any invalid columns are found.
//
// Example:
//
//	err := tq.ValidateFieldMask([]string{"name", "email", "invalid_field"})
//	// Error: invalid fields: [invalid_field] (valid columns: [created_at email id name updated_at])
func (tq *TypedQuery[T]) ValidateFieldMask(paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	tq.mu.RLock()
	defer tq.mu.RUnlock()

	invalid := make([]string, 0)
	for _, path := range paths {
		// Strip table prefix if present (e.g., "user.name" -> "name")
		col := stripTablePrefix(path, tq.tableName)

		if !tq.validCols[col] {
			invalid = append(invalid, path)
		}
	}

	if len(invalid) > 0 {
		return fmt.Errorf(
			"invalid fields: %v (valid columns: %v)",
			invalid,
			tq.columns,
		)
	}

	return nil
}

// ValidateFields checks if all field names are valid columns.
// This is useful for validating dynamic field selections in DataModel.
//
// Example:
//
//	err := tq.ValidateFields([]string{"id", "name", "nonexistent"})
//	// Error: invalid fields: [nonexistent] (valid columns: [created_at email id name updated_at])
func (tq *TypedQuery[T]) ValidateFields(fields []string) error {
	return tq.ValidateFieldMask(fields)
}

// ValidateDataModel checks if all field names in the DataModel are valid.
// Returns nil if the DataModel has no field names (SELECT * will be used).
//
// Example:
//
//	dm := models.DataModel[User]{
//	    TableName:  "users",
//	    FieldNames: []string{"id", "name", "invalid"},
//	}
//	err := tq.ValidateDataModel(dm)
//	// Error: invalid fields: [invalid] (valid columns: [created_at email id name updated_at])
func (tq *TypedQuery[T]) ValidateDataModel(dm models.DataModel[T]) error {
	if len(dm.FieldNames) == 0 {
		return nil // SELECT * is always valid
	}
	return tq.ValidateFields(dm.FieldNames)
}

// GetValidColumns returns a sorted list of all valid column names.
// Useful for generating error messages or documentation.
func (tq *TypedQuery[T]) GetValidColumns() []string {
	tq.mu.RLock()
	defer tq.mu.RUnlock()

	result := make([]string, len(tq.columns))
	copy(result, tq.columns)
	return result
}

// IsValidColumn checks if a single column name is valid.
//
// Example:
//
//	if tq.IsValidColumn("email") {
//	    // Use the column
//	}
func (tq *TypedQuery[T]) IsValidColumn(col string) bool {
	tq.mu.RLock()
	defer tq.mu.RUnlock()
	return tq.validCols[col]
}

// stripTablePrefix removes the table name prefix from a field path.
// Examples:
//   - "user.name" -> "name"
//   - "name" -> "name"
func stripTablePrefix(path, tableName string) string {
	prefix := tableName + "."
	if strings.HasPrefix(path, prefix) {
		return strings.TrimPrefix(path, prefix)
	}
	return path
}

// SuggestColumn provides fuzzy matching suggestions for invalid column names.
// Uses Levenshtein distance to find the closest valid columns.
//
// Example:
//
//	suggestions := tq.SuggestColumn("emal")
//	// Returns: ["email"]
func (tq *TypedQuery[T]) SuggestColumn(invalid string) []string {
	tq.mu.RLock()
	defer tq.mu.RUnlock()

	type candidate struct {
		col      string
		distance int
	}

	candidates := make([]candidate, 0, len(tq.columns))
	for _, col := range tq.columns {
		dist := levenshteinDistance(strings.ToLower(invalid), strings.ToLower(col))
		if dist <= 3 { // Only suggest if distance is small
			candidates = append(candidates, candidate{col, dist})
		}
	}

	// Sort by distance
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].distance < candidates[j].distance
	})

	// Return top 3 suggestions
	suggestions := make([]string, 0, 3)
	for i := 0; i < len(candidates) && i < 3; i++ {
		suggestions = append(suggestions, candidates[i].col)
	}

	return suggestions
}

// levenshteinDistance calculates the Levenshtein distance between two strings.
// This is used for fuzzy column name matching.
func levenshteinDistance(s1, s2 string) int {
	if len(s1) == 0 {
		return len(s2)
	}
	if len(s2) == 0 {
		return len(s1)
	}

	matrix := make([][]int, len(s1)+1)
	for i := range matrix {
		matrix[i] = make([]int, len(s2)+1)
		matrix[i][0] = i
	}
	for j := range matrix[0] {
		matrix[0][j] = j
	}

	for i := 1; i <= len(s1); i++ {
		for j := 1; j <= len(s2); j++ {
			cost := 1
			if s1[i-1] == s2[j-1] {
				cost = 0
			}

			matrix[i][j] = min3(
				matrix[i-1][j]+1,      // deletion
				matrix[i][j-1]+1,      // insertion
				matrix[i-1][j-1]+cost, // substitution
			)
		}
	}

	return matrix[len(s1)][len(s2)]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
