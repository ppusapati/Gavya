// nolint:gomnd
package errors

import "fmt"

// BadRequest new BadRequest error that is mapped to a 400 response.
func BadRequest(reason, message string) *Error {
	return New(400, reason, message)
}

// IsBadRequest determines if err is an error which indicates a BadRequest error.
// It supports wrapped errors.
func IsBadRequest(err error) bool {
	return Code(err) == 400
}

// Unauthorized new Unauthorized error that is mapped to a 401 response.
func Unauthorized(reason, message string) *Error {
	return New(401, reason, message)
}

// IsUnauthorized determines if err is an error which indicates an Unauthorized error.
// It supports wrapped errors.
func IsUnauthorized(err error) bool {
	return Code(err) == 401
}

// Forbidden new Forbidden error that is mapped to a 403 response.
func Forbidden(reason, message string) *Error {
	return New(403, reason, message)
}

// IsForbidden determines if err is an error which indicates a Forbidden error.
// It supports wrapped errors.
func IsForbidden(err error) bool {
	return Code(err) == 403
}

// NotFound new NotFound error that is mapped to a 404 response.
func NotFound(reason, message string) *Error {
	return New(404, reason, message)
}

// IsNotFound determines if err is an error which indicates an NotFound error.
// It supports wrapped errors.
func IsNotFound(err error) bool {
	return Code(err) == 404
}

// Conflict new Conflict error that is mapped to a 409 response.
func Conflict(reason, message string) *Error {
	return New(409, reason, message)
}

// IsConflict determines if err is an error which indicates a Conflict error.
// It supports wrapped errors.
func IsConflict(err error) bool {
	return Code(err) == 409
}

// Internal creates an Internal error mapped to a 500 response.
// Supports fmt.Sprintf-style formatting.
func Internal(format string, args ...interface{}) *Error {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	return New(500, "INTERNAL", msg)
}

// AlreadyExists creates an AlreadyExists error mapped to a 409 response.
// Supports fmt.Sprintf-style formatting.
func AlreadyExists(format string, args ...interface{}) *Error {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	return New(409, "ALREADY_EXISTS", msg)
}

// InternalServer new InternalServer error that is mapped to a 500 response.
func InternalServer(reason, message string) *Error {
	return New(500, reason, message)
}

// IsInternalServer determines if err is an error which indicates an Internal error.
// It supports wrapped errors.
func IsInternalServer(err error) bool {
	return Code(err) == 500
}

// ServiceUnavailable new ServiceUnavailable error that is mapped to an HTTP 503 response.
func ServiceUnavailable(reason, message string) *Error {
	return New(503, reason, message)
}

// IsServiceUnavailable determines if err is an error which indicates an Unavailable error.
// It supports wrapped errors.
func IsServiceUnavailable(err error) bool {
	return Code(err) == 503
}

// GatewayTimeout new GatewayTimeout error that is mapped to an HTTP 504 response.
func GatewayTimeout(reason, message string) *Error {
	return New(504, reason, message)
}

// IsGatewayTimeout determines if err is an error which indicates a GatewayTimeout error.
// It supports wrapped errors.
func IsGatewayTimeout(err error) bool {
	return Code(err) == 504
}

// NewValidation creates a validation error mapped to a 400 response.
func NewValidation(reason, message string) *Error {
	return New(400, reason, message)
}

// ClientClosed new ClientClosed error that is mapped to an HTTP 499 response.
func ClientClosed(reason, message string) *Error {
	return New(499, reason, message)
}

// IsClientClosed determines if err is an error which indicates a IsClientClosed error.
// It supports wrapped errors.
func IsClientClosed(err error) bool {
	return Code(err) == 499
}

// Error code constants for common database and domain errors.
const (
	CodeDatabaseError = "DATABASE_ERROR"
	CodeDomainError   = "DOMAIN_ERROR"
)

// WrapNotFoundOrDBError wraps a database error as either NotFound (if no rows) or InternalServer.
// Accepts optional extra context strings (e.g., entity ID).
func WrapNotFoundOrDBError(err error, entityName string, context ...string) error {
	if err == nil {
		return nil
	}
	id := ""
	if len(context) > 0 {
		id = context[0]
	}
	if err.Error() == "no rows in result set" || err.Error() == "sql: no rows in result set" {
		if id != "" {
			return NotFound("NOT_FOUND", fmt.Sprintf("%s not found: %s", entityName, id))
		}
		return NotFound("NOT_FOUND", fmt.Sprintf("%s not found", entityName))
	}
	if id != "" {
		return InternalServer(CodeDatabaseError, fmt.Sprintf("database error for %s (%s): %v", entityName, id, err))
	}
	return InternalServer(CodeDatabaseError, fmt.Sprintf("database error for %s: %v", entityName, err))
}

// WrapDomainError wraps an error as an InternalServer domain error.
// Usage: WrapDomainError(code, message, err)
func WrapDomainError(code string, message string, err error) error {
	if err == nil {
		return nil
	}
	return InternalServer(code, fmt.Sprintf("%s: %v", message, err))
}

// NewDomainError creates a domain error mapped to a 400 response.
// Supports fmt.Sprintf-style formatting.
func NewDomainError(code string, format string, args ...interface{}) *Error {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	return New(400, code, msg)
}

// NewServiceUnavailable creates a ServiceUnavailable error.
func NewServiceUnavailable(reason, message string) *Error {
	return New(503, reason, message)
}

// NotImplemented creates a NotImplemented error mapped to a 501 response.
func NotImplemented(reason, message string) *Error {
	return New(501, reason, message)
}
