// Tests for the errors package. Focus: structural contracts (New, Code,
// Reason, Wrap, Is, FromError) + the named status helpers (BadRequest,
// NotFound, Internal, …) that 1,896 importers rely on.
//
// Deliberately no external dependencies beyond stdlib; no DB / network.
package errors_test

import (
	stderrors "errors"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	"p9e.in/samavaya/packages/errors"
)

func TestNew_SetsCodeReasonMessage(t *testing.T) {
	e := errors.New(418, "TEAPOT", "short and stout")
	if got := errors.Code(e); got != 418 {
		t.Fatalf("Code = %d, want 418", got)
	}
	if got := errors.Reason(e); got != "TEAPOT" {
		t.Fatalf("Reason = %q, want TEAPOT", got)
	}
	if want := "short and stout"; e.Message != want {
		t.Fatalf("Message = %q, want %q", e.Message, want)
	}
}

func TestNewf_FormatsMessage(t *testing.T) {
	e := errors.Newf(404, "NOT_FOUND", "user %s", "alice")
	if want := "user alice"; e.Message != want {
		t.Fatalf("Message = %q, want %q", e.Message, want)
	}
}

func TestCode_OnPlainError_ReturnsUnknown(t *testing.T) {
	plain := stderrors.New("boom")
	if got := errors.Code(plain); got != errors.UnknownCode {
		t.Fatalf("Code(plain) = %d, want UnknownCode (%d)", got, errors.UnknownCode)
	}
}

func TestCode_OnNil_ReturnsZero(t *testing.T) {
	if got := errors.Code(nil); got != 200 {
		// errors.Code returns 200 for nil per the source; record that here.
		t.Fatalf("Code(nil) = %d, want 200 (success/no-error sentinel)", got)
	}
}

func TestWrap_Nil_ReturnsNil(t *testing.T) {
	// This is the contract referenced in memory feedback_errors_wrap_nil.md —
	// Wrap must NOT synthesize an error from a nil. Otherwise callers that
	// use `return errors.Wrap(err, "...")` silently swallow success paths.
	if got := errors.Wrap(nil, "ignored"); got != nil {
		t.Fatalf("Wrap(nil, ...) = %v, want nil", got)
	}
}

func TestWrap_NonNil_PreservesChain(t *testing.T) {
	inner := stderrors.New("root cause")
	wrapped := errors.Wrap(inner, "during step 3")
	if wrapped == nil {
		t.Fatal("Wrap returned nil on non-nil input")
	}
	if !stderrors.Is(wrapped, inner) {
		t.Fatalf("errors.Is(wrapped, inner) = false; want true — lost the chain")
	}
	if want := "during step 3: root cause"; wrapped.Error() != want {
		t.Fatalf("Error() = %q, want %q", wrapped.Error(), want)
	}
}

func TestWrapf_FormatsAndPreservesChain(t *testing.T) {
	inner := stderrors.New("db timeout")
	wrapped := errors.Wrapf(inner, "user %s lookup", "bob")
	if !stderrors.Is(wrapped, inner) {
		t.Fatal("wrap chain lost")
	}
	if want := "user bob lookup: db timeout"; wrapped.Error() != want {
		t.Fatalf("Error() = %q, want %q", wrapped.Error(), want)
	}
}

func TestIs_MatchesOnCodeAndReason(t *testing.T) {
	sentinel := errors.New(404, "USER_NOT_FOUND", "no such user")
	actual := errors.New(404, "USER_NOT_FOUND", "different message")

	// Same Code+Reason → Is returns true regardless of message differences.
	if !stderrors.Is(actual, sentinel) {
		t.Fatal("Is(actual, sentinel) = false; want true for matching Code+Reason")
	}

	// Different Reason → no match.
	other := errors.New(404, "TENANT_NOT_FOUND", "msg")
	if stderrors.Is(other, sentinel) {
		t.Fatal("Is returned true for non-matching Reason")
	}
}

func TestWithCause_WrapsAndUnwraps(t *testing.T) {
	inner := stderrors.New("io: permission denied")
	e := errors.New(500, "FS_ERROR", "failed to read config").WithCause(inner)

	if got := stderrors.Unwrap(e); got != inner {
		t.Fatalf("Unwrap(e) = %v, want %v", got, inner)
	}
}

