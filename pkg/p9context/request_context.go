package p9context

import (
	"context"

	"p9e.in/samavaya/packages/ulid"
)

// RequestContext contains request-level tracing and logging information.
// This is set by the request ID middleware at the start of each request.
type RequestContext struct {
	RequestID string
	TraceID   string
	SpanID    string
	ClientIP  string
	UserAgent string
	Method    string
	Path      string
}

type requestContextKey struct{}

// NewRequestContext creates a new context with the request context.
func NewRequestContext(ctx context.Context, req RequestContext) context.Context {
	return context.WithValue(ctx, requestContextKey{}, &req)
}

// FromRequestContext retrieves the request context from context.
// Returns nil and false if not present.
func FromRequestContext(ctx context.Context) (*RequestContext, bool) {
	v, ok := ctx.Value(requestContextKey{}).(*RequestContext)
	if ok && v != nil {
		return v, true
	}
	return nil, false
}

// MustRequestContext retrieves the request context from context.
// Panics if not present.
func MustRequestContext(ctx context.Context) RequestContext {
	v, ok := FromRequestContext(ctx)
	if !ok || v == nil {
		panic("request context not found in context")
	}
	return *v
}

// RequestID retrieves the request ID from context.
// Returns empty string if not present.
func RequestID(ctx context.Context) string {
	if req, ok := FromRequestContext(ctx); ok {
		return req.RequestID
	}
	return ""
}

// TraceID retrieves the trace ID from context.
// Returns empty string if not present.
func TraceID(ctx context.Context) string {
	if req, ok := FromRequestContext(ctx); ok {
		return req.TraceID
	}
	return ""
}

// SpanID retrieves the span ID from context.
// Returns empty string if not present.
func SpanID(ctx context.Context) string {
	if req, ok := FromRequestContext(ctx); ok {
		return req.SpanID
	}
	return ""
}

// ClientIP retrieves the client IP from context.
// Returns empty string if not present.
func ClientIP(ctx context.Context) string {
	if req, ok := FromRequestContext(ctx); ok {
		return req.ClientIP
	}
	return ""
}

// RequestUserAgent retrieves the user agent from request context.
// Returns empty string if not present.
func RequestUserAgent(ctx context.Context) string {
	if req, ok := FromRequestContext(ctx); ok {
		return req.UserAgent
	}
	return ""
}

// RequestMethod retrieves the HTTP/gRPC method from context.
// Returns empty string if not present.
func RequestMethod(ctx context.Context) string {
	if req, ok := FromRequestContext(ctx); ok {
		return req.Method
	}
	return ""
}

// RequestPath retrieves the request path from context.
// Returns empty string if not present.
func RequestPath(ctx context.Context) string {
	if req, ok := FromRequestContext(ctx); ok {
		return req.Path
	}
	return ""
}

// HasRequestContext returns true if the context has request context set.
func HasRequestContext(ctx context.Context) bool {
	_, ok := FromRequestContext(ctx)
	return ok
}

// RequestIDOrGenerate retrieves the request ID from context, or generates a new one.
// Useful for ensuring a request ID always exists.
func RequestIDOrGenerate(ctx context.Context) string {
	if reqID := RequestID(ctx); reqID != "" {
		return reqID
	}
	return ulid.NewString()
}

// TraceIDOrGenerate retrieves the trace ID from context, or generates a new one.
// Useful for ensuring a trace ID always exists.
func TraceIDOrGenerate(ctx context.Context) string {
	if traceID := TraceID(ctx); traceID != "" {
		return traceID
	}
	return ulid.NewString()
}

// LogFields returns a map of request context fields suitable for structured logging.
// Returns an empty map if request context is not present.
func LogFields(ctx context.Context) map[string]string {
	req, ok := FromRequestContext(ctx)
	if !ok || req == nil {
		return map[string]string{}
	}

	fields := make(map[string]string)
	if req.RequestID != "" {
		fields["request_id"] = req.RequestID
	}
	if req.TraceID != "" {
		fields["trace_id"] = req.TraceID
	}
	if req.SpanID != "" {
		fields["span_id"] = req.SpanID
	}
	if req.Method != "" {
		fields["method"] = req.Method
	}
	if req.Path != "" {
		fields["path"] = req.Path
	}
	if req.ClientIP != "" {
		fields["client_ip"] = req.ClientIP
	}
	return fields
}
