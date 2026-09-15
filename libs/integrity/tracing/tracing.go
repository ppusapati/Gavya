// Package tracing carries one identifier across every service a request touches.
//
// # WHAT WAS ACTUALLY THE CASE
//
// There was no tracing, and two things made that worse than simply absent.
//
// svcclient set an X-Request-Id on every call, and every caller generated a
// fresh one — so the identifier existed, looked like a correlation id, and
// correlated nothing. A settlement that gathers a fortnight touches procurement,
// canonical and notification, and each hop was a different id. Asking "what did
// this request do" meant reading four services' logs and matching on timestamps.
//
// And audit.Entry has carried a TraceID field for as long as the trail has
// existed. Nothing ever set it. Every audit row in the platform has an empty
// trace_id, and the column is in the schema, in the service's domain model, and
// on its wire format — a field that reports success while doing nothing, which
// is the shape of defect this repository keeps finding. This is what makes it
// live.
//
// # WHAT THIS IS
//
// W3C Trace Context, which is the standard one and is one header:
//
//	traceparent: 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01
//	             ^^ ^                              ^ ^              ^ ^^
//	             version        trace id             span id        flags
//
// The trace id is the same for every hop; the span id identifies this one. A
// service that receives a traceparent continues that trace, and one that does
// not start a new one — so a trace begins wherever a request enters the platform
// and reaches everything it touches.
//
// Parsed strictly rather than leniently, and the reason is the usual one: a
// malformed traceparent that is silently repaired produces a trace that looks
// complete and joins two unrelated requests, which is worse than a trace that
// visibly starts again here.
//
// Hand-rolled rather than taking the OpenTelemetry SDK, for the same reason
// libs/integrity/observe writes Prometheus text itself: the wire formats are
// small and specified, and the dependency is not small. What is on the wire is
// the standard, so a collector does not know the difference.
package tracing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// Header is the W3C name, lower case as the specification writes it. Go's
// http.Header canonicalises on the way in and out, so either spelling is read.
const Header = "traceparent"

// version is the only one defined. A traceparent announcing a later version is
// still read — the specification requires that, so that a future field does not
// break today's services — but one announcing "ff" is invalid.
const version = "00"

// Context is a position in a trace: which trace, which span within it, and
// whether anybody is recording.
type Context struct {
	// TraceID is the whole request, across every service.
	TraceID [16]byte
	// SpanID is this one hop.
	SpanID [8]byte
	// Sampled says whether this trace is being recorded. Propagated rather than
	// decided per service: a trace sampled in one service and not the next is a
	// waterfall with a hole in it.
	Sampled bool
}

// Valid reports whether this is a usable position.
//
// An all-zero trace or span id is explicitly invalid in the specification, and
// treating one as valid is how a trace ends up with every service reporting the
// same zero id and appearing to be one enormous request.
func (c Context) Valid() bool {
	return c.TraceID != [16]byte{} && c.SpanID != [8]byte{}
}

func (c Context) Trace() string { return hex.EncodeToString(c.TraceID[:]) }
func (c Context) Span() string  { return hex.EncodeToString(c.SpanID[:]) }

// String renders the traceparent header value.
func (c Context) String() string {
	if !c.Valid() {
		return ""
	}
	flags := "00"
	if c.Sampled {
		flags = "01"
	}
	return version + "-" + c.Trace() + "-" + c.Span() + "-" + flags
}

// Parse reads a traceparent, and returns false for anything it is not sure of.
func Parse(header string) (Context, bool) {
	parts := strings.Split(strings.TrimSpace(header), "-")
	if len(parts) < 4 {
		return Context{}, false
	}
	// A later version may append fields. It may not shorten these four.
	if len(parts[0]) != 2 || len(parts[1]) != 32 || len(parts[2]) != 16 || len(parts[3]) != 2 {
		return Context{}, false
	}
	if parts[0] == "ff" {
		return Context{}, false
	}
	// Only lower-case hex is valid, and DecodeString accepts upper too, so the
	// cheap check that rejects "4BF9..." is done here rather than relying on it.
	if strings.ToLower(header) != header {
		return Context{}, false
	}

	var c Context
	trace, err := hex.DecodeString(parts[1])
	if err != nil {
		return Context{}, false
	}
	span, err := hex.DecodeString(parts[2])
	if err != nil {
		return Context{}, false
	}
	flags, err := hex.DecodeString(parts[3])
	if err != nil {
		return Context{}, false
	}
	copy(c.TraceID[:], trace)
	copy(c.SpanID[:], span)
	c.Sampled = flags[0]&0x01 == 1
	if !c.Valid() {
		return Context{}, false
	}
	return c, true
}

// New starts a trace.
//
// Sampled is true: this platform records everything. At a co-operative's volume
// — the load measurement put a busy morning at a few thousand collections a
// second, in bursts twice a day — the storage is not the constraint, and a
// sampled trace is exactly the one that is missing when somebody asks about the
// request that went wrong.
func New() Context {
	c := Context{Sampled: true}
	rand.Read(c.TraceID[:])
	rand.Read(c.SpanID[:])
	// rand.Read from crypto/rand cannot fail in Go 1.24 and later; it panics
	// rather than returning an error. An all-zero id would be invalid anyway and
	// Valid below is what would catch it.
	return c
}

// Child is the next hop: the same trace, a new span.
func (c Context) Child() Context {
	if !c.Valid() {
		return New()
	}
	next := Context{TraceID: c.TraceID, Sampled: c.Sampled}
	rand.Read(next.SpanID[:])
	return next
}

type contextKey struct{}

// With puts a position on a context, for everything downstream to find.
func With(ctx context.Context, c Context) context.Context {
	return context.WithValue(ctx, contextKey{}, c)
}

// From reads it back.
func From(ctx context.Context) (Context, bool) {
	c, ok := ctx.Value(contextKey{}).(Context)
	return c, ok && c.Valid()
}

// TraceIDFrom is the trace id alone, or empty.
//
// This is what audit.Write and a log line want: they are not spans, they are
// things that happened during one, and the id is what ties them to it.
func TraceIDFrom(ctx context.Context) string {
	if c, ok := From(ctx); ok {
		return c.Trace()
	}
	return ""
}
