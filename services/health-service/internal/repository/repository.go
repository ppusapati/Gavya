package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/services/health-service/internal/domain"
)

// ErrNotFound lets a caller tell a missing record from a failed query. Without
// it every outcome reaches the handler as an opaque error and is reported as
// internal, so a client cannot distinguish "no such treatment" from "the
// database is unreachable".
var ErrNotFound = errors.New("not found")

// Columns are listed explicitly rather than selected with *, because the scans
// below are positional: adding a column to the table would silently misalign
// every field after it.
const vaccinationCols = `id,tenant_id,cattle_id,vaccine_name,COALESCE(batch_number,''),administered_at,next_due_date,` +
	`COALESCE(veterinarian_id,''),COALESCE(dosage,''),created_at,updated_at,created_by,updated_by,deleted_at`

const treatmentCols = `id,tenant_id,cattle_id,COALESCE(diagnosis_code,''),diagnosis,COALESCE(medicine_name,''),COALESCE(dosage,''),treated_at,` +
	`COALESCE(treated_by,''),follow_up_date,status,created_at,updated_at,created_by,updated_by,deleted_at`

const vetVisitCols = `id,tenant_id,cattle_id,veterinarian_id,visit_date,COALESCE(purpose,''),COALESCE(notes,''),cost,` +
	`created_at,updated_at,created_by,updated_by,deleted_at`

type Repository interface {
	CreateVaccination(ctx context.Context, v *domain.Vaccination) (*domain.Vaccination, error)
	GetVaccination(ctx context.Context, id, tenantID string) (*domain.Vaccination, error)
	ListVaccinationHistory(ctx context.Context, tenantID, cattleID string) ([]*domain.Vaccination, error)
	ListUpcomingVaccinations(ctx context.Context, tenantID string) ([]*domain.Vaccination, error)
	CreateTreatment(ctx context.Context, t *domain.Treatment) (*domain.Treatment, error)
	GetTreatment(ctx context.Context, id, tenantID string) (*domain.Treatment, error)
	ListTreatmentHistory(ctx context.Context, tenantID, cattleID string) ([]*domain.Treatment, error)
	UpdateTreatmentStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.Treatment, error)
	CreateVetVisit(ctx context.Context, v *domain.VetVisit) (*domain.VetVisit, error)
	GetVetVisit(ctx context.Context, id, tenantID string) (*domain.VetVisit, error)
	ListVetVisits(ctx context.Context, tenantID, cattleID string) ([]*domain.VetVisit, error)
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

func (r *repo) CreateVaccination(ctx context.Context, v *domain.Vaccination) (*domain.Vaccination, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO vaccinations (id,tenant_id,cattle_id,vaccine_name,batch_number,administered_at,next_due_date,veterinarian_id,dosage,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+vaccinationCols,
		v.ID, v.TenantID, v.CattleID, v.VaccineName, v.BatchNumber, v.AdministeredAt, v.NextDueDate,
		v.VeterinarianID, v.Dosage, v.CreatedBy, v.UpdatedBy,
	)
	return scanVaccination(row)
}

func (r *repo) GetVaccination(ctx context.Context, id, tenantID string) (*domain.Vaccination, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+vaccinationCols+` FROM vaccinations WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanVaccination(row)
}

func (r *repo) ListVaccinationHistory(ctx context.Context, tenantID, cattleID string) ([]*domain.Vaccination, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+vaccinationCols+` FROM vaccinations WHERE tenant_id=$1 AND cattle_id=$2 AND deleted_at IS NULL ORDER BY administered_at DESC`,
		tenantID, cattleID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Vaccination
	for rows.Next() {
		v, err := scanVaccination(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func (r *repo) ListUpcomingVaccinations(ctx context.Context, tenantID string) ([]*domain.Vaccination, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+vaccinationCols+` FROM vaccinations
		 WHERE tenant_id=$1 AND next_due_date <= NOW() + INTERVAL '7 days'
		   AND next_due_date >= NOW() AND deleted_at IS NULL
		 ORDER BY next_due_date`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Vaccination
	for rows.Next() {
		v, err := scanVaccination(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func (r *repo) CreateTreatment(ctx context.Context, t *domain.Treatment) (*domain.Treatment, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO treatments (id,tenant_id,cattle_id,diagnosis_code,diagnosis,medicine_name,dosage,treated_at,treated_by,follow_up_date,status,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING `+treatmentCols,
		t.ID, t.TenantID, t.CattleID, t.DiagnosisCode, t.Diagnosis, t.MedicineName, t.Dosage,
		t.TreatedAt, t.TreatedBy, t.FollowUpDate, t.Status, t.CreatedBy, t.UpdatedBy,
	)
	return scanTreatment(row)
}

