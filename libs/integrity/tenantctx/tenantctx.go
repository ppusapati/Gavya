// Package tenantctx carries who a request is acting as: the tenant, and the
// person or service inside it.
//
// It is deliberately a leaf: no database driver, no HTTP, nothing. This has to
// be readable by the transport layer that discovers it, by the database layer
// that enforces the tenant, and by the audit layer that records the actor, and
// none of those should have to depend on another to agree on where it is kept.
//
// The actor is here rather than being passed as an argument for the same reason
// the tenant is. An argument is something a caller chooses, and a caller that
// can choose whose name goes on a change can put somebody else's there — which
// is precisely what an audit trail exists to prevent.
package tenantctx

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type contextKey struct{}

type actorKey struct{}

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

// Actor is who is acting inside the tenant.
//
// Exactly one of ID and ServiceID is set. A service is not a person and must not
// borrow one's name: an action attributed to the account a service happens to
// use is an action nobody can be asked about.
type Actor struct {
	// ID is a person.
	ID string
	// ServiceID is a service identity.
	ServiceID string
}

// Kind names what is acting, for the audit trail's actor_type.
func (a Actor) Kind() string {
	switch {
	case a.ServiceID != "":
		return "service"
	case a.ID != "":
		return "user"
	default:
		return ""
	}
}

// Identifier is whichever of the two is set.
func (a Actor) Identifier() string {
	if a.ServiceID != "" {
		return a.ServiceID
	}
	return a.ID
}

// Known reports whether this context has an actor at all.
func (a Actor) Known() bool { return a.Identifier() != "" }

// ErrNoActor is returned when a caller asks who is acting and nothing said.
var ErrNoActor = errors.New("tenantctx: no actor on this context")

// WithActor returns a context carrying who is acting.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// ActorFrom returns who is acting.
func ActorFrom(ctx context.Context) (Actor, error) {
	a, ok := ctx.Value(actorKey{}).(Actor)
	if !ok || !a.Known() {
		return Actor{}, ErrNoActor
	}
	return a, nil
}
