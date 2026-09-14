package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/audit"
	"github.com/ppusapati/gavya/libs/integrity/currency"
	"github.com/ppusapati/gavya/libs/integrity/money"

	"github.com/ppusapati/gavya/services/billing-service/internal/domain"
)

// moneyColumnScale is how many decimals the money columns hold.
//
// They are NUMERIC(18,4) so that one schema serves a yen deployment and a dinar
// one: four is the most any ISO 4217 currency has. It is not how many decimals
// any particular amount has — a rupee total stored there reads back as
// "1250.0000", and those trailing zeros are the column's padding.
const moneyColumnScale int32 = 4

// parseAmount turns a stored decimal literal into money at its currency's scale.
//
// A row whose currency is unreadable is an error rather than a zero: an amount
// with no currency is not an amount, and returning one as though it were is how
// a rupee figure ends up being read as dollars. money.ParseStored is what
// separates the column's padding from the amount's real precision.
func parseAmount(literal, code string) (money.Money, error) {
	normalised, err := currency.Normalise(code)
	if err != nil {
		return money.Money{}, fmt.Errorf("currency %q: %w", code, err)
	}
	scale, err := currency.Scale(normalised)
	if err != nil {
		return money.Money{}, fmt.Errorf("currency %q: %w", code, err)
	}
	return money.ParseStored(literal, moneyColumnScale, scale, normalised)
}

// ErrNotFound lets a caller tell a missing record from a failed query. Without
// it every outcome reaches the handler as an opaque error and is reported as
// internal, so a client cannot distinguish "no such invoice" from "the database
// is unreachable".
var ErrNotFound = errors.New("not found")

// ErrDuplicateInvoiceNumber is the unique violation on (tenant_id,
// invoice_number), named so the handler can report a conflict rather than an
// internal failure.
var ErrDuplicateInvoiceNumber = errors.New("an invoice with that number already exists")

// Columns are listed explicitly rather than selected with *, because the scans
// below are positional: adding a column to the table would silently misalign
// every field after it.
const invoiceCols = `id,tenant_id,customer_id,invoice_number,COALESCE(reference_id,''),COALESCE(reference_type,''),status,` +
	`sub_total,tax_amount,total_amount,currency,tax_inclusive,issued_at,due_at,COALESCE(paid_at,'0001-01-01 00:00:00+00'::timestamptz),COALESCE(notes,''),created_at,` +
	`updated_at,created_by,updated_by,deleted_at`

// An invoice line has no currency column of its own: its currency is the
// invoice's, and a second copy on the line is a second thing that can disagree
// with the first. The subquery works in a RETURNING clause as well as a SELECT,
// so the insert path and the read path get it the same way rather than one of
// them being handed it by a caller who might be wrong.
const invoiceItemCols = `id,tenant_id,invoice_id,description,quantity,unit_price,total_price,` +
	`tax_rate,created_at,updated_at,created_by,updated_by,` +
	`(SELECT currency FROM invoices WHERE invoices.id = invoice_items.invoice_id)`

const paymentCols = `id,tenant_id,invoice_id,amount,currency,payment_method,COALESCE(reference_no,''),paid_at,` +
	`COALESCE(notes,''),created_at,updated_at,created_by,updated_by`

type Repository interface {
	CreateInvoice(ctx context.Context, inv *domain.Invoice) (*domain.Invoice, error)
	GetInvoice(ctx context.Context, id, tenantID string) (*domain.Invoice, error)
	UpdateInvoiceStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.Invoice, error)
	// TenantMoney reports the currency this tenant records money in.
	TenantMoney(ctx context.Context, tenantID string) (Money, error)
	// PinTenantMoney fixes that currency, or confirms the one already fixed.
	PinTenantMoney(ctx context.Context, tenantID string, money Money) error
	// AddItemAndRetotal writes a line and its invoice's totals together, so an
	// invoice can never disagree with the sum of its own lines.
	AddItemAndRetotal(ctx context.Context, item *domain.InvoiceItem, unitPrice string, money Money) (*ItemOutcome, error)
	ListOutstandingInvoices(ctx context.Context, tenantID string) ([]*domain.Invoice, error)
	ListInvoiceItems(ctx context.Context, invoiceID, tenantID string) ([]*domain.InvoiceItem, error)
	// RecordPaymentAndSettle writes a payment and settles the invoice together,
	// so an invoice's status cannot disagree with the money against it.
	RecordPaymentAndSettle(ctx context.Context, p *domain.Payment, amount string, money Money) (*PaymentOutcome, error)
}

// IDs supplies the identifier each audit entry carries.
type IDs interface{ New() string }

type repo struct {
	pool *pgxpool.Pool
	ids  IDs
}

func New(pool *pgxpool.Pool, ids IDs) Repository {
	return &repo{pool: pool, ids: ids}
}

const serviceName = "billing-service"

type scanner interface {
	Scan(dest ...any) error
}

