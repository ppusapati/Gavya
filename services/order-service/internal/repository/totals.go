package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/services/order-service/internal/domain"
)

// ErrNotDraft is an order that has moved past the point where its contents can
// change. It is named so the caller is told the order's state refused the
// request, rather than that the service failed.
var ErrNotDraft = errors.New("only a draft order can be changed")

// Money describes the currency an amount is in. It travels with every write so
// the repository never has to assume one, and so a tenant's records cannot end
// up in two currencies that later get added together.
type Money struct {
	Code string
	// Scale is how many digits after the point this currency has: 2 for a rupee,
	// 0 for a yen, 3 for a dinar. Every rounding step below uses it rather than
	// a hardcoded 2, which is what lets one schema serve every country.
	Scale int32
}

// ItemOutcome is the item as written and the order it left behind.
type ItemOutcome struct {
	Item  *domain.OrderItem
	Order *domain.Order
}

// AddItemAndRetotal inserts one item and brings the order's totals back in step
// with its contents, in a single transaction.
//
// It replaced a sequence of four separate round trips — read the order, insert
// the item, sum the items, update the totals — with three problems:
//
//   - The line total was computed in Go as quantity * unit_price in float64 and
//     then stored into NUMERIC(12,2), so the figure written was a rounded
//     version of a number that was already slightly wrong. Both multiplications
//     now happen in the database, in the column's own type, and round exactly
//     once at the end.
//   - The totals update's error was logged and discarded before returning
//     success. An item was added, the order's totals silently stayed as they
//     were, and the caller was told it had worked. The order then disagreed with
//     the sum of its own lines.
//   - The draft check read the order in one call and acted on it in another, so
//     an order could be confirmed in between and still take a new line. The
//     check now happens under the lock that the write holds.
//
// quantity, unitPrice and taxRate are decimal literals, so what the caller asked
// for is what the database multiplies.
func (r *repo) AddItemAndRetotal(ctx context.Context, item *domain.OrderItem, quantity, unitPrice, taxRate string, money Money) (*ItemOutcome, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	// FOR UPDATE holds the order for the rest of the transaction, so its status
	// cannot change between being checked and being relied on.
	var status string
	var taxInclusive bool
	if err := tx.QueryRow(ctx,
		`SELECT status, tax_inclusive FROM orders
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`,
		item.OrderID, item.TenantID).Scan(&status, &taxInclusive); err != nil {
		if isNoRows(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("lock order: %w", err)
	}
	if status != domain.OrderDraft {
		return nil, fmt.Errorf("%w: this one is %s", ErrNotDraft, status)
	}
	if err := pinCurrency(ctx, tx, item.TenantID, money.Code, money.Scale); err != nil {
		return nil, err
	}

	// The line total is the product of two exact decimals, rounded once to the
	// column's scale. Rounding once at the end is what makes the figure
	// reproducible: rounding the inputs first would give a different answer.
	newItem, err := scanOrderItem(tx.QueryRow(ctx,
		`INSERT INTO order_items (id,tenant_id,order_id,sku_id,product_id,quantity,unit_price,total_price,tax_rate,status,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6::numeric,$7::numeric,ROUND($6::numeric * $7::numeric, $11),$8::numeric,$9,$10,$12)
		 RETURNING `+orderItemCols,
		item.ID, item.TenantID, item.OrderID, item.SKUID, item.ProductID,
		quantity, unitPrice, taxRate, item.Status, item.CreatedBy, money.Scale, item.UpdatedBy))
	if err != nil {
		return nil, fmt.Errorf("add item: %w", err)
	}

	// The totals are recomputed from the lines rather than adjusted by the new
	// one, so they cannot drift away from the contents over an order's life.
	// Tax is worked out per line, from that line's own rate, and rounded there.
	// Where the price is quoted tax-inclusive the tax is extracted from it
	// rather than added to it.
	order, err := scanOrder(tx.QueryRow(ctx,
		`WITH lines AS (
		     SELECT
		       COALESCE(SUM(total_price), 0) AS gross,
		       COALESCE(SUM(
		         CASE WHEN $3 THEN ROUND(total_price * tax_rate / (100 + tax_rate), $4)
		              ELSE ROUND(total_price * tax_rate / 100, $4) END
		       ), 0) AS tax
		     FROM order_items WHERE order_id=$1 AND tenant_id=$2
		 )
		 UPDATE orders SET
		     sub_total    = CASE WHEN $3 THEN lines.gross - lines.tax ELSE lines.gross END,
		     tax_amount   = lines.tax,
		     total_amount = CASE WHEN $3 THEN lines.gross ELSE lines.gross + lines.tax END,
		     updated_by   = $5,
		     updated_at   = NOW()
		 FROM lines
		 WHERE orders.id=$1 AND orders.tenant_id=$2 AND orders.deleted_at IS NULL
		 RETURNING `+orderCols,
		item.OrderID, item.TenantID, taxInclusive, money.Scale, item.UpdatedBy))
	if err != nil {
		return nil, fmt.Errorf("retotal order: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return &ItemOutcome{Item: newItem, Order: order}, nil
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
