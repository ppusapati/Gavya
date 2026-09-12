package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/libs/integrity/audit"
	"github.com/ppusapati/gavya/libs/integrity/sys"

	"github.com/ppusapati/gavya/services/tenant-service/internal/domain"
)

// ErrNotFound lets a caller tell a missing record from a failed query. Without
// it every outcome reaches the handler as an opaque error and is reported as
// internal, so a client cannot distinguish "no such tenant" from "the database
// is unreachable".
var ErrNotFound = errors.New("not found")

// ErrDuplicateSlug is the unique violation on a tenant slug, named so the
// handler can report a conflict rather than an internal failure.
var ErrDuplicateSlug = errors.New("a tenant with that slug already exists")

// Columns are listed explicitly rather than selected with *, because the scans
// below are positional: adding a column to the table would silently misalign
// every field after it.
const tenantCols = `id,name,slug,plan,status,contact_email,COALESCE(contact_phone,''),COALESCE(address,''),country,` +
	`timezone,currency,currency_scale,max_users,max_cattle,created_at,updated_at,created_by,updated_by,deleted_at`

const settingCols = `id,tenant_id,key,COALESCE(value,''),data_type,created_at,updated_at,created_by,updated_by`

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
	ids  audit.IDs
}

// serviceName is what this service's audit entries are attributed to.
const serviceName = "tenant-service"

func New(pool *pgxpool.Pool) Repository {
	return &repo{pool: pool, ids: sys.IDs{}}
}

type scanner interface {
	Scan(dest ...any) error
}

func (r *repo) CreateTenant(ctx context.Context, t *domain.Tenant) (*domain.Tenant, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO tenants (id,name,slug,plan,status,contact_email,contact_phone,address,country,timezone,currency,currency_scale,max_users,max_cattle,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		 RETURNING `+tenantCols,
		t.ID, t.Name, t.Slug, t.Plan, t.Status, t.ContactEmail, t.ContactPhone,
		t.Address, t.Country, t.Timezone, t.Currency, t.CurrencyScale, t.MaxUsers, t.MaxCattle,
		t.CreatedBy, t.UpdatedBy,
	)
	out, err := scanTenant(row)
	if isUniqueViolation(err) {
		return nil, ErrDuplicateSlug
	}
	return out, err
}

func (r *repo) GetTenant(ctx context.Context, id string) (*domain.Tenant, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+tenantCols+` FROM tenants WHERE id=$1 AND deleted_at IS NULL`,
		id,
	)
	return scanTenant(row)
}

func (r *repo) GetTenantBySlug(ctx context.Context, slug string) (*domain.Tenant, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+tenantCols+` FROM tenants WHERE slug=$1 AND deleted_at IS NULL`,
		slug,
	)
	return scanTenant(row)
}

func (r *repo) ListTenants(ctx context.Context) ([]*domain.Tenant, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+tenantCols+` FROM tenants WHERE deleted_at IS NULL ORDER BY name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]*domain.Tenant, 0)
	for rows.Next() {
		t, err := scanTenant(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, t)
	}
	return result, rows.Err()
}

// UpdateTenant rewrites a society's own record, keeping what it said before.
//
// The fields here are not administrative detail. currency and timezone decide
// what every figure in the platform means and when a day starts; max_users and
// max_cattle decide what the society may do. All of them were overwritten in
// place, and the row afterwards named whoever last touched it and nothing said
// what it had been — so a society whose currency changed could not be shown the
// change, only the result.
func (r *repo) UpdateTenant(ctx context.Context, t *domain.Tenant) (*domain.Tenant, error) {
	return r.recordedTenantUpdate(ctx, t.ID, "update_tenant",
		`UPDATE tenants SET name=$2,contact_email=$3,contact_phone=$4,address=$5,country=$6,timezone=$7,currency=$8,max_users=$9,max_cattle=$10,updated_by=$11,updated_at=NOW()
		 WHERE id=$1 AND deleted_at IS NULL
		 RETURNING `+tenantCols,
		[]any{t.ID, t.Name, t.ContactEmail, t.ContactPhone, t.Address, t.Country,
			t.Timezone, t.Currency, t.MaxUsers, t.MaxCattle, t.UpdatedBy})
}

// UpdateTenantStatus suspends or restores a whole society.
func (r *repo) UpdateTenantStatus(ctx context.Context, id, status, updatedBy string) (*domain.Tenant, error) {
	return r.recordedTenantUpdate(ctx, id, "update_tenant_status",
		`UPDATE tenants SET status=$2,updated_by=$3,updated_at=NOW()
		 WHERE id=$1 AND deleted_at IS NULL
		 RETURNING `+tenantCols,
		[]any{id, status, updatedBy})
}

// recordedTenantUpdate applies an update and records both sides of it.
//
// The before is the whole row, read under the lock the update will take, and the
// after is the row the update returns — so the two are the same shape and a
// reader comparing them sees exactly the fields that moved. Both are written in
// the transaction that makes the change, so a trail that cannot be written takes
// the change down with it rather than leaving one without the other.
func (r *repo) recordedTenantUpdate(ctx context.Context, id, action, query string, args []any) (*domain.Tenant, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	before, err := scanTenant(tx.QueryRow(ctx,
		`SELECT `+tenantCols+` FROM tenants WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id))
	if err != nil {
		return nil, err
	}

	after, err := scanTenant(tx.QueryRow(ctx, query, args...))
	if err != nil {
		return nil, err
	}

	if err := audit.Write(ctx, tx, r.ids, audit.Entry{
		Action: action, ResourceType: "tenant", ResourceID: id,
		Before: before, After: after, ServiceName: serviceName,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return after, nil
}

func (r *repo) UpsertTenantSetting(ctx context.Context, s *domain.TenantSetting) (*domain.TenantSetting, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO tenant_settings (id,tenant_id,key,value,data_type,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 ON CONFLICT (tenant_id,key) DO UPDATE SET value=EXCLUDED.value,data_type=EXCLUDED.data_type,updated_by=EXCLUDED.updated_by,updated_at=NOW()
		 RETURNING `+settingCols,
		s.ID, s.TenantID, s.Key, s.Value, s.DataType, s.CreatedBy, s.UpdatedBy,
	)
	return scanTenantSetting(row)
}

func (r *repo) ListTenantSettings(ctx context.Context, tenantID string) ([]*domain.TenantSetting, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+settingCols+` FROM tenant_settings WHERE tenant_id=$1 ORDER BY key`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]*domain.TenantSetting, 0)
	for rows.Next() {
		s, err := scanTenantSetting(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

func scanTenant(s scanner) (*domain.Tenant, error) {
	t := &domain.Tenant{}
	err := s.Scan(&t.ID, &t.Name, &t.Slug, &t.Plan, &t.Status, &t.ContactEmail, &t.ContactPhone,
		&t.Address, &t.Country, &t.Timezone, &t.Currency, &t.CurrencyScale, &t.MaxUsers, &t.MaxCattle,
		&t.CreatedAt, &t.UpdatedAt, &t.CreatedBy, &t.UpdatedBy, &t.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return t, nil
}

func scanTenantSetting(s scanner) (*domain.TenantSetting, error) {
	ts := &domain.TenantSetting{}
	err := s.Scan(&ts.ID, &ts.TenantID, &ts.Key, &ts.Value, &ts.DataType,
		&ts.CreatedAt, &ts.UpdatedAt, &ts.CreatedBy, &ts.UpdatedBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return ts, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
