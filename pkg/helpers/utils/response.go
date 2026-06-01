package helpers_utils

import (
	"context"
	"errors"
	"strconv"

	pbr "p9e.in/samavaya/packages/api/v1/response"
	"p9e.in/samavaya/packages/middleware/localize"
	"p9e.in/samavaya/packages/models"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// ============================================================================
// Response helpers
// ============================================================================
//
// These wrap the canonical BaseResponse + Status + CanonicalReason
// contract from packages/api/v1/response. Updated 2026-04-19 after the
// proto was regenerated — the old split between ErrorReason and
// SuccessReason was collapsed into a single CanonicalReason enum that
// carries both outcome families.
//
// OperationResponse (the old split-result message) is gone; every caller
// now returns *pbr.BaseResponse. The Id field switched from int64 → string
// (ULID convention); UUID as a separate field no longer exists. Callers
// that previously passed (id, uuid) collapse both into Id as a string.

// CreateSuccessResponse builds a BaseResponse for success flows.
// reason is the canonical code; message is the human-readable text
// (defaults to reason.String() when empty).
func CreateSuccessResponse(
	ctx context.Context,
	code int32,
	reason pbr.CanonicalReason,
	message string,
	data map[string]interface{},
	id string,
) (*pbr.BaseResponse, error) {
	msgKey := reason.String()
	if message == "" {
		message = msgKey
	}
	return &pbr.BaseResponse{
		Status: &pbr.Status{
			Code:    code,
			Reason:  reason,
			Message: localize.GetMsg(ctx, msgKey, message, data, nil),
		},
		Id:      wrapperspb.String(id),
		Success: true,
		Message: message,
	}, nil
}

// CreateErrorResponse builds a BaseResponse for error flows. Returns the
// response + a Go error so callers can propagate with errors.Is.
func CreateErrorResponse(
	ctx context.Context,
	code int32,
	reason pbr.CanonicalReason,
	message string,
	data map[string]interface{},
) (*pbr.BaseResponse, error) {
	msgKey := reason.String()
	if message == "" {
		message = msgKey
	}
	return &pbr.BaseResponse{
		Status: &pbr.Status{
			Code:    code,
			Reason:  reason,
			Message: localize.GetMsg(ctx, msgKey, message, data, nil),
		},
		Success: false,
		Message: message,
	}, errors.New(msgKey)
}

// ErrorResponse is the generic helper that wraps a typed entity into
// an error response. Call sites pass a CanonicalReason value that
// categorises the failure.
func ErrorResponse[T models.Entity](
	ctx context.Context,
	message string,
	entity T,
	reason pbr.CanonicalReason,
) (*pbr.BaseResponse, error) {
	return CreateErrorResponse(
		ctx,
		int32(reason),
		reason,
		message,
		map[string]interface{}{"Entity": entity},
	)
}

// SuccessResponse is the generic helper that wraps a typed entity into
// a success response. The entity's ID and UUID are folded into the
// BaseResponse's single Id field — numeric IDs get stringified; UUIDs
// are used verbatim when present.
func SuccessResponse[T models.Entity](
	ctx context.Context,
	message string,
	entity T,
	reason pbr.CanonicalReason,
) (*pbr.BaseResponse, error) {
	return CreateSuccessResponse(
		ctx,
		int32(reason),
		reason,
		message,
		nil,
		entityIDString(entity),
	)
}

// SuccessArrayResponse mirrors SuccessResponse for list operations.
// The entity slice is metadata; the response doesn't carry per-row IDs,
// only the operation outcome + message.
func SuccessArrayResponse[T []*models.Entity](
	ctx context.Context,
	message string,
	entities []*T,
	reason pbr.CanonicalReason,
) (*pbr.BaseResponse, error) {
	return CreateSuccessResponse(
		ctx,
		int32(reason),
		reason,
		message,
		nil,
		"",
	)
}

// entityIDString prefers UUID (stable external identifier) over the
// numeric ID; when UUID is empty, falls back to the stringified numeric
// ID. Matches the pre-collapse behaviour where both fields co-existed.
func entityIDString(e models.Entity) string {
	if u := e.GetUUID(); u != "" {
		return u
	}
	if id := e.GetID(); id != 0 {
		return strconv.FormatInt(id, 10)
	}
	return ""
}
