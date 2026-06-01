package helpers_service

import (
	"context"
	"time"

	pbr "p9e.in/samavaya/packages/api/v1/response"
	hu "p9e.in/samavaya/packages/helpers/utils"
	"p9e.in/samavaya/packages/metrics"
	"p9e.in/samavaya/packages/models"
	"p9e.in/samavaya/packages/tracing"

	"google.golang.org/protobuf/proto"
)

// ListEntity is a generic function for listing entities
func ListEntity[
	T models.Entity,
	P proto.Message,
	ProtoT proto.Message,
](
	ctx context.Context,
	req P,
	tracer *tracing.TracingProvider,
	metrics metrics.MetricsProvider,
	repoEntityFunc func(P) *models.SearchCriteria,
	convertFunc func(T) ProtoT,
	repoListFunc func(context.Context, *models.SearchCriteria) ([]T, error),
) (*pbr.BaseResponse, []ProtoT, error) {
	var zeroValue T
	var zeroProtoT []ProtoT
	entityType := hu.GetTypeName[T]()

	// Start tracing
	ctx, span := tracer.StartSpan(ctx, "List"+entityType)
	defer span.End()
	tracer.AddSpanTags(ctx, map[string]string{"operation": entityType + "_list"})

	// Validate request
	if err := hu.ValidateProto(req); err != nil {
		errorResponse, err := hu.ErrorResponse(ctx, entityType+" validation failed", zeroValue, pbr.CanonicalReason_VALIDATION_FAILED)

		return errorResponse, zeroProtoT, err
	}

	search := repoEntityFunc(req)

	startTime := time.Now()
	entities, err := repoListFunc(ctx, search)
	recordMetric(metrics, entityType, "List", startTime, err == nil)

	if err != nil {
		errres, errs := hu.ErrorResponse(ctx, "Failed to list "+entityType, zeroValue, pbr.CanonicalReason_INVALID_REQUEST)
		return errres, zeroProtoT, errs
	}

	// Convert entities to Protobuf
	protoEntities := make([]ProtoT, len(entities))
	for i, e := range entities {
		protoEntities[i] = convertFunc(e)
	}

	r, e := hu.SuccessResponse(ctx, entityType+" listed successfully", models.Entity(entities[0]), pbr.CanonicalReason_FOUND_SUCCESSFULLY)

	return r, protoEntities, e
}
