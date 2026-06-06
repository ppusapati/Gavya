CREATE TABLE IF NOT EXISTS file_records (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    original_name VARCHAR(500) NOT NULL,
    stored_name VARCHAR(500) NOT NULL,
    content_type VARCHAR(100) NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    storage_path TEXT NOT NULL,
    storage_provider VARCHAR(50) NOT NULL,
    entity_type VARCHAR(100),
    entity_id VARCHAR(26),
    uploaded_by VARCHAR(26) NOT NULL,
    is_public BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_file_records_tenant ON file_records(tenant_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_file_records_entity ON file_records(tenant_id, entity_type, entity_id);
