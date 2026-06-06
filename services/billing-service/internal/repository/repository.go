package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ppusapati/gavya/services/billing-service/internal/domain"
)

type Repository interface {
	CreateInvoice(ctx context.Context, inv *domain.Invoice) (*domain.Invoice, error)
	GetInvoice(ctx context.Context, id, tenantID string) (*domain.Invoice, error)
	UpdateInvoiceStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.Invoice, error)
	UpdateInvoiceTotals(ctx context.Context, id, tenantID string, subTotal, taxAmount, totalAmount float64, updatedBy string) (*domain.Invoice, error)
	MarkInvoicePaid(ctx context.Context, id, tenantID, updatedBy string, paidAt time.Time) (*domain.Invoice, error)
	ListOutstandingInvoices(ctx context.Context, tenantID string) ([]*domain.Invoice, error)
	CreateInvoiceItem(ctx context.Context, item *domain.InvoiceItem) (*domain.InvoiceItem, error)
	ListInvoiceItems(ctx context.Context, invoiceID, tenantID string) ([]*domain.InvoiceItem, error)
	SumInvoiceItems(ctx context.Context, invoiceID, tenantID string) (float64, error)
	SumPayments(ctx context.Context, invoiceID, tenantID string) (float64, error)
	CreatePayment(ctx context.Context, p *domain.Payment) (*domain.Payment, error)
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
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING *`,
		inv.ID, inv.TenantID, inv.CustomerID, inv.InvoiceNumber, inv.ReferenceID, inv.ReferenceType,
		inv.Status, inv.SubTotal, inv.TaxAmount, inv.TotalAmount, inv.Currency,
		inv.IssuedAt, inv.DueAt, inv.Notes, inv.CreatedBy, inv.UpdatedBy,
	)
	return scanInvoice(row)
}

func (r *repo) GetInvoice(ctx context.Context, id, tenantID string) (*domain.Invoice, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT * FROM invoices WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanInvoice(row)
}

func (r *repo) UpdateInvoiceStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.Invoice, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE invoices SET status=$3,updated_by=$4,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *`,
		id, tenantID, status, updatedBy,
	)
	return scanInvoice(row)
}

func (r *repo) UpdateInvoiceTotals(ctx context.Context, id, tenantID string, subTotal, taxAmount, totalAmount float64, updatedBy string) (*domain.Invoice, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE invoices SET sub_total=$3,tax_amount=$4,total_amount=$5,updated_by=$6,updated_at=NOW()
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *`,
		id, tenantID, subTotal, taxAmount, totalAmount, updatedBy,
	)
	return scanInvoice(row)
}

func (r *repo) MarkInvoicePaid(ctx context.Context, id, tenantID, updatedBy string, paidAt time.Time) (*domain.Invoice, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE invoices SET status='paid',paid_at=$3,updated_by=$4,updated_at=NOW()
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *`,
		id, tenantID, paidAt, updatedBy,
	)
	return scanInvoice(row)
}

func (r *repo) ListOutstandingInvoices(ctx context.Context, tenantID string) ([]*domain.Invoice, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM invoices WHERE tenant_id=$1 AND status IN ('sent','overdue') AND deleted_at IS NULL ORDER BY due_at`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Invoice
	for rows.Next() {
		inv := &domain.Invoice{}
		if err := rows.Scan(&inv.ID, &inv.TenantID, &inv.CustomerID, &inv.InvoiceNumber, &inv.ReferenceID,
			&inv.ReferenceType, &inv.Status, &inv.SubTotal, &inv.TaxAmount, &inv.TotalAmount, &inv.Currency,
			&inv.IssuedAt, &inv.DueAt, &inv.PaidAt, &inv.Notes,
			&inv.CreatedAt, &inv.UpdatedAt, &inv.CreatedBy, &inv.UpdatedBy, &inv.DeletedAt); err != nil {
			return nil, err
		}
		result = append(result, inv)
	}
	return result, rows.Err()
}

func (r *repo) CreateInvoiceItem(ctx context.Context, item *domain.InvoiceItem) (*domain.InvoiceItem, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO invoice_items (id,tenant_id,invoice_id,description,quantity,unit_price,total_price,tax_rate,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING *`,
		item.ID, item.TenantID, item.InvoiceID, item.Description, item.Quantity,
		item.UnitPrice, item.TotalPrice, item.TaxRate, item.CreatedBy, item.UpdatedBy,
	)
	return scanInvoiceItem(row)
}

func (r *repo) ListInvoiceItems(ctx context.Context, invoiceID, tenantID string) ([]*domain.InvoiceItem, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM invoice_items WHERE invoice_id=$1 AND tenant_id=$2 ORDER BY created_at`,
		invoiceID, tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.InvoiceItem
	for rows.Next() {
		item := &domain.InvoiceItem{}
		if err := rows.Scan(&item.ID, &item.TenantID, &item.InvoiceID, &item.Description,
			&item.Quantity, &item.UnitPrice, &item.TotalPrice, &item.TaxRate,
			&item.CreatedAt, &item.UpdatedAt, &item.CreatedBy, &item.UpdatedBy); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *repo) SumInvoiceItems(ctx context.Context, invoiceID, tenantID string) (float64, error) {
	var total float64
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(total_price),0) FROM invoice_items WHERE invoice_id=$1 AND tenant_id=$2`,
		invoiceID, tenantID,
	).Scan(&total)
	return total, err
}

func (r *repo) SumPayments(ctx context.Context, invoiceID, tenantID string) (float64, error) {
	var total float64
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount),0) FROM payments WHERE invoice_id=$1 AND tenant_id=$2`,
		invoiceID, tenantID,
	).Scan(&total)
	return total, err
}

func (r *repo) CreatePayment(ctx context.Context, p *domain.Payment) (*domain.Payment, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO payments (id,tenant_id,invoice_id,amount,currency,payment_method,reference_no,paid_at,notes,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING *`,
		p.ID, p.TenantID, p.InvoiceID, p.Amount, p.Currency, p.PaymentMethod,
		p.ReferenceNo, p.PaidAt, p.Notes, p.CreatedBy, p.UpdatedBy,
	)
	return scanPayment(row)
}

func scanInvoice(s scanner) (*domain.Invoice, error) {
	inv := &domain.Invoice{}
	err := s.Scan(&inv.ID, &inv.TenantID, &inv.CustomerID, &inv.InvoiceNumber, &inv.ReferenceID,
		&inv.ReferenceType, &inv.Status, &inv.SubTotal, &inv.TaxAmount, &inv.TotalAmount, &inv.Currency,
		&inv.IssuedAt, &inv.DueAt, &inv.PaidAt, &inv.Notes,
		&inv.CreatedAt, &inv.UpdatedAt, &inv.CreatedBy, &inv.UpdatedBy, &inv.DeletedAt)
	if err != nil {
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
		return nil, err
	}
	return item, nil
}

func scanPayment(s scanner) (*domain.Payment, error) {
	p := &domain.Payment{}
	err := s.Scan(&p.ID, &p.TenantID, &p.InvoiceID, &p.Amount, &p.Currency, &p.PaymentMethod,
		&p.ReferenceNo, &p.PaidAt, &p.Notes, &p.CreatedAt, &p.UpdatedAt, &p.CreatedBy, &p.UpdatedBy)
	if err != nil {
		return nil, err
	}
	return p, nil
}
