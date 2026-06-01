package builder

import (
	"context"
	"strings"

	"p9e.in/samavaya/packages/database/pgxpostgres/validator"
	"p9e.in/samavaya/packages/models"
	"p9e.in/samavaya/packages/p9context"
)

const (
	selectClause    = "SELECT "
	updateClause    = "UPDATE "
	deleteClause    = "DELETE "
	fromClause      = " FROM "
	setClause       = " SET "
	joinClause      = " JOIN "
	leftJoinClause  = " LEFT JOIN "
	rightJoinClause = " RIGHT JOIN "
	fullJoinClause  = " FULL JOIN "
	crossJoinClause = " CROSS JOIN "
	innerJoinClause = " INNER JOIN "
	outerJoinClause = " OUTER JOIN "
	onClause        = " ON "
	whereClause     = " WHERE "
	andClause       = " AND "
	orClause        = " OR "
	notClause       = " NOT "
	inClause        = " IN "
	likeClause      = " LIKE "
	betweenClause   = " BETWEEN "
	notInClause     = " NOT IN "
	orderClause     = " ORDER BY "
	groupClause     = " GROUP BY "
	limitClause     = " LIMIT "
	offsetClause    = " OFFSET "
	ascClause       = " ASC"
	descClause      = " DESC"
	nullClause      = " IS NULL"
	notNullClause   = " IS NOT NULL"
	emptyClause     = " = ''"
	notEmptyClause  = " != ''"
	trueValue       = "true"
	falseValue      = "false"
)

// QuoteIdentifier safely quotes an identifier for use in SQL queries
// by escaping quotes and wrapping the identifier in double quotes.
// Example: user"name -> "user""name"
func QuoteIdentifier(identifier string) string {
	// Pre-allocate the string builder with a reasonable size
	var quoted strings.Builder
	quoted.Grow(len(identifier) + 2) // +2 for surrounding quotes

	quoted.WriteByte('"')
	quoted.WriteString(strings.Replace(identifier, `"`, `""`, -1))
	quoted.WriteByte('"')

	return quoted.String()
}

// WhereCondition creates a WHERE clause from either a map of conditions or a raw string condition
func ParseWhereCondition(ctx context.Context, condition interface{}, startPosition int) (string, []interface{}) {
	switch v := condition.(type) {
	case map[string]interface{}:
		return WhereCondition(ctx, v, startPosition, true)
	case string:
		if v == "" {
			return "", nil
		}
		return v, nil
	default:
		return "", nil
	}
}

// ParseOrderBy converts ORDER BY clause into safe SQL
func ParseOrderBy(ctx context.Context, orderBy string) string {
	securityCtx := p9context.GetSecurityContextOrDefault(ctx)
	if err := validator.ValidateOrderBy(orderBy, securityCtx); err != nil {
		return ""
	}

	validColumns := strings.Split(orderBy, ",")
	sanitized := make([]string, 0, len(validColumns))

	for _, col := range validColumns {
		col = strings.TrimSpace(col)
		direction := "ASC"

		if strings.HasPrefix(col, "-") {
			direction = "DESC"
			col = strings.TrimPrefix(col, "-")
		} else if strings.HasPrefix(col, "+") {
			col = strings.TrimPrefix(col, "+")
		}

		sanitized = append(sanitized, QuoteIdentifier(col)+" "+direction)
	}

	return strings.Join(sanitized, ", ")
}

// ParseGroupBy converts GROUP BY clause into safe SQL
func ParseGroupBy(ctx context.Context, groupBy string) string {
	securityCtx := p9context.GetSecurityContextOrDefault(ctx)

	if err := validator.ValidateGroupBy(groupBy, securityCtx); err != nil {
		return ""
	}

	columns := strings.Split(groupBy, ",")
	sanitized := make([]string, len(columns))

	for i, col := range columns {
		sanitized[i] = QuoteIdentifier(strings.TrimSpace(col))
	}

	return strings.Join(sanitized, ", ")
}

// contains checks if a string slice contains a string
func contains(slice []string, str string) bool {
	for _, v := range slice {
		if v == str {
			return true
		}
	}
	return false
}

// Helper function to map a slice of strings
func Map(vs []string, f func(string) string) []string {
	vsm := make([]string, len(vs))
	for i, v := range vs {
		vsm[i] = f(v)
	}
	return vsm
}

// EstimateBufferSize estimates the buffer size needed for the query builder
func EstimateBufferSize[T any](dm models.DataModel[T], extra int) int {
	// Base size for the query
	size := 256

	// Add size for table name and field names
	size += len(dm.TableName) + len(dm.FieldNames)*10

	// Add size for WHERE, ORDER BY, GROUP BY clauses
	if dm.Where != "" {
		size += len(dm.Where) + len(dm.WhereArgs)*10
	}
	if dm.OrderBy != "" {
		size += len(dm.OrderBy)
	}
	if dm.GroupBy != "" {
		size += len(dm.GroupBy)
	}

	// Add extra size for other parts of the query
	size += extra

	return size
}
