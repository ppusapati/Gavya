// Package filter provides complex filter operations for SQL WHERE clauses.
//
// This package handles conversion of protobuf filter operations (from api/v1/query)
// into SQL WHERE conditions with proper parameterization. It supports:
//   - String filters (equals, contains, in, not in, etc.)
//   - Numeric filters (int32, int64, float, double) with comparison operators
//   - Boolean filters
//   - Date/time filters
//   - Null checks
//   - FilterBuilder for programmatic filter construction
//
// Example usage:
//
//	fb := filter.NewFilterBuilder()
//	fb.AddCondition("name", "LIKE", "%john%")
//	fb.AddCondition("age", ">", 18)
//	whereClause, args := fb.BuildWhereClause()
//
// See also: builder package for SQL query construction
package filter

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"p9e.in/samavaya/packages/api/v1/query"

	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/jackc/pgx/v5"
)

// Helper functions to replace go-funk
func mapStringValues(values []*wrappers.StringValue) []string {
	result := make([]string, len(values))
	for i, v := range values {
		result[i] = v.Value
	}
	return uniqueStrings(result)
}

func mapDoubleValues(values []*wrappers.DoubleValue) []float64 {
	result := make([]float64, len(values))
	for i, v := range values {
		result[i] = v.Value
	}
	return uniqueDoubles(result)
}

func mapFloatValues(values []*wrappers.FloatValue) []float32 {
	result := make([]float32, len(values))
	for i, v := range values {
		result[i] = v.Value
	}
	return uniqueFloats(result)
}

func mapInt32Values(values []*wrappers.Int32Value) []int32 {
	result := make([]int32, len(values))
	for i, v := range values {
		result[i] = v.Value
	}
	return uniqueInt32s(result)
}

func mapInt64Values(values []*wrappers.Int64Value) []int64 {
	result := make([]int64, len(values))
	for i, v := range values {
		result[i] = v.Value
	}
	return uniqueInt64s(result)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool)
	result := []string{}
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

