// Package tenantdb hands every database connection the tenant it is acting for,
// so the row-level security policies have something to enforce.
//
// The policies in libs/integrity/isolation read a session parameter,
// app.tenant_id, and refuse to return anything when it is not set. Something has
// to set it, on the right connection, for the right request, every time.
//
// The obvious place is each repository method — begin a transaction, SET LOCAL,
// run the query. That is several hundred call sites across twenty-two services,
// and the isolation is only as good as the least-reviewed one of them. It is the
// same shape of problem the policies exist to solve, moved one layer up.
//
// So this sets it where connections are handed out instead. pgxpool calls
// BeforeAcquire with the caller's context every time a connection leaves the
// pool, which is once per query and once per transaction. The tenant is read
// from that context and applied there, and no repository changes at all.
//
// # Always set, never merely cleared
//
// The parameter is set on every acquire, including when the context carries no
// tenant — in which case it is cleared. That ordering is the point. Relying on
// clearing the value when a connection is returned means a missed cleanup path,
// a panic, or a cancelled request leaves the previous tenant's identity on a
// connection that is about to serve somebody else. Setting on the way out makes
// the previous value irrelevant: whatever was there is overwritten before a
// single statement runs.
//
// # Why the tenant is bound as a parameter
//
// PostgreSQL does not accept bind parameters on SET, which is why the older code
// in this tree interpolates the value into the statement and validates it
// character by character to keep the quoting safe. set_config() is a function
// and does take parameters, so the value never reaches the parser as text and
// there is nothing to validate against. The tenant is still checked for shape
// here, but as an argument check rather than as a defence.
package tenantdb

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The session parameter the isolation policies read.
const SettingName = "app.tenant_id"

type contextKey struct{}

// ErrNoTenant is returned when a caller asks for the tenant of a context that
// has none.
var ErrNoTenant = errors.New("tenantdb: no tenant on this context")

// WithTenant returns a context that will scope every database connection taken
// under it to this tenant.
func WithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, contextKey{}, tenantID)
}

// TenantFrom returns the tenant a context carries.
func TenantFrom(ctx context.Context) (string, error) {
	v, ok := ctx.Value(contextKey{}).(string)
	if !ok || v == "" {
		return "", ErrNoTenant
	}
	return v, nil
}

// ErrBadTenant reports a tenant identifier that cannot be one.
type ErrBadTenant struct {
	Value  string
	Reason string
}

func (e *ErrBadTenant) Error() string {
	return fmt.Sprintf("tenantdb: %q is not a tenant identifier: %s", e.Value, e.Reason)
}

// checkTenant rejects a value that cannot be an identifier.
//
// Not a defence against injection — set_config binds the value as a parameter,
// so there is no statement for it to escape from. It is here because a tenant
// carrying a newline or a control character is a sign that something upstream is
// putting the wrong string in the context, and that is worth failing on rather
// than storing rows under.
func checkTenant(v string) error {
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

// Execer is the part of a connection this package needs. Both *pgx.Conn and
// pgx.Tx satisfy it.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Apply sets the tenant on one connection. Exported so a caller holding its own
// connection — a migration, a test, a background job — can scope it the same way
// the pool does.
func Apply(ctx context.Context, conn Execer, tenantID string) error {
	if err := checkTenant(tenantID); err != nil {
		return err
	}
	// false: session scope. The parameter has to outlive the statement that sets
	// it, because a transaction the caller opens afterwards would otherwise roll
	// it back along with everything else.
	if _, err := conn.Exec(ctx, "SELECT set_config($1, $2, false)", SettingName, tenantID); err != nil {
		return fmt.Errorf("tenantdb: set %s: %w", SettingName, err)
	}
	return nil
}

// Clear removes the tenant from a connection, leaving it unable to read anything
// until one is set again.
func Clear(ctx context.Context, conn Execer) error {
	if _, err := conn.Exec(ctx, "SELECT set_config($1, '', false)", SettingName); err != nil {
		return fmt.Errorf("tenantdb: clear %s: %w", SettingName, err)
	}
	return nil
}

// NewPool opens a pool whose connections carry the tenant of the context that
// acquired them.
//
// A pool built any other way works against these schemas right up until a policy
// is consulted, and then returns nothing, so this is the only supported way to
// connect.
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("tenantdb: %w", err)
	}
	Configure(cfg)
	return pgxpool.NewWithConfig(ctx, cfg)
}

// Configure installs the tenant handling on a pool configuration the caller has
// built for itself, so pool sizing and TLS stay the caller's business.
func Configure(cfg *pgxpool.Config) {
	cfg.BeforeAcquire = func(ctx context.Context, conn *pgx.Conn) bool {
		tenant, err := TenantFrom(ctx)
		if err != nil {
			// No tenant on this context. Clear whatever the last request left,
			// so the query that follows is refused by the policy rather than
			// answered as somebody else. Returning false here instead would
			// destroy the connection and retry, which turns a caller's mistake
			// into a pool that churns.
			return Clear(ctx, conn) == nil
		}
		return Apply(ctx, conn, tenant) == nil
	}
}
