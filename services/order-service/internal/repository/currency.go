package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrCurrencyMismatch is money recorded against a tenant in a currency that is
// not the one that tenant records money in.
//
// It is named rather than left as a constraint violation because it is the one
// failure a caller can actually act on: they have sent the wrong currency, and
// the message tells them which one this tenant uses.
var ErrCurrencyMismatch = errors.New("this tenant does not record money in that currency")

// pinCurrency fixes a tenant's currency on first use and refuses anything else
// afterwards.
//
// A tenant records money in exactly one currency. Without this the invariant
// lives only in whatever the caller happened to send, and one mistyped request
// leaves a tenant with rupee orders and dollar orders sitting side by side,
// indistinguishable in every total that adds them up.
//
// The upsert takes the row lock even when it changes nothing, so two first
// writes racing each other cannot both decide they are the first.
func pinCurrency(ctx context.Context, tx pgx.Tx, tenantID, code string, scale int32) error {
	var have string
	var haveScale int32
	err := tx.QueryRow(ctx,
		`INSERT INTO tenant_currency (tenant_id, currency, currency_scale)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (tenant_id) DO UPDATE SET tenant_id = EXCLUDED.tenant_id
		 RETURNING currency, currency_scale`,
		tenantID, code, scale).Scan(&have, &haveScale)
	if err != nil {
		return fmt.Errorf("pin currency: %w", err)
	}
	if have != code {
		return fmt.Errorf("%w: it records in %s, not %s", ErrCurrencyMismatch, have, code)
	}
	if haveScale != scale {
		// The code matched but the scale did not, which means one of the two
		// records is reading the same digits as a different amount of money.
		return fmt.Errorf("%w: %s is recorded here at %d decimal places, not %d",
			ErrCurrencyMismatch, have, haveScale, scale)
	}
	return nil
}

// TenantMoney reports the currency a tenant records money in.
//
// It is read from the pin rather than from tenant-service, so recording an
// an order line does not depend on another service being reachable. The pin is
// written when the tenant's first order states its currency.
func (r *repo) TenantMoney(ctx context.Context, tenantID string) (Money, error) {
	var m Money
	err := r.pool.QueryRow(ctx,
		`SELECT currency, currency_scale FROM tenant_currency WHERE tenant_id=$1`,
		tenantID).Scan(&m.Code, &m.Scale)
	if errors.Is(err, pgx.ErrNoRows) {
		return Money{}, ErrNotFound
	}
	if err != nil {
		return Money{}, fmt.Errorf("read tenant currency: %w", err)
	}
	return m, nil
}

// PinTenantMoney fixes a tenant's currency, or confirms the one already fixed.
// This is what the first order for a tenant calls.
func (r *repo) PinTenantMoney(ctx context.Context, tenantID string, money Money) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)
	if err := pinCurrency(ctx, tx, tenantID, money.Code, money.Scale); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
