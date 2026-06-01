package helpers_repo

import (
	"context"

	"p9e.in/samavaya/packages/database/pgxpostgres/operations"
	"p9e.in/samavaya/packages/deps"
	"p9e.in/samavaya/packages/helpers"
	"p9e.in/samavaya/packages/models"
)

// DeleteEntity deletes an entity from the database using the observability middleware.
// Returns a pointer to the deleted entity for audit purposes.
func DeleteEntity[T any](ctx context.Context, deps deps.ServiceDeps, tableName string, id int64) (*T, error) {
	return helpers.WithObservability(ctx, &deps, "DeleteEntity", func(opCtx *helpers.OperationContext) (*T, error) {
		dm := models.DataModel[T]{
			TableName: tableName,
			Where:     "id = $1",
			WhereArgs: []any{id},
		}

		result, err := operations.ExecuteQuery(
			opCtx.Ctx,
			opCtx.Deps.Pool,
			&dm,
			operations.QueryTypeDelete,
		)

		if err != nil {
			return nil, err
		}

		return &result, nil
	})
}
