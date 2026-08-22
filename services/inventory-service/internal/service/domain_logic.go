package service

import (
	"context"
	"errors"
	"time"

	"github.com/ppusapati/gavya/services/inventory-service/internal/domain"
	ulidpkg "p9e.in/samavaya/packages/ULID"
)

// ErrInvalidArgument marks a caller mistake. Without it the handler cannot tell
// "you did not supply a tenant_id" from "the query failed", and would have to
// report both the same way.
var ErrInvalidArgument = errors.New("invalid argument")

// invalidArgument carries the reason alone. The marker is matched through Is,
// so errors.Is finds it while the message stays free of a prefix the error code
// already conveys.
type invalidArgument struct{ reason string }

func (e *invalidArgument) Error() string { return e.reason }

func (e *invalidArgument) Is(target error) bool { return target == ErrInvalidArgument }

func invalid(msg string) error { return &invalidArgument{reason: msg} }

func (s *Service) CreateWarehouse(ctx context.Context, w *domain.Warehouse) (*domain.Warehouse, error) {
	if w.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if w.Name == "" {
		return nil, invalid("name is required")
	}
	if w.Code == "" {
		return nil, invalid("code is required")
	}
	w.ID = ulidpkg.New().String()
	if w.Status == "" {
		w.Status = "active"
	}
	if w.CreatedBy == "" {
		w.CreatedBy = "system"
	}
	w.UpdatedBy = w.CreatedBy
	return s.repo.CreateWarehouse(ctx, w)
}

func (s *Service) GetWarehouse(ctx context.Context, id, tenantID string) (*domain.Warehouse, error) {
	if id == "" || tenantID == "" {
		return nil, invalid("id and tenant_id are required")
	}
	return s.repo.GetWarehouse(ctx, id, tenantID)
}

func (s *Service) ListWarehouses(ctx context.Context, tenantID string) ([]*domain.Warehouse, error) {
	if tenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	return s.repo.ListWarehouses(ctx, tenantID)
}

// AdjustStock creates a StockMovement and updates inventory_item quantity.
// in = add, out = subtract, adjustment = set absolute value
func (s *Service) AdjustStock(ctx context.Context, m *domain.StockMovement, updatedBy string) (*domain.StockMovement, error) {
	if m.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if m.WarehouseID == "" {
		return nil, invalid("warehouse_id is required")
	}
	if m.SKUID == "" {
		return nil, invalid("sku_id is required")
	}
	switch m.MovementType {
	case "in", "out", "adjustment", "transfer":
	default:
		return nil, invalid("invalid movement_type")
	}
	m.ID = ulidpkg.New().String()
	if m.MovedAt.IsZero() {
		m.MovedAt = time.Now()
	}
	if m.MovedBy == "" {
		m.MovedBy = updatedBy
	}
	if m.CreatedBy == "" {
		m.CreatedBy = updatedBy
	}
	m.UpdatedBy = m.CreatedBy

	movement, err := s.repo.CreateStockMovement(ctx, m)
	if err != nil {
		return nil, err
	}

	// Get or compute new quantity
	existing, err := s.repo.GetInventoryItem(ctx, m.WarehouseID, m.SKUID, m.TenantID)
	var newQty float64
	if err != nil {
		// item doesn't exist yet
		newQty = 0
	} else {
		newQty = existing.QuantityOnHand
	}

	switch m.MovementType {
	case "in":
		newQty += m.Quantity
	case "out":
		newQty -= m.Quantity
	case "adjustment":
		newQty = m.Quantity
	case "transfer":
		newQty -= m.Quantity
	}

	itemID := ulidpkg.New().String()
	reorderPoint := 0.0
	maxStock := 0.0
	if existing != nil {
		itemID = existing.ID
		reorderPoint = existing.ReorderPoint
		maxStock = existing.MaxStock
	}

	item := &domain.InventoryItem{
		ID:               itemID,
		TenantID:         m.TenantID,
		WarehouseID:      m.WarehouseID,
		SKUID:            m.SKUID,
		QuantityOnHand:   newQty,
		QuantityReserved: 0,
		ReorderPoint:     reorderPoint,
		MaxStock:         maxStock,
		LastUpdatedAt:    time.Now(),
		CreatedBy:        updatedBy,
		UpdatedBy:        updatedBy,
	}
	if _, err := s.repo.UpsertInventoryItem(ctx, item); err != nil {
		s.log.Errorf("failed to upsert inventory item: %v", err)
	}

	return movement, nil
}

func (s *Service) ListStockMovements(ctx context.Context, tenantID, warehouseID string, limit, offset int) ([]*domain.StockMovement, error) {
	if tenantID == "" || warehouseID == "" {
		return nil, invalid("tenant_id and warehouse_id are required")
	}
	if limit <= 0 {
		limit = 50
	}
	return s.repo.ListStockMovements(ctx, tenantID, warehouseID, limit, offset)
}

func (s *Service) CreateBatch(ctx context.Context, b *domain.Batch) (*domain.Batch, error) {
	if b.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if b.WarehouseID == "" {
		return nil, invalid("warehouse_id is required")
	}
	if b.BatchNumber == "" {
		return nil, invalid("batch_number is required")
	}
	b.ID = ulidpkg.New().String()
	if b.Status == "" {
		b.Status = "available"
	}
	if b.CreatedBy == "" {
		b.CreatedBy = "system"
	}
	b.UpdatedBy = b.CreatedBy
	return s.repo.CreateBatch(ctx, b)
}

func (s *Service) GetBatch(ctx context.Context, id, tenantID string) (*domain.Batch, error) {
	if id == "" || tenantID == "" {
		return nil, invalid("id and tenant_id are required")
	}
	return s.repo.GetBatch(ctx, id, tenantID)
}

func (s *Service) ListExpiringBatches(ctx context.Context, tenantID string) ([]*domain.Batch, error) {
	if tenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	return s.repo.ListExpiringBatches(ctx, tenantID)
}
