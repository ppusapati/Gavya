package domain

import "time"

type AuditLog struct {
	ID           string
	TenantID     string
	ActorID      string
	ActorType    string
	Action       string
	ResourceType string
	ResourceID   string
	OldValue     string
	NewValue     string
	IPAddress    string
	UserAgent    string
	ServiceName  string
	TraceID      string
	CreatedAt    time.Time
	CreatedBy    string
}
