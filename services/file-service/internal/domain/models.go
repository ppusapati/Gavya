package domain

import "time"

type FileRecord struct {
	ID              string
	TenantID        string
	OriginalName    string
	StoredName      string
	ContentType     string
	SizeBytes       int64
	StoragePath     string
	StorageProvider string
	EntityType      string
	EntityID        string
	UploadedBy      string
	IsPublic        bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
	CreatedBy       string
	UpdatedBy       string
	DeletedAt       *time.Time
}
