package service

import (
	"context"
	"errors"
	"time"

	"github.com/ppusapati/gavya/services/breeding-service/internal/domain"
	ulidpkg "p9e.in/samavaya/packages/ULID"
)

func (s *Service) CreateBreedingCycle(ctx context.Context, b *domain.BreedingCycle) (*domain.BreedingCycle, error) {
	if b.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if b.CattleID == "" {
		return nil, errors.New("cattle_id is required")
	}
	b.ID = ulidpkg.New().String()
	if b.Status == "" {
		b.Status = "heat"
	}
	if b.HeatDate.IsZero() {
		b.HeatDate = time.Now()
	}
	if b.CreatedBy == "" {
		b.CreatedBy = "system"
	}
	b.UpdatedBy = b.CreatedBy
	return s.repo.CreateBreedingCycle(ctx, b)
}

func (s *Service) RecordInsemination(ctx context.Context, ins *domain.Insemination) (*domain.Insemination, error) {
	if ins.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if ins.CycleID == "" {
		return nil, errors.New("cycle_id is required")
	}
	if ins.CattleID == "" {
		return nil, errors.New("cattle_id is required")
	}
	ins.ID = ulidpkg.New().String()
	if ins.Method == "" {
		ins.Method = "AI"
	}
	if ins.InseminatedAt.IsZero() {
		ins.InseminatedAt = time.Now()
	}
	if ins.CreatedBy == "" {
		ins.CreatedBy = "system"
	}
	ins.UpdatedBy = ins.CreatedBy
	result, err := s.repo.CreateInsemination(ctx, ins)
	if err != nil {
		return nil, err
	}
	// Update cycle status to inseminated
	if _, err := s.repo.UpdateCycleStatus(ctx, ins.CycleID, ins.TenantID, "inseminated", ins.CreatedBy); err != nil {
		s.log.Errorf("failed to update cycle status: %v", err)
	}
	return result, nil
}

func (s *Service) ConfirmPregnancy(ctx context.Context, p *domain.Pregnancy) (*domain.Pregnancy, error) {
	if p.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if p.InseminationID == "" {
		return nil, errors.New("insemination_id is required")
	}
	if p.CattleID == "" {
		return nil, errors.New("cattle_id is required")
	}
	if p.ExpectedCalvingDate.IsZero() {
		return nil, errors.New("expected_calving_date is required")
	}
	p.ID = ulidpkg.New().String()
	p.Status = "active"
	if p.ConfirmedAt.IsZero() {
		p.ConfirmedAt = time.Now()
	}
	if p.CreatedBy == "" {
		p.CreatedBy = "system"
	}
	p.UpdatedBy = p.CreatedBy
	result, err := s.repo.CreatePregnancy(ctx, p)
	if err != nil {
		return nil, err
	}
	// Get insemination to find cycle_id
	ins, err := s.repo.GetInsemination(ctx, p.InseminationID, p.TenantID)
	if err == nil && ins != nil {
		if _, err := s.repo.UpdateCycleStatus(ctx, ins.CycleID, p.TenantID, "pregnant", p.CreatedBy); err != nil {
			s.log.Errorf("failed to update cycle status: %v", err)
		}
	}
	return result, nil
}

func (s *Service) RecordCalving(ctx context.Context, c *domain.CalvingRecord) (*domain.CalvingRecord, error) {
	if c.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if c.PregnancyID == "" {
		return nil, errors.New("pregnancy_id is required")
	}
	if c.CattleID == "" {
		return nil, errors.New("cattle_id is required")
	}
	c.ID = ulidpkg.New().String()
	if c.Status == "" {
		c.Status = "normal"
	}
	if c.CalvingDate.IsZero() {
		c.CalvingDate = time.Now()
	}
	if c.CreatedBy == "" {
		c.CreatedBy = "system"
	}
	c.UpdatedBy = c.CreatedBy
	result, err := s.repo.CreateCalvingRecord(ctx, c)
	if err != nil {
		return nil, err
	}
	// Update pregnancy status to delivered
	if _, err := s.repo.UpdatePregnancyStatus(ctx, c.PregnancyID, c.TenantID, "delivered", c.CreatedBy); err != nil {
		s.log.Errorf("failed to update pregnancy status: %v", err)
	}
	return result, nil
}

func (s *Service) GetBreedingHistory(ctx context.Context, tenantID, cattleID string) ([]*domain.BreedingCycle, error) {
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if cattleID == "" {
		return nil, errors.New("cattle_id is required")
	}
	return s.repo.ListCattleBreedingCycles(ctx, tenantID, cattleID)
}

func (s *Service) ListActivePregnancies(ctx context.Context, tenantID string) ([]*domain.Pregnancy, error) {
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	return s.repo.ListActivePregnancies(ctx, tenantID)
}
