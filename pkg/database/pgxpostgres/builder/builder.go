// Package builder provides SQL query construction from DataModel structs.
//
// This package generates type-safe SQL queries (SELECT, INSERT, UPDATE, DELETE)
// from generic DataModel[T] structures. It handles:
//   - Query generation with proper parameterization (SQL injection prevention)
//   - WHERE clause construction from SearchCriteria
//   - Security context validation via p9context
//   - Integration with filter package for complex filter operations
//
// Example usage:
//
//	dm := models.DataModel[User]{
//	    TableName: "users",
//	    Where: "status = $1",
//	    WhereArgs: []any{"active"},
//	}
//	query, args, err := builder.SelectQuery(ctx, dm)
//
// See also: filter package for FilterBuilder, operations package for query execution
package builder

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"p9e.in/samavaya/packages/api/v1/query"
	"p9e.in/samavaya/packages/database/pgxpostgres/filter"
	"p9e.in/samavaya/packages/database/pgxpostgres/validator"
	"p9e.in/samavaya/packages/models"
	"p9e.in/samavaya/packages/p9context"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

func BuildWhereClause(ctx context.Context, criteria *models.SearchCriteria) (string, []interface{}) {
	conditions := make(map[string]interface{})

	if criteria == nil {
		return "", nil
	}

	// Handle basic filters
	if len(criteria.Filters) > 0 {
		for _, filter := range criteria.Filters {
			switch filter.Operator {
			case models.OperatorEquals:
				conditions[filter.Field] = filter.Value
			case models.OperatorContains:
				if strVal, ok := filter.Value.(string); ok {
					conditions[filter.Field] = &query.StringFilterOperation{
						Contains: &wrapperspb.StringValue{Value: strVal},
					}
				}
			case models.OperatorIn:
				if strArray, ok := filter.Value.([]string); ok {
					conditions[filter.Field] = strArray
				}
			}
		}
	}

	// Handle standard identifier fields
	if criteria.ID != nil && criteria.ID.Eq != nil {
		conditions["id"] = criteria.ID
	}

	if criteria.UUID != nil && criteria.UUID.Eq != nil {
		conditions["uuid"] = criteria.UUID
	}

	// Handle search term
	if criteria.SearchTerm != nil && criteria.SearchTerm.Eq != nil {
		conditions["name"] = &query.StringFilterOperation{
			Contains: &wrapperspb.StringValue{Value: criteria.SearchTerm.Eq.Value},
		}
	}

	// Handle time-based filters
	if criteria.CreatedAtFrom != nil && criteria.CreatedAtFrom.Eq != nil {
		conditions["created_at"] = &query.DateFilterOperators{
			Gte: criteria.CreatedAtFrom.Eq,
		}
	}
	if criteria.CreatedAtTo != nil && criteria.CreatedAtTo.Eq != nil {
		conditions["created_at"] = &query.DateFilterOperators{
			Lte: criteria.CreatedAtTo.Eq,
		}
	}
	if criteria.UpdatedAtFrom != nil && criteria.UpdatedAtFrom.Eq != nil {
		conditions["updated_at"] = &query.DateFilterOperators{
			Gte: criteria.UpdatedAtFrom.Eq,
		}
	}
	if criteria.UpdatedAtTo != nil && criteria.UpdatedAtTo.Eq != nil {
		conditions["updated_at"] = &query.DateFilterOperators{
			Lte: criteria.UpdatedAtTo.Eq,
		}
	}

	// Handle status and active filtering
	if len(criteria.Statuses) > 0 {
		conditions["status"] = criteria.Statuses
	}

	if criteria.ActiveOnly != nil && criteria.ActiveOnly.Eq != nil {
		conditions["is_active"] = criteria.ActiveOnly
	}

	// Handle tenant and role IDs
	if len(criteria.TenantIds) > 0 {
		conditions["tenant_id"] = criteria.TenantIds
	}

	if len(criteria.RoleIds) > 0 {
		conditions["role_id"] = criteria.RoleIds
	}

	// Handle dynamic filters
	if len(criteria.DynamicFilters) > 0 {
		for field, filter := range criteria.DynamicFilters {
			conditions[field] = filter
		}
	}

	// Build the WHERE clause
	whereClause, args := WhereCondition(ctx, conditions, 1, true)

	// Handle sorting
	var orderByClause string
	if len(criteria.Sort) > 0 {
		orderByClause = " ORDER BY " + strings.Join(criteria.Sort, ", ")
		if criteria.SortDesc != nil && *criteria.SortDesc {
			orderByClause += " DESC"
		}
	}

	return whereClause + orderByClause, args
}

