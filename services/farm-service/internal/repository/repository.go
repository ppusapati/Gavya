package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/audit"
	"github.com/ppusapati/gavya/libs/integrity/sys"

	"github.com/ppusapati/gavya/services/farm-service/internal/domain"
)

// ErrNotFound lets a caller tell a missing record from a failed query. Without
// it every outcome reaches the handler as an opaque error and is reported as
// internal, so a client cannot distinguish "no such farm" from "the database is
// unreachable".
var ErrNotFound = errors.New("not found")

// ErrDuplicateCode is the unique violation on (tenant_id, code), named so the
// handler can report a conflict rather than an internal failure.
var ErrDuplicateCode = errors.New("a farm with that code already exists")

// Columns are listed explicitly rather than selected with *, because the scans
// below are positional: adding a column to the table would silently misalign
// every field after it.
const farmCols = `id,tenant_id,name,code,COALESCE(address,''),COALESCE(city,''),COALESCE(state,''),COALESCE(country,''),capacity,COALESCE(manager_id,''),status,` +
	`created_at,updated_at,created_by,updated_by,deleted_at`

const farmSectionCols = `id,tenant_id,farm_id,name,COALESCE(section_type,''),capacity,current_occupancy,` +
	`created_at,updated_at,created_by,updated_by,deleted_at`

type Repository interface {
	CreateFarm(ctx context.Context, f *domain.Farm) (*domain.Farm, error)
	GetFarm(ctx context.Context, id, tenantID string) (*domain.Farm, error)
	ListFarms(ctx context.Context, tenantID string) ([]*domain.Farm, error)
	UpdateFarm(ctx context.Context, f *domain.Farm) (*domain.Farm, error)
	UpdateFarmCapacity(ctx context.Context, id, tenantID string, capacity int, updatedBy string) (*domain.Farm, error)
	CreateFarmSection(ctx context.Context, s *domain.FarmSection) (*domain.FarmSection, error)
	GetFarmSection(ctx context.Context, id, tenantID string) (*domain.FarmSection, error)
	ListFarmSections(ctx context.Context, tenantID, farmID string) ([]*domain.FarmSection, error)
}

type repo struct {
	pool *pgxpool.Pool
	ids  audit.IDs
}

// serviceName is what this service's audit entries are attributed to.
const serviceName = "farm-service"

func New(pool *pgxpool.Pool) Repository {
	return &repo{pool: pool, ids: sys.IDs{}}
}

type scanner interface {
	Scan(dest ...any) error
}

func (r *repo) CreateFarm(ctx context.Context, f *domain.Farm) (*domain.Farm, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO farms (id,tenant_id,name,code,address,city,state,country,capacity,manager_id,status,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING `+farmCols,
		f.ID, f.TenantID, f.Name, f.Code, f.Address, f.City, f.State, f.Country,
		f.Capacity, f.ManagerID, f.Status, f.CreatedBy, f.UpdatedBy,
	)
	out, err := scanFarm(row)
	if isUniqueViolation(err) {
		return nil, ErrDuplicateCode
	}
	return out, err
}

func (r *repo) GetFarm(ctx context.Context, id, tenantID string) (*domain.Farm, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+farmCols+` FROM farms WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanFarm(row)
}

func (r *repo) ListFarms(ctx context.Context, tenantID string) ([]*domain.Farm, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+farmCols+` FROM farms WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Farm
	for rows.Next() {
		f, err := scanFarm(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, f)
	}
	return result, rows.Err()
}

// UpdateFarm rewrites a farm's record and keeps what it said before.
//
// A farm's manager and status decide who is answerable for the animals on it.
// The row afterwards named the last person to touch it and nothing said what
// it had been.
func (r *repo) UpdateFarm(ctx context.Context, f *domain.Farm) (*domain.Farm, error) {
	return r.recordedFarmUpdate(ctx, f.ID, f.TenantID, "update_farm",
		`UPDATE farms SET name=$3,address=$4,city=$5,state=$6,country=$7,manager_id=$8,status=$9,updated_by=$10,updated_at=NOW()
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING `+farmCols,
		[]any{f.ID, f.TenantID, f.Name, f.Address, f.City, f.State, f.Country,
			f.ManagerID, f.Status, f.UpdatedBy})
}

// UpdateFarmCapacity changes how many animals a farm may hold.
func (r *repo) UpdateFarmCapacity(ctx context.Context, id, tenantID string, capacity int, updatedBy string) (*domain.Farm, error) {
	return r.recordedFarmUpdate(ctx, id, tenantID, "update_farm_capacity",
		`UPDATE farms SET capacity=$3,updated_by=$4,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING `+farmCols,
		[]any{id, tenantID, capacity, updatedBy})
}

// recordedFarmUpdate applies an update and records the whole row either side.
//
// The before is read under the lock the update will take; both are written in
// the transaction that makes the change, so a trail that cannot be written
// takes the change down with it rather than leaving one without the other.
func (r *repo) recordedFarmUpdate(ctx context.Context, id, tenantID, action, query string, args []any) (*domain.Farm, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	before, err := scanFarm(tx.QueryRow(ctx,
		`SELECT `+farmCols+` FROM farms WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL FOR UPDATE`, id, tenantID))
	if err != nil {
		return nil, err
	}
	after, err := scanFarm(tx.QueryRow(ctx, query, args...))
	if err != nil {
		return nil, err
	}
	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: action, ResourceType: "farm", ResourceID: id,
		Before: before, After: after, ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return after, nil
}

func (r *repo) CreateFarmSection(ctx context.Context, s *domain.FarmSection) (*domain.FarmSection, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO farm_sections (id,tenant_id,farm_id,name,section_type,capacity,current_occupancy,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING `+farmSectionCols,
		s.ID, s.TenantID, s.FarmID, s.Name, s.SectionType, s.Capacity, s.CurrentOccupancy, s.CreatedBy, s.UpdatedBy,
	)
	return scanFarmSection(row)
}

func (r *repo) GetFarmSection(ctx context.Context, id, tenantID string) (*domain.FarmSection, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+farmSectionCols+` FROM farm_sections WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanFarmSection(row)
}

func (r *repo) ListFarmSections(ctx context.Context, tenantID, farmID string) ([]*domain.FarmSection, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+farmSectionCols+` FROM farm_sections WHERE tenant_id=$1 AND farm_id=$2 AND deleted_at IS NULL ORDER BY name`,
		tenantID, farmID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.FarmSection
	for rows.Next() {
		s, err := scanFarmSection(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func scanFarm(s scanner) (*domain.Farm, error) {
	f := &domain.Farm{}
	err := s.Scan(&f.ID, &f.TenantID, &f.Name, &f.Code, &f.Address, &f.City, &f.State, &f.Country,
		&f.Capacity, &f.ManagerID, &f.Status, &f.CreatedAt, &f.UpdatedAt, &f.CreatedBy, &f.UpdatedBy, &f.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return f, nil
}

func scanFarmSection(s scanner) (*domain.FarmSection, error) {
	sec := &domain.FarmSection{}
	err := s.Scan(&sec.ID, &sec.TenantID, &sec.FarmID, &sec.Name, &sec.SectionType,
		&sec.Capacity, &sec.CurrentOccupancy, &sec.CreatedAt, &sec.UpdatedAt, &sec.CreatedBy, &sec.UpdatedBy, &sec.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return sec, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
