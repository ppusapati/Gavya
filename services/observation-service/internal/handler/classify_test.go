package handler

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/services/observation-service/internal/repository"
	"github.com/ppusapati/gavya/services/observation-service/internal/service"
)

// A failure is reported by what went wrong, not by which procedure it happened
// in.
//
// This handler used two codes across sixteen call sites and never internal, so a
// database outage on RecordObservation reached the caller as invalid_argument —
// an instruction to correct something they had typed, for a failure that had
// nothing to do with them. This is the service the settlement path reads from,
// and a booth told its reading was malformed does not send it again.
//
// The codes differ in what they ask of whoever sent the request, which is the
// only reason to have more than one of them: fix it and resend, or do not
// bother resending, or it was not your fault and retrying may work. The case
// that matters most here is the last one, because it is the one that was absent.
func TestAFailureIsReportedByItsCauseAndNotByItsProcedure(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want connect.Code
	}{
		{
			name: "a reading finer than its column is the caller's to fix",
			err:  fmt.Errorf("value: %w", service.ErrInvalidArgument),
			want: connect.CodeInvalidArgument,
		},
		{
			name: "a missing row is not found",
			err:  fmt.Errorf("get observation: %w", repository.ErrNotFound),
			want: connect.CodeNotFound,
		},
		{
			name: "a write-once estimate already attached is a precondition, not a mistake",
			err:  fmt.Errorf("attach: %w", repository.ErrAlreadyAttached),
			want: connect.CodeFailedPrecondition,
		},
		{
			name: "anything else is ours, and retrying may work",
			err:  errors.New("dial tcp 10.0.0.5:5432: connect: connection refused"),
			want: connect.CodeInternal,
		},
		{
			name: "a wrapped database failure is still ours",
			err:  fmt.Errorf("create observation: %w", errors.New("server closed the connection")),
			want: connect.CodeInternal,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := connect.CodeOf(classify(tc.err)); got != tc.want {
				t.Errorf("classify(%v) = %s, want %s", tc.err, got, tc.want)
			}
		})
	}
}

// The error itself has to survive, or the code is all the caller gets and the
// message that says which field was refused is lost.
func TestClassifyKeepsTheReasonAndTheCause(t *testing.T) {
	cause := fmt.Errorf("value: is finer than this field is recorded to: %w", service.ErrInvalidArgument)
	err := classify(cause)
	if !errors.Is(err, service.ErrInvalidArgument) {
		t.Errorf("the cause was dropped: %v", err)
	}
	if got := err.Error(); !strings.Contains(got, "is finer than this field is recorded to") {
		t.Errorf("the reason was dropped: %q", got)
	}
}