func uniqueDoubles(values []float64) []float64 {
	seen := make(map[float64]bool)
	result := []float64{}
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

func uniqueFloats(values []float32) []float32 {
	seen := make(map[float32]bool)
	result := []float32{}
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

func uniqueInt32s(values []int32) []int32 {
	seen := make(map[int32]bool)
	result := []int32{}
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

func uniqueInt64s(values []int64) []int64 {
	seen := make(map[int64]bool)
	result := []int64{}
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}

// FilterCondition represents a generic filter condition
type FilterCondition struct {
	Field    string
	Operator string
	Value    interface{}
}

// FilterBuilder helps construct complex filter conditions
type FilterBuilder struct {
	conditions []FilterCondition
}

// NewFilterBuilder creates a new FilterBuilder
func NewFilterBuilder() *FilterBuilder {
	return &FilterBuilder{
		conditions: []FilterCondition{},
	}
}

// AddCondition adds a new filter condition
func (fb *FilterBuilder) AddCondition(field, operator string, value interface{}) *FilterBuilder {
	fb.conditions = append(fb.conditions, FilterCondition{
		Field:    field,
		Operator: operator,
		Value:    value,
	})
	return fb
}

// BuildWhereClause generates a SQL WHERE clause and arguments
func (fb *FilterBuilder) BuildWhereClause() (string, []interface{}) {
	var whereClauses []string
	var args []interface{}
	paramCounter := 1

	for _, condition := range fb.conditions {
		clause, condArgs := fb.buildSingleCondition(condition, &paramCounter)
		whereClauses = append(whereClauses, clause)
		args = append(args, condArgs...)
	}

	return strings.Join(whereClauses, " AND "), args
}

// buildSingleCondition creates a SQL condition for a single filter
func (fb *FilterBuilder) buildSingleCondition(condition FilterCondition, paramCounter *int) (string, []interface{}) {
	switch condition.Operator {
	case "=", "!=", "<", ">", "<=", ">=":
		return fmt.Sprintf("%s %s $%d", condition.Field, condition.Operator, *paramCounter), []interface{}{condition.Value}
	case "LIKE":
		return fmt.Sprintf("%s LIKE $%d", condition.Field, *paramCounter), []interface{}{fmt.Sprintf("%%%v%%", condition.Value)}
	case "IN":
		slice := reflect.ValueOf(condition.Value)
		if slice.Kind() != reflect.Slice {
			return "", nil
		}
		placeholders := make([]string, slice.Len())
		args := make([]interface{}, slice.Len())
		for i := 0; i < slice.Len(); i++ {
			placeholders[i] = fmt.Sprintf("$%d", *paramCounter+i)
			args[i] = slice.Index(i).Interface()
		}
		*paramCounter += len(args)
		return fmt.Sprintf("%s IN (%s)", condition.Field, strings.Join(placeholders, ", ")), args
	default:
		return "", nil
	}
}

// Utility functions for common filter scenarios
func StringEquals(field string, value string) FilterCondition {
	return FilterCondition{Field: field, Operator: "=", Value: value}
}

func StringContains(field string, value string) FilterCondition {
	return FilterCondition{Field: field, Operator: "LIKE", Value: value}
}

func NumericGreaterThan(field string, value interface{}) FilterCondition {
	return FilterCondition{Field: field, Operator: ">", Value: value}
}

func InList(field string, values interface{}) FilterCondition {
	return FilterCondition{Field: field, Operator: "IN", Value: values}
}

func BuildStringFilter(field string, filter *query.StringFilterOperation) func(tx pgx.Tx) (pgx.Tx, error) {
	return func(tx pgx.Tx) (pgx.Tx, error) {
		query := strings.Builder{}
		args := []interface{}{}

		if filter == nil {
			return tx, nil
		}

		if filter.Eq != nil {
			query.WriteString(fmt.Sprintf("%s = $%d", field, len(args)+1))
			args = append(args, filter.Eq.Value)
		}
		if filter.Neq != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s <> $%d", field, len(args)+1))
			args = append(args, filter.Neq.Value)
		}
		if filter.Contains != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s LIKE $%d", field, len(args)+1))
			args = append(args, fmt.Sprintf("%%%v%%", filter.Contains.Value))
		}
		if filter.StartsWith != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s LIKE $%d", field, len(args)+1))
			args = append(args, fmt.Sprintf("%%%v", filter.StartsWith.Value))
		}
		if filter.NstartsWith != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s NOT LIKE $%d", field, len(args)+1))
			args = append(args, fmt.Sprintf("%%%v", filter.NstartsWith.Value))
		}
		if filter.EndsWith != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s LIKE $%d", field, len(args)+1))
			args = append(args, fmt.Sprintf("%v%%", filter.EndsWith.Value))
		}
		if filter.NendsWith != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s NOT LIKE $%d", field, len(args)+1))
			args = append(args, fmt.Sprintf("%v%%", filter.NendsWith.Value))
		}
		if filter.In != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s IN ($%d)", field, len(args)+1))
			args = append(args, mapStringValues(filter.In))
		}
		if filter.Nin != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s NOT IN ($%d)", field, len(args)+1))
			args = append(args, mapStringValues(filter.Nin))
		}
		if filter.Null != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s IS NULL", field))
		}
		if filter.Nnull != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s IS NOT NULL", field))
		}
		if filter.Empty != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("(%s IS NULL OR %s = '')", field, field))
		}
		if filter.Nempty != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("(%s IS NOT NULL AND %s <> '')", field, field))
		}
		if filter.Like != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s LIKE $%d", field, len(args)+1))
			args = append(args, fmt.Sprintf("%%%v%%", filter.Like.Value))
		}

		// Example usage:
		_, err := tx.Exec(context.Background(), query.String(), args...)
		if err != nil {
			return tx, err
		}

		return tx, nil
	}
}

func BuildBooleanFilter(field string, filter *query.BooleanFilterOperators) func(tx pgx.Tx) (pgx.Tx, error) {
	return func(tx pgx.Tx) (pgx.Tx, error) {
		query := strings.Builder{}
		args := []interface{}{}

		if filter == nil {
			return tx, nil
		}

		if filter.Eq != nil {
			query.WriteString(fmt.Sprintf("%s = $%d", field, len(args)+1))
			args = append(args, filter.Eq.Value)
		}
		if filter.Neq != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s <> $%d", field, len(args)+1))
			args = append(args, filter.Neq.Value)
		}
		if filter.Null != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s IS NULL", field))
		}
		if filter.Nnull != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s IS NOT NULL", field))
		}

		// Example usage:
		_, err := tx.Exec(context.Background(), query.String(), args...)
		if err != nil {
			return tx, err
		}

		return tx, nil
	}
}

