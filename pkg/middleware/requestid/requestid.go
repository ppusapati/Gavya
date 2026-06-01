package requestid

import (
	"context"
	"net/http"
	"strings"

	"p9e.in/samavaya/packages/ulid"
	"p9e.in/samavaya/packages/p9context"
	"p9e.in/samavaya/packages/p9log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const (
	// RequestIDHeader is the HTTP header for request ID
	RequestIDHeader = "X-Request-ID"
	// TraceIDHeader is the HTTP header for distributed trace ID
	TraceIDHeader = "X-Trace-ID"
	// SpanIDHeader is the HTTP header for distributed span ID
	SpanIDHeader = "X-Span-ID"
	// ForwardedForHeader is the HTTP header for client IP
	ForwardedForHeader = "X-Forwarded-For"
	// RealIPHeader is the HTTP header for real client IP
	RealIPHeader = "X-Real-IP"
)

// RequestIDMiddleware provides request ID generation and tracking for gRPC and HTTP services.
type RequestIDMiddleware struct {
	// generateIfMissing controls whether to generate IDs if not provided in headers.
	generateIfMissing bool
}

// RequestIDMiddlewareOption is a functional option for configuring RequestIDMiddleware.
type RequestIDMiddlewareOption func(*RequestIDMiddleware)

// WithGenerateIfMissing sets whether to generate IDs if not provided.
func WithGenerateIfMissing(generate bool) RequestIDMiddlewareOption {
	return func(m *RequestIDMiddleware) {
		m.generateIfMissing = generate
	}
}

// NewRequestIDMiddleware creates a new request ID middleware.
func NewRequestIDMiddleware(opts ...RequestIDMiddlewareOption) *RequestIDMiddleware {
	m := &RequestIDMiddleware{
		generateIfMissing: true, // Default: generate if missing
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// GrpcRequestIDMiddleware is a gRPC unary interceptor that sets request context.
// It extracts or generates request ID, trace ID, and captures client info.
func (m *RequestIDMiddleware) GrpcRequestIDMiddleware(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	var requestID, traceID, spanID, clientIP, userAgent string

	// Extract from incoming metadata
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		requestID = getFirstValue(md, "x-request-id")
		traceID = getFirstValue(md, "x-trace-id")
		spanID = getFirstValue(md, "x-span-id")
		clientIP = extractClientIP(md)
		userAgent = getFirstValue(md, "user-agent")
	}

	// Generate if missing
	if m.generateIfMissing {
		if requestID == "" {
			requestID = ulid.NewString()
		}
		if traceID == "" {
			traceID = requestID // Use request ID as trace ID if not provided
		}
		if spanID == "" {
			spanID = ulid.NewString()
		}
	}

	// Set request context
	ctx = p9context.NewRequestContext(ctx, p9context.RequestContext{
		RequestID: requestID,
		TraceID:   traceID,
		SpanID:    spanID,
		ClientIP:  clientIP,
		UserAgent: userAgent,
		Method:    info.FullMethod,
		Path:      info.FullMethod,
	})

	// Add to outgoing metadata for downstream services
	ctx = metadata.AppendToOutgoingContext(ctx,
		"x-request-id", requestID,
		"x-trace-id", traceID,
		"x-span-id", spanID,
	)

	p9log.Context(ctx).Debugf("request middleware: request_id=%s, trace_id=%s, method=%s",
		requestID, traceID, info.FullMethod)

	return handler(ctx, req)
}

// HttpRequestIDMiddleware is an HTTP middleware that sets request context.
func (m *RequestIDMiddleware) HttpRequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(RequestIDHeader)
		traceID := r.Header.Get(TraceIDHeader)
		spanID := r.Header.Get(SpanIDHeader)
		clientIP := extractClientIPFromHTTP(r)
		userAgent := r.Header.Get("User-Agent")

		// Generate if missing
		if m.generateIfMissing {
			if requestID == "" {
				requestID = ulid.NewString()
			}
			if traceID == "" {
				traceID = requestID
			}
			if spanID == "" {
				spanID = ulid.NewString()
			}
		}

		// Set request context
		ctx := p9context.NewRequestContext(r.Context(), p9context.RequestContext{
			RequestID: requestID,
			TraceID:   traceID,
			SpanID:    spanID,
			ClientIP:  clientIP,
			UserAgent: userAgent,
			Method:    r.Method,
			Path:      r.URL.Path,
		})

		// Add request ID to response headers for client correlation
		w.Header().Set(RequestIDHeader, requestID)
		w.Header().Set(TraceIDHeader, traceID)

		p9log.Context(ctx).Debugf("request middleware: request_id=%s, trace_id=%s, method=%s, path=%s",
			requestID, traceID, r.Method, r.URL.Path)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// getFirstValue extracts the first value from metadata for a given key.
func getFirstValue(md metadata.MD, key string) string {
	if values := md.Get(key); len(values) > 0 {
		return values[0]
	}
	return ""
}

// extractClientIP extracts the client IP from gRPC metadata.
// Checks X-Forwarded-For and X-Real-IP headers.
func extractClientIP(md metadata.MD) string {
	// Try X-Forwarded-For first (may contain multiple IPs)
	if xff := getFirstValue(md, "x-forwarded-for"); xff != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one (client)
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Try X-Real-IP
	if realIP := getFirstValue(md, "x-real-ip"); realIP != "" {
		return realIP
	}

	return ""
}

// extractClientIPFromHTTP extracts the client IP from HTTP request.
func extractClientIPFromHTTP(r *http.Request) string {
	// Try X-Forwarded-For first
	if xff := r.Header.Get(ForwardedForHeader); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Try X-Real-IP
	if realIP := r.Header.Get(RealIPHeader); realIP != "" {
		return realIP
	}

	// Fall back to RemoteAddr
	if r.RemoteAddr != "" {
		// RemoteAddr may include port, extract just the IP
		ip := r.RemoteAddr
		if colonIdx := strings.LastIndex(ip, ":"); colonIdx != -1 {
			ip = ip[:colonIdx]
		}
		return ip
	}

	return ""
}

// GenerateRequestID generates a new request ID using ULID.
func GenerateRequestID() string {
	return ulid.NewString()
}

// GenerateTraceID generates a new trace ID using ULID.
func GenerateTraceID() string {
	return ulid.NewString()
}

// GenerateSpanID generates a new span ID using ULID.
func GenerateSpanID() string {
	return ulid.NewString()
}

// UnaryServerInterceptor returns a gRPC unary interceptor for request ID middleware.
// This is a convenience function for easy middleware chain integration.
func UnaryServerInterceptor(opts ...RequestIDMiddlewareOption) grpc.UnaryServerInterceptor {
	m := NewRequestIDMiddleware(opts...)
	return m.GrpcRequestIDMiddleware
}

// HTTPMiddleware returns an HTTP middleware for request ID handling.
// This is a convenience function for easy middleware chain integration.
func HTTPMiddleware(opts ...RequestIDMiddlewareOption) func(http.Handler) http.Handler {
	m := NewRequestIDMiddleware(opts...)
	return m.HttpRequestIDMiddleware
}
