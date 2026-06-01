package helpers_service

import (
	"context"
	"time"

	pbr "p9e.in/samavaya/packages/api/v1/response"
	hu "p9e.in/samavaya/packages/helpers/utils"
	"p9e.in/samavaya/packages/metrics"
	"p9e.in/samavaya/packages/models"
	"p9e.in/samavaya/packages/p9context"
	"p9e.in/samavaya/packages/tracing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// Generic UpdateEntity function
func UpdateEntity[T models.Entity, P proto.Message](
	ctx context.Context,
	req P,
	id int64,
	uuid string,
	updateMask *fieldmaskpb.FieldMask,
	convertFunc func(P) T, // Converts Protobuf to DB model
	toProtoFunc func(T) P, // Converts DB model to Protobuf
	tracer *tracing.TracingProvider,
	metrics metrics.MetricsProvider,
	repoGetByIDFunc func(context.Context, int64) (T, error),
	repoGetByUUIDFunc func(context.Context, string) (T, error),
	repoUpdateFunc func(context.Context, hu.UpdateRequest[T]) (T, error),
) (*pbr.BaseResponse, error) {
	var zeroValue T                   // Declare zero value once
	entityType := hu.GetTypeName[T]() // Get entity name

	// Start tracing
	ctx, span := tracer.StartSpan(ctx, "Update"+entityType)
	defer span.End()
	tracer.AddSpanTags(ctx, map[string]string{"operation": entityType + "_update"})

	// Validate request
	if err := hu.ValidateProto(req); err != nil {
		return hu.ErrorResponse(ctx, entityType+" validation failed", zeroValue, pbr.CanonicalReason_VALIDATION_FAILED)
	}

	// Retrieve existing entity
	existingEntity, err := fetchEntity(ctx, id, uuid, repoGetByIDFunc, repoGetByUUIDFunc)
	if err != nil {
		return hu.ErrorResponse(ctx, entityType+" not found", zeroValue, pbr.CanonicalReason_NOT_FOUND)
	}

	// Apply field mask if provided
	if updateMask != nil {
		hu.ApplyFieldMask(updateMask, toProtoFunc(existingEntity), req)
	}

	var fieldMaskPaths []string
	if updateMask != nil {
		fieldMaskPaths = updateMask.Paths
	}
	// Convert updated Protobuf model back to DB model
	dbModel := convertFunc(req)

	// Create update request with field mask
	// Get user from security context
	secCtx := p9context.GetSecurityContextOrDefault(ctx)
	updateReq := hu.UpdateRequest[T]{
		Entity:    dbModel,
		FieldMask: fieldMaskPaths,
		UpdatedBy: secCtx.Username,
		UpdatedAt: time.Now(),
	}
	// Perform update operation with metrics
	startTime := time.Now()
	updatedEntity, err := repoUpdateFunc(ctx, updateReq)
	recordMetric(metrics, entityType, "Update", startTime, err == nil)

	if err != nil {
		return hu.ErrorResponse(ctx, "Failed to update "+entityType, zeroValue, pbr.CanonicalReason_UPDATE_FAILED)
	}

	return hu.SuccessResponse(ctx, entityType+" updated successfully", updatedEntity, pbr.CanonicalReason_UPDATED_SUCCESSFULLY)
}
