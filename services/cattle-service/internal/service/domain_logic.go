package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	ulidpkg "p9e.in/samavaya/packages/ulid"

	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/services/cattle-service/internal/domain"
)

// ErrInvalidArgument marks a caller mistake. Without it the handler cannot tell
// "that weight does not fit the column" from "the query failed", and this
// handler reported the whole of CreateCattle as a bad request and the whole of
// UpdateCattle as an internal one — a code chosen by which procedure it was
// rather than by what went wrong.
var ErrInvalidArgument = errors.New("invalid argument")

// invalidArgument carries the reason alone. The marker is matched through Is,
// so errors.Is finds it while the message stays free of a prefix the error code
// already conveys.
type invalidArgument struct{ reason string }

func (e *invalidArgument) Error() string { return e.reason }

func (e *invalidArgument) Is(target error) bool { return target == ErrInvalidArgument }

func invalid(msg string) error { return &invalidArgument{reason: msg} }

// weightForColumn checks a weight against the column that holds it.
//
// Nothing checked it before. A figure finer than two decimals was rounded into
// NUMERIC(8,2) by PostgreSQL without anyone being told, and one too large
// reached the database as a constraint violation reported as an internal
// failure. An animal's weight is what a dose is worked out from and what a sale
// is priced against, so neither belongs in the set of things somebody discovers
// afterwards.
func weightForColumn(w exact.Fixed) (exact.Fixed, error) {
	at, err := w.NonNegativeColumn(domain.WeightScale, domain.WeightPrecision)
	if err != nil {
		return exact.Fixed{}, invalid(exact.Field("weight", err).Error())
	}
	return at, nil
}

// ─── Cattle ──────────────────────────────────────────────────────────────────

// CreateCattle validates input, assigns a ULID, and persists a new cattle record.
func (s *Service) CreateCattle(ctx context.Context, c *domain.Cattle) (*domain.Cattle, error) {
	if c.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if c.TagNumber == "" {
		return nil, invalid("tag_number is required")
	}
	if c.Gender != "M" && c.Gender != "F" {
		return nil, invalid("gender must be M or F")
	}
	if c.CreatedBy == "" {
		return nil, invalid("created_by is required")
	}
	weight, err := weightForColumn(c.Weight)
	if err != nil {
		return nil, err
	}
	c.Weight = weight

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
		return nil, invalid("id and tenant_id are required")
	}
	if c.UpdatedBy == "" {
		return nil, invalid("updated_by is required")
	}
	weight, err := weightForColumn(c.Weight)
	if err != nil {
		return nil, err
	}
	c.Weight = weight
	c.UpdatedAt = time.Now()

	result, err := s.repo.UpdateCattle(ctx, c)
	if err != nil {
		s.log.Errorf("UpdateCattle: %v", err)
		return nil, fmt.Errorf("update cattle: %w", err)
	}
	return result, nil
}

// DeleteCattle soft-deletes a cattle record.
// DeleteCattle marks an animal deleted.
//
// deletedBy is required. The row it writes is the only account of an animal
// disappearing from every list, and one that cannot say who did it is an account
// nobody can act on.
func (s *Service) DeleteCattle(ctx context.Context, id, tenantID, deletedBy string) error {
	if id == "" || tenantID == "" {
		return fmt.Errorf("id and tenant_id are required")
	}
	if deletedBy == "" {
		return fmt.Errorf("deleted_by is required: an animal that vanished from every list " +
			"with nobody's name against it is not something anybody can follow up")
	}
	if err := s.repo.SoftDeleteCattle(ctx, id, tenantID, deletedBy); err != nil {
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
