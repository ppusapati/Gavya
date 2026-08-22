package service

import (
	"context"
	"errors"
	"time"

	"github.com/ppusapati/gavya/libs/integrity/exact"
	"github.com/ppusapati/gavya/services/health-service/internal/domain"
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

func (s *Service) RecordVaccination(ctx context.Context, v *domain.Vaccination) (*domain.Vaccination, error) {
	if v.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if v.CattleID == "" {
		return nil, invalid("cattle_id is required")
	}
	if v.VaccineName == "" {
		return nil, invalid("vaccine_name is required")
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
		return nil, invalid("tenant_id is required")
	}
	if cattleID == "" {
		return nil, invalid("cattle_id is required")
	}
	return s.repo.ListVaccinationHistory(ctx, tenantID, cattleID)
}

func (s *Service) RecordTreatment(ctx context.Context, t *domain.Treatment) (*domain.Treatment, error) {
	if t.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if t.CattleID == "" {
		return nil, invalid("cattle_id is required")
	}
	if t.Diagnosis == "" {
		return nil, invalid("diagnosis is required")
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
		return nil, invalid("tenant_id is required")
	}
	if cattleID == "" {
		return nil, invalid("cattle_id is required")
	}
	return s.repo.ListTreatmentHistory(ctx, tenantID, cattleID)
}

func (s *Service) ScheduleVetVisit(ctx context.Context, v *domain.VetVisit) (*domain.VetVisit, error) {
	// cost is stored as NUMERIC(10,2). A finer value would be rounded
	// into the column without anyone being told, so it is refused instead.
	if _, err := exact.NonNegativeDecimal(v.Cost, 2, 10); err != nil {
		return nil, invalid(exact.Field("cost", err).Error())
	}
	if v.TenantID == "" {
		return nil, invalid("tenant_id is required")
	}
	if v.CattleID == "" {
		return nil, invalid("cattle_id is required")
	}
	if v.VeterinarianID == "" {
		return nil, invalid("veterinarian_id is required")
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
		return nil, invalid("tenant_id is required")
	}
	return s.repo.ListUpcomingVaccinations(ctx, tenantID)
}
