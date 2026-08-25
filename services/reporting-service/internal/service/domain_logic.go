package service

import (
	"context"
	"errors"

	"github.com/ppusapati/gavya/services/reporting-service/internal/domain"
	ulidpkg "p9e.in/samavaya/packages/ulid"
)

// ErrInvalidArgument marks a caller mistake. Without it the handler cannot tell
// "the report is not ready yet" from "the query failed", and would have to
// report both the same way.
var ErrInvalidArgument = errors.New("invalid argument")

// invalidArgument carries the reason alone. The marker is matched through Is,
// so errors.Is finds it while the message stays free of a prefix the error code
// already conveys.
type invalidArgument struct{ reason string }

func (e *invalidArgument) Error() string { return e.reason }

func (e *invalidArgument) Is(target error) bool { return target == ErrInvalidArgument }

func invalid(msg string) error { return &invalidArgument{reason: msg} }

func (s *Service) RequestReport(ctx context.Context, tenantID, name, reportType, parameters, fileFormat, requestedBy, createdBy string) (*domain.Report, error) {
	r := &domain.Report{
		ID:          ulidpkg.New().String(),
		TenantID:    tenantID,
		Name:        name,
		ReportType:  reportType,
		Parameters:  parameters,
		Status:      "pending",
		FileFormat:  fileFormat,
		RequestedBy: requestedBy,
		CreatedBy:   createdBy,
		UpdatedBy:   createdBy,
	}
	return s.repo.CreateReport(ctx, r)
}

func (s *Service) GetReport(ctx context.Context, id, tenantID string) (*domain.Report, error) {
	return s.repo.GetReport(ctx, id, tenantID)
}

func (s *Service) ListReports(ctx context.Context, tenantID string) ([]*domain.Report, error) {
	return s.repo.ListReports(ctx, tenantID)
}

func (s *Service) GetReportDownloadURL(ctx context.Context, id, tenantID string) (string, error) {
	rep, err := s.repo.GetReport(ctx, id, tenantID)
	if err != nil {
		return "", err
	}
	if rep.FilePath == "" {
		return "", invalid("report file not yet available")
	}
	return rep.FilePath, nil
}

func (s *Service) CreateSchedule(ctx context.Context, tenantID, reportType, schedule, parameters, createdBy string) (*domain.ReportSchedule, error) {
	sched := &domain.ReportSchedule{
		ID:         ulidpkg.New().String(),
		TenantID:   tenantID,
		ReportType: reportType,
		Schedule:   schedule,
		Parameters: parameters,
		IsActive:   true,
		CreatedBy:  createdBy,
		UpdatedBy:  createdBy,
	}
	return s.repo.CreateReportSchedule(ctx, sched)
}

func (s *Service) ListSchedules(ctx context.Context, tenantID string) ([]*domain.ReportSchedule, error) {
	return s.repo.ListReportSchedules(ctx, tenantID)
}

func (s *Service) UpdateSchedule(ctx context.Context, id, tenantID string, isActive bool, updatedBy string) (*domain.ReportSchedule, error) {
	return s.repo.UpdateScheduleActive(ctx, id, tenantID, isActive, updatedBy)
}

func (s *Service) DeleteSchedule(ctx context.Context, id, tenantID, updatedBy string) error {
	return s.repo.SoftDeleteSchedule(ctx, id, tenantID, updatedBy)
}
