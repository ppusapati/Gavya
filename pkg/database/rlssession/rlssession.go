// Package rlssession is the single source of truth for setting the
// PostgreSQL session GUCs (`app.tenant_id`, `app.company_id`, `app.branch_id`)
// that drive Row-Level Security policies across the platform.
//
// # Why this package exists
//
// FORCE RLS policies in this codebase use `current_setting('app.tenant_id',
// true)` (and the analogous company/branch settings). Every policy-protected
// read and write must run under a connection whose session has those GUCs
// set, otherwise the policy returns zero rows under `samavaya_app`
// (NOSUPERUSER NOBYPASSRLS) — silent data loss.
//
// Setting GUCs has two correctness pitfalls:
//
//  1. PostgreSQL does not accept bind parameters on `SET` statements.
//     `SET LOCAL app.tenant_id = $1` errors with `syntax error at or near
//     "$1"` (SQLSTATE 42601). Values must be string-interpolated.
//
//  2. Interpolation is unsafe with arbitrary input. The values come from
//     server-validated identity (JWT claims or trusted middleware), but
//     defence-in-depth requires rejecting any character that could break
//     out of the single-quoted literal: single-quote, backslash,
//     null byte, and newlines (which Postgres's `\\` literal continuation
//     can exploit in some configurations).
//
// Before this package, eight separate sites in the codebase reimplemented
// the same SET LOCAL fmt.Sprintf with subtly different validation rules.
// One of them used parameter binding and was silently broken until it
// finally hit a runtime path. Funnel everything through here.
//
// # API
//
// SetLocal — for use INSIDE a transaction (preferred). The variables are
// scoped to the current tx and released on COMMIT/ROLLBACK; the pool
// can hand the connection to the next request without leaking state.
//
// SetSession — for use on a held connection OUTSIDE a transaction (rare).
// The variables persist on the connection and must be RESET before
// release. Callers using this must own the connection lifecycle.
package rlssession

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"p9e.in/samavaya/packages/p9context"
)

// Execer is the minimal surface needed to run SET statements. Both
// pgx.Tx and *pgxpool.Conn satisfy it. Defined inline to avoid importing
// pgx and creating a dependency loop.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Vars enumerates the three RLS GUC names. Exposed as constants so other
// packages (notably packages/uow's WithRLSScope) can reference them
// without re-typing the strings.
const (
	VarTenantID  = "app.tenant_id"
	VarCompanyID = "app.company_id"
	VarBranchID  = "app.branch_id"
)

// ErrInvalidValue is returned when a GUC value contains a character that
// would break the single-quoted SQL literal. Callers should treat this
// as a hard 500 — server-validated identity should never contain these
// chars; if it does, something upstream is corrupted.
type ErrInvalidValue struct {
	GUC  string
	Char rune
}

func (e *ErrInvalidValue) Error() string {
	return fmt.Sprintf("rlssession: %s contains invalid char %q", e.GUC, e.Char)
}

// IsInvalidValue reports whether err is a value-validation error. Useful
// when callers want to map the error to a specific HTTP status.
func IsInvalidValue(err error) bool {
	var v *ErrInvalidValue
	return errors.As(err, &v)
}

// SetLocal sets app.tenant_id, app.company_id, app.branch_id on the
// current transaction (LOCAL scope). Empty values are skipped — tenant
// scope is required by every business policy, but some test/admin paths
// run with company/branch unset.
//
// Returns the first ErrInvalidValue encountered (no SET statements have
// run yet) or the underlying pgx exec error wrapped with the GUC name.
//
// Safe to call multiple times in the same tx; subsequent SET LOCALs
// override prior values.
func SetLocal(ctx context.Context, tx Execer, scope p9context.RLSScope) error {
	return setVars(ctx, tx, scope, "SET LOCAL")
}

// SetSession sets app.tenant_id, app.company_id, app.branch_id at SESSION
// scope on a held connection. The values persist until RESET (or until
// the connection is closed). Callers must own connection lifecycle and
// run RESET before releasing back to the pool — otherwise the next
// request gets the previous tenant's scope (security bug).
//
// Use SetLocal instead unless you specifically need session scope.
func SetSession(ctx context.Context, conn Execer, scope p9context.RLSScope) error {
	return setVars(ctx, conn, scope, "SET")
}

// vars is the iteration list for both SetLocal and SetSession. Packed
// into a stable order so logs / panics are deterministic.
type kv struct {
	guc string
	val string
}

func collect(scope p9context.RLSScope) []kv {
	return []kv{
		{VarTenantID, scope.TenantID},
		{VarCompanyID, scope.CompanyID},
		{VarBranchID, scope.BranchID},
	}
}

func setVars(ctx context.Context, ex Execer, scope p9context.RLSScope, verb string) error {
	pairs := collect(scope)

	// Validate ALL non-empty values up front — atomicity guarantee.
	// If we ran validate-then-Exec per-iteration, a valid tenant_id would
	// SET while an invalid company_id rolled back, leaving a partial scope.
	// In a transaction the partial SET would still apply to subsequent
	// statements until COMMIT/ROLLBACK; on a session-scoped connection it
	// would persist past the failed call. Both are RLS bugs, not just
	// correctness annoyances.
	for _, v := range pairs {
		if v.val == "" {
			continue
		}
		if err := validate(v.guc, v.val); err != nil {
			return err
		}
	}

	// All values valid — apply.
	for _, v := range pairs {
		if v.val == "" {
			continue
		}
		// Postgres does not accept bind parameters on SET; interpolate
		// after validation. The verb is a constant ("SET" or "SET LOCAL"),
		// the GUC name is one of three constants, and the value passed
		// validate(). No injection surface remains.
		stmt := fmt.Sprintf("%s %s = '%s'", verb, v.guc, v.val)
		if _, err := ex.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("rlssession: %s %s: %w", verb, v.guc, err)
		}
	}
	return nil
}

// validate rejects characters that could escape the single-quoted SQL
// literal. The list comes from PostgreSQL's string-literal grammar
// (https://www.postgresql.org/docs/current/sql-syntax-lexical.html):
//
//   '   — closes the literal
//   \   — when standard_conforming_strings = off (legacy), starts an
//          escape sequence; deny unconditionally for forward-compat
//   ;   — would terminate the SET statement and start a new one
//   \0  — Postgres rejects null bytes in text but client may not strip
//   \n, \r — newline / carriage return; some terminals treat as command
//
// Server-validated identity should never contain any of these. Failing
// fast with a clear error means an upstream corruption surfaces visibly
// instead of silently slipping into a SQL statement.
func validate(guc, val string) error {
	for _, c := range val {
		switch c {
		case '\'', '\\', ';', '\x00', '\n', '\r':
			return &ErrInvalidValue{GUC: guc, Char: c}
		}
	}
	return nil
}
