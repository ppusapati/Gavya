package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ppusapati/gavya/services/file-service/internal/domain"
)

// ErrNotFound lets a caller tell a missing record from a failed query. Without
// it every outcome reaches the handler as an opaque error and is reported as
// internal, so a client cannot distinguish "no such file" from "the database is
// unreachable".
var ErrNotFound = errors.New("not found")

// Columns are listed explicitly rather than selected with *, because the scans
// below are positional: adding a column to the table would silently misalign
// every field after it.
const fileRecordCols = `id,tenant_id,original_name,stored_name,content_type,size_bytes,storage_path,` +
	`storage_provider,COALESCE(entity_type,''),COALESCE(entity_id,''),uploaded_by,is_public,created_at,updated_at,created_by,updated_by,deleted_at`

type Repository interface {
	CreateFileRecord(ctx context.Context, f *domain.FileRecord) (*domain.FileRecord, error)
	GetFileRecord(ctx context.Context, id, tenantID string) (*domain.FileRecord, error)
	ListEntityFiles(ctx context.Context, tenantID, entityType, entityID string) ([]*domain.FileRecord, error)
	SoftDeleteFile(ctx context.Context, id, tenantID, updatedBy string) error
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

func (r *repo) CreateFileRecord(ctx context.Context, f *domain.FileRecord) (*domain.FileRecord, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO file_records (id,tenant_id,original_name,stored_name,content_type,size_bytes,storage_path,storage_provider,entity_type,entity_id,uploaded_by,is_public,created_by,updated_by)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING `+fileRecordCols,
		f.ID, f.TenantID, f.OriginalName, f.StoredName, f.ContentType, f.SizeBytes,
		f.StoragePath, f.StorageProvider, f.EntityType, f.EntityID, f.UploadedBy,
		f.IsPublic, f.CreatedBy, f.UpdatedBy,
	)
	return scanFileRecord(row)
}

func (r *repo) GetFileRecord(ctx context.Context, id, tenantID string) (*domain.FileRecord, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+fileRecordCols+` FROM file_records WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanFileRecord(row)
}

func (r *repo) ListEntityFiles(ctx context.Context, tenantID, entityType, entityID string) ([]*domain.FileRecord, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+fileRecordCols+` FROM file_records WHERE tenant_id=$1 AND entity_type=$2 AND entity_id=$3 AND deleted_at IS NULL ORDER BY created_at DESC`,
		tenantID, entityType, entityID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.FileRecord
	for rows.Next() {
		f, err := scanFileRecord(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, f)
	}
	return result, rows.Err()
}

func (r *repo) SoftDeleteFile(ctx context.Context, id, tenantID, updatedBy string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE file_records SET deleted_at=NOW(),updated_by=$3,updated_at=NOW() WHERE id=$1 AND tenant_id=$2`,
		id, tenantID, updatedBy,
	)
	return err
}

func scanFileRecord(s scanner) (*domain.FileRecord, error) {
	f := &domain.FileRecord{}
	err := s.Scan(&f.ID, &f.TenantID, &f.OriginalName, &f.StoredName, &f.ContentType,
		&f.SizeBytes, &f.StoragePath, &f.StorageProvider, &f.EntityType, &f.EntityID,
		&f.UploadedBy, &f.IsPublic, &f.CreatedAt, &f.UpdatedAt, &f.CreatedBy, &f.UpdatedBy, &f.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return f, nil
}
