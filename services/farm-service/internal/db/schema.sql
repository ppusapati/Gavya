CREATE TABLE IF NOT EXISTS farms (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    name VARCHAR(200) NOT NULL,
    code VARCHAR(50) NOT NULL,
    address TEXT,
    city VARCHAR(100),
    state VARCHAR(100),
    country VARCHAR(100),
    capacity INT NOT NULL DEFAULT 0,
    manager_id VARCHAR(26),
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ,
    UNIQUE(tenant_id, code)
);

CREATE TABLE IF NOT EXISTS farm_sections (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    farm_id VARCHAR(26) NOT NULL REFERENCES farms(id),
    name VARCHAR(200) NOT NULL,
    section_type VARCHAR(50),
    capacity INT NOT NULL DEFAULT 0,
    current_occupancy INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_farms_tenant ON farms(tenant_id);
CREATE INDEX IF NOT EXISTS idx_farm_sections_farm ON farm_sections(farm_id);
