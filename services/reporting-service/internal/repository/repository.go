package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/services/reporting-service/internal/domain"
)

// ErrNotFound lets a caller tell a missing record from a failed query. Without
// it every outcome reaches the handler as an opaque error and is reported as
// internal, so a client cannot distinguish "no such report" from "the database
// is unreachable".
var ErrNotFound = errors.New("not found")

// Columns are listed explicitly rather than selected with *, because the scans
// below are positional: adding a column to the table would silently misalign
// every field after it.
const reportCols = `id,tenant_id,name,report_type,COALESCE(parameters::text,''),status,COALESCE(file_path,''),COALESCE(file_format,''),` +
	`requested_by,started_at,completed_at,created_at,updated_at,created_by,updated_by,deleted_at`

const scheduleCols = `id,tenant_id,report_type,schedule,COALESCE(parameters::text,''),is_active,last_run_at,next_run_at,` +
	`created_at,updated_at,created_by,updated_by,deleted_at`

type Repository interface {
	CreateReport(ctx context.Context, r *domain.Report) (*domain.Report, error)
	GetReport(ctx context.Context, id, tenantID string) (*domain.Report, error)
	ListReports(ctx context.Context, tenantID string) ([]*domain.Report, error)
	UpdateReportStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.Report, error)
	CreateReportSchedule(ctx context.Context, s *domain.ReportSchedule) (*domain.ReportSchedule, error)
	GetReportSchedule(ctx context.Context, id, tenantID string) (*domain.ReportSchedule, error)
	ListReportSchedules(ctx context.Context, tenantID string) ([]*domain.ReportSchedule, error)
	UpdateScheduleActive(ctx context.Context, id, tenantID string, isActive bool, updatedBy string) (*domain.ReportSchedule, error)
	SoftDeleteSchedule(ctx context.Context, id, tenantID, updatedBy string) error
}

type repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) Repository {
	return &repo{pool: pool}
}

type scanner interface {
	Scan(dest ...any) error
}

func (r *repo) CreateReport(ctx context.Context, rep *domain.Report) (*domain.Report, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO reports (id,tenant_id,name,report_type,parameters,status,file_format,requested_by,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,NULLIF($5,'')::jsonb,$6,$7,$8,$9,$10) RETURNING `+reportCols,
		rep.ID, rep.TenantID, rep.Name, rep.ReportType, rep.Parameters, rep.Status,
		rep.FileFormat, rep.RequestedBy, rep.CreatedBy, rep.UpdatedBy,
	)
	return scanReport(row)
}

func (r *repo) GetReport(ctx context.Context, id, tenantID string) (*domain.Report, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+reportCols+` FROM reports WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanReport(row)
}

func (r *repo) ListReports(ctx context.Context, tenantID string) ([]*domain.Report, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+reportCols+` FROM reports WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY created_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Report
	for rows.Next() {
		rep, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, rep)
	}
	return result, rows.Err()
}

func (r *repo) UpdateReportStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.Report, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE reports SET status=$3,updated_by=$4,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING `+reportCols,
		id, tenantID, status, updatedBy,
	)
	return scanReport(row)
}

func (r *repo) CreateReportSchedule(ctx context.Context, s *domain.ReportSchedule) (*domain.ReportSchedule, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO report_schedules (id,tenant_id,report_type,schedule,parameters,is_active,next_run_at,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,NULLIF($5,'')::jsonb,$6,$7,$8,$9) RETURNING `+scheduleCols,
		s.ID, s.TenantID, s.ReportType, s.Schedule, s.Parameters, s.IsActive, s.NextRunAt, s.CreatedBy, s.UpdatedBy,
	)
	return scanSchedule(row)
}

func (r *repo) GetReportSchedule(ctx context.Context, id, tenantID string) (*domain.ReportSchedule, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+scheduleCols+` FROM report_schedules WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanSchedule(row)
}

func (r *repo) ListReportSchedules(ctx context.Context, tenantID string) ([]*domain.ReportSchedule, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+scheduleCols+` FROM report_schedules WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY created_at`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.ReportSchedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func (r *repo) UpdateScheduleActive(ctx context.Context, id, tenantID string, isActive bool, updatedBy string) (*domain.ReportSchedule, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE report_schedules SET is_active=$3,updated_by=$4,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING `+scheduleCols,
		id, tenantID, isActive, updatedBy,
	)
	return scanSchedule(row)
}

func (r *repo) SoftDeleteSchedule(ctx context.Context, id, tenantID, updatedBy string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE report_schedules SET deleted_at=NOW(),updated_by=$3,updated_at=NOW() WHERE id=$1 AND tenant_id=$2`,
		id, tenantID, updatedBy,
	)
	return err
}

func scanReport(s scanner) (*domain.Report, error) {
	rep := &domain.Report{}
	err := s.Scan(&rep.ID, &rep.TenantID, &rep.Name, &rep.ReportType, &rep.Parameters,
		&rep.Status, &rep.FilePath, &rep.FileFormat, &rep.RequestedBy, &rep.StartedAt, &rep.CompletedAt,
		&rep.CreatedAt, &rep.UpdatedAt, &rep.CreatedBy, &rep.UpdatedBy, &rep.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return rep, nil
}

func scanSchedule(s scanner) (*domain.ReportSchedule, error) {
	sched := &domain.ReportSchedule{}
	err := s.Scan(&sched.ID, &sched.TenantID, &sched.ReportType, &sched.Schedule, &sched.Parameters,
		&sched.IsActive, &sched.LastRunAt, &sched.NextRunAt, &sched.CreatedAt, &sched.UpdatedAt,
		&sched.CreatedBy, &sched.UpdatedBy, &sched.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return sched, nil
}
