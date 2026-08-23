package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/ppusapati/gavya/services/billing-service/internal/domain"
)

// ErrNotDraft is an invoice that has been sent. Its contents are what a customer
// was shown, so they stop being editable at that point.
var ErrNotDraft = errors.New("only a draft invoice can be changed")

// ErrNotPayable is a payment against an invoice that cannot take one.
var ErrNotPayable = errors.New("this invoice cannot take a payment")

// InvoiceCancelledStatus is the one status that refuses a payment.
const InvoiceCancelledStatus = "cancelled"

// ItemOutcome is the line as written and the invoice it left behind.
type ItemOutcome struct {
	Item    *domain.InvoiceItem
	Invoice *domain.Invoice
}

// AddItemAndRetotal writes one line and the invoice's totals in a single
// transaction.
//
// It replaced four separate round trips — read the invoice, insert the line, sum
// the lines, update the totals — which went wrong in three ways:
//
//   - The line total was quantity * unit_price computed in float64 and stored
//     into NUMERIC(12,2), so an already-inexact product was rounded into the
//     column. On an invoice, that is the figure a customer is asked to pay.
//   - The sum-and-update was skipped entirely when the sum failed, and its own
//     error was logged and discarded. Either way the caller was told the line
//     had been added, and the invoice's total no longer matched its lines.
//   - The draft check happened in a different call from the write, so an invoice
//     could be sent in between and still take another line afterwards.
//
// The arithmetic now happens in the database, in the columns' own type, under
// the lock that the write holds.
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

