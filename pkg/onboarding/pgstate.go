package onboarding

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"p9e.in/samavaya/packages/errors"
	"p9e.in/samavaya/packages/ulid"
)

// PgStateStore is the pgxpool-backed ProvisioningStateStore. It reads
// and writes `onboarding.tenant_provisioning_state` and
// `onboarding.tenant_provisioning_step` from migration 000071.
type PgStateStore struct {
	pool *pgxpool.Pool
}

// NewPgStateStore builds a store bound to the shared pool.
func NewPgStateStore(pool *pgxpool.Pool) *PgStateStore {
	return &PgStateStore{pool: pool}
}

// Compile-time assertion.
var _ ProvisioningStateStore = (*PgStateStore)(nil)

// LoadOrStart looks up the existing (tenant, industry) row or creates
// a fresh 'running' one. Returns (provisioningID, existingState)
// where existingState is "" for freshly-created rows.
func (s *PgStateStore) LoadOrStart(
	ctx context.Context,
	tenantID, industryCode, profileVersion, actorID string,
) (string, string, error) {
	if s.pool == nil {
		return "", "", errors.InternalServer(
			"ONBOARDING_PGSTATE_NO_POOL",
			"PgStateStore was constructed without a pgxpool.Pool",
		)
	}

	var (
		existingID    string
		existingState string
		newID         string
	)
	err := s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		scanErr := tx.QueryRow(ctx, `
SELECT id, state
  FROM onboarding.tenant_provisioning_state
 WHERE tenant_id = $1 AND industry_code = $2`,
			tenantID, industryCode,
		).Scan(&existingID, &existingState)

		switch scanErr {
		case nil:
			if existingState == "completed" {
				return nil
			}
			_, err := tx.Exec(ctx, `
UPDATE onboarding.tenant_provisioning_state
   SET state           = 'running',
       last_error      = NULL,
       profile_version = $1,
       updated_by      = $2,
       updated_at      = NOW()
 WHERE id = $3`,
				profileVersion, actorID, existingID,
			)
			if err != nil {
				return fmt.Errorf("reset provisioning state: %w", err)
			}
			return nil
		case pgx.ErrNoRows:
			newID = ulid.New().String()
			_, err := tx.Exec(ctx, `
INSERT INTO onboarding.tenant_provisioning_state
       (id, tenant_id, industry_code, profile_version, state, created_by, updated_by)
VALUES ($1, $2, $3, $4, 'running', $5, $5)`,
				newID, tenantID, industryCode, profileVersion, actorID,
			)
			if err != nil {
				return fmt.Errorf("insert provisioning state: %w", err)
			}
			return nil
		default:
			return fmt.Errorf("read provisioning state: %w", scanErr)
		}
	})
	if err != nil {
		return "", "", err
	}
	if newID != "" {
		return newID, "", nil
	}
	return existingID, existingState, nil
}

// CompleteStep records a step as completed. ON CONFLICT DO NOTHING so
// re-recording is a no-op.
func (s *PgStateStore) CompleteStep(
	ctx context.Context,
	provisioningID, stepKind, stepKey string,
) error {
	_, err := s.pool.Exec(ctx, `
INSERT INTO onboarding.tenant_provisioning_step
       (id, provisioning_id, step_kind, step_key, state)
VALUES ($1, $2, $3, $4, 'completed')
    ON CONFLICT (provisioning_id, step_kind, step_key) DO NOTHING`,
		ulid.New().String(), provisioningID, stepKind, stepKey,
	)
	if err != nil {
		return fmt.Errorf("record step completion: %w", err)
	}
	return nil
}

// FailStep records a step failure with its error message. Idempotent:
// calling twice on the same (provisioningID, kind, key) is a no-op.
func (s *PgStateStore) FailStep(
	ctx context.Context,
	provisioningID, stepKind, stepKey, errorMsg string,
) error {
	_, err := s.pool.Exec(ctx, `
INSERT INTO onboarding.tenant_provisioning_step
       (id, provisioning_id, step_kind, step_key, state, last_error)
VALUES ($1, $2, $3, $4, 'failed', $5)
    ON CONFLICT (provisioning_id, step_kind, step_key) DO NOTHING`,
		ulid.New().String(), provisioningID, stepKind, stepKey, errorMsg,
	)
	if err != nil {
		return fmt.Errorf("record step failure: %w", err)
	}
	return nil
}

// IsStepCompleted reports whether the step has a 'completed' row.
func (s *PgStateStore) IsStepCompleted(
	ctx context.Context,
	provisioningID, stepKind, stepKey string,
) (bool, error) {
	var state string
	err := s.pool.QueryRow(ctx, `
SELECT state
  FROM onboarding.tenant_provisioning_step
 WHERE provisioning_id = $1 AND step_kind = $2 AND step_key = $3`,
		provisioningID, stepKind, stepKey,
	).Scan(&state)
	if err == pgx.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read step state: %w", err)
	}
	return state == "completed", nil
}

// MarkCompleted flips the parent row to 'completed' and sets completed_at.
// tenantID is required to set app.tenant_id LOCAL so RLS UPDATE policies pass
// under non-superuser DB role.
func (s *PgStateStore) MarkCompleted(ctx context.Context, tenantID, provisioningID, actorID string) error {
	return s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
UPDATE onboarding.tenant_provisioning_state
   SET state        = 'completed',
       last_error   = NULL,
       completed_at = NOW(),
       updated_by   = $1,
       updated_at   = NOW()
 WHERE id = $2`,
			actorID, provisioningID,
		)
		if err != nil {
			return fmt.Errorf("mark provisioning completed: %w", err)
		}
		return nil
	})
}

// MarkFailed flips the parent row to 'failed' and records last_error.
// tenantID is required to set app.tenant_id LOCAL so RLS UPDATE policies pass
// under non-superuser DB role.
func (s *PgStateStore) MarkFailed(ctx context.Context, tenantID, provisioningID, actorID, errorMsg string) error {
	return s.withTenantTx(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
UPDATE onboarding.tenant_provisioning_state
   SET state       = 'failed',
       last_error  = $1,
       updated_by  = $2,
       updated_at  = NOW()
 WHERE id = $3`,
			errorMsg, actorID, provisioningID,
		)
		if err != nil {
			return fmt.Errorf("mark provisioning failed: %w", err)
		}
		return nil
	})
}