func (r *repo) GetTreatment(ctx context.Context, id, tenantID string) (*domain.Treatment, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+treatmentCols+` FROM treatments WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanTreatment(row)
}

func (r *repo) ListTreatmentHistory(ctx context.Context, tenantID, cattleID string) ([]*domain.Treatment, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+treatmentCols+` FROM treatments WHERE tenant_id=$1 AND cattle_id=$2 AND deleted_at IS NULL ORDER BY treated_at DESC`,
		tenantID, cattleID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Treatment
	for rows.Next() {
		t, err := scanTreatment(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

func (r *repo) UpdateTreatmentStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.Treatment, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE treatments SET status=$3,updated_by=$4,updated_at=NOW()
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING `+treatmentCols,
		id, tenantID, status, updatedBy,
	)
	return scanTreatment(row)
}

func (r *repo) CreateVetVisit(ctx context.Context, v *domain.VetVisit) (*domain.VetVisit, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO vet_visits (id,tenant_id,cattle_id,veterinarian_id,visit_date,purpose,notes,cost,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING `+vetVisitCols,
		v.ID, v.TenantID, v.CattleID, v.VeterinarianID, v.VisitDate, v.Purpose, v.Notes,
		v.Cost, v.CreatedBy, v.UpdatedBy,
	)
	return scanVetVisit(row)
}

func (r *repo) GetVetVisit(ctx context.Context, id, tenantID string) (*domain.VetVisit, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+vetVisitCols+` FROM vet_visits WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanVetVisit(row)
}

func (r *repo) ListVetVisits(ctx context.Context, tenantID, cattleID string) ([]*domain.VetVisit, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+vetVisitCols+` FROM vet_visits WHERE tenant_id=$1 AND cattle_id=$2 AND deleted_at IS NULL ORDER BY visit_date DESC`,
		tenantID, cattleID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.VetVisit
	for rows.Next() {
		v, err := scanVetVisit(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

func scanVaccination(s scanner) (*domain.Vaccination, error) {
	v := &domain.Vaccination{}
	err := s.Scan(&v.ID, &v.TenantID, &v.CattleID, &v.VaccineName, &v.BatchNumber,
		&v.AdministeredAt, &v.NextDueDate, &v.VeterinarianID, &v.Dosage,
		&v.CreatedAt, &v.UpdatedAt, &v.CreatedBy, &v.UpdatedBy, &v.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return v, nil
}

func scanTreatment(s scanner) (*domain.Treatment, error) {
	t := &domain.Treatment{}
	err := s.Scan(&t.ID, &t.TenantID, &t.CattleID, &t.DiagnosisCode, &t.Diagnosis,
		&t.MedicineName, &t.Dosage, &t.TreatedAt, &t.TreatedBy, &t.FollowUpDate, &t.Status,
		&t.CreatedAt, &t.UpdatedAt, &t.CreatedBy, &t.UpdatedBy, &t.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return t, nil
}

func scanVetVisit(s scanner) (*domain.VetVisit, error) {
	v := &domain.VetVisit{}
	err := s.Scan(&v.ID, &v.TenantID, &v.CattleID, &v.VeterinarianID, &v.VisitDate,
		&v.Purpose, &v.Notes, &v.Cost, &v.CreatedAt, &v.UpdatedAt, &v.CreatedBy, &v.UpdatedBy, &v.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return v, nil
}
