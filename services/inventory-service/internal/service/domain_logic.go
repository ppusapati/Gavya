package service

import (
	"context"
	"errors"
	"github.com/ppusapati/gavya/libs/integrity/exact"
	"time"

	"github.com/ppusapati/gavya/services/inventory-service/internal/domain"
	"github.com/ppusapati/gavya/services/inventory-service/internal/repository"
	ulidpkg "p9e.in/samavaya/packages/ulid"
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

// AdjustStock records a movement and moves the stock it describes.
//
// The whole of it — the movement row, the new quantity, and the refusal to let
// stock go below zero — happens in one transaction in the repository. The
// arithmetic is deliberately not done here: a quantity computed in Go and
// written back is a lost update waiting for a second concurrent movement, and
// float64 addition against a NUMERIC(12,3) column drifts over an item's life.
func (s *Service) AdjustStock(ctx context.Context, m *domain.StockMovement, updatedBy string) (*repository.MovementOutcome, error) {
	if m.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if m.WarehouseID == "" {
		return nil, invalid("warehouse_id is required")
	}
	if m.SKUID == "" {
		return nil, invalid("sku_id is required")
	}
	if !domain.ValidMovementType(m.MovementType) {
		return nil, invalid("movement_type must be one of in, out, adjustment, transfer")
	}

	// The quantity is held at the stock columns' own scale, and a value they
	// cannot hold is refused rather than rounded on the way in.
	quantity, err := domain.AtColumn(m.Quantity)
	if err != nil {
		return nil, invalid(exact.Field("quantity", err).Error())
	}
	m.Quantity = quantity
	// A movement of nothing only means something as a stocktake that found none.
	if m.Quantity.IsZero() && m.MovementType != domain.MovementAdjustment {
		return nil, invalid("a " + m.MovementType + " movement must carry a quantity")
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

	return s.repo.ApplyStockMovement(ctx, m, ulidpkg.New().String())
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
	// The same door a movement's quantity goes through. A batch used to take
	// whatever float arrived, negative included, and hand it to the column.
	quantity, err := domain.AtColumn(b.Quantity)
	if err != nil {
		return nil, invalid(exact.Field("quantity", err).Error())
	}
	b.Quantity = quantity
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
