package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ppusapati/gavya/services/order-service/internal/domain"
)

type Repository interface {
	CreateOrder(ctx context.Context, o *domain.Order) (*domain.Order, error)
	GetOrder(ctx context.Context, id, tenantID string) (*domain.Order, error)
	ListOrders(ctx context.Context, tenantID, status string) ([]*domain.Order, error)
	UpdateOrderStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.Order, error)
	UpdateOrderTotals(ctx context.Context, id, tenantID string, subTotal, taxAmount, totalAmount float64, updatedBy string) (*domain.Order, error)
	CreateOrderItem(ctx context.Context, item *domain.OrderItem) (*domain.OrderItem, error)
	ListOrderItems(ctx context.Context, orderID, tenantID string) ([]*domain.OrderItem, error)
	SumOrderItems(ctx context.Context, orderID, tenantID string) (float64, error)
	CreateInvoice(ctx context.Context, inv *domain.Invoice) (*domain.Invoice, error)
	GetInvoice(ctx context.Context, id, tenantID string) (*domain.Invoice, error)
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

func (r *repo) CreateOrder(ctx context.Context, o *domain.Order) (*domain.Order, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO orders (id,tenant_id,customer_id,order_number,status,sub_total,tax_amount,total_amount,currency,shipping_address,notes,ordered_at,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING *`,
		o.ID, o.TenantID, o.CustomerID, o.OrderNumber, o.Status, o.SubTotal, o.TaxAmount,
		o.TotalAmount, o.Currency, o.ShippingAddress, o.Notes, o.OrderedAt, o.CreatedBy, o.UpdatedBy,
	)
	return scanOrder(row)
}

func (r *repo) GetOrder(ctx context.Context, id, tenantID string) (*domain.Order, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT * FROM orders WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanOrder(row)
}

func (r *repo) ListOrders(ctx context.Context, tenantID, status string) ([]*domain.Order, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM orders WHERE tenant_id=$1 AND status=$2 AND deleted_at IS NULL ORDER BY ordered_at DESC`,
		tenantID, status,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Order
	for rows.Next() {
		o := &domain.Order{}
		if err := rows.Scan(&o.ID, &o.TenantID, &o.CustomerID, &o.OrderNumber, &o.Status,
			&o.SubTotal, &o.TaxAmount, &o.TotalAmount, &o.Currency, &o.ShippingAddress, &o.Notes,
			&o.OrderedAt, &o.DeliveredAt, &o.CreatedAt, &o.UpdatedAt, &o.CreatedBy, &o.UpdatedBy, &o.DeletedAt); err != nil {
			return nil, err
		}
		result = append(result, o)
	}
	return result, rows.Err()
}

func (r *repo) UpdateOrderStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.Order, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE orders SET status=$3,updated_by=$4,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *`,
		id, tenantID, status, updatedBy,
	)
	return scanOrder(row)
}

func (r *repo) UpdateOrderTotals(ctx context.Context, id, tenantID string, subTotal, taxAmount, totalAmount float64, updatedBy string) (*domain.Order, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE orders SET sub_total=$3,tax_amount=$4,total_amount=$5,updated_by=$6,updated_at=NOW()
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *`,
		id, tenantID, subTotal, taxAmount, totalAmount, updatedBy,
	)
	return scanOrder(row)
}

func (r *repo) CreateOrderItem(ctx context.Context, item *domain.OrderItem) (*domain.OrderItem, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO order_items (id,tenant_id,order_id,sku_id,product_id,quantity,unit_price,total_price,status,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING *`,
		item.ID, item.TenantID, item.OrderID, item.SKUID, item.ProductID, item.Quantity,
		item.UnitPrice, item.TotalPrice, item.Status, item.CreatedBy, item.UpdatedBy,
	)
	return scanOrderItem(row)
}

func (r *repo) ListOrderItems(ctx context.Context, orderID, tenantID string) ([]*domain.OrderItem, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM order_items WHERE order_id=$1 AND tenant_id=$2 ORDER BY created_at`,
		orderID, tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.OrderItem
	for rows.Next() {
		item := &domain.OrderItem{}
		if err := rows.Scan(&item.ID, &item.TenantID, &item.OrderID, &item.SKUID, &item.ProductID,
			&item.Quantity, &item.UnitPrice, &item.TotalPrice, &item.Status,
			&item.CreatedAt, &item.UpdatedAt, &item.CreatedBy, &item.UpdatedBy); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *repo) SumOrderItems(ctx context.Context, orderID, tenantID string) (float64, error) {
	var total float64
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(total_price),0) AS total FROM order_items WHERE order_id=$1 AND tenant_id=$2`,
		orderID, tenantID,
	).Scan(&total)
	return total, err
}

func (r *repo) CreateInvoice(ctx context.Context, inv *domain.Invoice) (*domain.Invoice, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO invoices (id,tenant_id,order_id,invoice_number,status,sub_total,tax_amount,total_amount,currency,issued_at,due_at,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING *`,
		inv.ID, inv.TenantID, inv.OrderID, inv.InvoiceNumber, inv.Status, inv.SubTotal, inv.TaxAmount,
		inv.TotalAmount, inv.Currency, inv.IssuedAt, inv.DueAt, inv.CreatedBy, inv.UpdatedBy,
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

func scanOrder(s scanner) (*domain.Order, error) {
	o := &domain.Order{}
	err := s.Scan(&o.ID, &o.TenantID, &o.CustomerID, &o.OrderNumber, &o.Status,
		&o.SubTotal, &o.TaxAmount, &o.TotalAmount, &o.Currency, &o.ShippingAddress, &o.Notes,
		&o.OrderedAt, &o.DeliveredAt, &o.CreatedAt, &o.UpdatedAt, &o.CreatedBy, &o.UpdatedBy, &o.DeletedAt)
	if err != nil {
		return nil, err
	}
	return o, nil
}

func scanOrderItem(s scanner) (*domain.OrderItem, error) {
	item := &domain.OrderItem{}
	err := s.Scan(&item.ID, &item.TenantID, &item.OrderID, &item.SKUID, &item.ProductID,
		&item.Quantity, &item.UnitPrice, &item.TotalPrice, &item.Status,
		&item.CreatedAt, &item.UpdatedAt, &item.CreatedBy, &item.UpdatedBy)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func scanInvoice(s scanner) (*domain.Invoice, error) {
	inv := &domain.Invoice{}
	err := s.Scan(&inv.ID, &inv.TenantID, &inv.OrderID, &inv.InvoiceNumber, &inv.Status,
		&inv.SubTotal, &inv.TaxAmount, &inv.TotalAmount, &inv.Currency,
		&inv.IssuedAt, &inv.DueAt, &inv.PaidAt, &inv.CreatedAt, &inv.UpdatedAt, &inv.CreatedBy, &inv.UpdatedBy, &inv.DeletedAt)
	if err != nil {
		return nil, err
	}
	return inv, nil
}

// ensure time import used
var _ = time.Now