// BuildUpsertQuery generates an UPSERT (INSERT ... ON CONFLICT) query
func UpsertQuery[T any](dm models.DataModel[T]) (string, []T, error) {
	tableName := dm.TableName
	columns := dm.FieldNames
	values := dm.Values
	conflictColumns := dm.ConflictColumns

	// Construct column and placeholder lists
	columnNames := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	for i, col := range columns {
		columnNames[i] = QuoteIdentifier(col)
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}

	// Construct conflict columns
	quotedConflictColumns := make([]string, len(conflictColumns))
	for i, col := range conflictColumns {
		quotedConflictColumns[i] = QuoteIdentifier(col)
	}

	// Construct conflict target
	conflictTarget := strings.Join(
		quotedConflictColumns,
		", ",
	)

	// Construct update clauses
	updateClauses := make([]string, 0, len(columns))
	for _, col := range columns {
		if !contains(conflictColumns, col) {
			updateClauses = append(updateClauses,
				fmt.Sprintf("%s = EXCLUDED.%s",
					QuoteIdentifier(col),
					QuoteIdentifier(col)))
		}
	}

	// Build UPSERT query
	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s) ON CONFLICT (%s) DO UPDATE SET %s",
		QuoteIdentifier(tableName),
		strings.Join(columnNames, ", "),
		strings.Join(placeholders, ", "),
		conflictTarget,
		strings.Join(updateClauses, ", "),
	)

	return query, values, nil
}

// BuildSelectQuery constructs a SELECT query dynamically
func BuildSelectQuery[T any](dm models.DataModel[T]) (string, []interface{}) {
	// Select fields
	fields := dm.FieldNames
	if len(dm.Aggregations) > 0 {
		aggFields := make([]string, 0, len(dm.Aggregations))
		for alias, agg := range dm.Aggregations {
			aggFields = append(aggFields, fmt.Sprintf("%s AS %s", agg, alias))
		}
		fields = append(fields, aggFields...)
	}
	selectClause := strings.Join(fields, ", ")

	// Base query
	query := fmt.Sprintf("SELECT %s FROM %s", selectClause, dm.TableName)

	// Joins
	if len(dm.Joins) > 0 {
		query += " " + strings.Join(dm.Joins, " ")
	}

	// Where clause
	args := dm.WhereArgs
	if dm.Where != "" {
		query += fmt.Sprintf(" WHERE %s", dm.Where)
	}

	// Group By
	if dm.GroupBy != "" {
		query += fmt.Sprintf(" GROUP BY %s", dm.GroupBy)
	}

	// Order By
	if dm.OrderBy != "" {
		query += fmt.Sprintf(" ORDER BY %s", dm.OrderBy)
	}

	// Limit and Offset
	if dm.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", dm.Limit)
	}
	if dm.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", dm.Offset)
	}

	return query, args
}

// BuildInsertQuery constructs an INSERT query
func BuildInsertQuery[T any](dm models.DataModel[T]) (string, []T) {
	placeholders := make([]string, len(dm.FieldNames))
	for i := range dm.FieldNames {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		dm.TableName,
		strings.Join(dm.FieldNames, ", "),
		strings.Join(placeholders, ", "),
	)

	// Handle conflict resolution
	if len(dm.ConflictColumns) > 0 {
		query += fmt.Sprintf(" ON CONFLICT (%s)", strings.Join(dm.ConflictColumns, ", "))
		if dm.OnConflict != "" {
			query += " " + dm.OnConflict
		} else {
			query += " DO NOTHING"
		}
	}

	return query, dm.Values
}

