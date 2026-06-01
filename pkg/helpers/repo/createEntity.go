package helpers_repo

import (
	"context"

	"p9e.in/samavaya/packages/database/pgxpostgres/operations"
	"p9e.in/samavaya/packages/deps"
	"p9e.in/samavaya/packages/helpers"
	"p9e.in/samavaya/packages/models"
)

// CreateEntity creates a new entity in the database using the observability middleware.
// Returns a pointer to the created entity for consistency with Get operations.
func CreateEntity[T any](ctx context.Context, deps deps.ServiceDeps,
	tableName string, entity T, fieldNames []string, conflictColumns []string) (*T, error) {
	return helpers.WithObservability(ctx, &deps, "CreateEntity", func(opCtx *helpers.OperationContext) (*T, error) {
		dm := models.DataModel[T]{
			TableName:       tableName,
			FieldNames:      fieldNames,
			Values:          []T{entity},
			ConflictColumns: conflictColumns,
		}

		result, err := operations.ExecuteQuery(
			opCtx.Ctx,
			opCtx.Deps.Pool,
			&dm,
			operations.QueryTypeInsert,
		)

		if err != nil {
			return nil, err
		}

		return &result, nil
	})
}
