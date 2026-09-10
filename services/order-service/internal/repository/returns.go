package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ppusapati/gavya/libs/integrity/audit"

	"github.com/ppusapati/gavya/services/order-service/internal/domain"
)

// ErrRefundTooLarge is a refund that would take the total refunded on an order
// past what the order charged. The database refuses it; this names it so the
// caller is told what is wrong rather than that something failed.
var ErrRefundTooLarge = errors.New("that would refund more than the order charged")

// ErrNotReturnable is a return requested against an order that has nothing to
// return: one still in draft, or one that was cancelled.
var ErrNotReturnable = errors.New("this order cannot be returned")

// ErrBadTransition is a return being moved somewhere it cannot go from where it
// is.
var ErrBadTransition = errors.New("a return cannot move to that state from this one")

const returnCols = `id,tenant_id,order_id,COALESCE(reason,''),status,refund_amount,currency,` +
	`requested_at,processed_at,created_at,updated_at,created_by,updated_by,deleted_at`

// RequestReturn records that somebody has asked for a refund.
//
// A request is not money. It is recorded whatever it asks for — including more
// than the order charged — because refusing to write it down loses the fact that
// it was made, and what somebody asked for is part of the record of what was
// decided. The database refuses it at approval, which is when it becomes money.
//
// The order is checked here rather than left to the foreign key, because "no such
// order" and "that order is still a draft" are different things to be told.
func (r *repo) RequestReturn(ctx context.Context, ret *domain.Return, amount string, money Money) (*domain.Return, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var status string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM orders WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		ret.OrderID, ret.TenantID).Scan(&status); err != nil {
		if isNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read order: %w", err)
	}
	switch status {
	case domain.OrderDraft:
		return nil, fmt.Errorf("%w: it is still a draft, so nothing has been sent", ErrNotReturnable)
	case domain.OrderCancelled:
		return nil, fmt.Errorf("%w: it was cancelled", ErrNotReturnable)
	}
	if err := pinCurrency(ctx, tx, ret.TenantID, money.Code, money.Scale); err != nil {
		return nil, err
	}

	out, err := scanReturn(tx.QueryRow(ctx,
		`INSERT INTO returns (id,tenant_id,order_id,reason,status,refund_amount,currency,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6::numeric,$7,$8,$8) RETURNING `+returnCols,
		ret.ID, ret.TenantID, ret.OrderID, ret.Reason, domain.ReturnRequested,
		amount, money.Code, ret.CreatedBy))
	if err != nil {
		return nil, fmt.Errorf("request return: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return out, nil
}

// TransitionReturn moves a return through the states its type declares, and
// records what it moved from.
//
// Approving, rejecting and completing a return are each a decision somebody
// could be asked to defend — a refund is money leaving — so each writes an audit
// entry in the same transaction as the change. Without it a return reading
// "rejected" gives no indication whether it was ever approved, or by whom, and
// `updated_by` names only whoever touched it last.
func (r *repo) TransitionReturn(ctx context.Context, id, tenantID, to, updatedBy string) (*domain.Return, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	// Read under the lock the update will take, so a concurrent transition
	// cannot land in between and be recorded as the state this one started from.
	var from string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM returns WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`,
		id, tenantID).Scan(&from); err != nil {
		if isNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("lock return: %w", err)
	}
	if !domain.ReturnMayMove(from, to) {
		return nil, fmt.Errorf("%w: it is %s and cannot become %s", ErrBadTransition, from, to)
	}

	// processed_at is set by the transition that ends the decision, and only the
	// first time. A return approved and then completed was processed when it was
	// approved; overwriting it would lose when the decision was actually made.
	out, err := scanReturn(tx.QueryRow(ctx,
		// $3 is cast because it is read twice — once as the new status and once
		// in the CASE — and PostgreSQL deduces a different type from each,
		// which it refuses rather than guesses at.
		`UPDATE returns
		    SET status=$3::varchar, updated_by=$4, updated_at=NOW(),
		        processed_at = CASE WHEN $3::varchar <> 'requested' AND processed_at IS NULL
		                            THEN NOW() ELSE processed_at END
		  WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL
		  RETURNING `+returnCols,
		id, tenantID, to, updatedBy))
	if err != nil {
		if isRefundTooLarge(err) {
			return nil, fmt.Errorf("%w: %s", ErrRefundTooLarge, refundDetail(err))
		}
		return nil, fmt.Errorf("move return: %w", err)
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "update_return_status", ResourceType: "return", ResourceID: id,
		Before: map[string]any{"status": from},
		After: map[string]any{
			"status": to,
			// The figure is on the entry because a refund's amount is what the
			// decision was about, and reading the return afterwards shows only
			// what it is now.
			"refund_amount": out.RefundAmount.String(),
			"currency":      out.RefundAmount.Currency,
		},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return out, nil
}

func (r *repo) GetReturn(ctx context.Context, id, tenantID string) (*domain.Return, error) {
	return scanReturn(r.pool.QueryRow(ctx,
		`SELECT `+returnCols+` FROM returns WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID))
}

func (r *repo) ListOrderReturns(ctx context.Context, orderID, tenantID string) ([]*domain.Return, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+returnCols+` FROM returns
		  WHERE order_id=$1 AND tenant_id=$2 AND deleted_at IS NULL
		  ORDER BY requested_at`,
		orderID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Return
	for rows.Next() {
		ret, err := scanReturn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ret)
	}
	return out, rows.Err()
}

func scanReturn(s scanner) (*domain.Return, error) {
	ret := &domain.Return{}
	var amount, code string
	err := s.Scan(&ret.ID, &ret.TenantID, &ret.OrderID, &ret.Reason, &ret.Status,
		&amount, &code, &ret.RequestedAt, &ret.ProcessedAt,
		&ret.CreatedAt, &ret.UpdatedAt, &ret.CreatedBy, &ret.UpdatedBy, &ret.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if ret.RefundAmount, err = parseAmount(amount, code); err != nil {
		return nil, fmt.Errorf("return %s: %w", ret.ID, err)
	}
	return ret, nil
}

// isRefundTooLarge recognises the trigger's refusal. It is a check violation
// raised by name rather than a named constraint, so the message is what
// identifies it.
func isRefundTooLarge(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23514" && pgErr.Message != "" &&
		len(pgErr.Message) > 9 && pgErr.Message[:9] == "refunding"
}

func refundDetail(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Message
	}
	return err.Error()
}
