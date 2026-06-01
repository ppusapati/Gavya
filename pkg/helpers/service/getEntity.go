package helpers_service

import (
	"context"
	"errors"
	"strconv"
	"time"

	pbr "p9e.in/samavaya/packages/api/v1/response"
	hu "p9e.in/samavaya/packages/helpers/utils"
	"p9e.in/samavaya/packages/metrics"
	"p9e.in/samavaya/packages/models"
	"p9e.in/samavaya/packages/tracing"
)

// Generic GetEntity function to fetch an entity by ID, UUID, or any other field
func GetEntity[T models.Entity, ProtoT any](
	ctx context.Context,
	id int64,
	uuid string,
	// fieldValues map[string]interface{},
	fieldName string, fieldValue any,
	tracer *tracing.TracingProvider,
	metrics metrics.MetricsProvider,
	repoGetByIDFunc func(context.Context, int64) (T, error),
	repoGetByUUIDFunc func(context.Context, string) (T, error),
	repoGetByFieldFunc func(context.Context, string, interface{}) (T, error),
	convertFunc func(T) *ProtoT,
) (*pbr.BaseResponse, *ProtoT, error) {
	var zeroValue T
	entityType := hu.GetTypeName[T]()
	// Start tracing span
	ctx, span := tracer.StartSpan(ctx, "Get"+entityType)
	defer span.End()

	// Validate at least one identifier is provided
	if id <= 0 && uuid == "" && (fieldName == "" || fieldValue == "") {
		err := errors.New("no valid identifier provided")
		tracer.AddSpanError(ctx, err)
		r, e := hu.ErrorResponse(ctx, "Invalid request: missing ID, UUID, or field", zeroValue, pbr.CanonicalReason_INVALID_REQUEST)
		return r, nil, e
	}

	// Tracing tags
	spanTags := map[string]string{"operation": entityType + "_fetch"}
	if id > 0 {
		spanTags["id"] = strconv.FormatInt(id, 10)
	} else if uuid != "" {
		spanTags["uuid"] = uuid
	} else {
		spanTags["field"] = fieldName
		if strValue, ok := fieldValue.(string); ok {
			spanTags["value"] = strValue
		}
	}
	tracer.AddSpanTags(ctx, spanTags)

	// Record start time
	startTime := time.Now()

	// Fetch entity
	var entity T
	var err error
	// Retrieve existing entity
	if id > 0 {
		entity, err = repoGetByIDFunc(ctx, id)
	} else if uuid != "" {
		entity, err = repoGetByUUIDFunc(ctx, uuid)
	} else {
		entity, err = repoGetByFieldFunc(ctx, fieldName, fieldValue)
	}
	if err != nil {
		tracer.AddSpanError(ctx, err)
		r, e := hu.ErrorResponse(ctx, entityType+" Not Found", zeroValue, pbr.CanonicalReason_NOT_FOUND)
		return r, nil, e
	}

	// Record total operation time
	recordMetric(metrics, entityType, "Get Record", startTime, err == nil)

	// Convert entity to Protobuf
	protoEntity := convertFunc(entity)

	// Return success response
	r, e := hu.SuccessResponse(ctx, entityType+" Found Successfully", entity, pbr.CanonicalReason_FOUND_SUCCESSFULLY)
	return r, protoEntity, e
}
