package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ppusapati/gavya/services/file-service/internal/domain"
)

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
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING *`,
		f.ID, f.TenantID, f.OriginalName, f.StoredName, f.ContentType, f.SizeBytes,
		f.StoragePath, f.StorageProvider, f.EntityType, f.EntityID, f.UploadedBy,
		f.IsPublic, f.CreatedBy, f.UpdatedBy,
	)
	return scanFileRecord(row)
}

func (r *repo) GetFileRecord(ctx context.Context, id, tenantID string) (*domain.FileRecord, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT * FROM file_records WHERE id=$1 AND tenant_id=$2 AND deleted_at IS NULL`,
		id, tenantID,
	)
	return scanFileRecord(row)
}

func (r *repo) ListEntityFiles(ctx context.Context, tenantID, entityType, entityID string) ([]*domain.FileRecord, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM file_records WHERE tenant_id=$1 AND entity_type=$2 AND entity_id=$3 AND deleted_at IS NULL ORDER BY created_at DESC`,
		tenantID, entityType, entityID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.FileRecord
	for rows.Next() {
		f := &domain.FileRecord{}
		if err := rows.Scan(&f.ID, &f.TenantID, &f.OriginalName, &f.StoredName, &f.ContentType,
			&f.SizeBytes, &f.StoragePath, &f.StorageProvider, &f.EntityType, &f.EntityID,
			&f.UploadedBy, &f.IsPublic, &f.CreatedAt, &f.UpdatedAt, &f.CreatedBy, &f.UpdatedBy, &f.DeletedAt); err != nil {
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
		return nil, err
	}
	return f, nil
}
