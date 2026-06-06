package domain

import "time"

type Notification struct {
	ID, TenantID, RecipientID, RecipientType string
	Channel, Title, Body, Status, Priority   string
	ReferenceID, ReferenceType               string
	SentAt, ReadAt                           *time.Time
	CreatedAt, UpdatedAt                     time.Time
	CreatedBy, UpdatedBy                     string
	DeletedAt                                *time.Time
}

type NotificationTemplate struct {
	ID, TenantID, EventType, Channel string
	Title, BodyTemplate              string
	IsActive                         bool
	CreatedAt, UpdatedAt             time.Time
	CreatedBy, UpdatedBy             string
	DeletedAt                        *time.Time
}
