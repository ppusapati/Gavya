package handler

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"connectrpc.com/connect"

	"github.com/ppusapati/gavya/services/product-catalog-service/internal/repository"
	"github.com/ppusapati/gavya/services/product-catalog-service/internal/service"
)

// Every failure used to be reported as internal, so a caller could not tell a
// missing product from an unreachable database. These pin the distinctions.
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
			err:  fmt.Errorf("load product: %w", repository.ErrNotFound),
			want: connect.CodeNotFound,
		},
		{
			name: "a category slug already taken",
			err:  repository.ErrDuplicateCategorySlug,
			want: connect.CodeAlreadyExists,
		},
		{
			name: "a brand slug already taken",
			err:  repository.ErrDuplicateBrandSlug,
			want: connect.CodeAlreadyExists,
		},
		{
			name: "a product slug already taken",
			err:  repository.ErrDuplicateProductSlug,
			want: connect.CodeAlreadyExists,
		},
		{
			name: "a sku code already taken",
			err:  repository.ErrDuplicateSKUCode,
			want: connect.CodeAlreadyExists,
		},
		{
			name: "a caller mistake",
			err:  fmt.Errorf("%w: id and tenant_id are required", service.ErrInvalidArgument),
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

// The reason has to survive classification, or an operator reading the log
// learns only that something was invalid.
func TestClassifyKeepsTheReason(t *testing.T) {
	err := classify(fmt.Errorf("%w: product_type is required", service.ErrInvalidArgument))

	var connectErr *connect.Error
	if !errors.As(err, &connectErr) {
		t.Fatalf("got %v, want a connect error", err)
	}
	if !strings.Contains(connectErr.Message(), "product_type is required") {
		t.Errorf("message = %q, want it to name the missing field", connectErr.Message())
	}
}