// BuildUpdateQuery constructs an UPDATE query
func BuildUpdateQuery[T any](dm models.DataModel[T]) (string, []T) {
	updateParts := make([]string, len(dm.FieldNames))
	for i, field := range dm.FieldNames {
		updateParts[i] = fmt.Sprintf("%s = $%d", field, i+1)
	}

	query := fmt.Sprintf(
		"UPDATE %s SET %s",
		dm.TableName,
		strings.Join(updateParts, ", "),
	)

	// Combine update values and where args
	args := append(dm.Values, make([]T, len(dm.WhereArgs))...)
	for i, v := range dm.WhereArgs {
		args[len(dm.Values)+i] = v.(T)
	}

	if dm.Where != "" {
		query += fmt.Sprintf(" WHERE %s", dm.Where)
	}

	return query, args
}

// BuildDeleteQuery constructs a DELETE query
func BuildDeleteQuery[T any](dm models.DataModel[T]) (string, []interface{}) {
	query := fmt.Sprintf("DELETE FROM %s", dm.TableName)

	if dm.Where != "" {
		query += fmt.Sprintf(" WHERE %s", dm.Where)
	}

	return query, dm.WhereArgs
}

// SelectQuery now uses the new filter approach
func SelectQuery[T any](ctx context.Context, dm models.DataModel[T]) (string, []T, error) {
	// Validate inputs
	securityCtx := p9context.GetSecurityContextOrDefault(ctx)

	if err := validator.ValidateDataModel(dm, securityCtx); err != nil {
		return "", nil, fmt.Errorf("invalid data model: %w", err)
	}

	var queryBuilder strings.Builder
	var args []T
	queryBuilder.Grow(EstimateBufferSize(dm, 0))

	// Build base SELECT query
	queryBuilder.WriteString(selectClause)
	if len(dm.FieldNames) == 0 {
		queryBuilder.WriteString("*")
	} else {
		queryBuilder.WriteString(strings.Join(
			Map(dm.FieldNames, QuoteIdentifier),
			", "))
	}
	queryBuilder.WriteString(fromClause)
	queryBuilder.WriteString(QuoteIdentifier(dm.TableName))

	// Add WHERE clause if filter is present
	if dm.Where != "" {
		queryBuilder.WriteString(whereClause)
		queryBuilder.WriteString(dm.Where)
		if len(dm.Values) > 0 {
			for _, v := range dm.Values {
				args = append(args, v)
			}
		} else if len(dm.WhereArgs) > 0 {
			for _, v := range dm.WhereArgs {
				args = append(args, v.(T))
			}
		}
	}

	// Add ORDER BY clause
	if dm.OrderBy != "" {
		queryBuilder.WriteString(orderClause)
		queryBuilder.WriteString(ParseOrderBy(ctx, dm.OrderBy))
	}

	// Add GROUP BY clause
	if dm.GroupBy != "" {
		queryBuilder.WriteString(groupClause)
		queryBuilder.WriteString(ParseGroupBy(ctx, dm.GroupBy))
	}

	// Add LIMIT and OFFSET
	if dm.Limit > 0 {
		queryBuilder.WriteString(fmt.Sprintf(" LIMIT %d", dm.Limit))
	}
	if dm.Offset > 0 {
		queryBuilder.WriteString(fmt.Sprintf(" OFFSET %d", dm.Offset))
	}

	queryBuilder.WriteString(";")
	query := queryBuilder.String()

	// Query logging is handled at the operations layer with proper context
	// Debug logging removed - use operations package tracing instead

	return query, args, nil
}

