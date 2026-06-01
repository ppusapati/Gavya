package interceptors

import (
	"context"

	"connectrpc.com/connect"

	ulid "p9e.in/samavaya/packages/ulid"
	"p9e.in/samavaya/packages/p9context"
)

const (
	// RequestIDHeader is the header for request ID
	RequestIDHeader = "X-Request-ID"
	// TraceIDHeader is the header for distributed trace ID
	TraceIDHeader = "X-Trace-ID"
	// SpanIDHeader is the header for distributed span ID
	SpanIDHeader = "X-Span-ID"
)

// RequestIDInterceptorOption configures the RequestID interceptor.
type RequestIDInterceptorOption func(*requestIDConfig)

type requestIDConfig struct {
	generateIfMissing bool
}

// WithGenerateRequestID controls whether to generate IDs if not provided.
func WithGenerateRequestID(generate bool) RequestIDInterceptorOption {
	return func(c *requestIDConfig) {
		c.generateIfMissing = generate
	}
}

// RequestIDInterceptor returns a Connect interceptor that sets request context.
// It extracts or generates request ID, trace ID, and captures client info.
func RequestIDInterceptor(opts ...RequestIDInterceptorOption) connect.UnaryInterceptorFunc {
	cfg := &requestIDConfig{
		generateIfMissing: true,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			// Extract from headers
			requestID := req.Header().Get(RequestIDHeader)
			traceID := req.Header().Get(TraceIDHeader)
			spanID := req.Header().Get(SpanIDHeader)
			clientIP := extractClientIP(req)
			userAgent := req.Header().Get("User-Agent")

			// Generate if missing
			if cfg.generateIfMissing {
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
				Method:    req.Spec().Procedure,
				Path:      req.Spec().Procedure,
			})

			// Call next handler
			resp, err := next(ctx, req)

			// Add request ID to response headers for client correlation
			if resp != nil {
				resp.Header().Set(RequestIDHeader, requestID)
				resp.Header().Set(TraceIDHeader, traceID)
			}

			return resp, err
		}
	}
}

// extractClientIP extracts client IP from Connect request headers.
func extractClientIP(req connect.AnyRequest) string {
	// Try X-Forwarded-For first (may contain multiple IPs)
	if xff := req.Header().Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one (client)
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}

	// Try X-Real-IP
	if realIP := req.Header().Get("X-Real-IP"); realIP != "" {
		return realIP
	}

	return ""
}
