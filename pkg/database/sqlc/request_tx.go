// Package sqlc — request-scoped transaction helpers.
//
// `rlsConnMiddleware` (in app/cmd/main.go) acquires a pgxpool.Conn at the
// start of each authenticated HTTP request, opens a transaction, runs
// `SET LOCAL app.tenant_id` (and company/branch) from the request's
// RLSScope, and stashes the *pgx.Tx in the request context via
// `WithRequestTx`. The middleware commits + releases the conn when the
// handler returns.
//
// Inside the handler, every sqlc-generated repository call goes through
// the RLSPool (registered as the DBTX in fx.go provideQueries). The
// RLSPool consults `RequestTxFromCtx` and runs the query against the
// shared tx if present — so all N queries in the request share the same
// BEGIN/SET LOCAL/COMMIT triple instead of paying it N times.
//
// Without a request-scoped tx (background jobs, fx OnStart hooks, Kafka
// consumers), RLSPool falls back to the per-call implicit transaction —
// preserving correctness but at higher cost.

package sqlc

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type requestTxKey struct{}

// WithRequestTx attaches a per-request pgx.Tx to ctx. Called by
// rlsConnMiddleware after BEGIN + SET LOCAL.
func WithRequestTx(ctx context.Context, tx pgx.Tx) context.Context {
	if tx == nil {
		return ctx
	}
	return context.WithValue(ctx, requestTxKey{}, tx)
}

// RequestTxFromCtx returns the request-scoped tx from ctx, or nil if
// none is attached. RLSPool's Exec/Query/QueryRow/CopyFrom check this
// first — when present, they run against this tx instead of opening
// their own implicit transaction.
func RequestTxFromCtx(ctx context.Context) pgx.Tx {
	if v := ctx.Value(requestTxKey{}); v != nil {
		if tx, ok := v.(pgx.Tx); ok {
			return tx
		}
	}
	return nil
}
