CREATE TABLE IF NOT EXISTS tenants (
    id VARCHAR(26) PRIMARY KEY,
    name VARCHAR(200) NOT NULL,
    slug VARCHAR(200) NOT NULL UNIQUE,
    plan VARCHAR(30) NOT NULL DEFAULT 'free',
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    contact_email VARCHAR(300) NOT NULL,
    contact_phone VARCHAR(50),
    address TEXT,
    country VARCHAR(100) NOT NULL DEFAULT 'India',
    timezone VARCHAR(100) NOT NULL DEFAULT 'UTC',
    currency VARCHAR(3) NOT NULL DEFAULT 'INR',
    max_users INT NOT NULL DEFAULT 5,
    max_cattle INT NOT NULL DEFAULT 100,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS tenant_settings (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL REFERENCES tenants(id),
    key VARCHAR(100) NOT NULL,
    value TEXT,
    data_type VARCHAR(30) NOT NULL DEFAULT 'string',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    UNIQUE(tenant_id, key)
);

CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants(status);
CREATE INDEX IF NOT EXISTS idx_tenant_settings_tenant ON tenant_settings(tenant_id);
