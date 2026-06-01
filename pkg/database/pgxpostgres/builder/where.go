package builder

import (
	"context"
	"fmt"
	"strings"
	"time"

	"p9e.in/samavaya/packages/api/v1/query"
	"p9e.in/samavaya/packages/database/pgxpostgres/validator"
	"p9e.in/samavaya/packages/p9context"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// WhereCondition builds a WHERE clause with proper security and parameter handling
func WhereCondition(ctx context.Context, conditions map[string]interface{}, startPosition int, useAnd bool) (string, []interface{}) {
	if len(conditions) == 0 {
		return "", nil
	}

	var whereBuilder strings.Builder
	var args []interface{}
	position := startPosition
	first := true

	// Initialize security context
	securityCtx := p9context.GetSecurityContextOrDefault(ctx)

	for field, value := range conditions {
		// Validate field name for security
		if err := validator.ValidateIdentifier(field, securityCtx); err != nil {
			continue
		}

		// Handle join operators
		if !first {
			if useAnd {
				whereBuilder.WriteString(andClause)
			} else {
				whereBuilder.WriteString(orClause)
			}
		}
		first = false

		// Quote the field identifier for SQL injection protection
		quotedField := QuoteIdentifier(field)

		switch v := value.(type) {
		case *query.StringFilterOperation:
			for _, filter := range ConvertStringFilter(quotedField, v) {
				HandleFilter(&whereBuilder, filter, &args, &position)
			}

		case *query.Int32FilterOperators:
			handleInt32Operation(&whereBuilder, quotedField, v, &args, &position)

		case *query.Int64FilterOperators:
			handleInt64Operation(&whereBuilder, quotedField, v, &args, &position)

		case *query.DateFilterOperators:
			handleDateOperation(&whereBuilder, quotedField, v, &args, &position)

		case *query.BooleanFilterOperators:
			handleBooleanOperation(&whereBuilder, quotedField, v, &args, &position)

		case []string:
			handleStringArray(&whereBuilder, quotedField, v, &args, &position)

		case time.Time:
			whereBuilder.WriteString(fmt.Sprintf("%s = $%d", quotedField, position))
			args = append(args, v.UTC())
			position++

		case nil:
			whereBuilder.WriteString(quotedField)
			whereBuilder.WriteString(nullClause)

		default:
			// Handle basic equality for other types
			whereBuilder.WriteString(fmt.Sprintf("%s = $%d", quotedField, position))
			args = append(args, v)
			position++
		}
	}

	if whereBuilder.Len() == 0 {
		return "", nil
	}

	return "WHERE " + whereBuilder.String(), args
}

// Helper functions for different filter types
func FilterStringOperation(builder *strings.Builder, field string, op *query.StringFilterOperation, args *[]interface{}, position *int) {
	if op == nil {
		return
	}

	if op.Eq != nil {
		builder.WriteString(fmt.Sprintf("%s = $%d", field, *position))
		*args = append(*args, op.Eq.Value)
		*position++
	}
	if op.Neq != nil {
		builder.WriteString(fmt.Sprintf("%s != $%d", field, *position))
		*args = append(*args, op.Neq.Value)
		*position++
	}
	if op.Contains != nil {
		builder.WriteString(fmt.Sprintf("%s ILIKE $%d", field, *position))
		*args = append(*args, "%"+op.Contains.Value+"%")
		*position++
	}
	if op.StartsWith != nil {
		builder.WriteString(fmt.Sprintf("%s ILIKE $%d", field, *position))
		*args = append(*args, op.StartsWith.Value+"%")
		*position++
	}
	if op.EndsWith != nil {
		builder.WriteString(fmt.Sprintf("%s ILIKE $%d", field, *position))
		*args = append(*args, "%"+op.EndsWith.Value)
		*position++
	}
	if len(op.In) > 0 {
		placeholders := make([]string, len(op.In))
		for i, inVal := range op.In {
			placeholders[i] = fmt.Sprintf("$%d", *position)
			*args = append(*args, inVal.Value)
			*position++
		}
		builder.WriteString(fmt.Sprintf("%s IN (%s)", field, strings.Join(placeholders, ",")))
	}
	if op.Null != nil && op.Null.Value {
		builder.WriteString(field)
		builder.WriteString(nullClause)
	}
	if op.Nnull != nil && op.Nnull.Value {
		builder.WriteString(field)
		builder.WriteString(notNullClause)
	}
}

func handleInt32Operation(builder *strings.Builder, field string, op *query.Int32FilterOperators, args *[]interface{}, position *int) {
	if op == nil {
		return
	}

	handleNumericOperations(builder, field, op.Eq, op.Neq, op.Gt, op.Gte, op.Lt, op.Lte, op.In, op.Null, op.Nnull, args, position)
}

func handleInt64Operation(builder *strings.Builder, field string, op *query.Int64FilterOperators, args *[]interface{}, position *int) {
	if op == nil {
		return
	}

	handleNumericOperations(builder, field, op.Eq, op.Neq, op.Gt, op.Gte, op.Lt, op.Lte, op.In, op.Null, op.Nnull, args, position)
}

func handleDateOperation(builder *strings.Builder, field string, op *query.DateFilterOperators, args *[]interface{}, position *int) {
	if op == nil {
		return
	}

	if op.Eq != nil {
		builder.WriteString(fmt.Sprintf("%s = $%d", field, *position))
		*args = append(*args, op.Eq.AsTime().UTC())
		*position++
	}
	if op.Gt != nil {
		builder.WriteString(fmt.Sprintf("%s > $%d", field, *position))
		*args = append(*args, op.Gt.AsTime().UTC())
		*position++
	}
	// ... similar for other date operations
}

func handleBooleanOperation(builder *strings.Builder, field string, op *query.BooleanFilterOperators, args *[]interface{}, position *int) {
	if op == nil {
		return
	}

	if op.Eq != nil {
		builder.WriteString(fmt.Sprintf("%s = $%d", field, *position))
		*args = append(*args, op.Eq.Value)
		*position++
	}
	if op.Null != nil && op.Null.Value {
		builder.WriteString(field)
		builder.WriteString(nullClause)
	}
	if op.Nnull != nil && op.Nnull.Value {
		builder.WriteString(field)
		builder.WriteString(notNullClause)
	}
}

func handleStringArray(builder *strings.Builder, field string, values []string, args *[]interface{}, position *int) {
	if len(values) == 0 {
		return
	}

	placeholders := make([]string, len(values))
	for i, val := range values {
		placeholders[i] = fmt.Sprintf("$%d", *position)
		*args = append(*args, val)
		*position++
	}
	builder.WriteString(fmt.Sprintf("%s IN (%s)", field, strings.Join(placeholders, ",")))
}

// Generic helper for numeric operations
func handleNumericOperations(builder *strings.Builder, field string,
	eq, neq, gt, gte, lt, lte interface{},
	in interface{},
	null, nnull *wrapperspb.BoolValue,
	args *[]interface{}, position *int) {

	// Implementation for numeric operations...
	// Similar to string operations but for numeric types
}

// Example usage of WhereCondition function
// conditions := map[string]interface{}{
//     "name": &query.StringFilterOperation{
//         Contains: &wrapperspb.StringValue{Value: "test"},
//     },
//     "created_at": &query.DateFilterOperators{
//         Gte: timestamppb.New(time.Now().AddDate(0, -1, 0)), // Last month
//     },
//     "is_active": &query.BooleanFilterOperators{
//         Eq: &wrapperspb.BoolValue{Value: true},
//     },
//     "tenant_ids": []string{"tenant1", "tenant2"},
//     "status": "ACTIVE", // Direct value
// }

// whereClause, args := WhereCondition(conditions, 1, true)
