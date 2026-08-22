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
func (r *repo) AddItemAndRetotal(ctx context.Context, item *domain.InvoiceItem, quantity, unitPrice, taxRate string) (*ItemOutcome, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var status string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM invoices WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`,
		item.InvoiceID, item.TenantID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("lock invoice: %w", err)
	}
	if status != domain.InvoiceDraft {
		return nil, fmt.Errorf("%w: this one is %s", ErrNotDraft, status)
	}

	// Rounded once, at the end. Rounding the inputs first would give a different
	// figure, and the two would be indistinguishable after the fact.
	newItem, err := scanInvoiceItem(tx.QueryRow(ctx,
		`INSERT INTO invoice_items (id,tenant_id,invoice_id,description,quantity,unit_price,total_price,tax_rate,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5::numeric,$6::numeric,ROUND($5::numeric * $6::numeric, 2),$7::numeric,$8,$9)
		 RETURNING `+invoiceItemCols,
		item.ID, item.TenantID, item.InvoiceID, item.Description,
		quantity, unitPrice, item.TaxRate, item.CreatedBy, item.UpdatedBy))
	if err != nil {
		return nil, fmt.Errorf("add item: %w", err)
	}

	// Recomputed from the lines rather than adjusted by the new one, so the
	// invoice cannot drift away from what it is billing for.
	invoice, err := scanInvoice(tx.QueryRow(ctx,
		`WITH lines AS (
		     SELECT COALESCE(SUM(total_price), 0) AS lines_total
		     FROM invoice_items WHERE invoice_id=$1 AND tenant_id=$2
		 )
		 UPDATE invoices SET
		     sub_total    = lines.lines_total,
		     tax_amount   = ROUND(lines.lines_total * $3::numeric, 2),
		     total_amount = lines.lines_total + ROUND(lines.lines_total * $3::numeric, 2),
		     updated_by   = $4,
		     updated_at   = NOW()
		 FROM lines
		 WHERE invoices.id=$1 AND invoices.tenant_id=$2 AND invoices.deleted_at IS NULL
		 RETURNING `+invoiceCols,
		item.InvoiceID, item.TenantID, taxRate, item.UpdatedBy))
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
func (r *repo) RecordPaymentAndSettle(ctx context.Context, p *domain.Payment, amount string) (*PaymentOutcome, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	// Locked so that two payments cannot each decide the invoice is short.
	var status string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM invoices WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`,
		p.InvoiceID, p.TenantID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("lock invoice: %w", err)
	}
	if status == InvoiceCancelledStatus {
		return nil, fmt.Errorf("%w: this one is cancelled", ErrNotPayable)
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
