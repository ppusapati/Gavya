package service

import (
	"context"
	"errors"

	"github.com/ppusapati/gavya/services/farm-service/internal/domain"
	ulidpkg "p9e.in/samavaya/packages/ulid"
)

// ErrInvalidArgument marks a caller mistake. Without it the handler cannot tell
// "you did not supply an id" from "the query failed", and would have to report
// both the same way.
var ErrInvalidArgument = errors.New("invalid argument")

// invalidArgument carries the reason alone. The marker is matched through Is,
// so errors.Is finds it while the message stays free of a prefix the error code
// already conveys.
type invalidArgument struct{ reason string }

func (e *invalidArgument) Error() string { return e.reason }

func (e *invalidArgument) Is(target error) bool { return target == ErrInvalidArgument }

func invalid(msg string) error { return &invalidArgument{reason: msg} }

func (s *Service) CreateFarm(ctx context.Context, f *domain.Farm) (*domain.Farm, error) {
	if f.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if f.Name == "" {
		return nil, invalid("name is required")
	}
	if f.Code == "" {
		return nil, invalid("code is required")
	}
	f.ID = ulidpkg.New().String()
	if f.Status == "" {
		f.Status = "active"
	}
	if f.CreatedBy == "" {
		f.CreatedBy = "system"
	}
	f.UpdatedBy = f.CreatedBy
	return s.repo.CreateFarm(ctx, f)
}

func (s *Service) GetFarm(ctx context.Context, id, tenantID string) (*domain.Farm, error) {
	if id == "" || tenantID == "" {
		return nil, invalid("id and tenant_id are required")
	}
	return s.repo.GetFarm(ctx, id, tenantID)
}

func (s *Service) ListFarms(ctx context.Context, tenantID string) ([]*domain.Farm, error) {
	if tenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	return s.repo.ListFarms(ctx, tenantID)
}

func (s *Service) UpdateFarm(ctx context.Context, f *domain.Farm) (*domain.Farm, error) {
	if f.ID == "" || f.TenantID == "" {
		return nil, invalid("id and tenant_id are required")
	}
	if f.UpdatedBy == "" {
		f.UpdatedBy = "system"
	}
	return s.repo.UpdateFarm(ctx, f)
}

func (s *Service) CreateFarmSection(ctx context.Context, sec *domain.FarmSection) (*domain.FarmSection, error) {
	if sec.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if sec.FarmID == "" {
		return nil, invalid("farm_id is required")
	}
	if sec.Name == "" {
		return nil, invalid("name is required")
	}
	sec.ID = ulidpkg.New().String()
	if sec.CreatedBy == "" {
		sec.CreatedBy = "system"
	}
	sec.UpdatedBy = sec.CreatedBy
	return s.repo.CreateFarmSection(ctx, sec)
}

func (s *Service) ListFarmSections(ctx context.Context, tenantID, farmID string) ([]*domain.FarmSection, error) {
	if tenantID == "" || farmID == "" {
		return nil, invalid("tenant_id and farm_id are required")
	}
	return s.repo.ListFarmSections(ctx, tenantID, farmID)
}

func (s *Service) UpdateFarmCapacity(ctx context.Context, id, tenantID string, capacity int, updatedBy string) (*domain.Farm, error) {
	if id == "" || tenantID == "" {
		return nil, invalid("id and tenant_id are required")
	}
	if updatedBy == "" {
		updatedBy = "system"
	}
	return s.repo.UpdateFarmCapacity(ctx, id, tenantID, capacity, updatedBy)
}
