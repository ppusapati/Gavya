// Package saas provides the multi-tenant database-pool resolver used to
// route every request to the correct Postgres pool based on the caller's
// tenant.
//
// Two pool strategies coexist:
//
//   - Shared pool: the default. All tenants share one pgxpool.Pool;
//     isolation is enforced by RLS policies keyed on current_setting('app.tenant_id').
//   - Independent pool: premium tenants get their own pgxpool.Pool, resolved
//     per-request from the ClientProvider. This lets heavy tenants run on
//     dedicated Postgres instances without changing service code.
//
// DefaultDbProvider[TClient] is the generic entry point. Construct one with
// NewDbProvider(connStrResolver, clientProvider) where:
//
//   - ConnStrResolver (from packages/data) produces the DSN for a given
//     tenant ID.
//   - ClientProvider[TClient] creates a new client (typically *pgxpool.Pool
//     wrapped in the service's repo shim) from a DSN.
//
// The PoolConfig struct controls pool sizing, idle timeout, and max
// lifetime; DefaultPoolConfig() returns sensible production values
// (30 conns, 10 idle, 60s lifetime, 10s idle timeout).
//
// Middleware integration: packages/middleware/dbmiddleware reads the tenant
// from context, calls the provider, and plants the resolved pool on the
// p9context.DBPoolContext — repositories retrieve it via
// p9context.MustDBPoolContext(ctx).
package saas
