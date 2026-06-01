package helpers_repo

import (
	"context"

	"p9e.in/samavaya/packages/database/pgxpostgres/operations"
	"p9e.in/samavaya/packages/deps"
	"p9e.in/samavaya/packages/helpers"
	"p9e.in/samavaya/packages/models"
)

// CountQuery counts entities in a table using the observability middleware.
// Returns a count value (typically int64), not an entity pointer.
func CountQuery[T any](ctx context.Context, deps deps.ServiceDeps, tableName string) (T, error) {
	return helpers.WithObservability(ctx, &deps, "CountQuery", func(opCtx *helpers.OperationContext) (T, error) {
		dm := models.DataModel[T]{
			TableName: tableName,
		}

		result, err := operations.ExecuteQuery(
			opCtx.Ctx,
			opCtx.Deps.Pool,
			&dm,
			operations.QueryTypeCount,
		)

		if err != nil {
			var zero T
			return zero, err
		}

		return result, nil
	})
}