// Helper method to create a DataModel with filters
func WithFilter[T any](tableName string, filterBuilder *filter.FilterBuilder) models.DataModel[T] {
	whereClause, whereArgs := filterBuilder.BuildWhereClause()
	return models.DataModel[T]{
		TableName: tableName,
		Where:     whereClause,
		WhereArgs: whereArgs,
	}
}

// InsertQuery generates a safe INSERT query with parameterized values
func InsertQuery[T any](ctx context.Context, dm models.DataModel[T]) (string, []T, error) {
	securityCtx := p9context.GetSecurityContextOrDefault(ctx)
	if err := validator.ValidateTableName(dm.TableName, securityCtx); err != nil {
		return "", nil, fmt.Errorf("invalid table name: %w", err)
	}
	if err := validator.ValidateFieldNames(dm.FieldNames, securityCtx); err != nil {
		return "", nil, fmt.Errorf("invalid field names: %w", err)
	}

	var queryBuilder strings.Builder
	var args []T
	queryBuilder.Grow(EstimateBufferSize(dm, 0))

	// Build INSERT clause
	queryBuilder.WriteString("INSERT INTO ")
	queryBuilder.WriteString(QuoteIdentifier(dm.TableName))

	// Build columns
	queryBuilder.WriteString(" (")
	queryBuilder.WriteString(strings.Join(
		Map(dm.FieldNames, QuoteIdentifier),
		", "))
	queryBuilder.WriteString(") VALUES (")

	// Build values placeholders
	placeholders := make([]string, len(dm.Values))
	for i := range dm.Values {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args = append(args, dm.Values[i])
	}

	queryBuilder.WriteString(strings.Join(placeholders, ", "))
	queryBuilder.WriteString(")")

	// Add WHERE clause if present
	if dm.Where != "" {
		queryBuilder.WriteString(whereClause)
		whereClause, whereArgs := ParseWhereCondition(ctx, dm.Where, len(args)+1)
		queryBuilder.WriteString(whereClause)
		for _, arg := range whereArgs {
			args = append(args, arg.(T))
		}
	}

	queryBuilder.WriteString(";")
	return queryBuilder.String(), args, nil
}

// InsertBulkQuery generates a safe bulk INSERT query with parameterized values
func InsertBulkQuery[T any](ctx context.Context, dm models.DataModel[T]) (string, [][]T, error) {
	securityCtx := p9context.GetSecurityContextOrDefault(ctx)
	if err := validator.ValidateTableName(dm.TableName, securityCtx); err != nil {
		return "", nil, fmt.Errorf("invalid table name: %w", err)
	}
	if err := validator.ValidateFieldNames(dm.FieldNames, securityCtx); err != nil {
		return "", nil, fmt.Errorf("invalid field names: %w", err)
	}

	// Use BulkValues if provided, otherwise fallback to single Values
	valuesToInsert := dm.BulkValues
	if len(valuesToInsert) == 0 && len(dm.Values) > 0 {
		valuesToInsert = [][]T{dm.Values}
	}

	if len(valuesToInsert) == 0 {
		return "", nil, errors.New("no values to insert")
	}

	var queryBuilder strings.Builder
	var args [][]T
	queryBuilder.Grow(EstimateBufferSize(dm, 0))

	// Build INSERT clause
	queryBuilder.WriteString("INSERT INTO ")
	queryBuilder.WriteString(QuoteIdentifier(dm.TableName))

	// Build columns
	queryBuilder.WriteString(" (")
	queryBuilder.WriteString(strings.Join(
		Map(dm.FieldNames, QuoteIdentifier),
		", "))
	queryBuilder.WriteString(") VALUES ")

	// Build values placeholders for multiple rows
	placeholderGroups := make([]string, len(valuesToInsert))
	placeholderCount := 1

	for i, rowValues := range valuesToInsert {
		if len(rowValues) != len(dm.FieldNames) {
			return "", nil, fmt.Errorf("mismatch in number of values for row %d", i)
		}

		// Create placeholders for this row
		rowPlaceholders := make([]string, len(rowValues))
		for j := range rowValues {
			rowPlaceholders[j] = fmt.Sprintf("$%d", placeholderCount)
			args = append(args, rowValues)
			placeholderCount++
		}

		placeholderGroups[i] = "(" + strings.Join(rowPlaceholders, ", ") + ")"
	}

	// Add all row placeholders
	queryBuilder.WriteString(strings.Join(placeholderGroups, ", "))

	// Add WHERE clause if present
	if dm.Where != "" {
		queryBuilder.WriteString(whereClause)
		whereClause, whereArgs := ParseWhereCondition(ctx, dm.Where, placeholderCount)
		queryBuilder.WriteString(whereClause)
		for _, arg := range whereArgs {
			singlevalued := []T{arg.(T)}
			args = append(args, singlevalued)
		}
	}

	queryBuilder.WriteString(";")
	return queryBuilder.String(), args, nil
}

