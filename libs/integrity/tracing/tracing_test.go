package tracing

import (
	"context"
	"strings"
	"testing"
)

// The example from the specification, read and written back unchanged.
//
// A round trip is the whole contract with every other tracing system in the
// world: what this platform writes, a collector has to read, and what a caller
// sends, this has to continue rather than restart.
func TestTheSpecificationsExampleRoundTrips(t *testing.T) {
	const header = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	c, ok := Parse(header)
	if !ok {
		t.Fatal("the example from the W3C specification was rejected")
	}
	if c.Trace() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace id read as %s", c.Trace())
	}
	if c.Span() != "00f067aa0ba902b7" {
		t.Errorf("span id read as %s", c.Span())
	}
	if !c.Sampled {
		t.Error("the sampled flag was set and read as unset")
	}
	if got := c.String(); got != header {
		t.Errorf("written back as %s, want %s", got, header)
	}
}

// Anything malformed starts a new trace rather than being repaired.
//
// A traceparent that is silently fixed up produces a trace that looks whole and
// joins two unrelated requests, which is worse than one that visibly begins
// again here: the first is wrong and convincing, the second is obviously a gap.
func TestAMalformedTraceparentIsRefusedRatherThanRepaired(t *testing.T) {
	for _, bad := range []struct {
		header string
		why    string
	}{
		{"", "empty"},
		{"nonsense", "not a traceparent at all"},
		{"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7", "no flags"},
		{"00-4bf92f3577b34da6a3ce929d0e0e473-00f067aa0ba902b7-01", "trace id one character short"},
		{"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b-01", "span id one character short"},
		{"00-00000000000000000000000000000000-00f067aa0ba902b7-01", "all-zero trace id, which the specification forbids"},
		{"00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01", "all-zero span id, likewise"},
		{"ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "version ff, which is reserved"},
		{"00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01", "upper case, which the specification forbids"},
		{"00-4bf92f3577b34da6a3ce929d0e0e473g-00f067aa0ba902b7-01", "not hexadecimal"},
	} {
		if _, ok := Parse(bad.header); ok {
			t.Errorf("accepted %q (%s)", bad.header, bad.why)
		}
	}
}

// A later version is still read. The specification requires it: a field added in
// a future version must not stop today's services continuing the trace.
func TestALaterVersionIsStillContinued(t *testing.T) {
	c, ok := Parse("01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-something-new")
	if !ok {
		t.Fatal("a traceparent from a later version was rejected, which breaks the trace " +
			"at this service for a field this service does not even read")
	}
	if c.Trace() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace id read as %s", c.Trace())
	}
}

// A child keeps the trace and takes a new span. That is the whole of what makes
// a call chain reconstructable.
func TestAChildKeepsTheTraceAndTakesANewSpan(t *testing.T) {
	parent := New()
	child := parent.Child()

	if child.TraceID != parent.TraceID {
		t.Error("the child is on a different trace from its parent, so the chain is broken")
	}
	if child.SpanID == parent.SpanID {
		t.Error("the child reused its parent's span id, so the two hops are indistinguishable")
	}
	if child.Sampled != parent.Sampled {
		t.Error("the sampling decision was not carried, which leaves a hole in the waterfall")
	}

	// And a child of nothing is a fresh trace rather than an invalid one.
	orphan := Context{}.Child()
	if !orphan.Valid() {
		t.Error("a child of an invalid context is not a usable trace")
	}
	if orphan.TraceID == ([16]byte{}) {
		t.Error("a fresh trace has an all-zero id")
	}
}

// Two traces are not the same trace.
func TestEachTraceIsItsOwn(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		c := New()
		if !c.Valid() {
			t.Fatal("New produced an invalid context")
		}
		if seen[c.Trace()] {
			t.Fatalf("two traces share the id %s", c.Trace())
		}
		seen[c.Trace()] = true
	}
}

func TestTheContextCarriesItAndTheTraceIDIsReadable(t *testing.T) {
	c := New()
	ctx := With(context.Background(), c)

	got, ok := From(ctx)
	if !ok || got != c {
		t.Errorf("read back %+v, put in %+v", got, c)
	}
	if TraceIDFrom(ctx) != c.Trace() {
		t.Errorf("trace id reads as %q, want %q", TraceIDFrom(ctx), c.Trace())
	}

	// A context with nothing on it says so rather than inventing one: an audit
	// entry with a made-up trace id is worse than one with none, because it
	// points somewhere.
	if _, ok := From(context.Background()); ok {
		t.Error("a bare context reported that it carries a trace")
	}
	if TraceIDFrom(context.Background()) != "" {
		t.Error("a bare context produced a trace id")
	}

	// An invalid one put on deliberately is not readable either.
	if _, ok := From(With(context.Background(), Context{})); ok {
		t.Error("an all-zero context was read back as valid")
	}
}

func TestAnInvalidContextRendersAsNothing(t *testing.T) {
	if got := (Context{}).String(); got != "" {
		t.Errorf("an invalid context rendered as %q; sending that as a header would make "+
			"the next service reject it and start again, silently", got)
	}
}

// The sampled flag survives a round trip in both states.
func TestTheSampledFlagIsCarriedBothWays(t *testing.T) {
	for _, sampled := range []bool{true, false} {
		c := New()
		c.Sampled = sampled
		back, ok := Parse(c.String())
		if !ok {
			t.Fatalf("could not read back %s", c)
		}
		if back.Sampled != sampled {
			t.Errorf("sampled=%v went out as %s and came back %v", sampled, c, back.Sampled)
		}
	}
}

func TestTheHeaderNameIsTheStandardOne(t *testing.T) {
	// Lower case, as the specification writes it. Anything else is a header a
	// collector and every other tracing system will not recognise.
	if Header != "traceparent" || strings.ToLower(Header) != Header {
		t.Errorf("the header is %q and the standard is %q", Header, "traceparent")
	}
}
