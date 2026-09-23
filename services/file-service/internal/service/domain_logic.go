package service

import (
	"context"
	"errors"
	"os"

	"github.com/ppusapati/gavya/libs/integrity/signedurl"

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

// GetDownloadURL issues a signed link to a file.
//
// It used to return the configured bucket and the stored name joined with a
// slash: not a URL, not signed, not fetchable by anything, and true of a file
// that was never written as readily as one that was. What comes back now is a
// link a browser can follow, and it is only issued when the object is actually
// there — a link handed out for a file the store does not hold is a link that
// fails after somebody has emailed it.
func (s *Service) GetDownloadURL(ctx context.Context, id, tenantID string) (string, error) {
	f, err := s.repo.GetFileRecord(ctx, id, tenantID)
	if err != nil {
		return "", err
	}
	if s.keys == nil {
		return "", invalid("this deployment has no DOWNLOAD_SIGNING_KEY set, so it cannot " +
			"issue a download link")
	}

	// Resolved now rather than when the link is followed, so a link is only
	// ever handed out for something that can be served. The path itself is
	// discarded here: the route resolves it again at the moment it opens it,
	// because what is true now is not what matters then.
	if _, err := resolve(s.cfg.StorageBucket, f.StoredName); err != nil {
		switch {
		case errors.Is(err, ErrNoStore):
			return "", invalid("this deployment has no STORAGE_BUCKET set, so there is " +
				"nowhere to read files from")
		case errors.Is(err, ErrNotOnDisk):
			return "", invalid("this record names a file the store does not hold; " +
				"file-service records where something else put a file and never receives " +
				"one itself, so a record can outlive its object")
		default:
			return "", invalid(err.Error())
		}
	}

	lifetime := s.linkFor
	if lifetime <= 0 {
		lifetime = signedurl.DefaultLifetime
	}
	now := s.now()
	token, err := s.keys.Sign(signedurl.Grant{
		Purpose:  handlerPurpose,
		TenantID: tenantID,
		Resource: f.ID,
		Expires:  now.Add(lifetime),
	}, now)
	if err != nil {
		return "", err
	}
	return signedurl.Link(s.linkBase, downloadPath, token), nil
}

// The path and purpose a file link carries.
//
// Duplicated from the handler package rather than imported, because the handler
// imports this one and Go will not have it both ways. Held to the handler's by
// a test there, so the two cannot drift into a link that points at a route
// nothing serves.
const (
	downloadPath   = "/download/file"
	handlerPurpose = "file"
)

// OpenForDownload finds the bytes behind a record, for a signed link.
//
// The tenant is the one the verified token named. Every check in resolve runs
// here rather than being trusted from when the link was issued, because a
// store is a filesystem something else writes to and what was a file an hour
// ago can be a symlink now.
func (s *Service) OpenForDownload(ctx context.Context, id, tenantID string) (*os.File, *domain.FileRecord, error) {
	f, err := s.repo.GetFileRecord(ctx, id, tenantID)
	if err != nil {
		return nil, nil, err
	}
	path, err := resolve(s.cfg.StorageBucket, f.StoredName)
	if err != nil {
		return nil, nil, err
	}
	// Opened by the path resolve returned, which is the one it checked. Joining
	// anything to it here would undo every check above.
	handle, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return handle, f, nil
}
