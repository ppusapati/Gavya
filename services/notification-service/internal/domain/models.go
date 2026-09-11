package domain

import "time"

type Notification struct {
	ID            string     `json:"id"`
	TenantID      string     `json:"tenant_id"`
	RecipientID   string     `json:"recipient_id"`
	RecipientType string     `json:"recipient_type"`
	Channel       string     `json:"channel"`
	Title         string     `json:"title"`
	Body          string     `json:"body"`
	Status        string     `json:"status"`
	Priority      string     `json:"priority"`
	ReferenceID   string     `json:"reference_id"`
	ReferenceType string     `json:"reference_type"`
	SentAt        *time.Time `json:"sent_at"`
	ReadAt        *time.Time `json:"read_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	CreatedBy     string     `json:"created_by"`
	UpdatedBy     string     `json:"updated_by"`
	DeletedAt     *time.Time `json:"deleted_at"`
}

type NotificationTemplate struct {
	ID           string     `json:"id"`
	TenantID     string     `json:"tenant_id"`
	EventType    string     `json:"event_type"`
	Channel      string     `json:"channel"`
	Title        string     `json:"title"`
	BodyTemplate string     `json:"body_template"`
	IsActive     bool       `json:"is_active"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	CreatedBy    string     `json:"created_by"`
	UpdatedBy    string     `json:"updated_by"`
	DeletedAt    *time.Time `json:"deleted_at"`
}
