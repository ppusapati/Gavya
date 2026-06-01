// Package onboarding — RLS session helper for PgStateStore.
//
// onboarding.tenant_provisioning_state has FORCE RLS + tenant_isolation
// policy (added by migration 000218). Under non-superuser DB role
// samavaya_app, every UPDATE/INSERT/SELECT on it must run with
// app.tenant_id set or the policy rejects.

package onboarding

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"p9e.in/samavaya/packages/database/rlssession"
	"p9e.in/samavaya/packages/p9context"
)

// withTenantTx runs fn inside a transaction with `app.tenant_id` SET LOCAL.
// tenantID must be non-empty — empty would skip the SET (per rlssession's
// empty-skip semantics) and the FORCE RLS policy would reject every read.
func (s *PgStateStore) withTenantTx(
	ctx context.Context,
	tenantID string,
	fn func(tx pgx.Tx) error,
) error {
	if tenantID == "" {
		return fmt.Errorf("withTenantTx: tenantID is required")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := rlssession.SetLocal(ctx, tx, p9context.RLSScope{TenantID: tenantID}); err != nil {
		return fmt.Errorf("withTenantTx: %w", err)
	}

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