func (r *repo) AddItemAndRetotal(ctx context.Context, item *domain.InvoiceItem, quantity, unitPrice, taxRate string, money Money) (*ItemOutcome, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var status string
	var taxInclusive bool
	if err := tx.QueryRow(ctx,
		`SELECT status, tax_inclusive FROM invoices
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`,
		item.InvoiceID, item.TenantID).Scan(&status, &taxInclusive); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("lock invoice: %w", err)
	}
	if status != domain.InvoiceDraft {
		return nil, fmt.Errorf("%w: this one is %s", ErrNotDraft, status)
	}
	if err := pinCurrency(ctx, tx, item.TenantID, money.Code, money.Scale); err != nil {
		return nil, err
	}

	// Rounded once, at the end. Rounding the inputs first would give a different
	// figure, and the two would be indistinguishable after the fact.
	newItem, err := scanInvoiceItem(tx.QueryRow(ctx,
		`INSERT INTO invoice_items (id,tenant_id,invoice_id,description,quantity,unit_price,total_price,tax_rate,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5::numeric,$6::numeric,ROUND($5::numeric * $6::numeric, $10),$7::numeric,$8,$9)
		 RETURNING `+invoiceItemCols,
		item.ID, item.TenantID, item.InvoiceID, item.Description,
		quantity, unitPrice, taxRate, item.CreatedBy, item.UpdatedBy, money.Scale))
	if err != nil {
		return nil, fmt.Errorf("add item: %w", err)
	}

	// Recomputed from the lines rather than adjusted by the new one, so the
	// invoice cannot drift away from what it is billing for.
	// Tax is worked out per line, from that line's own rate, and rounded there —
	// which is how it appears on a printed invoice and what a tax authority
	// expects to be able to check line by line.
	//
	// Where the price is quoted tax-inclusive the tax is extracted from it
	// rather than added to it: gross * rate / (100 + rate). Getting that
	// backwards overcharges every customer in half the world.
	invoice, err := scanInvoice(tx.QueryRow(ctx,
		`WITH lines AS (
		     SELECT
		       COALESCE(SUM(total_price), 0) AS gross,
		       COALESCE(SUM(
		         CASE WHEN $3 THEN ROUND(total_price * tax_rate / (100 + tax_rate), $4)
		              ELSE ROUND(total_price * tax_rate / 100, $4) END
		       ), 0) AS tax
		     FROM invoice_items WHERE invoice_id=$1 AND tenant_id=$2
		 )
		 UPDATE invoices SET
		     sub_total    = CASE WHEN $3 THEN lines.gross - lines.tax ELSE lines.gross END,
		     tax_amount   = lines.tax,
		     total_amount = CASE WHEN $3 THEN lines.gross ELSE lines.gross + lines.tax END,
		     updated_by   = $5,
		     updated_at   = NOW()
		 FROM lines
		 WHERE invoices.id=$1 AND invoices.tenant_id=$2 AND invoices.deleted_at IS NULL
		 RETURNING `+invoiceCols,
		item.InvoiceID, item.TenantID, taxInclusive, money.Scale, item.UpdatedBy))
	if err != nil {
		return nil, fmt.Errorf("retotal invoice: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return &ItemOutcome{Item: newItem, Invoice: invoice}, nil
}

// PaymentOutcome is the payment as written and the invoice it left behind.
type PaymentOutcome struct {
	Payment *domain.Payment
	Invoice *domain.Invoice
}

// RecordPaymentAndSettle writes a payment and settles the invoice if that
// payment covered it, in one transaction.
//
// It replaced four separate round trips — insert the payment, read the invoice,
// sum the payments, mark it paid — which left three ways for an invoice's state
// to disagree with the money against it:
//
//   - Two payments arriving together could each sum only the other's absence and
//     neither cross the total, leaving a fully paid invoice unsettled. The
//     invoice is now locked, so the second payment sums both.
//   - The mark-paid error was logged and discarded. The payment stood, the
//     invoice stayed unpaid, and nothing said so.
//   - The comparison ran on two float64 values read out of NUMERIC columns.
//     Whether a payment covers an invoice is now decided by the database, in
//     the columns' own type.
func (r *repo) RecordPaymentAndSettle(ctx context.Context, p *domain.Payment, amount string, money Money) (*PaymentOutcome, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	// Locked so that two payments cannot each decide the invoice is short.
	var status, invoiceCurrency string
	if err := tx.QueryRow(ctx,
		`SELECT status, currency FROM invoices
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`,
		p.InvoiceID, p.TenantID).Scan(&status, &invoiceCurrency); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("lock invoice: %w", err)
	}
	if status == InvoiceCancelledStatus {
		return nil, fmt.Errorf("%w: this one is cancelled", ErrNotPayable)
	}
	if err := pinCurrency(ctx, tx, p.TenantID, money.Code, money.Scale); err != nil {
		return nil, err
	}
	// A payment in a different currency from its invoice cannot be compared
	// against the amount owed, and adding it to the total paid would be adding
	// two different kinds of money together.
	if invoiceCurrency != money.Code {
		return nil, fmt.Errorf("%w: this invoice is in %s, the payment is in %s",
			ErrCurrencyMismatch, invoiceCurrency, money.Code)
	}

	payment, err := scanPayment(tx.QueryRow(ctx,
		`INSERT INTO payments (id,tenant_id,invoice_id,amount,currency,payment_method,reference_no,paid_at,notes,created_by,updated_by)
		 VALUES ($1,$2,$3,$4::numeric,$5,$6,$7,$8,$9,$10,$11) RETURNING `+paymentCols,
		p.ID, p.TenantID, p.InvoiceID, amount, p.Currency, p.PaymentMethod,
		p.ReferenceNo, p.PaidAt, p.Notes, p.CreatedBy, p.UpdatedBy))
	if err != nil {
		return nil, fmt.Errorf("record payment: %w", err)
	}

	// Whether the invoice is covered is decided in NUMERIC, by comparing the sum
	// of its payments against its total. An invoice already settled is left as
	// it is, so its paid_at keeps saying when it was actually settled.
	invoice, err := scanInvoice(tx.QueryRow(ctx,
		`WITH paid AS (
		     SELECT COALESCE(SUM(amount), 0) AS taken
		     FROM payments WHERE invoice_id=$1 AND tenant_id=$2
		 )
		 UPDATE invoices SET
		     status     = CASE WHEN paid.taken >= invoices.total_amount AND invoices.status <> 'paid'
		                       THEN 'paid' ELSE invoices.status END,
		     paid_at    = CASE WHEN paid.taken >= invoices.total_amount AND invoices.paid_at IS NULL
		                       THEN $3 ELSE invoices.paid_at END,
		     updated_by = $4,
		     updated_at = NOW()
		 FROM paid
		 WHERE invoices.id=$1 AND invoices.tenant_id=$2 AND invoices.deleted_at IS NULL
		 RETURNING `+invoiceCols,
		p.InvoiceID, p.TenantID, p.PaidAt, p.UpdatedBy))
	if err != nil {
		return nil, fmt.Errorf("settle invoice: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return &PaymentOutcome{Payment: payment, Invoice: invoice}, nil
}
