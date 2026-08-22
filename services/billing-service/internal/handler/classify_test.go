package handler

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/services/billing-service/internal/repository"
	"github.com/ppusapati/gavya/services/billing-service/internal/service"
)

// Every failure used to be reported as internal, so a caller could not tell a
// missing invoice from an unreachable database. These pin the distinctions.
func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want connect.Code
	}{
		{
			name: "a missing record",
			err:  repository.ErrNotFound,
			want: connect.CodeNotFound,
		},
		{
			name: "a missing record wrapped by a caller",
			err:  fmt.Errorf("load invoice: %w", repository.ErrNotFound),
			want: connect.CodeNotFound,
		},
		{
			name: "an invoice number already taken",
			err:  repository.ErrDuplicateInvoiceNumber,
			want: connect.CodeAlreadyExists,
		},
		{
			name: "a caller mistake",
			err:  fmt.Errorf("%w: customer_id is required", service.ErrInvalidArgument),
			want: connect.CodeInvalidArgument,
		},
		{
			name: "anything else",
			err:  errors.New("connection refused"),
			want: connect.CodeInternal,
		},
	}

	for _, c := range cases {
		got := connect.CodeOf(classify(c.err))
		if got != c.want {
			t.Errorf("%s: code = %s, want %s", c.name, got, c.want)
		}
	}
}

// A violated business rule is a caller mistake, not an internal failure: the
// client asked for something the invoice's state does not allow, and retrying
// it will not help.
func TestBusinessRuleViolationIsInvalidArgument(t *testing.T) {
	err := classify(fmt.Errorf("%w: cannot void a paid invoice", service.ErrInvalidArgument))

	if got := connect.CodeOf(err); got != connect.CodeInvalidArgument {
		t.Errorf("code = %s, want invalid_argument", got)
	}
}

// The reason has to survive classification, or an operator reading the log
// learns only that something was invalid.
func TestClassifyKeepsTheReason(t *testing.T) {
	err := classify(fmt.Errorf("%w: tenant_id is required", service.ErrInvalidArgument))

	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		t.Fatalf("got %v, want a connect error", err)
	}
	if !strings.Contains(connectErr.Message(), "tenant_id is required") {
		t.Errorf("message = %q, want it to name the missing field", connectErr.Message())
	}
}
