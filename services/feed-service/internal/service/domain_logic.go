package service

import (
	"context"
	"errors"
	"time"

	"github.com/ppusapati/gavya/services/feed-service/internal/domain"
	ulidpkg "p9e.in/samavaya/packages/ULID"
)

func (s *Service) CreateFeedType(ctx context.Context, f *domain.FeedType) (*domain.FeedType, error) {
	if f.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if f.Name == "" {
		return nil, errors.New("name is required")
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
		return nil, errors.New("tenant_id is required")
	}
	return s.repo.ListFeedTypes(ctx, tenantID)
}

func (s *Service) CreateNutritionPlan(ctx context.Context, p *domain.NutritionPlan) (*domain.NutritionPlan, error) {
	if p.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if p.CattleID == "" {
		return nil, errors.New("cattle_id is required")
	}
	if p.FeedTypeID == "" {
		return nil, errors.New("feed_type_id is required")
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
		return nil, errors.New("id and tenant_id are required")
	}
	return s.repo.GetNutritionPlan(ctx, id, tenantID)
}

func (s *Service) RecordFeedConsumption(ctx context.Context, c *domain.FeedConsumption) (*domain.FeedConsumption, error) {
	if c.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if c.CattleID == "" {
		return nil, errors.New("cattle_id is required")
	}
	if c.FeedTypeID == "" {
		return nil, errors.New("feed_type_id is required")
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
		return nil, errors.New("tenant_id is required")
	}
	if cattleID == "" {
		return nil, errors.New("cattle_id is required")
	}
	return s.repo.GetFeedConsumptionReport(ctx, tenantID, cattleID, from, to)
}
