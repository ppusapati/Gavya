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

	// What the run produced, and what it could not.
	//
	// Content is the report itself. It lives in the row rather than on a disk
	// because there is no object storage in this platform and no volume shared
	// between replicas, so a file written by one process is one the next
	// request cannot read.
	Content     []byte `json:"-"`
	ContentType string `json:"content_type,omitempty"`
	RowCount    *int64 `json:"row_count,omitempty"`
	// Truncated says the render stopped at its ceiling. A truncated total
	// somebody acts on is short by an amount nothing else on the page
	// discloses, so this travels with the report rather than being inferred.
	Truncated bool `json:"truncated"`
	// FailureReason is why it failed. A report that failed with nothing
	// recorded leaves the person who asked for it with nothing to correct.
	FailureReason string `json:"failure_reason,omitempty"`
	// Attempts bounds a crash loop: a report whose render kills the process
	// would otherwise be picked up again by the replacement, for ever.
	Attempts int32 `json:"attempts"`
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

	// Timezone is the zone this schedule's times are in, as an IANA name.
	//
	// Not the process's zone and not UTC. Seven in the morning is seven where
	// the society is; read in UTC a schedule set in Maharashtra fires at half
	// past twelve in the afternoon, and the report covering "yesterday" covers
	// a day that ended five and a half hours before the one everybody means.
	Timezone string `json:"timezone"`
	// LastError is what went wrong the last time this fired. A schedule that
	// has stopped producing its report is noticed weeks later, when somebody
	// asks where the report went; this is where the answer is.
	LastError string `json:"last_error,omitempty"`
}
