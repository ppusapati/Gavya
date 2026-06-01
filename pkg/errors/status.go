package errors

import (
	"net/http"

	"connectrpc.com/connect"
	"google.golang.org/grpc/codes"
)

// Converter is a status converter.
type Converter interface {
	// ToGRPCCode converts an HTTP error code into the corresponding gRPC response status.
	ToGRPCCode(code int) codes.Code

	// FromGRPCCode converts a gRPC error code into the corresponding HTTP response status.
	FromGRPCCode(code codes.Code) int
}

type statusConverter struct{}

// DefaultConverter default converter.
var DefaultConverter Converter = statusConverter{}

// ToGRPCCode converts an HTTP error code into the corresponding gRPC response status.
// See: https://github.com/googleapis/googleapis/blob/master/google/rpc/code.proto
func (c statusConverter) ToGRPCCode(code int) codes.Code {
	switch code {
	case http.StatusOK:
		return codes.OK
	case http.StatusBadRequest:
		return codes.InvalidArgument
	case http.StatusUnauthorized:
		return codes.Unauthenticated
	case http.StatusForbidden:
		return codes.PermissionDenied
	case http.StatusNotFound:
		return codes.NotFound
	case http.StatusConflict:
		return codes.Aborted
	case http.StatusTooManyRequests:
		return codes.ResourceExhausted
	case http.StatusInternalServerError:
		return codes.Internal
	case http.StatusNotImplemented:
		return codes.Unimplemented
	case http.StatusServiceUnavailable:
		return codes.Unavailable
	case http.StatusGatewayTimeout:
		return codes.DeadlineExceeded
	}
	return codes.Unknown
}

// FromGRPCCode converts a gRPC error code into the corresponding HTTP response status.
// See: https://github.com/googleapis/googleapis/blob/master/google/rpc/code.proto
func (c statusConverter) FromGRPCCode(code codes.Code) int {
	switch code {
	case codes.OK:
		return http.StatusOK
	case codes.Unknown:
		return http.StatusInternalServerError
	case codes.InvalidArgument:
		return http.StatusBadRequest
	case codes.DeadlineExceeded:
		return http.StatusGatewayTimeout
	case codes.NotFound:
		return http.StatusNotFound
	case codes.AlreadyExists:
		return http.StatusConflict
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests
	case codes.FailedPrecondition:
		return http.StatusBadRequest
	case codes.Aborted:
		return http.StatusConflict
	case codes.OutOfRange:
		return http.StatusBadRequest
	case codes.Unimplemented:
		return http.StatusNotImplemented
	case codes.Internal:
		return http.StatusInternalServerError
	case codes.Unavailable:
		return http.StatusServiceUnavailable
	case codes.DataLoss:
		return http.StatusInternalServerError
	}
	return http.StatusInternalServerError
}

// ToGRPCCode converts an HTTP error code into the corresponding gRPC response status.
func ToGRPCCode(code int) codes.Code {
	return DefaultConverter.ToGRPCCode(code)
}

// FromGRPCCode converts a gRPC error code into the corresponding HTTP response status.
func FromGRPCCode(code codes.Code) int {
	return DefaultConverter.FromGRPCCode(code)
}

// ToConnectCode converts an HTTP error code into the corresponding
// ConnectRPC code. Connect codes are numerically aligned with gRPC
// codes by design (CodeInvalidArgument=3, CodeNotFound=5, ...), so
// the mapping is a single chained lookup through the established
// HTTP↔gRPC table — a separate switch would duplicate that table.
//
// 2026-04-27 (Audit #4 of base-domain audits, docs/BASE_DOMAIN_AUDITS.md):
// added so ToConnectError can hand ConnectRPC the right Code.
// Without this mapping, BadRequest/NotFound/Unauthorized errors
// surfaced as HTTP 500 + connect.CodeUnknown because the *Error
// type was returned bare and ConnectRPC couldn't classify it.
func ToConnectCode(httpCode int) connect.Code {
	switch ToGRPCCode(httpCode) {
	case codes.OK:
		return 0
	case codes.Canceled:
		return connect.CodeCanceled
	case codes.Unknown:
		return connect.CodeUnknown
	case codes.InvalidArgument:
		return connect.CodeInvalidArgument
	case codes.DeadlineExceeded:
		return connect.CodeDeadlineExceeded
	case codes.NotFound:
		return connect.CodeNotFound
	case codes.AlreadyExists:
		return connect.CodeAlreadyExists
	case codes.PermissionDenied:
		return connect.CodePermissionDenied
	case codes.ResourceExhausted:
		return connect.CodeResourceExhausted
	case codes.FailedPrecondition:
		return connect.CodeFailedPrecondition
	case codes.Aborted:
		return connect.CodeAborted
	case codes.OutOfRange:
		return connect.CodeOutOfRange
	case codes.Unimplemented:
		return connect.CodeUnimplemented
	case codes.Internal:
		return connect.CodeInternal
	case codes.Unavailable:
		return connect.CodeUnavailable
	case codes.DataLoss:
		return connect.CodeDataLoss
	case codes.Unauthenticated:
		return connect.CodeUnauthenticated
	}
	return connect.CodeUnknown
}