// UpdateQuery generates a safe UPDATE query with parameterized values
func UpdateQuery[T any](ctx context.Context, dm models.DataModel[T]) (string, []T, error) {
	// Validate inputs
	securityCtx := p9context.GetSecurityContextOrDefault(ctx)
	if err := validator.ValidateTableName(dm.TableName, securityCtx); err != nil {
		return "", nil, fmt.Errorf("invalid table name: %w", err)
	}
	if err := validator.ValidateFieldNames(dm.FieldNames, securityCtx); err != nil {
		return "", nil, fmt.Errorf("invalid field names: %w", err)
	}

	var queryBuilder strings.Builder
	var args []T
	queryBuilder.Grow(EstimateBufferSize(dm, 0))

	// Build UPDATE clause
	queryBuilder.WriteString(updateClause)
	queryBuilder.WriteString(QuoteIdentifier(dm.TableName))
	queryBuilder.WriteString(setClause)

	// Build SET clause
	setClauses := make([]string, len(dm.FieldNames))
	for i, field := range dm.FieldNames {
		setClauses[i] = fmt.Sprintf("%s = $%d",
			QuoteIdentifier(field), i+1)
		args = append(args, dm.Values[i])
	}

	queryBuilder.WriteString(strings.Join(setClauses, ", "))

	// Add WHERE clause
	if dm.Where != "" {
		queryBuilder.WriteString(whereClause)
		whereClause, whereArgs := ParseWhereCondition(ctx, dm.Where, len(args)+1)
		queryBuilder.WriteString(whereClause)
		for _, arg := range whereArgs {
			args = append(args, arg.(T))
		}
	}

	queryBuilder.WriteString(";")
	return queryBuilder.String(), args, nil
}

// DeleteQuery generates a safe DELETE query with parameterized values
func DeleteQuery[T any](ctx context.Context, dm models.DataModel[T]) (string, []T, error) {
	// Validate inputs
	securityCtx := p9context.GetSecurityContextOrDefault(ctx)

	if err := validator.ValidateTableName(dm.TableName, securityCtx); err != nil {
		return "", nil, fmt.Errorf("invalid table name: %w", err)
	}
	// Validate WHERE condition
	if dm.Where != "" {
		if err := validator.ValidateWhereCondition(dm.Where, securityCtx); err != nil {
			return "", nil, fmt.Errorf("invalid where condition: %w", err)
		}
	}
	var queryBuilder strings.Builder
	var args []T
	argPosition := 1
	queryBuilder.Grow(EstimateBufferSize(dm, 0))

	// Build UPDATE clause (soft delete)
	queryBuilder.WriteString(updateClause)
	queryBuilder.WriteString(QuoteIdentifier(dm.TableName))
	queryBuilder.WriteString(setClause)

	// Add system fields
	currentTime := time.Now()
	// Get user from security context (extracted from request context in Sprint 1)
	secCtx := p9context.GetSecurityContextOrDefault(ctx)
	deletedBy := secCtx.Username
	setClauses := []string{
		fmt.Sprintf("is_active = $%d", argPosition),
		fmt.Sprintf("deleted_by = $%d", argPosition+1),
		fmt.Sprintf("deleted_at = $%d", argPosition+2),
	}

	// Append system fields to args
	bm := models.BaseModel{
		IsActive:  false,
		DeletedBy: &deletedBy,
		DeletedAt: &currentTime,
	}

	args = append(args, any(bm.IsActive).(T), any(*bm.DeletedBy).(T), any(*bm.DeletedAt).(T))
	argPosition += 3

	queryBuilder.WriteString(strings.Join(setClauses, ", "))

	// Add WHERE clause
	if dm.Where != "" {
		queryBuilder.WriteString(whereClause)
		whereClause, whereArgs := ParseWhereCondition(ctx, dm.Where, argPosition)
		queryBuilder.WriteString(whereClause)
		for _, arg := range whereArgs {
			args = append(args, arg.(T))
		}
	}

	queryBuilder.WriteString(";")
	return queryBuilder.String(), args, nil
}