func (r *repo) CreateInvoice(ctx context.Context, inv *domain.Invoice) (*domain.Invoice, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO invoices (id,tenant_id,customer_id,invoice_number,reference_id,reference_type,status,sub_total,tax_amount,total_amount,currency,tax_inclusive,issued_at,due_at,notes,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8::numeric,$9::numeric,$10::numeric,$11,$12,$13,$14,$15,$16,$17) RETURNING `+invoiceCols,
		inv.ID, inv.TenantID, inv.CustomerID, inv.InvoiceNumber, inv.ReferenceID, inv.ReferenceType,
		inv.Status, inv.SubTotal.String(), inv.TaxAmount.String(), inv.TotalAmount.String(),
		inv.TotalAmount.Currency, inv.TaxInclusive,
		inv.IssuedAt, inv.DueAt, inv.Notes, inv.CreatedBy, inv.UpdatedBy,
	)
	out, err := scanInvoice(row)
	if isUniqueViolation(err) {
		return nil, ErrDuplicateInvoiceNumber
	}
	return out, err
}

func (r *repo) GetInvoice(ctx context.Context, id, tenantID string) (*domain.Invoice, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+invoiceCols+` FROM invoices WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanInvoice(row)
}

// UpdateInvoiceStatus moves an invoice between states and records what it was in.
//
// An invoice's status is a decision, not a fact about the world: sending it,
// voiding it, marking it paid. `updated_by` names whoever made the last one and
// nothing said what they changed it from — so an invoice reading "void" gave no
// indication whether it had been a draft nobody had sent or something a customer
// had already paid against. Those are different conversations.
//
// The entry goes in the same transaction as the change, so an invoice that moved
// and a record of it moving cannot come apart.
func (r *repo) UpdateInvoiceStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.Invoice, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// Read under the lock the update will take, so a concurrent transition
	// cannot land between the read and the write and be recorded as the state
	// this change started from.
	var before, total, code string
	if err := tx.QueryRow(ctx,
		`SELECT status, total_amount, currency FROM invoices
		  WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`,
		id, tenantID).Scan(&before, &total, &code); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	row := tx.QueryRow(ctx,
		`UPDATE invoices SET status=$3,updated_by=$4,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING `+invoiceCols,
		id, tenantID, status, updatedBy,
	)
	inv, err := scanInvoice(row)
	if err != nil {
		return nil, err
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "update_invoice_status", ResourceType: "invoice", ResourceID: id,
		Before: map[string]any{"status": before},
		// The total travels with it because what an invoice was worth is what
		// makes a void worth asking about.
		After:       map[string]any{"status": status, "total_amount": total},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	return inv, tx.Commit(ctx)
}

func (r *repo) ListOutstandingInvoices(ctx context.Context, tenantID string) ([]*domain.Invoice, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+invoiceCols+` FROM invoices WHERE tenant_id=$1 AND status IN ('sent','overdue') AND deleted_at IS NULL ORDER BY due_at`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Invoice
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, inv)
	}
	return result, rows.Err()
}

func (r *repo) ListInvoiceItems(ctx context.Context, invoiceID, tenantID string) ([]*domain.InvoiceItem, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+invoiceItemCols+` FROM invoice_items WHERE invoice_id=$1 AND tenant_id=$2 ORDER BY created_at`,
		invoiceID, tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.InvoiceItem
	for rows.Next() {
		item, err := scanInvoiceItem(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func scanInvoice(s scanner) (*domain.Invoice, error) {
	inv := &domain.Invoice{}
	var sub, tax, total, code string
	err := s.Scan(&inv.ID, &inv.TenantID, &inv.CustomerID, &inv.InvoiceNumber, &inv.ReferenceID,
		&inv.ReferenceType, &inv.Status, &sub, &tax, &total, &code, &inv.TaxInclusive,
		&inv.IssuedAt, &inv.DueAt, &inv.PaidAt, &inv.Notes,
		&inv.CreatedAt, &inv.UpdatedAt, &inv.CreatedBy, &inv.UpdatedBy, &inv.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	for _, f := range []struct {
		name string
		in   string
		out  *money.Money
	}{
		{"sub_total", sub, &inv.SubTotal},
		{"tax_amount", tax, &inv.TaxAmount},
		{"total_amount", total, &inv.TotalAmount},
	} {
		if *f.out, err = parseAmount(f.in, code); err != nil {
			return nil, fmt.Errorf("invoice %s %s: %w", inv.ID, f.name, err)
		}
	}
	return inv, nil
}

func scanInvoiceItem(s scanner) (*domain.InvoiceItem, error) {
	item := &domain.InvoiceItem{}
	var unit, total, code string
	err := s.Scan(&item.ID, &item.TenantID, &item.InvoiceID, &item.Description,
		&item.Quantity, &unit, &total, &item.TaxRate,
		&item.CreatedAt, &item.UpdatedAt, &item.CreatedBy, &item.UpdatedBy, &code)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if item.UnitPrice, err = parseAmount(unit, code); err != nil {
		return nil, fmt.Errorf("invoice item %s unit_price: %w", item.ID, err)
	}
	if item.TotalPrice, err = parseAmount(total, code); err != nil {
		return nil, fmt.Errorf("invoice item %s total_price: %w", item.ID, err)
	}
	return item, nil
}

func scanPayment(s scanner) (*domain.Payment, error) {
	p := &domain.Payment{}
	var amount, code string
	err := s.Scan(&p.ID, &p.TenantID, &p.InvoiceID, &amount, &code, &p.PaymentMethod,
		&p.ReferenceNo, &p.PaidAt, &p.Notes, &p.CreatedAt, &p.UpdatedAt, &p.CreatedBy, &p.UpdatedBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if p.Amount, err = parseAmount(amount, code); err != nil {
		return nil, fmt.Errorf("payment %s: %w", p.ID, err)
	}
	return p, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
