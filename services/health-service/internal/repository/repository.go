package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/audit"
	"github.com/ppusapati/gavya/libs/integrity/currency"
	"github.com/ppusapati/gavya/libs/integrity/money"

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

const vetVisitCols = `id,tenant_id,cattle_id,veterinarian_id,visit_date,COALESCE(purpose,''),COALESCE(notes,''),cost,currency,` +
	`created_at,updated_at,created_by,updated_by,deleted_at`

type Repository interface {
	// PinTenantMoney fixes the currency this tenant records money in.
	PinTenantMoney(ctx context.Context, tenantID string, money Money) error
	// TenantMoney reports it.
	TenantMoney(ctx context.Context, tenantID string) (Money, error)

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

// IDs supplies the identifier each audit entry carries.
type IDs interface{ New() string }

type repo struct {
	pool *pgxpool.Pool
	ids  IDs
}

func New(pool *pgxpool.Pool, ids IDs) Repository {
	return &repo{pool: pool, ids: ids}
}

const serviceName = "health-service"

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

// UpdateTreatmentStatus moves a treatment between states and records what it was in.
//
// A treatment's status decides when milk from that animal may be sold again.
// Marking one complete early shortens a withdrawal period, and a status with no
// history behind it cannot be checked against the date the milk went in the can.
//
// The entry goes in the same transaction as the change, so a state that moved
// and the record of it moving cannot come apart.
func (r *repo) UpdateTreatmentStatus(ctx context.Context, id, tenantID, status, updatedBy string) (*domain.Treatment, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// Read under the lock the update will take, so a concurrent transition
	// cannot land in between and be recorded as the state this one started from.
	var before string
	if err := tx.QueryRow(ctx,
		`SELECT status FROM treatments WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`,
		id, tenantID).Scan(&before); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	row := tx.QueryRow(ctx,
		`UPDATE treatments SET status=$3,updated_by=$4,updated_at=NOW()
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING `+treatmentCols,
		id, tenantID, status, updatedBy,
	)
	out, err := scanTreatment(row)
	if err != nil {
		return nil, err
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: "update_treatment_status", ResourceType: "treatment", ResourceID: id,
		Before:      map[string]any{"status": before},
		After:       map[string]any{"status": status},
		ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

func (r *repo) CreateVetVisit(ctx context.Context, v *domain.VetVisit) (*domain.VetVisit, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO vet_visits (id,tenant_id,cattle_id,veterinarian_id,visit_date,purpose,notes,cost,currency,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8::numeric,$9,$10,$11) RETURNING `+vetVisitCols,
		v.ID, v.TenantID, v.CattleID, v.VeterinarianID, v.VisitDate, v.Purpose, v.Notes,
		v.Cost.String(), v.Cost.Currency, v.CreatedBy, v.UpdatedBy,
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
	var cost, code string
	err := s.Scan(&v.ID, &v.TenantID, &v.CattleID, &v.VeterinarianID, &v.VisitDate,
		&v.Purpose, &v.Notes, &cost, &code, &v.CreatedAt, &v.UpdatedAt, &v.CreatedBy, &v.UpdatedBy, &v.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if v.Cost, err = parseAmount(cost, code); err != nil {
		return nil, fmt.Errorf("vet visit %s: %w", v.ID, err)
	}
	return v, nil
}

// moneyColumnScale is how many decimals the money columns hold.
//
// They are NUMERIC(18,4) so that one schema serves a yen deployment and a dinar
// one: four is the most any ISO 4217 currency has. It is not how many decimals
// any particular amount has — a rupee cost stored there reads back as
// "450.0000", and those trailing zeros are the column's padding.
const moneyColumnScale int32 = 4

// parseAmount turns a stored decimal literal into money at its currency's scale.
//
// A row whose currency is unreadable is an error rather than a zero: an amount
// with no currency is not an amount, and returning one as though it were is how
// a rupee figure ends up being read as dollars. money.ParseStored is what
// separates the column's padding from the amount's real precision.
func parseAmount(literal, code string) (money.Money, error) {
	normalised, err := currency.Normalise(code)
	if err != nil {
		return money.Money{}, fmt.Errorf("currency %q: %w", code, err)
	}
	scale, err := currency.Scale(normalised)
	if err != nil {
		return money.Money{}, fmt.Errorf("currency %q: %w", code, err)
	}
	return money.ParseStored(literal, moneyColumnScale, scale, normalised)
}