// CountQuery generates a safe COUNT query with parameterized values
func CountQuery[T any](ctx context.Context, dm models.DataModel[T]) (string, []T, error) {
	// Validate inputs
	securityCtx := p9context.GetSecurityContextOrDefault(ctx)
	if err := validator.ValidateTableName(dm.TableName, securityCtx); err != nil {
		return "", nil, fmt.Errorf("invalid table name: %w", err)
	}

	var queryBuilder strings.Builder
	var args []T
	argPosition := 1
	queryBuilder.Grow(EstimateBufferSize(dm, 0))

	// Build COUNT query
	queryBuilder.WriteString("SELECT COUNT(*) FROM ")
	queryBuilder.WriteString(QuoteIdentifier(dm.TableName))

	// Add WHERE clause
	queryBuilder.WriteString(" WHERE is_active = $1")
	args = append(args, any(true).(T))
	argPosition++

	if dm.Where != "" {
		queryBuilder.WriteString(" AND (")
		whereClause, whereArgs := ParseWhereCondition(ctx, dm.Where, argPosition)
		queryBuilder.WriteString(whereClause)
		queryBuilder.WriteString(")")
		for _, arg := range whereArgs {
			args = append(args, arg.(T))
		}
	}

	queryBuilder.WriteString(";")
	return queryBuilder.String(), args, nil
}

// BuildBulkInsertQuery constructs a bulk INSERT query
func BulkInsertQuery[T any](dm models.DataModel[T]) (string, [][]T, error) {
	if len(dm.BulkValues) == 0 {
		return "", nil, errors.New("no values to insert")
	}

	// Generate placeholders for bulk insert
	placeholderGroups := make([]string, len(dm.BulkValues))
	flattenedValues := make([]T, 0, len(dm.BulkValues)*len(dm.FieldNames))

	for i, rowValues := range dm.BulkValues {
		rowPlaceholders := make([]string, len(dm.FieldNames))
		for j := range dm.FieldNames {
			rowPlaceholders[j] = fmt.Sprintf("$%d", i*len(dm.FieldNames)+j+1)
		}
		placeholderGroups[i] = fmt.Sprintf("(%s)", strings.Join(rowPlaceholders, ", "))
		for _, value := range rowValues {
			flattenedValues = append(flattenedValues, value)
		}
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES %s",
		dm.TableName,
		strings.Join(dm.FieldNames, ", "),
		strings.Join(placeholderGroups, ", "),
	)

	// Handle conflict resolution for bulk insert
	if len(dm.ConflictColumns) > 0 {
		query += fmt.Sprintf(" ON CONFLICT (%s)", strings.Join(dm.ConflictColumns, ", "))
		if dm.OnConflict != "" {
			query += " " + dm.OnConflict
		} else {
			query += " DO NOTHING"
		}
	}

	return query, [][]T{flattenedValues}, nil
}
