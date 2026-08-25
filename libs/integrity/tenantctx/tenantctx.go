// Package tenantctx carries the tenant a request is acting for.
//
// It is deliberately a leaf: no database driver, no HTTP, nothing. The tenant
// has to be readable by the transport layer that discovers it and by the
// database layer that enforces it, and neither of those should have to depend on
// the other to agree on where it is kept.
package tenantctx

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type contextKey struct{}

// ErrNoTenant is returned when a caller asks for the tenant of a context that
// has none.
var ErrNoTenant = errors.New("tenantctx: no tenant on this context")

// With returns a context carrying this tenant.
func With(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, contextKey{}, tenantID)
}

// From returns the tenant a context carries.
func From(ctx context.Context) (string, error) {
	v, ok := ctx.Value(contextKey{}).(string)
	if !ok || v == "" {
		return "", ErrNoTenant
	}
	return v, nil
}

// ErrBadTenant reports a value that cannot be a tenant identifier.
type ErrBadTenant struct {
	Value  string
	Reason string
}

func (e *ErrBadTenant) Error() string {
	return fmt.Sprintf("tenantctx: %q is not a tenant identifier: %s", e.Value, e.Reason)
}

// Check rejects a value that cannot be an identifier.
//
// Not a defence against injection — the tenant reaches PostgreSQL as a bound
// parameter, so there is no statement for it to escape from. It is here because
// a tenant carrying a newline or a control character means something upstream is
// putting the wrong string in the context, and that is worth failing on rather
// than storing rows under.
func Check(v string) error {
	if v == "" {
		return &ErrBadTenant{v, "it is empty"}
	}
	if len(v) > 64 {
		return &ErrBadTenant{v, "it is longer than any identifier this system issues"}
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return &ErrBadTenant{v, "it contains a control character"}
		}
	}
	if strings.TrimSpace(v) != v {
		return &ErrBadTenant{v, "it is padded with whitespace"}
	}
	return nil
}
