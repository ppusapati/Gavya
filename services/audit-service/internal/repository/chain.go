package repository

import (
	"context"
	"fmt"
	"time"
)

// ChainRepository is the part of the trail that is about the trail itself
// rather than about what happened.
type ChainRepository interface {
	Seal(ctx context.Context, tenantID string, limit int) (Seal, error)
	Verify(ctx context.Context, tenantID string) (Verification, error)
	Checkpoint(ctx context.Context, tenantID string) (Checkpoint, error)
	Status(ctx context.Context, tenantID string) (Status, error)
}

// Seal is what one run of the sealer did.
type Seal struct {
	Sealed   int64
	LastSeq  int64
	LastHash string
}

// Verification is whether a tenant's chain still hangs together.
type Verification struct {
	Intact      bool
	RowsChecked int64
	BrokenAt    *int64
	BrokenID    string
	Detail      string
}

// Checkpoint is an anchor: where the chain had got to at a moment.
type Checkpoint struct {
	Seq     *int64
	RowHash string
	Taken   bool
}

// Status is how much of the trail is covered.
//
// UnsealedRows and OldestUnsealedAt are the honest part. Sealing happens after
// the fact, so the newest rows are not yet tamper-evident, and how far behind
// that is should be something an operator can see rather than assume.
type Status struct {
	TotalRows        int64
	SealedRows       int64
	UnsealedRows     int64
	LastSeq          *int64
	OldestUnsealedAt *time.Time
}

func (r *repo) Seal(ctx context.Context, tenantID string, limit int) (Seal, error) {
	if limit <= 0 {
		limit = 10000
	}
	var s Seal
	err := r.pool.QueryRow(ctx,
		"SELECT sealed, last_seq, last_hash FROM gavya_seal_audit_log($1, $2)", tenantID, limit).
		Scan(&s.Sealed, &s.LastSeq, &s.LastHash)
	if err != nil {
		return Seal{}, fmt.Errorf("seal: %w", err)
	}
	return s, nil
}

func (r *repo) Verify(ctx context.Context, tenantID string) (Verification, error) {
	var v Verification
	var brokenID *string
	var detail *string
	err := r.pool.QueryRow(ctx,
		`SELECT intact, rows_checked, broken_at, broken_id, detail
		 FROM gavya_verify_audit_chain($1)`, tenantID).
		Scan(&v.Intact, &v.RowsChecked, &v.BrokenAt, &brokenID, &detail)
	if err != nil {
		return Verification{}, fmt.Errorf("verify: %w", err)
	}
	if brokenID != nil {
		v.BrokenID = *brokenID
	}
	if detail != nil {
		v.Detail = *detail
	}
	return v, nil
}

func (r *repo) Checkpoint(ctx context.Context, tenantID string) (Checkpoint, error) {
	var c Checkpoint
	var hash *string
	err := r.pool.QueryRow(ctx,
		"SELECT seq, row_hash, taken FROM gavya_checkpoint_audit_chain($1)", tenantID).
		Scan(&c.Seq, &hash, &c.Taken)
	if err != nil {
		return Checkpoint{}, fmt.Errorf("checkpoint: %w", err)
	}
	if hash != nil {
		c.RowHash = *hash
	}
	return c, nil
}

func (r *repo) Status(ctx context.Context, tenantID string) (Status, error) {
	var s Status
	err := r.pool.QueryRow(ctx, `
		SELECT total_rows, sealed_rows, unsealed_rows, last_seq, oldest_unsealed_at
		FROM gavya_audit_chain_status WHERE tenant_id = $1`, tenantID).
		Scan(&s.TotalRows, &s.SealedRows, &s.UnsealedRows, &s.LastSeq, &s.OldestUnsealedAt)
	if err != nil {
		// A tenant with no rows has no status row, which is not a failure — it
		// is a tenant that has not done anything yet.
		return Status{}, nil
	}
	return s, nil
}
