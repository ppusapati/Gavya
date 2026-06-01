package helpers_repo

import (
	"context"
	"strings"

	"p9e.in/samavaya/packages/database/pgxpostgres/builder"
	"p9e.in/samavaya/packages/database/pgxpostgres/operations"
	"p9e.in/samavaya/packages/deps"
	"p9e.in/samavaya/packages/helpers"
	"p9e.in/samavaya/packages/models"
)

// ListEntity retrieves a list of entities matching search criteria using the observability middleware.
// The extractFunc is used to convert FieldMask paths to database field names.
func ListEntity[T any](ctx context.Context, deps deps.ServiceDeps, tableName string, search *models.SearchCriteria,
	extractFunc func([]string) ([]string, error)) ([]*T, error) {

	return helpers.WithObservability(ctx, &deps, "ListEntity", func(opCtx *helpers.OperationContext) ([]*T, error) {
		// Build where clause dynamically based on search criteria
		whereClause, args := builder.BuildWhereClause(opCtx.Ctx, search)

		// Determine which fields to select based on FieldMask
		var fieldNames []string
		if search.FieldMask != nil && len(search.FieldMask.GetPaths()) > 0 {
			// Use field mask if provided
			fieldNames, _ = extractFunc(search.FieldMask.GetPaths())
		} else {
			// No field mask provided, select all fields
			fieldNames = []string{"*"}
		}

		dm := models.DataModel[T]{
			TableName:  tableName,
			FieldNames: fieldNames,
			Where:      whereClause,
			WhereArgs:  args,
			OrderBy:    strings.Join(search.Sort, ", "),
			Limit:      search.PageSize,
			Offset:     search.PageOffset,
		}

		result, err := operations.ExecuteQuerySlice(
			opCtx.Ctx,
			opCtx.Deps.Pool,
			&dm,
			operations.QueryTypeSelect,
		)

		if err != nil {
			return nil, err
		}

		// Convert result to []*T
		convertedResult := make([]*T, len(result))
		for i := range result {
			convertedResult[i] = &result[i]
		}

		return convertedResult, nil
	})
}
