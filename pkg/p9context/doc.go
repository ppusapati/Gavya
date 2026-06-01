// Package p9context extends Go's context.Context with platform-specific
// keys for tenant isolation, auth, database pool routing, and localization.
//
// Key bundles:
//
//   - DBContext / DBPoolContext — carries the pgxpool.Pool to use for the
//     current request. dbmiddleware plants this after resolving the
//     tenant's DB strategy (shared vs independent).
//   - SaasContext / CurrentTenant — tenant-id + company-id + branch-id
//     scope used by RLS policies and sqlc query WHERE clauses.
//   - UserID / RequestID — per-request correlation fields surfaced by
//     the tenant middleware + request-id middleware respectively.
//   - RLSScope — typed wrapper around (tenant_id, company_id, branch_id)
//     used by core/bi/* repositories to build row-level-security WHERE
//     clauses. MustRLSScope panics when the context is missing scope —
//     defence in depth: RLS Postgres policies also enforce isolation.
//
// All setters follow the `New<Key>Context(ctx, value)` pattern and all
// readers follow either `From<Key>(ctx) (value, ok)` or `Must<Key>(ctx) value`.
// Always pass the returned context down the call chain — losing the context
// at a middleware boundary breaks tenant isolation.
package p9context