func BuildNullFilter(field string, filter *query.NullFilterOperators) func(tx pgx.Tx) (pgx.Tx, error) {
	return func(tx pgx.Tx) (pgx.Tx, error) {
		query := strings.Builder{}
		args := []interface{}{}

		if filter == nil {
			return tx, nil
		}

		if filter.Null != nil {
			query.WriteString(fmt.Sprintf("%s IS NULL", field))
		}
		if filter.Nnull != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s IS NOT NULL", field))
		}

		// Example usage:
		_, err := tx.Exec(context.Background(), query.String(), args...)
		if err != nil {
			return tx, err
		}

		return tx, nil
	}
}

func BuildDateFilter(field string, filter *query.DateFilterOperators) func(tx pgx.Tx) (pgx.Tx, error) {
	return func(tx pgx.Tx) (pgx.Tx, error) {
		query := strings.Builder{}
		args := []interface{}{}

		if filter == nil {
			return tx, nil
		}

		if filter.Eq != nil {
			query.WriteString(fmt.Sprintf("%s = $%d", field, len(args)+1))
			args = append(args, filter.Eq.AsTime())
		}
		if filter.Neq != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s <> $%d", field, len(args)+1))
			args = append(args, filter.Neq.AsTime())
		}
		if filter.Gt != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s > $%d", field, len(args)+1))
			args = append(args, filter.Gt.AsTime())
		}
		if filter.Gte != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s >= $%d", field, len(args)+1))
			args = append(args, filter.Gte.AsTime())
		}
		if filter.Lt != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s < $%d", field, len(args)+1))
			args = append(args, filter.Lt.AsTime())
		}
		if filter.Lte != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s <= $%d", field, len(args)+1))
			args = append(args, filter.Lte.AsTime())
		}
		if filter.Null != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s IS NULL", field))
		}
		if filter.Nnull != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s IS NOT NULL", field))
		}

		// Example usage:
		_, err := tx.Exec(context.Background(), query.String(), args...)
		if err != nil {
			return tx, err
		}

		return tx, nil
	}
}

func BuildDoubleFilter(field string, filter *query.DoubleFilterOperators) func(tx pgx.Tx) (pgx.Tx, error) {
	return func(tx pgx.Tx) (pgx.Tx, error) {
		query := strings.Builder{}
		args := []interface{}{}

		if filter == nil {
			return tx, nil
		}

		if filter.Eq != nil {
			query.WriteString(fmt.Sprintf("%s = $%d", field, len(args)+1))
			args = append(args, filter.Eq.Value)
		}
		if filter.Neq != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s <> $%d", field, len(args)+1))
			args = append(args, filter.Neq.Value)
		}
		if filter.In != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s IN ($%d)", field, len(args)+1))
			args = append(args, mapDoubleValues(filter.In))
		}
		if filter.Nin != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s NOT IN ($%d)", field, len(args)+1))
			args = append(args, mapDoubleValues(filter.Nin))
		}
		if filter.Gt != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s > $%d", field, len(args)+1))
			args = append(args, filter.Gt.Value)
		}
		if filter.Gte != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s >= $%d", field, len(args)+1))
			args = append(args, filter.Gte.Value)
		}
		if filter.Lt != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s < $%d", field, len(args)+1))
			args = append(args, filter.Lt.Value)
		}
		if filter.Lte != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s <= $%d", field, len(args)+1))
			args = append(args, filter.Lte.Value)
		}
		if filter.Null != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s IS NULL", field))
		}
		if filter.Nnull != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s IS NOT NULL", field))
		}

		// Example usage:
		_, err := tx.Exec(context.Background(), query.String(), args...)
		if err != nil {
			return tx, err
		}

		return tx, nil
	}
}

