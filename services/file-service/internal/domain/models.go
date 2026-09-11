package domain

import "time"

type FileRecord struct {
	ID              string     `json:"id"`
	TenantID        string     `json:"tenant_id"`
	OriginalName    string     `json:"original_name"`
	StoredName      string     `json:"stored_name"`
	ContentType     string     `json:"content_type"`
	SizeBytes       int64      `json:"size_bytes"`
	StoragePath     string     `json:"storage_path"`
	StorageProvider string     `json:"storage_provider"`
	EntityType      string     `json:"entity_type"`
	EntityID        string     `json:"entity_id"`
	UploadedBy      string     `json:"uploaded_by"`
	IsPublic        bool       `json:"is_public"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	CreatedBy       string     `json:"created_by"`
	UpdatedBy       string     `json:"updated_by"`
	DeletedAt       *time.Time `json:"deleted_at"`
}
