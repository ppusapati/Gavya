package builder

import (
	"fmt"
	"strings"

	"p9e.in/samavaya/packages/api/v1/query"
	"p9e.in/samavaya/packages/models"
)

// Helper function to handle a filter and append it to the query
func HandleFilter(builder *strings.Builder, filter models.Filter, args *[]interface{}, position *int) {
	switch filter.Operator {
	case models.OperatorIn:
		values := filter.Value.([]interface{})
		placeholders := make([]string, len(values))
		for i, val := range values {
			placeholders[i] = fmt.Sprintf("$%d", *position)
			*args = append(*args, val)
			*position++
		}
		builder.WriteString(fmt.Sprintf("%s IN (%s)", filter.Field, strings.Join(placeholders, ",")))
	case models.OperatorIsNull, models.OperatorIsNotNull:
		builder.WriteString(fmt.Sprintf("%s %s", filter.Field, filter.Operator))
	default:
		builder.WriteString(fmt.Sprintf("%s %s $%d", filter.Field, filter.Operator, *position))
		*args = append(*args, filter.Value)
		*position++
	}
}

// ConvertStringFilter converts a StringFilterOperation to a slice of Filters
func ConvertStringFilter(field string, op *query.StringFilterOperation) []models.Filter {
	if op == nil {
		return nil
	}

	var filters []models.Filter

	// Handle each possible string operation
	if op.Eq != nil {
		filters = append(filters, models.Filter{
			Field:    field,
			Operator: models.OperatorEquals,
			Value:    op.Eq.Value,
		})
	}
	if op.Neq != nil {
		filters = append(filters, models.Filter{
			Field:    field,
			Operator: models.OperatorNotEquals,
			Value:    op.Neq.Value,
		})
	}
	if op.Contains != nil {
		filters = append(filters, models.Filter{
			Field:    field,
			Operator: models.OperatorContains,
			Value:    "%" + op.Contains.Value + "%",
		})
	}
	if op.StartsWith != nil {
		filters = append(filters, models.Filter{
			Field:    field,
			Operator: models.OperatorStartsWith,
			Value:    op.StartsWith.Value + "%",
		})
	}
	if op.EndsWith != nil {
		filters = append(filters, models.Filter{
			Field:    field,
			Operator: models.OperatorEndsWith,
			Value:    "%" + op.EndsWith.Value,
		})
	}
	if len(op.In) > 0 {
		inValues := make([]interface{}, len(op.In))
		for i, inVal := range op.In {
			inValues[i] = inVal.Value
		}
		filters = append(filters, models.Filter{
			Field:    field,
			Operator: models.OperatorIn,
			Value:    inValues,
		})
	}
	if op.Null != nil && op.Null.Value {
		filters = append(filters, models.Filter{
			Field:    field,
			Operator: models.OperatorIsNull,
		})
	}
	if op.Nnull != nil && op.Nnull.Value {
		filters = append(filters, models.Filter{
			Field:    field,
			Operator: models.OperatorIsNotNull,
		})
	}

	return filters
}
