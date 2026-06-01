package helpers_repo

import (
	"context"

	"p9e.in/samavaya/packages/database/pgxpostgres/operations"
	"p9e.in/samavaya/packages/deps"
	"p9e.in/samavaya/packages/helpers"
	"p9e.in/samavaya/packages/models"
)

// UpdateEntity updates an entity in the database using the observability middleware.
// Returns a pointer to the updated entity for consistency with Get operations.
func UpdateEntity[T any](ctx context.Context, deps deps.ServiceDeps, tableName string, values T, fieldNames []string, id int64) (*T, error) {
	return helpers.WithObservability(ctx, &deps, "UpdateEntity", func(opCtx *helpers.OperationContext) (*T, error) {
		dm := models.DataModel[T]{
			TableName:  tableName,
			FieldNames: fieldNames,
			Values:     []T{values},
			Where:      "id = $1",
			WhereArgs:  []any{id},
		}

		result, err := operations.ExecuteQuery(
			opCtx.Ctx,
			opCtx.Deps.Pool,
			&dm,
			operations.QueryTypeUpdate,
		)

		if err != nil {
			return nil, err
		}

		return &result, nil
	})
}
