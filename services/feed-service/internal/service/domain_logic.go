package service

import (
	"context"
	"errors"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/services/feed-service/internal/domain"
	ulidpkg "p9e.in/samavaya/packages/ULID"
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

func (s *Service) CreateFeedType(ctx context.Context, f *domain.FeedType) (*domain.FeedType, error) {
	if f.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if f.Name == "" {
		return nil, invalid("name is required")
	}
	f.ID = ulidpkg.New().String()
	if f.Unit == "" {
		f.Unit = "kg"
	}
	if f.CreatedBy == "" {
		f.CreatedBy = "system"
	}
	f.UpdatedBy = f.CreatedBy
	return s.repo.CreateFeedType(ctx, f)
}

func (s *Service) ListFeedTypes(ctx context.Context, tenantID string) ([]*domain.FeedType, error) {
	if tenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	return s.repo.ListFeedTypes(ctx, tenantID)
}

func (s *Service) CreateNutritionPlan(ctx context.Context, p *domain.NutritionPlan) (*domain.NutritionPlan, error) {
	// daily_quantity_kg is stored as NUMERIC(8,3). A finer value would be rounded
	// into the column without anyone being told, so it is refused instead.
	if _, err := exact.NonNegativeDecimal(p.DailyQuantityKg, 3, 8); err != nil {
		return nil, invalid(exact.Field("daily_quantity_kg", err).Error())
	}
	if p.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if p.CattleID == "" {
		return nil, invalid("cattle_id is required")
	}
	if p.FeedTypeID == "" {
		return nil, invalid("feed_type_id is required")
	}
	p.ID = ulidpkg.New().String()
	if p.StartDate.IsZero() {
		p.StartDate = time.Now()
	}
	if p.CreatedBy == "" {
		p.CreatedBy = "system"
	}
	p.UpdatedBy = p.CreatedBy
	return s.repo.CreateNutritionPlan(ctx, p)
}

func (s *Service) GetNutritionPlan(ctx context.Context, id, tenantID string) (*domain.NutritionPlan, error) {
	if id == "" || tenantID == "" {
		return nil, invalid("id and tenant_id are required")
	}
	return s.repo.GetNutritionPlan(ctx, id, tenantID)
}

func (s *Service) RecordFeedConsumption(ctx context.Context, c *domain.FeedConsumption) (*domain.FeedConsumption, error) {
	// quantity_kg is stored as NUMERIC(8,3). A finer value would be rounded
	// into the column without anyone being told, so it is refused instead.
	if _, err := exact.NonNegativeDecimal(c.QuantityKg, 3, 8); err != nil {
		return nil, invalid(exact.Field("quantity_kg", err).Error())
	}
	if c.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if c.CattleID == "" {
		return nil, invalid("cattle_id is required")
	}
	if c.FeedTypeID == "" {
		return nil, invalid("feed_type_id is required")
	}
	c.ID = ulidpkg.New().String()
	if c.FedAt.IsZero() {
		c.FedAt = time.Now()
	}
	if c.CreatedBy == "" {
		c.CreatedBy = "system"
	}
	c.UpdatedBy = c.CreatedBy
	return s.repo.CreateFeedConsumption(ctx, c)
}

func (s *Service) GetFeedConsumptionReport(ctx context.Context, tenantID, cattleID string, from, to time.Time) ([]*domain.FeedConsumptionReport, error) {
	if tenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if cattleID == "" {
		return nil, invalid("cattle_id is required")
	}
	return s.repo.GetFeedConsumptionReport(ctx, tenantID, cattleID, from, to)
}
