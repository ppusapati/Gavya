package service

import (
	"context"
	"errors"
	"time"

	"github.com/ppusapati/gavya/services/health-service/internal/domain"
	ulidpkg "p9e.in/samavaya/packages/ULID"
)

func (s *Service) RecordVaccination(ctx context.Context, v *domain.Vaccination) (*domain.Vaccination, error) {
	if v.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if v.CattleID == "" {
		return nil, errors.New("cattle_id is required")
	}
	if v.VaccineName == "" {
		return nil, errors.New("vaccine_name is required")
	}
	v.ID = ulidpkg.New().String()
	if v.AdministeredAt.IsZero() {
		v.AdministeredAt = time.Now()
	}
	if v.CreatedBy == "" {
		v.CreatedBy = "system"
	}
	v.UpdatedBy = v.CreatedBy
	return s.repo.CreateVaccination(ctx, v)
}

func (s *Service) GetVaccinationHistory(ctx context.Context, tenantID, cattleID string) ([]*domain.Vaccination, error) {
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if cattleID == "" {
		return nil, errors.New("cattle_id is required")
	}
	return s.repo.ListVaccinationHistory(ctx, tenantID, cattleID)
}

func (s *Service) RecordTreatment(ctx context.Context, t *domain.Treatment) (*domain.Treatment, error) {
	if t.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if t.CattleID == "" {
		return nil, errors.New("cattle_id is required")
	}
	if t.Diagnosis == "" {
		return nil, errors.New("diagnosis is required")
	}
	t.ID = ulidpkg.New().String()
	if t.Status == "" {
		t.Status = "ongoing"
	}
	if t.TreatedAt.IsZero() {
		t.TreatedAt = time.Now()
	}
	if t.CreatedBy == "" {
		t.CreatedBy = "system"
	}
	t.UpdatedBy = t.CreatedBy
	return s.repo.CreateTreatment(ctx, t)
}

func (s *Service) GetTreatmentHistory(ctx context.Context, tenantID, cattleID string) ([]*domain.Treatment, error) {
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if cattleID == "" {
		return nil, errors.New("cattle_id is required")
	}
	return s.repo.ListTreatmentHistory(ctx, tenantID, cattleID)
}

func (s *Service) ScheduleVetVisit(ctx context.Context, v *domain.VetVisit) (*domain.VetVisit, error) {
	if v.TenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	if v.CattleID == "" {
		return nil, errors.New("cattle_id is required")
	}
	if v.VeterinarianID == "" {
		return nil, errors.New("veterinarian_id is required")
	}
	v.ID = ulidpkg.New().String()
	if v.VisitDate.IsZero() {
		v.VisitDate = time.Now()
	}
	if v.CreatedBy == "" {
		v.CreatedBy = "system"
	}
	v.UpdatedBy = v.CreatedBy
	return s.repo.CreateVetVisit(ctx, v)
}

func (s *Service) ListUpcomingVaccinations(ctx context.Context, tenantID string) ([]*domain.Vaccination, error) {
	if tenantID == "" {
		return nil, errors.New("tenant_id is required")
	}
	return s.repo.ListUpcomingVaccinations(ctx, tenantID)
}
