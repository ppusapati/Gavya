package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ppusapati/gavya/libs/integrity/audit"

	"github.com/ppusapati/gavya/services/order-service/internal/domain"
)

// ErrNotFound lets a caller tell a missing record from a failed query. Without
// it every outcome reaches the handler as an opaque error and is reported as
// internal, so a client cannot distinguish "no such order" from "the database
// is unreachable".
var ErrNotFound = errors.New("not found")

// ErrDuplicateOrderNumber is the unique violation on (tenant_id, order_number),
// named so the handler can report a conflict rather than an internal failure.
var ErrDuplicateOrderNumber = errors.New("an order with that number already exists")

// Columns are listed explicitly rather than selected with *, because the scans
// below are positional: adding a column to the table would silently misalign
// every field after it.
const orderCols = `id,tenant_id,customer_id,order_number,status,sub_total,tax_amount,total_amount,` +
	`currency,tax_inclusive,COALESCE(shipping_address,''),COALESCE(notes,''),ordered_at,delivered_at,created_at,updated_at,created_by,` +
	`updated_by,deleted_at`

const orderItemCols = `id,tenant_id,order_id,sku_id,product_id,quantity,unit_price,total_price,tax_rate,` +
	`status,created_at,updated_at,created_by,updated_by`

const invoiceCols = `id,tenant_id,order_id,invoice_number,status,sub_total,tax_amount,total_amount,` +
	`currency,issued_at,due_at,paid_at,created_at,updated_at,created_by,updated_by,deleted_at`

type Repository interface {
	CreateOrder(ctx context.Context, o *domain.Order) (*domain.Order, error)
	GetOrder(ctx context.Context, id, tenantID string) (*domain.Order, error)
	ListOrders(ctx context.Context, tenantID, status string) ([]*domain.Order, error)
	UpdateOrderStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.Order, error)
	// AddItemAndRetotal writes a line and its order's totals together, so an
	// order can never disagree with the sum of its own lines.
	// TenantMoney reports the currency this tenant records money in.
	TenantMoney(ctx context.Context, tenantID string) (Money, error)
	// PinTenantMoney fixes that currency, or confirms the one already fixed.
	PinTenantMoney(ctx context.Context, tenantID string, money Money) error
	AddItemAndRetotal(ctx context.Context, item *domain.OrderItem, quantity, unitPrice, taxRate string, money Money) (*ItemOutcome, error)
	ListOrderItems(ctx context.Context, orderID, tenantID string) ([]*domain.OrderItem, error)
	CreateInvoice(ctx context.Context, inv *domain.Invoice) (*domain.Invoice, error)
	GetInvoice(ctx context.Context, id, tenantID string) (*domain.Invoice, error)
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

const serviceName = "order-service"

type scanner interface {
	Scan(dest ...any) error
}

func (r *repo) CreateOrder(ctx context.Context, o *domain.Order) (*domain.Order, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO orders (id,tenant_id,customer_id,order_number,status,sub_total,tax_amount,total_amount,currency,shipping_address,notes,ordered_at,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING `+orderCols,
		o.ID, o.TenantID, o.CustomerID, o.OrderNumber, o.Status, o.SubTotal, o.TaxAmount,
		o.TotalAmount, o.Currency, o.ShippingAddress, o.Notes, o.OrderedAt, o.CreatedBy, o.UpdatedBy,
	)
	out, err := scanOrder(row)
	if isUniqueViolation(err) {
		return nil, ErrDuplicateOrderNumber
	}
	return out, err
}

func (r *repo) GetOrder(ctx context.Context, id, tenantID string) (*domain.Order, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+orderCols+` FROM orders WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanOrder(row)
}

func (r *repo) ListOrders(ctx context.Context, tenantID, status string) ([]*domain.Order, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+orderCols+` FROM orders WHERE tenant_id=$1 AND status=$2 AND deleted_at IS NULL ORDER BY ordered_at DESC`,
		tenantID, status,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, o)
	}
	return result, rows.Err()
}

// UpdateOrderStatus moves a order between states and records what it was in.
//
// An order's status is a decision — confirmed, cancelled, delivered — and
// `updated_by` named whoever made the last one without saying what they changed
// it from. An order reading "cancelled" gave no indication whether it had been a
// draft nobody had confirmed or something already out for delivery.
//
// The entry goes in the same transaction as the change, so a state that moved
// and the record of it moving cannot come apart.
func (r *repo) UpdateOrderStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.Order, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// Read under the lock the update will take, so a concurrent transition
	// cannot land in between and be recorded as the state this one started from.
	var before string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM orders WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`,
		id, tenantID).Scan(&before); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	row := tx.QueryRow(ctx,
		`UPDATE orders SET status=$3,updated_by=$4,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING `+orderCols,
		id, tenantID, status, updatedBy,
	)
	out, err := scanOrder(row)
	if err != nil {
		return nil, err
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "update_order_status", ResourceType: "order", ResourceID: id,
		Before:      map[string]any{"status": before},
		After:       map[string]any{"status": status},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

func (r *repo) ListOrderItems(ctx context.Context, orderID, tenantID string) ([]*domain.OrderItem, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+orderItemCols+` FROM order_items WHERE order_id=$1 AND tenant_id=$2 ORDER BY created_at`,
		orderID, tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.OrderItem
	for rows.Next() {
		item, err := scanOrderItem(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *repo) CreateInvoice(ctx context.Context, inv *domain.Invoice) (*domain.Invoice, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO invoices (id,tenant_id,order_id,invoice_number,status,sub_total,tax_amount,total_amount,currency,issued_at,due_at,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING `+invoiceCols,
		inv.ID, inv.TenantID, inv.OrderID, inv.InvoiceNumber, inv.Status, inv.SubTotal, inv.TaxAmount,
		inv.TotalAmount, inv.Currency, inv.IssuedAt, inv.DueAt, inv.CreatedBy, inv.UpdatedBy,
	)
	return scanInvoice(row)
}

func (r *repo) GetInvoice(ctx context.Context, id, tenantID string) (*domain.Invoice, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+invoiceCols+` FROM invoices WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanInvoice(row)
}

func scanOrder(s scanner) (*domain.Order, error) {
	o := &domain.Order{}
	err := s.Scan(&o.ID, &o.TenantID, &o.CustomerID, &o.OrderNumber, &o.Status,
		&o.SubTotal, &o.TaxAmount, &o.TotalAmount, &o.Currency, &o.TaxInclusive, &o.ShippingAddress, &o.Notes,
		&o.OrderedAt, &o.DeliveredAt, &o.CreatedAt, &o.UpdatedAt, &o.CreatedBy, &o.UpdatedBy, &o.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return o, nil
}

func scanOrderItem(s scanner) (*domain.OrderItem, error) {
	item := &domain.OrderItem{}
	err := s.Scan(&item.ID, &item.TenantID, &item.OrderID, &item.SKUID, &item.ProductID,
		&item.Quantity, &item.UnitPrice, &item.TotalPrice, &item.TaxRate, &item.Status,
		&item.CreatedAt, &item.UpdatedAt, &item.CreatedBy, &item.UpdatedBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
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
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return inv, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// ensure time import used
var _ = time.Now
