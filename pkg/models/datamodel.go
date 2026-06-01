package models

// DataModel represents a flexible database model for various operations
type DataModel[T any] struct {
	// Table metadata
	TableName  string
	FieldNames []string
	Values     []T
	BulkValues [][]T

	// Query conditions
	Where     string
	WhereArgs []interface{}
	OrderBy   string
	GroupBy   string

	// Pagination and limits
	Limit  int32
	Offset int32

	// Conflict handling for upserts
	ConflictColumns []string
	OnConflict      string

	// Joins and complex queries
	Joins        []string
	Aggregations map[string]string
}