func TestWithMetadata_AttachesKVs(t *testing.T) {
	md := map[string]string{"tenant": "acme", "user": "alice"}
	e := errors.New(400, "VALIDATION", "bad input").WithMetadata(md)

	if got := e.Metadata; len(got) != 2 || got["tenant"] != "acme" || got["user"] != "alice" {
		t.Fatalf("Metadata = %v, want %v", got, md)
	}
}

func TestFromError_NilReturnsNil(t *testing.T) {
	if got := errors.FromError(nil); got != nil {
		t.Fatalf("FromError(nil) = %v, want nil", got)
	}
}

func TestFromError_TypedErrorRoundtrips(t *testing.T) {
	original := errors.New(404, "GONE", "zzz")
	got := errors.FromError(original)
	if got == nil {
		t.Fatal("FromError returned nil for typed Error")
	}
	if got.Code != 404 || got.Reason != "GONE" || got.Message != "zzz" {
		t.Fatalf("FromError mutated fields: %+v", got.Status)
	}
}

func TestFromError_PlainErrorWrapsAsUnknown(t *testing.T) {
	plain := fmt.Errorf("random: %w", stderrors.New("boom"))
	got := errors.FromError(plain)
	if got == nil {
		t.Fatal("FromError returned nil for plain error")
	}
	if got.Code != int32(errors.UnknownCode) {
		t.Fatalf("FromError(plain).Code = %d, want %d", got.Code, errors.UnknownCode)
	}
}

func TestClone_ProducesIndependentCopy(t *testing.T) {
	original := errors.New(400, "V", "m").WithMetadata(map[string]string{"x": "1"})
	clone := errors.Clone(original)

	// Mutating clone's metadata must not affect original.
	clone.Metadata["x"] = "2"
	if original.Metadata["x"] != "1" {
		t.Fatalf("Clone leaked mutation: original.Metadata[x] = %q", original.Metadata["x"])
	}
}

// Named status helpers + their Is* checks — these are the most used paths
// in the 1,896 importers (handler error returns).

func TestStatusHelpers_MapToCorrectCodes(t *testing.T) {
	cases := []struct {
		name string
		err  *errors.Error
		code int
	}{
		{"BadRequest", errors.BadRequest("BAD", "m"), 400},
		{"Unauthorized", errors.Unauthorized("AUTH", "m"), 401},
		{"Forbidden", errors.Forbidden("FORBID", "m"), 403},
		{"NotFound", errors.NotFound("GONE", "m"), 404},
		{"Conflict", errors.Conflict("DUP", "m"), 409},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := errors.Code(tc.err); got != tc.code {
				t.Fatalf("Code = %d, want %d", got, tc.code)
			}
		})
	}
}

