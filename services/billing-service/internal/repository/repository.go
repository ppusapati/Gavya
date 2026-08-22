package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ppusapati/gavya/services/billing-service/internal/domain"
)

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
	`sub_total,tax_amount,total_amount,currency,issued_at,due_at,COALESCE(paid_at,'0001-01-01 00:00:00+00'::timestamptz),COALESCE(notes,''),created_at,` +
	`updated_at,created_by,updated_by,deleted_at`

const invoiceItemCols = `id,tenant_id,invoice_id,description,quantity,unit_price,total_price,` +
	`tax_rate,created_at,updated_at,created_by,updated_by`

const paymentCols = `id,tenant_id,invoice_id,amount,currency,payment_method,COALESCE(reference_no,''),paid_at,` +
	`COALESCE(notes,''),created_at,updated_at,created_by,updated_by`

type Repository interface {
	CreateInvoice(ctx context.Context, inv *domain.Invoice) (*domain.Invoice, error)
	GetInvoice(ctx context.Context, id, tenantID string) (*domain.Invoice, error)
	UpdateInvoiceStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.Invoice, error)
	// AddItemAndRetotal writes a line and its invoice's totals together, so an
	// invoice can never disagree with the sum of its own lines.
	AddItemAndRetotal(ctx context.Context, item *domain.InvoiceItem, quantity, unitPrice, taxRate string) (*ItemOutcome, error)
	ListOutstandingInvoices(ctx context.Context, tenantID string) ([]*domain.Invoice, error)
	ListInvoiceItems(ctx context.Context, invoiceID, tenantID string) ([]*domain.InvoiceItem, error)
	// RecordPaymentAndSettle writes a payment and settles the invoice together,
	// so an invoice's status cannot disagree with the money against it.
	RecordPaymentAndSettle(ctx context.Context, p *domain.Payment, amount string) (*PaymentOutcome, error)
}

type repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) Repository {
	return &repo{pool: pool}
}

type scanner interface {
	Scan(dest ...any) error
}

func (r *repo) CreateInvoice(ctx context.Context, inv *domain.Invoice) (*domain.Invoice, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO invoices (id,tenant_id,customer_id,invoice_number,reference_id,reference_type,status,sub_total,tax_amount,total_amount,currency,issued_at,due_at,notes,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING `+invoiceCols,
		inv.ID, inv.TenantID, inv.CustomerID, inv.InvoiceNumber, inv.ReferenceID, inv.ReferenceType,
		inv.Status, inv.SubTotal, inv.TaxAmount, inv.TotalAmount, inv.Currency,
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

func (r *repo) UpdateInvoiceStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.Invoice, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE invoices SET status=$3,updated_by=$4,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING `+invoiceCols,
		id, tenantID, status, updatedBy,
	)
	return scanInvoice(row)
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
	err := s.Scan(&inv.ID, &inv.TenantID, &inv.CustomerID, &inv.InvoiceNumber, &inv.ReferenceID,
		&inv.ReferenceType, &inv.Status, &inv.SubTotal, &inv.TaxAmount, &inv.TotalAmount, &inv.Currency,
		&inv.IssuedAt, &inv.DueAt, &inv.PaidAt, &inv.Notes,
		&inv.CreatedAt, &inv.UpdatedAt, &inv.CreatedBy, &inv.UpdatedBy, &inv.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return inv, nil
}

func scanInvoiceItem(s scanner) (*domain.InvoiceItem, error) {
	item := &domain.InvoiceItem{}
	err := s.Scan(&item.ID, &item.TenantID, &item.InvoiceID, &item.Description,
		&item.Quantity, &item.UnitPrice, &item.TotalPrice, &item.TaxRate,
		&item.CreatedAt, &item.UpdatedAt, &item.CreatedBy, &item.UpdatedBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return item, nil
}

func scanPayment(s scanner) (*domain.Payment, error) {
	p := &domain.Payment{}
	err := s.Scan(&p.ID, &p.TenantID, &p.InvoiceID, &p.Amount, &p.Currency, &p.PaymentMethod,
		&p.ReferenceNo, &p.PaidAt, &p.Notes, &p.CreatedAt, &p.UpdatedAt, &p.CreatedBy, &p.UpdatedBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return p, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
