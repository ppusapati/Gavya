package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ppusapati/gavya/services/inventory-service/internal/domain"
)

// ErrNotFound lets a caller tell a missing record from a failed query. Without
// it every outcome reaches the handler as an opaque error and is reported as
// internal, so a client cannot distinguish "no such warehouse" from "the
// database is unreachable".
var ErrNotFound = errors.New("not found")

// ErrDuplicateWarehouseCode is the unique violation on (tenant_id, code), named
// so the handler can report a conflict rather than an internal failure.
var ErrDuplicateWarehouseCode = errors.New("a warehouse with that code already exists")

// Columns are listed explicitly rather than selected with *, because the scans
// below are positional: adding a column to the table would silently misalign
// every field after it.
const warehouseCols = `id,tenant_id,name,code,COALESCE(address,''),COALESCE(manager_id,''),status,` +
	`created_at,updated_at,created_by,updated_by,deleted_at`

const inventoryItemCols = `id,tenant_id,warehouse_id,sku_id,quantity_on_hand,quantity_reserved,` +
	`reorder_point,max_stock,last_updated_at,created_at,updated_at,created_by,updated_by`

const stockMovementCols = `id,tenant_id,warehouse_id,sku_id,movement_type,quantity,COALESCE(reference_id,''),` +
	`COALESCE(reference_type,''),COALESCE(notes,''),moved_at,moved_by,created_at,updated_at,created_by,updated_by`

const batchCols = `id,tenant_id,warehouse_id,sku_id,batch_number,quantity,manufactured_at,` +
	`expires_at,status,created_at,updated_at,created_by,updated_by,deleted_at`

type Repository interface {
	CreateWarehouse(ctx context.Context, w *domain.Warehouse) (*domain.Warehouse, error)
	GetWarehouse(ctx context.Context, id, tenantID string) (*domain.Warehouse, error)
	ListWarehouses(ctx context.Context, tenantID string) ([]*domain.Warehouse, error)
	GetInventoryItem(ctx context.Context, warehouseID, skuID, tenantID string) (*domain.InventoryItem, error)
	// ApplyStockMovement records a movement and moves the stock together, so a
	// movement can never stand against stock that did not change.
	ApplyStockMovement(ctx context.Context, m *domain.StockMovement, quantity, itemID string) (*MovementOutcome, error)
	ListStockMovements(ctx context.Context, tenantID, warehouseID string, limit, offset int) ([]*domain.StockMovement, error)
	CreateBatch(ctx context.Context, b *domain.Batch) (*domain.Batch, error)
	GetBatch(ctx context.Context, id, tenantID string) (*domain.Batch, error)
	ListExpiringBatches(ctx context.Context, tenantID string) ([]*domain.Batch, error)
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

func (r *repo) CreateWarehouse(ctx context.Context, w *domain.Warehouse) (*domain.Warehouse, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO warehouses (id,tenant_id,name,code,address,manager_id,status,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+warehouseCols,
		w.ID, w.TenantID, w.Name, w.Code, w.Address, w.ManagerID, w.Status, w.CreatedBy, w.UpdatedBy,
	)
	out, err := scanWarehouse(row)
	if isUniqueViolation(err) {
		return nil, ErrDuplicateWarehouseCode
	}
	return out, err
}

func (r *repo) GetWarehouse(ctx context.Context, id, tenantID string) (*domain.Warehouse, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+warehouseCols+` FROM warehouses WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanWarehouse(row)
}

func (r *repo) ListWarehouses(ctx context.Context, tenantID string) ([]*domain.Warehouse, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+warehouseCols+` FROM warehouses WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Warehouse
	for rows.Next() {
		w, err := scanWarehouse(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, w)
	}
	return result, rows.Err()
}

func (r *repo) GetInventoryItem(ctx context.Context, warehouseID, skuID, tenantID string) (*domain.InventoryItem, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+inventoryItemCols+` FROM inventory_items WHERE warehouse_id=$1 AND sku_id=$2 AND tenant_id=$3`,
		warehouseID, skuID, tenantID,
	)
	return scanInventoryItem(row)
}

func (r *repo) ListStockMovements(ctx context.Context, tenantID, warehouseID string, limit, offset int) ([]*domain.StockMovement, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+stockMovementCols+` FROM stock_movements WHERE tenant_id=$1 AND warehouse_id=$2 ORDER BY moved_at DESC LIMIT $3 OFFSET $4`,
		tenantID, warehouseID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.StockMovement
	for rows.Next() {
		m, err := scanStockMovement(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func (r *repo) CreateBatch(ctx context.Context, b *domain.Batch) (*domain.Batch, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO batches (id,tenant_id,warehouse_id,sku_id,batch_number,quantity,manufactured_at,expires_at,status,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+batchCols,
		b.ID, b.TenantID, b.WarehouseID, b.SKUID, b.BatchNumber, b.Quantity,
		b.ManufacturedAt, b.ExpiresAt, b.Status, b.CreatedBy, b.UpdatedBy,
	)
	return scanBatch(row)
}

func (r *repo) GetBatch(ctx context.Context, id, tenantID string) (*domain.Batch, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+batchCols+` FROM batches WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanBatch(row)
}

func (r *repo) ListExpiringBatches(ctx context.Context, tenantID string) ([]*domain.Batch, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+batchCols+` FROM batches WHERE tenant_id=$1 AND expires_at <= NOW() + INTERVAL '7 days' AND status='available' AND deleted_at IS NULL ORDER BY expires_at`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Batch
	for rows.Next() {
		b, err := scanBatch(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

func scanWarehouse(s scanner) (*domain.Warehouse, error) {
	w := &domain.Warehouse{}
	err := s.Scan(&w.ID, &w.TenantID, &w.Name, &w.Code, &w.Address, &w.ManagerID, &w.Status,
		&w.CreatedAt, &w.UpdatedAt, &w.CreatedBy, &w.UpdatedBy, &w.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return w, nil
}

func scanInventoryItem(s scanner) (*domain.InventoryItem, error) {
	item := &domain.InventoryItem{}
	err := s.Scan(&item.ID, &item.TenantID, &item.WarehouseID, &item.SKUID, &item.QuantityOnHand,
		&item.QuantityReserved, &item.ReorderPoint, &item.MaxStock, &item.LastUpdatedAt,
		&item.CreatedAt, &item.UpdatedAt, &item.CreatedBy, &item.UpdatedBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return item, nil
}

func scanStockMovement(s scanner) (*domain.StockMovement, error) {
	m := &domain.StockMovement{}
	err := s.Scan(&m.ID, &m.TenantID, &m.WarehouseID, &m.SKUID, &m.MovementType, &m.Quantity,
		&m.ReferenceID, &m.ReferenceType, &m.Notes, &m.MovedAt, &m.MovedBy,
		&m.CreatedAt, &m.UpdatedAt, &m.CreatedBy, &m.UpdatedBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return m, nil
}

func scanBatch(s scanner) (*domain.Batch, error) {
	b := &domain.Batch{}
	err := s.Scan(&b.ID, &b.TenantID, &b.WarehouseID, &b.SKUID, &b.BatchNumber, &b.Quantity,
		&b.ManufacturedAt, &b.ExpiresAt, &b.Status, &b.CreatedAt, &b.UpdatedAt, &b.CreatedBy, &b.UpdatedBy, &b.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return b, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// ensure time import used
var _ = time.Now
