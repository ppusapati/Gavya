package helpers_service

import (
	"context"
	"errors"
	"strconv"
	"time"

	pbr "p9e.in/samavaya/packages/api/v1/response"
	helpers_utils "p9e.in/samavaya/packages/helpers/utils"
	"p9e.in/samavaya/packages/metrics"
	"p9e.in/samavaya/packages/models"
	"p9e.in/samavaya/packages/tracing"
)

// DeleteEntity handles entity deletion with tracing and metrics
func DeleteEntity[T models.Entity](
	ctx context.Context,
	id int64,
	uuid string,
	tracer *tracing.TracingProvider,
	metrics metrics.MetricsProvider,
	repoGetByIDFunc func(context.Context, int64) (models.Entity, error),
	repoGetByUUIDFunc func(context.Context, string) (models.Entity, error),
	repoDeleteFunc func(context.Context, int64) (T, error),
) (*pbr.BaseResponse, error) {
	var zeroValue T
	entityType := helpers_utils.GetTypeName[T]() // Get entity type name
	// Start tracing
	ctx, span := tracer.StartSpan(ctx, "Delete"+entityType)
	defer span.End()

	// Validate ID/UUID
	if id <= 0 && uuid == "" {
		err := errors.New("both ID and UUID are missing")
		tracer.AddSpanError(ctx, err)
		return helpers_utils.ErrorResponse(ctx, "Invalid request: missing ID and UUID", zeroValue, pbr.CanonicalReason_INVALID_REQUEST)
	}

	// Tracing tags
	tags := map[string]string{"operation": entityType + "_deletion"}
	if id > 0 {
		tags["id"] = strconv.FormatInt(id, 10)
	} else {
		tags["uuid"] = uuid
	}
	tracer.AddSpanTags(ctx, tags)

	// Record start time
	startTime := time.Now()

	// Retrieve existing entity
	entity, err := fetchEntity(ctx, id, uuid, repoGetByIDFunc, repoGetByUUIDFunc)
	if err != nil {
		tracer.AddSpanError(ctx, err)
		return helpers_utils.ErrorResponse(ctx, entityType+" not found", zeroValue, pbr.CanonicalReason_NOT_FOUND)
	}

	// Perform deletion
	_, e := repoDeleteFunc(ctx, entity.GetID())
	recordMetric(metrics, entityType, "Delete", startTime, e == nil)

	if e != nil {
		tracer.AddSpanError(ctx, e)
		return helpers_utils.ErrorResponse(ctx, "Failed to delete "+entityType, zeroValue, pbr.CanonicalReason_FORBIDDEN_OPERATION)
	}

	// Return success response
	return helpers_utils.SuccessResponse(ctx, entityType+" deleted successfully", entity, pbr.CanonicalReason_DELETED_SUCCESSFULLY)
}
