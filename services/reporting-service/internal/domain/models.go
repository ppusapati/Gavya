package domain

import "time"

type Report struct {
	ID, TenantID, Name, ReportType string
	Parameters, Status             string
	FilePath, FileFormat           string
	RequestedBy                    string
	StartedAt, CompletedAt         *time.Time
	CreatedAt, UpdatedAt           time.Time
	CreatedBy, UpdatedBy           string
	DeletedAt                      *time.Time
}

type ReportSchedule struct {
	ID, TenantID, ReportType, Schedule, Parameters string
	IsActive                                        bool
	LastRunAt, NextRunAt                            *time.Time
	CreatedAt, UpdatedAt                            time.Time
	CreatedBy, UpdatedBy                            string
	DeletedAt                                       *time.Time
}
