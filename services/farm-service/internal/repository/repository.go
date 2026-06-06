package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ppusapati/gavya/services/farm-service/internal/domain"
)

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
}

func New(pool *pgxpool.Pool) Repository {
	return &repo{pool: pool}
}

type scanner interface {
	Scan(dest ...any) error
}

func (r *repo) CreateFarm(ctx context.Context, f *domain.Farm) (*domain.Farm, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO farms (id,tenant_id,name,code,address,city,state,country,capacity,manager_id,status,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING *`,
		f.ID, f.TenantID, f.Name, f.Code, f.Address, f.City, f.State, f.Country,
		f.Capacity, f.ManagerID, f.Status, f.CreatedBy, f.UpdatedBy,
	)
	return scanFarm(row)
}

func (r *repo) GetFarm(ctx context.Context, id, tenantID string) (*domain.Farm, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT * FROM farms WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanFarm(row)
}

func (r *repo) ListFarms(ctx context.Context, tenantID string) ([]*domain.Farm, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM farms WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Farm
	for rows.Next() {
		f := &domain.Farm{}
		if err := rows.Scan(&f.ID, &f.TenantID, &f.Name, &f.Code, &f.Address, &f.City, &f.State, &f.Country,
			&f.Capacity, &f.ManagerID, &f.Status, &f.CreatedAt, &f.UpdatedAt, &f.CreatedBy, &f.UpdatedBy, &f.DeletedAt); err != nil {
			return nil, err
		}
		result = append(result, f)
	}
	return result, rows.Err()
}

func (r *repo) UpdateFarm(ctx context.Context, f *domain.Farm) (*domain.Farm, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE farms SET name=$3,address=$4,city=$5,state=$6,country=$7,manager_id=$8,status=$9,updated_by=$10,updated_at=NOW()
		 WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL RETURNING *`,
		f.ID, f.TenantID, f.Name, f.Address, f.City, f.State, f.Country,
		f.ManagerID, f.Status, f.UpdatedBy,
	)
	return scanFarm(row)
}

func (r *repo) UpdateFarmCapacity(ctx context.Context, id, tenantID string, capacity int, updatedBy string) (*domain.Farm, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE farms SET capacity=$3,updated_by=$4,updated_at=NOW() WHERE id=$1 AND tenant_id=$2 RETURNING *`,
		id, tenantID, capacity, updatedBy,
	)
	return scanFarm(row)
}

func (r *repo) CreateFarmSection(ctx context.Context, s *domain.FarmSection) (*domain.FarmSection, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO farm_sections (id,tenant_id,farm_id,name,section_type,capacity,current_occupancy,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING *`,
		s.ID, s.TenantID, s.FarmID, s.Name, s.SectionType, s.Capacity, s.CurrentOccupancy, s.CreatedBy, s.UpdatedBy,
	)
	return scanFarmSection(row)
}

func (r *repo) GetFarmSection(ctx context.Context, id, tenantID string) (*domain.FarmSection, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT * FROM farm_sections WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanFarmSection(row)
}

func (r *repo) ListFarmSections(ctx context.Context, tenantID, farmID string) ([]*domain.FarmSection, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM farm_sections WHERE tenant_id=$1 AND farm_id=$2 AND deleted_at IS NULL ORDER BY name`,
		tenantID, farmID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.FarmSection
	for rows.Next() {
		s := &domain.FarmSection{}
		if err := rows.Scan(&s.ID, &s.TenantID, &s.FarmID, &s.Name, &s.SectionType,
			&s.Capacity, &s.CurrentOccupancy, &s.CreatedAt, &s.UpdatedAt, &s.CreatedBy, &s.UpdatedBy, &s.DeletedAt); err != nil {
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
		return nil, err
	}
	return f, nil
}

func scanFarmSection(s scanner) (*domain.FarmSection, error) {
	sec := &domain.FarmSection{}
	err := s.Scan(&sec.ID, &sec.TenantID, &sec.FarmID, &sec.Name, &sec.SectionType,
		&sec.Capacity, &sec.CurrentOccupancy, &sec.CreatedAt, &sec.UpdatedAt, &sec.CreatedBy, &sec.UpdatedBy, &sec.DeletedAt)
	if err != nil {
		return nil, err
	}
	return sec, nil
}
