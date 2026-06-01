// Package errors is the structured error layer used across the platform.
//
// Every error carries:
//
//   - Code (int32) — maps to gRPC status / HTTP code at the transport boundary
//   - Reason (string) — stable machine-readable identifier (e.g. "BI_DATASET_NOT_FOUND")
//   - Message (string) — human-readable detail
//   - Metadata (map[string]string) — optional key/value context
//
// Constructors:
//
//   - New(code, reason, message)      — base form
//   - Newf(code, reason, format, …)   — with fmt-style formatting
//   - NewBusinessError(reason, …)     — 400-class domain error shortcut
//   - BadRequest / NotFound / Internal — named status constructors (types.go)
//   - Wrap(err, message)              — wraps an existing error;
//                                        returns nil when err is nil —
//                                        DO NOT use to construct fresh
//                                        sentinels (memory: feedback_errors_wrap_nil)
//
// Errors flow through transports:
//
//   - FromError(err) inspects any error and returns an *Error, mapping
//     arbitrary errors to their Code/Reason surface. Callers rarely use this
//     directly — the transport layer converts at the boundary.
//
// See also: cmd/protoc-gen-go-errors which generates typed error sentinels
// from ErrorReason proto enums (currently stubbed — see errors package
// below in packages/api/v1/errors/errors.go for the plugin's descriptor
// stubs and the B.1 sweep rationale).
package errors
