package helpers_repo

import (
	"context"
	"errors"

	"p9e.in/samavaya/packages/database/pgxpostgres/operations"
	"p9e.in/samavaya/packages/deps"
	"p9e.in/samavaya/packages/helpers"
	ph "p9e.in/samavaya/packages/helpers/utils"
	"p9e.in/samavaya/packages/models"
)

// GetByID retrieves an entity by its ID using the observability middleware.
// This eliminates boilerplate for timeout, logging, metrics, and tracing.
func GetByID[T any](ctx context.Context, deps deps.ServiceDeps, tableName string, identifier int64) (*T, error) {
	return helpers.WithObservability(ctx, &deps, "GetByID", func(opCtx *helpers.OperationContext) (*T, error) {
		dm := models.DataModel[T]{
			TableName:  tableName,
			Where:      "id = $1",
			WhereArgs:  []any{identifier},
			FieldNames: []string{"*"},
		}

		result, err := operations.ExecuteQuery(
			opCtx.Ctx,
			opCtx.Deps.Pool,
			&dm,
			operations.QueryTypeSelect,
		)

		if err != nil {
			return nil, err
		}

		return &result, nil
	})
}

// GetByUUID retrieves an entity by its UUID using the observability middleware.
func GetByUUID[T any](ctx context.Context, deps deps.ServiceDeps, tableName string, identifier string) (*T, error) {
	return helpers.WithObservability(ctx, &deps, "GetByUUID", func(opCtx *helpers.OperationContext) (*T, error) {
		dm := models.DataModel[T]{
			TableName:  tableName,
			Where:      "uuid = $1",
			WhereArgs:  []any{identifier},
			FieldNames: []string{"*"},
		}

		result, err := operations.ExecuteQuery(
			opCtx.Ctx,
			opCtx.Deps.Pool,
			&dm,
			operations.QueryTypeSelect,
		)

		if err != nil {
			return nil, err
		}

		return &result, nil
	})
}

// GetByField retrieves an entity by a custom field using a DataModel.
// Returns a pointer to the entity for consistency with other Get operations.
func GetByField[T any](ctx context.Context, deps deps.ServiceDeps, dm models.DataModel[T]) (*T, error) {
	return helpers.WithObservability(ctx, &deps, "GetByField", func(opCtx *helpers.OperationContext) (*T, error) {
		result, err := operations.ExecuteQuery(
			opCtx.Ctx,
			opCtx.Deps.Pool,
			&dm,
			operations.QueryTypeSelect,
		)

		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, context.DeadlineExceeded
			}
			return nil, err
		}

		return &result, nil
	})
}

// GetByIdentifier retrieves an entity by either ID or UUID from an Identifier struct.
// Prioritizes UUID if present, otherwise uses ID.
func GetByIdentifier[T any](ctx context.Context, deps deps.ServiceDeps,
	tableName string, identifier *models.Identifier) (*T, error) {

	return helpers.WithObservability(ctx, &deps, "GetByIdentifier", func(opCtx *helpers.OperationContext) (*T, error) {
		entityType := ph.GetTypeName[T]()

		// Validate identifier
		if identifier.Id == 0 && identifier.Uuid == "" {
			opCtx.Logger.Errorf("%s: no valid identifier provided", entityType)
			return nil, errors.New("no valid identifier provided")
		}

		dm := models.DataModel[T]{
			TableName: tableName,
		}

		// Prioritize Uuid if it's non-empty
		if identifier.Uuid != "" {
			dm.Where = "uuid = $1"
			dm.WhereArgs = []any{identifier.Uuid}
		} else {
			// Fallback to Id if Uuid is empty
			dm.Where = "id = $1"
			dm.WhereArgs = []any{identifier.Id}
		}

		result, err := operations.ExecuteQuery(
			opCtx.Ctx,
			opCtx.Deps.Pool,
			&dm,
			operations.QueryTypeSelect,
		)

		if err != nil {
			return nil, err
		}

		return &result, nil
	})
}
