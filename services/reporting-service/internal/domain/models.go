package domain

import "time"

type Report struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	Name        string     `json:"name"`
	ReportType  string     `json:"report_type"`
	Parameters  string     `json:"parameters"`
	Status      string     `json:"status"`
	FilePath    string     `json:"file_path"`
	FileFormat  string     `json:"file_format"`
	RequestedBy string     `json:"requested_by"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CreatedBy   string     `json:"created_by"`
	UpdatedBy   string     `json:"updated_by"`
	DeletedAt   *time.Time `json:"deleted_at"`
}

type ReportSchedule struct {
	ID         string     `json:"id"`
	TenantID   string     `json:"tenant_id"`
	ReportType string     `json:"report_type"`
	Schedule   string     `json:"schedule"`
	Parameters string     `json:"parameters"`
	IsActive   bool       `json:"is_active"`
	LastRunAt  *time.Time `json:"last_run_at"`
	NextRunAt  *time.Time `json:"next_run_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	CreatedBy  string     `json:"created_by"`
	UpdatedBy  string     `json:"updated_by"`
	DeletedAt  *time.Time `json:"deleted_at"`
}
