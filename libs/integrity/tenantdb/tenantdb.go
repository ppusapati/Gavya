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
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/tenantctx"
	"github.com/ppusapati/gavya/libs/integrity/tracing"
)

// The session parameter the isolation policies read.
const SettingName = "app.tenant_id"

// WithTenant returns a context that will scope every database connection taken
// under it to this tenant.
func WithTenant(ctx context.Context, tenantID string) context.Context {
	return tenantctx.With(ctx, tenantID)
}

// TenantFrom returns the tenant a context carries.
func TenantFrom(ctx context.Context) (string, error) { return tenantctx.From(ctx) }

// ErrNoTenant is returned when a caller asks for the tenant of a context that
// has none.
var ErrNoTenant = tenantctx.ErrNoTenant

// Execer is the part of a connection this package needs. Both *pgx.Conn and
// pgx.Tx satisfy it.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Apply sets the tenant on one connection. Exported so a caller holding its own
// connection — a migration, a test, a background job — can scope it the same way
// the pool does.
func Apply(ctx context.Context, conn Execer, tenantID string) error {
	if err := tenantctx.Check(tenantID); err != nil {
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
	// Before anything connects. Every service in this platform reaches its
	// database through here, so this is the one place the question "is this
	// connection encrypted" can be asked once instead of twenty-eight times.
	if err := checkSSLMode(dsn); err != nil {
		return nil, err
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("tenantdb: %w", err)
	}
	Configure(cfg)

	// Sizing and the two timeouts, for the same reason the sslmode check is
	// here: these are properties of the platform, and the twenty-eight copies of
	// a wrong answer that this package exists to prevent came from each service
	// deciding for itself. See limits.go for the arithmetic.
	applyLimits(cfg, dsn)

	// Every query this pool runs gets a span, inside whatever request is being
	// traced. Nothing when no collector is configured, which is the usual case.
	cfg.ConnConfig.Tracer = queryTracer{service: tracing.ServiceName()}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	watch(pool)
	return pool, nil
}

// Configure installs the tenant handling on a pool configuration the caller has
// built for itself, so pool sizing and TLS stay the caller's business.
//
// # WHY THIS IS PrepareConn AND NOT BeforeAcquire
//
// BeforeAcquire is pgx's older hook and is deprecated, which is how this came to
// be looked at; but the reason it is worth changing is what the two do when the
// setting cannot be made.
//
// BeforeAcquire answers with a bool and nothing else. A `false` destroys the
// connection and retries the acquire on a new one — so a database that has
// started refusing SET turns into a pool that opens connections, fails to
// configure them, throws them away and opens more, with the caller waiting and
// no error anywhere saying why.
//
// PrepareConn answers with a bool and an error, and the pairing below is the one
// that tells somebody what happened: the connection goes back to the pool and
// the query that wanted it fails with the reason. No churn, and no silence.
//
// Returning the connection is safe even though its app.tenant_id may now be
// stale, and that is the invariant this package is built on: the parameter is
// set on every acquire rather than cleared on release. The next caller's Apply
// overwrites whatever is there before a statement runs, and if that one fails
// too then that query fails too. A connection is never handed to a query under
// somebody else's tenant.
func Configure(cfg *pgxpool.Config) {
	cfg.PrepareConn = func(ctx context.Context, conn *pgx.Conn) (bool, error) {
		tenant, err := TenantFrom(ctx)
		if err != nil {
			// No tenant on this context. Clear whatever the last request left,
			// so the query that follows is refused by the policy rather than
			// answered as somebody else. Not an error: a background sweep or a
			// readiness ping legitimately has no tenant, and the policies are
			// what refuse it if it asks for rows.
			if clearErr := Clear(ctx, conn); clearErr != nil {
				return true, clearErr
			}
			return true, nil
		}
		if applyErr := Apply(ctx, conn, tenant); applyErr != nil {
			return true, applyErr
		}
		return true, nil
	}
}
