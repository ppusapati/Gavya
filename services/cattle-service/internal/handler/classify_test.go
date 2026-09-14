package handler

import (
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/services/cattle-service/internal/repository"
	"github.com/ppusapati/gavya/services/cattle-service/internal/service"
)

// A failure is reported by what went wrong, not by which procedure it happened
// in.
//
// Every procedure in this handler used to pick its code by position, and the
// three ways of being wrong were all present at once. CreateCattle and
// CreateBreed said invalid_argument, so an unreachable database read as a typo.
// GetCattle said not_found, so during an outage a caller was told their animal
// does not exist — the one answer somebody acts on by going to look for the
// animal. ListCattle, ListBreeds and DeleteCattle said internal, so a missing
// tenant_id read as worth retrying.
func TestAFailureIsReportedByItsCauseAndNotByItsProcedure(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want connect.Code
	}{
		{
			name: "a weight the column cannot hold is the caller's to fix",
			err:  fmt.Errorf("weight: %w", service.ErrInvalidArgument),
			want: connect.CodeInvalidArgument,
		},
		{
			name: "a missing animal is not found",
			err:  fmt.Errorf("get cattle: %w", repository.ErrNotFound),
			want: connect.CodeNotFound,
		},
		{
			name: "deleting an animal that is already gone is not found",
			err:  fmt.Errorf("delete cattle: %w", repository.ErrNotFound),
			want: connect.CodeNotFound,
		},
		{
			name: "an unreachable database is ours, and retrying may work",
			err:  errors.New("dial tcp 10.0.0.5:5432: connect: connection refused"),
			want: connect.CodeInternal,
		},
		{
			name: "a wrapped database failure is still ours",
			err:  fmt.Errorf("create cattle: %w", errors.New("server closed the connection")),
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