func BuildFloatFilter(field string, filter *query.FloatFilterOperators) func(tx pgx.Tx) (pgx.Tx, error) {
	return func(tx pgx.Tx) (pgx.Tx, error) {
		query := strings.Builder{}
		args := []interface{}{}

		if filter == nil {
			return tx, nil
		}

		if filter.Eq != nil {
			query.WriteString(fmt.Sprintf("%s = $%d", field, len(args)+1))
			args = append(args, filter.Eq.Value)
		}
		if filter.Neq != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s <> $%d", field, len(args)+1))
			args = append(args, filter.Neq.Value)
		}
		if filter.In != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s IN ($%d)", field, len(args)+1))
			args = append(args, mapFloatValues(filter.In))
		}
		if filter.Nin != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s NOT IN ($%d)", field, len(args)+1))
			args = append(args, mapFloatValues(filter.Nin))
		}
		if filter.Gt != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s > $%d", field, len(args)+1))
			args = append(args, filter.Gt.Value)
		}
		if filter.Gte != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s >= $%d", field, len(args)+1))
			args = append(args, filter.Gte.Value)
		}
		if filter.Lt != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s < $%d", field, len(args)+1))
			args = append(args, filter.Lt.Value)
		}
		if filter.Lte != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s <= $%d", field, len(args)+1))
			args = append(args, filter.Lte.Value)
		}
		if filter.Null != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s IS NULL", field))
		}
		if filter.Nnull != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s IS NOT NULL", field))
		}

		// Example usage:
		_, err := tx.Exec(context.Background(), query.String(), args...)
		if err != nil {
			return tx, err
		}

		return tx, nil
	}
}

func BuildInt32Filter(field string, filter *query.Int32FilterOperators) func(tx pgx.Tx) (pgx.Tx, error) {
	return func(tx pgx.Tx) (pgx.Tx, error) {
		query := strings.Builder{}
		args := []interface{}{}

		if filter == nil {
			return tx, nil
		}

		if filter.Eq != nil {
			query.WriteString(fmt.Sprintf("%s = $%d", field, len(args)+1))
			args = append(args, filter.Eq.Value)
		}
		if filter.Neq != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s <> $%d", field, len(args)+1))
			args = append(args, filter.Neq.Value)
		}
		if filter.In != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s IN ($%d)", field, len(args)+1))
			args = append(args, mapInt32Values(filter.In))
		}
		if filter.Nin != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s NOT IN ($%d)", field, len(args)+1))
			args = append(args, mapInt32Values(filter.Nin))
		}
		if filter.Gt != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s > $%d", field, len(args)+1))
			args = append(args, filter.Gt.Value)
		}
		if filter.Gte != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s >= $%d", field, len(args)+1))
			args = append(args, filter.Gte.Value)
		}
		if filter.Lt != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s < $%d", field, len(args)+1))
			args = append(args, filter.Lt.Value)
		}
		if filter.Lte != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s <= $%d", field, len(args)+1))
			args = append(args, filter.Lte.Value)
		}
		if filter.Null != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s IS NULL", field))
		}
		if filter.Nnull != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s IS NOT NULL", field))
		}

		// Example usage:
		_, err := tx.Exec(context.Background(), query.String(), args...)
		if err != nil {
			return tx, err
		}

		return tx, nil
	}
}

func BuildInt64Filter(field string, filter *query.Int64FilterOperators) func(tx pgx.Tx) (pgx.Tx, error) {
	return func(tx pgx.Tx) (pgx.Tx, error) {
		query := strings.Builder{}
		args := []interface{}{}

		if filter == nil {
			return tx, nil
		}

		if filter.Eq != nil {
			query.WriteString(fmt.Sprintf("%s = $%d", field, len(args)+1))
			args = append(args, filter.Eq.Value)
		}
		if filter.Neq != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s <> $%d", field, len(args)+1))
			args = append(args, filter.Neq.Value)
		}
		if filter.In != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s IN ($%d)", field, len(args)+1))
			args = append(args, mapInt64Values(filter.In))
		}
		if filter.Nin != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s NOT IN ($%d)", field, len(args)+1))
			args = append(args, mapInt64Values(filter.Nin))
		}
		if filter.Gt != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s > $%d", field, len(args)+1))
			args = append(args, filter.Gt.Value)
		}
		if filter.Gte != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s >= $%d", field, len(args)+1))
			args = append(args, filter.Gte.Value)
		}
		if filter.Lt != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s < $%d", field, len(args)+1))
			args = append(args, filter.Lt.Value)
		}
		if filter.Lte != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s <= $%d", field, len(args)+1))
			args = append(args, filter.Lte.Value)
		}
		if filter.Null != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s IS NULL", field))
		}
		if filter.Nnull != nil {
			if query.Len() > 0 {
				query.WriteString(" AND ")
			}
			query.WriteString(fmt.Sprintf("%s IS NOT NULL", field))
		}

		// Example usage:
		_, err := tx.Exec(context.Background(), query.String(), args...)
		if err != nil {
			return tx, err
		}

		return tx, nil
	}
}
