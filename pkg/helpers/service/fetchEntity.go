package helpers_service

import (
	"context"
	"fmt"

	"p9e.in/samavaya/packages/models"
)

// Fetch entity by ID or UUID
func fetchEntity[T models.Entity](
	ctx context.Context,
	id int64,
	uuid string,
	repoGetByIDFunc func(context.Context, int64) (T, error),
	repoGetByUUIDFunc func(context.Context, string) (T, error),
) (T, error) {
	var entity T
	var err error

	if id > 0 {
		entity, err = repoGetByIDFunc(ctx, id)
	} else if uuid != "" {
		entity, err = repoGetByUUIDFunc(ctx, uuid)
	} else {
		err = fmt.Errorf("missing valid identifier (ID or UUID)")
	}

	return entity, err
}
