// Package timeout centralises operation-timeout management.
//
// TimeoutProvider exposes two timeouts sourced from config.Data:
//
//   - Default: the 30s (tunable) ceiling for normal request paths — applied
//     by middleware before DB / cache / RPC calls.
//   - LongQuery: the 5min (tunable) ceiling for known-slow operations
//     (reports, bulk inserts, heavy aggregations) — callers opt in.
//
// The provider's ApplyTimeout(ctx) helper:
//
//   - Returns ctx unchanged when ctx already has an earlier deadline.
//     (Never EXTEND an upstream deadline — doing so breaks cascading
//     cancellation semantics.)
//   - Adds the configured Default timeout when ctx has no deadline yet.
//
// Plumbing:
//
//	func (r *Repo) Get(ctx context.Context, id string) (*Row, error) {
//	    ctx, cancel := r.deps.Tp.ApplyTimeout(ctx)
//	    defer cancel()
//	    // … run query on ctx …
//	}
//
// WithTimeout(defaultTimeout, longQueryTimeout) is the config-builder
// helper used at bootstrap to seed *config.Data with desired values.
package timeout
