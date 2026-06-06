CREATE TABLE IF NOT EXISTS reports (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    name VARCHAR(200) NOT NULL,
    report_type VARCHAR(100) NOT NULL,
    parameters JSONB,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    file_path TEXT,
    file_format VARCHAR(20),
    requested_by VARCHAR(26) NOT NULL,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS report_schedules (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    report_type VARCHAR(100) NOT NULL,
    schedule VARCHAR(100) NOT NULL,
    parameters JSONB,
    is_active BOOLEAN NOT NULL DEFAULT true,
    last_run_at TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_reports_tenant ON reports(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_schedules_tenant ON report_schedules(tenant_id, is_active);
