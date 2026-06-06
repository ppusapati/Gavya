package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ppusapati/gavya/services/inventory-service/internal/domain"
)

type Repository interface {
	CreateWarehouse(ctx context.Context, w *domain.Warehouse) (*domain.Warehouse, error)
	GetWarehouse(ctx context.Context, id, tenantID string) (*domain.Warehouse, error)
	ListWarehouses(ctx context.Context, tenantID string) ([]*domain.Warehouse, error)
	GetInventoryItem(ctx context.Context, warehouseID, skuID, tenantID string) (*domain.InventoryItem, error)
	UpsertInventoryItem(ctx context.Context, item *domain.InventoryItem) (*domain.InventoryItem, error)
	CreateStockMovement(ctx context.Context, m *domain.StockMovement) (*domain.StockMovement, error)
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
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *`,
		w.ID, w.TenantID, w.Name, w.Code, w.Address, w.ManagerID, w.Status, w.CreatedBy, w.UpdatedBy,
	)
	return scanWarehouse(row)
}

func (r *repo) GetWarehouse(ctx context.Context, id, tenantID string) (*domain.Warehouse, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT * FROM warehouses WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanWarehouse(row)
}

func (r *repo) ListWarehouses(ctx context.Context, tenantID string) ([]*domain.Warehouse, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM warehouses WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Warehouse
	for rows.Next() {
		w := &domain.Warehouse{}
		if err := rows.Scan(&w.ID, &w.TenantID, &w.Name, &w.Code, &w.Address, &w.ManagerID, &w.Status,
			&w.CreatedAt, &w.UpdatedAt, &w.CreatedBy, &w.UpdatedBy, &w.DeletedAt); err != nil {
			return nil, err
		}
		result = append(result, w)
	}
	return result, rows.Err()
}

func (r *repo) GetInventoryItem(ctx context.Context, warehouseID, skuID, tenantID string) (*domain.InventoryItem, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT * FROM inventory_items WHERE warehouse_id=$1 AND sku_id=$2 AND tenant_id=$3`,
		warehouseID, skuID, tenantID,
	)
	return scanInventoryItem(row)
}

func (r *repo) UpsertInventoryItem(ctx context.Context, item *domain.InventoryItem) (*domain.InventoryItem, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO inventory_items (id,tenant_id,warehouse_id,sku_id,quantity_on_hand,quantity_reserved,reorder_point,max_stock,last_updated_at,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW(),$9,$10)
		 ON CONFLICT (tenant_id,warehouse_id,sku_id) DO UPDATE SET
		   quantity_on_hand = EXCLUDED.quantity_on_hand,
		   last_updated_at = NOW(),
		   updated_by = EXCLUDED.updated_by,
		   updated_at = NOW()
		 RETURNING *`,
		item.ID, item.TenantID, item.WarehouseID, item.SKUID, item.QuantityOnHand,
		item.QuantityReserved, item.ReorderPoint, item.MaxStock, item.CreatedBy, item.UpdatedBy,
	)
	return scanInventoryItem(row)
}

func (r *repo) CreateStockMovement(ctx context.Context, m *domain.StockMovement) (*domain.StockMovement, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO stock_movements (id,tenant_id,warehouse_id,sku_id,movement_type,quantity,reference_id,reference_type,notes,moved_at,moved_by,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING *`,
		m.ID, m.TenantID, m.WarehouseID, m.SKUID, m.MovementType, m.Quantity,
		m.ReferenceID, m.ReferenceType, m.Notes, m.MovedAt, m.MovedBy, m.CreatedBy, m.UpdatedBy,
	)
	return scanStockMovement(row)
}

func (r *repo) ListStockMovements(ctx context.Context, tenantID, warehouseID string, limit, offset int) ([]*domain.StockMovement, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM stock_movements WHERE tenant_id=$1 AND warehouse_id=$2 ORDER BY moved_at DESC LIMIT $3 OFFSET $4`,
		tenantID, warehouseID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.StockMovement
	for rows.Next() {
		m := &domain.StockMovement{}
		if err := rows.Scan(&m.ID, &m.TenantID, &m.WarehouseID, &m.SKUID, &m.MovementType, &m.Quantity,
			&m.ReferenceID, &m.ReferenceType, &m.Notes, &m.MovedAt, &m.MovedBy,
			&m.CreatedAt, &m.UpdatedAt, &m.CreatedBy, &m.UpdatedBy); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func (r *repo) CreateBatch(ctx context.Context, b *domain.Batch) (*domain.Batch, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO batches (id,tenant_id,warehouse_id,sku_id,batch_number,quantity,manufactured_at,expires_at,status,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING *`,
		b.ID, b.TenantID, b.WarehouseID, b.SKUID, b.BatchNumber, b.Quantity,
		b.ManufacturedAt, b.ExpiresAt, b.Status, b.CreatedBy, b.UpdatedBy,
	)
	return scanBatch(row)
}

func (r *repo) GetBatch(ctx context.Context, id, tenantID string) (*domain.Batch, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT * FROM batches WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanBatch(row)
}

func (r *repo) ListExpiringBatches(ctx context.Context, tenantID string) ([]*domain.Batch, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM batches WHERE tenant_id=$1 AND expires_at <= NOW() + INTERVAL '7 days' AND status='available' AND deleted_at IS NULL ORDER BY expires_at`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Batch
	for rows.Next() {
		b := &domain.Batch{}
		if err := rows.Scan(&b.ID, &b.TenantID, &b.WarehouseID, &b.SKUID, &b.BatchNumber, &b.Quantity,
			&b.ManufacturedAt, &b.ExpiresAt, &b.Status, &b.CreatedAt, &b.UpdatedAt, &b.CreatedBy, &b.UpdatedBy, &b.DeletedAt); err != nil {
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
		return nil, err
	}
	return m, nil
}

func scanBatch(s scanner) (*domain.Batch, error) {
	b := &domain.Batch{}
	err := s.Scan(&b.ID, &b.TenantID, &b.WarehouseID, &b.SKUID, &b.BatchNumber, &b.Quantity,
		&b.ManufacturedAt, &b.ExpiresAt, &b.Status, &b.CreatedAt, &b.UpdatedAt, &b.CreatedBy, &b.UpdatedBy, &b.DeletedAt)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// ensure time import used
var _ = time.Now
