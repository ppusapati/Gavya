package service

import (
	"context"
	"fmt"
	"time"

	ulidpkg "p9e.in/samavaya/packages/ulid"

	"github.com/ppusapati/gavya/services/cattle-service/internal/domain"
)

// ─── Cattle ──────────────────────────────────────────────────────────────────

// CreateCattle validates input, assigns a ULID, and persists a new cattle record.
func (s *Service) CreateCattle(ctx context.Context, c *domain.Cattle) (*domain.Cattle, error) {
	if c.TenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if c.TagNumber == "" {
		return nil, fmt.Errorf("tag_number is required")
	}
	if c.Gender != "M" && c.Gender != "F" {
		return nil, fmt.Errorf("gender must be M or F")
	}
	if c.CreatedBy == "" {
		return nil, fmt.Errorf("created_by is required")
	}

	c.ID = ulidpkg.New().String()
	if c.Status == "" {
		c.Status = "active"
	}
	c.UpdatedBy = c.CreatedBy
	c.CreatedAt = time.Now()
	c.UpdatedAt = c.CreatedAt

	result, err := s.repo.CreateCattle(ctx, c)
	if err != nil {
		s.log.Errorf("CreateCattle: %v", err)
		return nil, fmt.Errorf("create cattle: %w", err)
	}
	return result, nil
}

// GetCattle retrieves a single cattle record by ID and tenant.
func (s *Service) GetCattle(ctx context.Context, id, tenantID string) (*domain.Cattle, error) {
	if id == "" || tenantID == "" {
		return nil, fmt.Errorf("id and tenant_id are required")
	}
	c, err := s.repo.GetCattle(ctx, id, tenantID)
	if err != nil {
		return nil, fmt.Errorf("get cattle: %w", err)
	}
	return c, nil
}

// ListCattle returns a paginated list of cattle for a tenant, optionally filtered by status.
func (s *Service) ListCattle(ctx context.Context, tenantID, status string, limit, offset int) ([]*domain.Cattle, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return s.repo.ListCattle(ctx, tenantID, status, limit, offset)
}

// UpdateCattle applies status and weight changes to an existing cattle record.
func (s *Service) UpdateCattle(ctx context.Context, c *domain.Cattle) (*domain.Cattle, error) {
	if c.ID == "" || c.TenantID == "" {
		return nil, fmt.Errorf("id and tenant_id are required")
	}
	if c.UpdatedBy == "" {
		return nil, fmt.Errorf("updated_by is required")
	}
	c.UpdatedAt = time.Now()

	result, err := s.repo.UpdateCattle(ctx, c)
	if err != nil {
		s.log.Errorf("UpdateCattle: %v", err)
		return nil, fmt.Errorf("update cattle: %w", err)
	}
	return result, nil
}

// DeleteCattle soft-deletes a cattle record.
func (s *Service) DeleteCattle(ctx context.Context, id, tenantID string) error {
	if id == "" || tenantID == "" {
		return fmt.Errorf("id and tenant_id are required")
	}
	if err := s.repo.SoftDeleteCattle(ctx, id, tenantID); err != nil {
		s.log.Errorf("DeleteCattle: %v", err)
		return fmt.Errorf("delete cattle: %w", err)
	}
	return nil
}

// ─── Breed ────────────────────────────────────────────────────────────────────

// CreateBreed validates input, assigns a ULID, and persists a new breed.
func (s *Service) CreateBreed(ctx context.Context, b *domain.Breed) (*domain.Breed, error) {
	if b.TenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	if b.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if b.CreatedBy == "" {
		return nil, fmt.Errorf("created_by is required")
	}

	b.ID = ulidpkg.New().String()
	b.UpdatedBy = b.CreatedBy
	b.CreatedAt = time.Now()
	b.UpdatedAt = b.CreatedAt

	result, err := s.repo.CreateBreed(ctx, b)
	if err != nil {
		s.log.Errorf("CreateBreed: %v", err)
		return nil, fmt.Errorf("create breed: %w", err)
	}
	return result, nil
}

// ListBreeds returns all active breeds for a tenant.
func (s *Service) ListBreeds(ctx context.Context, tenantID string) ([]*domain.Breed, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("tenant_id is required")
	}
	return s.repo.ListBreeds(ctx, tenantID)
}

// ─── CattleLineage ────────────────────────────────────────────────────────────

// CreateCattleLineage records parent-child relationships for cattle.
func (s *Service) CreateCattleLineage(ctx context.Context, l *domain.CattleLineage) (*domain.CattleLineage, error) {
	if l.TenantID == "" || l.CattleID == "" {
		return nil, fmt.Errorf("tenant_id and cattle_id are required")
	}
	l.ID = ulidpkg.New().String()

	result, err := s.repo.CreateCattleLineage(ctx, l)
	if err != nil {
		s.log.Errorf("CreateCattleLineage: %v", err)
		return nil, fmt.Errorf("create cattle lineage: %w", err)
	}
	return result, nil
}

// GetCattleLineage retrieves the lineage record for a specific cattle.
func (s *Service) GetCattleLineage(ctx context.Context, cattleID, tenantID string) (*domain.CattleLineage, error) {
	if cattleID == "" || tenantID == "" {
		return nil, fmt.Errorf("cattle_id and tenant_id are required")
	}
	return s.repo.GetCattleLineage(ctx, cattleID, tenantID)
}
