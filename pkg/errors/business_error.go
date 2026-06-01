package errors

import "fmt"

// NewBusinessError creates a domain-specific BadRequest-style error with a
// caller-defined reason code and printf-style message.
//
// This convenience constructor exists for service layers that emit a wide
// variety of business validation errors and want to attach a stable reason
// code without repeating the BadRequest("CODE", fmt.Sprintf(...)) boilerplate.
//
// TODO: Consider mapping reasons to dedicated HTTP/gRPC codes when business
// errors need richer status semantics than 400 BadRequest.
func NewBusinessError(reason, format string, args ...interface{}) *Error {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	return New(400, reason, msg)
}
