package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Money describes the currency an amount is in, and how many digits after the
// point that currency has: 2 for a rupee, 0 for a yen, 3 for a dinar.
type Money struct {
	Code  string
	Scale int32
}

// ErrCurrencyMismatch is money recorded against a tenant in a currency that is
// not the one that tenant records money in.
var ErrCurrencyMismatch = errors.New("this tenant does not record money in that currency")

// ErrCurrencyUnset is a tenant that has not yet recorded any money here, so
// nothing has fixed which currency it uses.
var ErrCurrencyUnset = errors.New("this tenant has no currency recorded yet")

// pinCurrencyTx is PinTenantMoney inside a caller's transaction, so a sale and
// its currency pin commit together.
func pinCurrencyTx(ctx context.Context, tx pgx.Tx, tenantID, code string, scale int32) error {
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
		return fmt.Errorf("%w: %s is recorded here at %d decimal places, not %d",
			ErrCurrencyMismatch, have, haveScale, scale)
	}
	return nil
}

// PinTenantMoney fixes a tenant's currency on first use and refuses any other
// afterwards.
//
// A tenant records money in exactly one currency. Without a pin the invariant
// lives only in whatever the caller happened to send, and one mistyped request
// leaves a tenant with amounts in two currencies sitting side by side,
// indistinguishable in every total that adds them together.
//
// The upsert takes the row lock even when it changes nothing, so two first
// writes racing each other cannot both decide they are the first.
func (r *repo) PinTenantMoney(ctx context.Context, tenantID string, money Money) error {
	var have string
	var haveScale int32
	err := r.db.QueryRow(ctx,
		`INSERT INTO tenant_currency (tenant_id, currency, currency_scale)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (tenant_id) DO UPDATE SET tenant_id = EXCLUDED.tenant_id
		 RETURNING currency, currency_scale`,
		tenantID, money.Code, money.Scale).Scan(&have, &haveScale)
	if err != nil {
		return fmt.Errorf("pin currency: %w", err)
	}
	if have != money.Code {
		return fmt.Errorf("%w: it records in %s, not %s", ErrCurrencyMismatch, have, money.Code)
	}
	if haveScale != money.Scale {
		// The code matched but the scale did not, which means one of the two
		// records is reading the same digits as a different amount of money.
		return fmt.Errorf("%w: %s is recorded here at %d decimal places, not %d",
			ErrCurrencyMismatch, have, haveScale, money.Scale)
	}
	return nil
}

// TenantMoney reports the currency a tenant records money in.
//
// It is read from the pin rather than from tenant-service, so recording an
// amount does not depend on another service being reachable.
func (r *repo) TenantMoney(ctx context.Context, tenantID string) (Money, error) {
	var m Money
	err := r.db.QueryRow(ctx,
		`SELECT currency, currency_scale FROM tenant_currency WHERE tenant_id=$1`,
		tenantID).Scan(&m.Code, &m.Scale)
	if errors.Is(err, pgx.ErrNoRows) {
		return Money{}, ErrCurrencyUnset
	}
	if err != nil {
		return Money{}, fmt.Errorf("read tenant currency: %w", err)
	}
	return m, nil
}
