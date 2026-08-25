package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/ppusapati/gavya/services/file-service/internal/domain"
	ulidpkg "p9e.in/samavaya/packages/ulid"
)

// ErrInvalidArgument marks a caller mistake. Without it the handler cannot tell
// "that file was never stored" from "the query failed", and would have to
// report both the same way.
var ErrInvalidArgument = errors.New("invalid argument")

// invalidArgument carries the reason alone. The marker is matched through Is,
// so errors.Is finds it while the message stays free of a prefix the error code
// already conveys.
type invalidArgument struct{ reason string }

func (e *invalidArgument) Error() string { return e.reason }

func (e *invalidArgument) Is(target error) bool { return target == ErrInvalidArgument }

func invalid(msg string) error { return &invalidArgument{reason: msg} }

func (s *Service) CreateFileRecord(ctx context.Context, tenantID, originalName, storedName, contentType string, sizeBytes int64, storagePath, entityType, entityID, uploadedBy string, isPublic bool, createdBy string) (*domain.FileRecord, error) {
	f := &domain.FileRecord{
		ID:              ulidpkg.New().String(),
		TenantID:        tenantID,
		OriginalName:    originalName,
		StoredName:      storedName,
		ContentType:     contentType,
		SizeBytes:       sizeBytes,
		StoragePath:     storagePath,
		StorageProvider: s.cfg.StorageProvider,
		EntityType:      entityType,
		EntityID:        entityID,
		UploadedBy:      uploadedBy,
		IsPublic:        isPublic,
		CreatedBy:       createdBy,
		UpdatedBy:       createdBy,
	}
	return s.repo.CreateFileRecord(ctx, f)
}

func (s *Service) GetFileRecord(ctx context.Context, id, tenantID string) (*domain.FileRecord, error) {
	return s.repo.GetFileRecord(ctx, id, tenantID)
}

func (s *Service) ListEntityFiles(ctx context.Context, tenantID, entityType, entityID string) ([]*domain.FileRecord, error) {
	return s.repo.ListEntityFiles(ctx, tenantID, entityType, entityID)
}

func (s *Service) DeleteFile(ctx context.Context, id, tenantID, updatedBy string) error {
	return s.repo.SoftDeleteFile(ctx, id, tenantID, updatedBy)
}

func (s *Service) GetDownloadURL(ctx context.Context, id, tenantID string) (string, error) {
	f, err := s.repo.GetFileRecord(ctx, id, tenantID)
	if err != nil {
		return "", err
	}
	if f.StoragePath == "" {
		return "", invalid("file storage path not found")
	}
	return fmt.Sprintf("%s/%s", s.cfg.StorageBucket, f.StoredName), nil
}