func TestIsStatusHelpers_RoundTrip(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		check func(error) bool
		want  bool
	}{
		{"IsBadRequest-pos", errors.BadRequest("B", "m"), errors.IsBadRequest, true},
		{"IsBadRequest-neg", errors.NotFound("N", "m"), errors.IsBadRequest, false},
		{"IsUnauthorized-pos", errors.Unauthorized("U", "m"), errors.IsUnauthorized, true},
		{"IsForbidden-pos", errors.Forbidden("F", "m"), errors.IsForbidden, true},
		{"IsNotFound-pos", errors.NotFound("N", "m"), errors.IsNotFound, true},
		{"IsConflict-pos", errors.Conflict("C", "m"), errors.IsConflict, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.check(tc.err); got != tc.want {
				t.Fatalf("%s check returned %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestInternal_FormatsMessage(t *testing.T) {
	e := errors.Internal("db down: %s", "timeout")
	if e.Code != 500 || e.Reason != "INTERNAL" || e.Message != "db down: timeout" {
		t.Fatalf("Internal fields = %+v", e.Status)
	}
}

func TestAlreadyExists_FormatsMessage(t *testing.T) {
	e := errors.AlreadyExists("user %s exists", "bob")
	if e.Code != 409 || e.Reason != "ALREADY_EXISTS" || e.Message != "user bob exists" {
		t.Fatalf("AlreadyExists fields = %+v", e.Status)
	}
}

func TestNewBusinessError_Is400Class(t *testing.T) {
	e := errors.NewBusinessError("VAL_FAIL", "field %s invalid", "email")
	if !errors.IsBadRequest(e) {
		t.Fatalf("NewBusinessError should be 400-class; got code %d", e.Code)
	}
	if e.Message != "field email invalid" {
		t.Fatalf("Message = %q", e.Message)
	}
}

// 2026-04-27 (Audit #4 of base-domain audits, docs/BASE_DOMAIN_AUDITS.md):
// pin the HTTP→connect.Code mapping that ToConnectError relies on. Without
// these tests a regression that swaps two cases would slip past CI because
// no caller pins the code values directly — they all just propagate.

func TestToConnectCode_MapsKnownStatuses(t *testing.T) {
	cases := []struct {
		http int
		want connect.Code
	}{
		{200, 0},
		{400, connect.CodeInvalidArgument},
		{401, connect.CodeUnauthenticated},
		{403, connect.CodePermissionDenied},
		{404, connect.CodeNotFound},
		{409, connect.CodeAborted}, // matches existing HTTP→gRPC table
		{429, connect.CodeResourceExhausted},
		{500, connect.CodeInternal},
		{501, connect.CodeUnimplemented},
		{503, connect.CodeUnavailable},
		{504, connect.CodeDeadlineExceeded},
		{999, connect.CodeUnknown}, // unknown HTTP → CodeUnknown
	}
	for _, tc := range cases {
		if got := errors.ToConnectCode(tc.http); got != tc.want {
			t.Errorf("ToConnectCode(%d) = %v, want %v", tc.http, got, tc.want)
		}
	}
}

func TestToConnectError_BadRequestBecomesInvalidArgument(t *testing.T) {
	se := errors.BadRequest("MISSING_REQUIRED_FIELD", "missing required field: entity_id")
	got := errors.ToConnectError(se)
	if got == nil {
		t.Fatal("ToConnectError returned nil for non-nil input")
	}
	var ce *connect.Error
	if !stderrors.As(got, &ce) {
		t.Fatalf("ToConnectError did not return *connect.Error; got %T", got)
	}
	if ce.Code() != connect.CodeInvalidArgument {
		t.Errorf("connect code = %v, want %v", ce.Code(), connect.CodeInvalidArgument)
	}
	// The structured *Error must remain reachable via errors.As so callers
	// using sentinel-style checks continue to work.
	var se2 *errors.Error
	if !stderrors.As(got, &se2) {
		t.Fatalf("structured *errors.Error must remain reachable via errors.As")
	}
	if se2.Reason != "MISSING_REQUIRED_FIELD" {
		t.Errorf("preserved Reason = %q, want MISSING_REQUIRED_FIELD", se2.Reason)
	}
}

func TestToConnectError_NotFoundBecomesCodeNotFound(t *testing.T) {
	se := errors.NotFound("USER_NOT_FOUND", "no such user")
	got := errors.ToConnectError(se)
	var ce *connect.Error
	if !stderrors.As(got, &ce) {
		t.Fatalf("ToConnectError did not return *connect.Error; got %T", got)
	}
	if ce.Code() != connect.CodeNotFound {
		t.Errorf("connect code = %v, want %v", ce.Code(), connect.CodeNotFound)
	}
}

func TestToConnectError_NilReturnsNil(t *testing.T) {
	if got := errors.ToConnectError(nil); got != nil {
		t.Fatalf("ToConnectError(nil) = %v, want nil", got)
	}
}

func TestToConnectError_PlainErrorBecomesInternal(t *testing.T) {
	// FromError on a plain (non-*Error) error returns
	// New(UnknownCode=500, …); UnknownCode=500 maps via the
	// HTTP↔gRPC table to codes.Internal → connect.CodeInternal.
	// Pinning this so a future refactor that swaps the fallback
	// (e.g. to UnknownCode→CodeUnknown) is a deliberate decision,
	// not a silent regression.
	got := errors.ToConnectError(stderrors.New("opaque"))
	var ce *connect.Error
	if !stderrors.As(got, &ce) {
		t.Fatalf("ToConnectError did not return *connect.Error; got %T", got)
	}
	if ce.Code() != connect.CodeInternal {
		t.Errorf("plain error code = %v, want CodeInternal (UnknownCode=500 → Internal)", ce.Code())
	}
}
