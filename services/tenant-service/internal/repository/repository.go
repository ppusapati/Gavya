package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ppusapati/gavya/services/tenant-service/internal/domain"
)

type Repository interface {
	CreateTenant(ctx context.Context, t *domain.Tenant) (*domain.Tenant, error)
	GetTenant(ctx context.Context, id string) (*domain.Tenant, error)
	GetTenantBySlug(ctx context.Context, slug string) (*domain.Tenant, error)
	ListTenants(ctx context.Context) ([]*domain.Tenant, error)
	UpdateTenant(ctx context.Context, t *domain.Tenant) (*domain.Tenant, error)
	UpdateTenantStatus(ctx context.Context, id, status, updatedBy string) (*domain.Tenant, error)
	UpsertTenantSetting(ctx context.Context, s *domain.TenantSetting) (*domain.TenantSetting, error)
	ListTenantSettings(ctx context.Context, tenantID string) ([]*domain.TenantSetting, error)
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

func (r *repo) CreateTenant(ctx context.Context, t *domain.Tenant) (*domain.Tenant, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO tenants (id,name,slug,plan,status,contact_email,contact_phone,address,country,timezone,currency,max_users,max_cattle,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING *`,
		t.ID, t.Name, t.Slug, t.Plan, t.Status, t.ContactEmail, t.ContactPhone,
		t.Address, t.Country, t.Timezone, t.Currency, t.MaxUsers, t.MaxCattle,
		t.CreatedBy, t.UpdatedBy,
	)
	return scanTenant(row)
}

func (r *repo) GetTenant(ctx context.Context, id string) (*domain.Tenant, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT * FROM tenants WHERE id=$1 AND deleted_at IS NULL`,
		id,
	)
	return scanTenant(row)
}

func (r *repo) GetTenantBySlug(ctx context.Context, slug string) (*domain.Tenant, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT * FROM tenants WHERE slug=$1 AND deleted_at IS NULL`,
		slug,
	)
	return scanTenant(row)
}

func (r *repo) ListTenants(ctx context.Context) ([]*domain.Tenant, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM tenants WHERE deleted_at IS NULL ORDER BY name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.Tenant
	for rows.Next() {
		t := &domain.Tenant{}
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &t.Plan, &t.Status, &t.ContactEmail, &t.ContactPhone,
			&t.Address, &t.Country, &t.Timezone, &t.Currency, &t.MaxUsers, &t.MaxCattle,
			&t.CreatedAt, &t.UpdatedAt, &t.CreatedBy, &t.UpdatedBy, &t.DeletedAt); err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

func (r *repo) UpdateTenant(ctx context.Context, t *domain.Tenant) (*domain.Tenant, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE tenants SET name=$2,contact_email=$3,contact_phone=$4,address=$5,country=$6,timezone=$7,currency=$8,max_users=$9,max_cattle=$10,updated_by=$11,updated_at=NOW()
		 WHERE id=$1 AND deleted_at IS NULL RETURNING *`,
		t.ID, t.Name, t.ContactEmail, t.ContactPhone, t.Address, t.Country, t.Timezone,
		t.Currency, t.MaxUsers, t.MaxCattle, t.UpdatedBy,
	)
	return scanTenant(row)
}

func (r *repo) UpdateTenantStatus(ctx context.Context, id, status, updatedBy string) (*domain.Tenant, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE tenants SET status=$2,updated_by=$3,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL RETURNING *`,
		id, status, updatedBy,
	)
	return scanTenant(row)
}

func (r *repo) UpsertTenantSetting(ctx context.Context, s *domain.TenantSetting) (*domain.TenantSetting, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO tenant_settings (id,tenant_id,key,value,data_type,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (tenant_id,key) DO UPDATE SET value=EXCLUDED.value,data_type=EXCLUDED.data_type,updated_by=EXCLUDED.updated_by,updated_at=NOW()
		 RETURNING *`,
		s.ID, s.TenantID, s.Key, s.Value, s.DataType, s.CreatedBy, s.UpdatedBy,
	)
	return scanTenantSetting(row)
}

func (r *repo) ListTenantSettings(ctx context.Context, tenantID string) ([]*domain.TenantSetting, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM tenant_settings WHERE tenant_id=$1 ORDER BY key`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.TenantSetting
	for rows.Next() {
		s := &domain.TenantSetting{}
		if err := rows.Scan(&s.ID, &s.TenantID, &s.Key, &s.Value, &s.DataType,
			&s.CreatedAt, &s.UpdatedAt, &s.CreatedBy, &s.UpdatedBy); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func scanTenant(s scanner) (*domain.Tenant, error) {
	t := &domain.Tenant{}
	err := s.Scan(&t.ID, &t.Name, &t.Slug, &t.Plan, &t.Status, &t.ContactEmail, &t.ContactPhone,
		&t.Address, &t.Country, &t.Timezone, &t.Currency, &t.MaxUsers, &t.MaxCattle,
		&t.CreatedAt, &t.UpdatedAt, &t.CreatedBy, &t.UpdatedBy, &t.DeletedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func scanTenantSetting(s scanner) (*domain.TenantSetting, error) {
	ts := &domain.TenantSetting{}
	err := s.Scan(&ts.ID, &ts.TenantID, &ts.Key, &ts.Value, &ts.DataType,
		&ts.CreatedAt, &ts.UpdatedAt, &ts.CreatedBy, &ts.UpdatedBy)
	if err != nil {
		return nil, err
	}
	return ts, nil
}
