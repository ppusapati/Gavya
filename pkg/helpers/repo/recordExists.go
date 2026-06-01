package helpers_repo

import (
	"context"

	"p9e.in/samavaya/packages/database/pgxpostgres/builder"
	"p9e.in/samavaya/packages/database/pgxpostgres/operations"
	"p9e.in/samavaya/packages/deps"
	"p9e.in/samavaya/packages/models"
	"p9e.in/samavaya/packages/p9log"
)

func RecordExists[T any](ctx context.Context, deps deps.ServiceDeps,
	tableName string, search *models.SearchCriteria) (bool, error) {
	// Create a contextual logger for this function
	lg := p9log.NewHelper(p9log.With(deps.Log, "RecordExists"))
	// Apply timeout, not considered a long query
	tctx, cancel := deps.Tp.ApplyTimeout(ctx, false)
	defer cancel()

	whereClause, args := builder.BuildWhereClause(ctx, search)
	dm := models.DataModel[T]{
		TableName:  tableName,
		FieldNames: []string{"1"},
		Where:      whereClause,
		WhereArgs:  []any{args},
		Limit:      1,
	}

	_, err := operations.ExecuteQuery(
		tctx,
		deps.Pool,
		&dm,
		operations.QueryTypeSelect,
	)

	if err != nil {
		// Log timeout or cancellation
		if tctx.Err() == context.DeadlineExceeded {
			lg.Errorf("record existence operation timed out")
		}
		lg.Errorf("failed to check record existence: %v", err)
		return false, err
	}

	return true, nil
}
