CREATE TABLE IF NOT EXISTS vaccinations (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    cattle_id VARCHAR(26) NOT NULL,
    vaccine_name VARCHAR(200) NOT NULL,
    batch_number VARCHAR(100),
    administered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    next_due_date TIMESTAMPTZ,
    veterinarian_id VARCHAR(26),
    dosage VARCHAR(100),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS treatments (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    cattle_id VARCHAR(26) NOT NULL,
    diagnosis_code VARCHAR(50),
    diagnosis TEXT NOT NULL,
    medicine_name VARCHAR(200),
    dosage VARCHAR(100),
    treated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    treated_by VARCHAR(26),
    follow_up_date TIMESTAMPTZ,
    status VARCHAR(30) NOT NULL DEFAULT 'ongoing',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS vet_visits (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    cattle_id VARCHAR(26) NOT NULL,
    veterinarian_id VARCHAR(26) NOT NULL,
    visit_date TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    purpose VARCHAR(300),
    notes TEXT,
    cost NUMERIC(10,2) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_vaccinations_tenant ON vaccinations(tenant_id, cattle_id);
CREATE INDEX IF NOT EXISTS idx_vaccinations_due ON vaccinations(next_due_date) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_treatments_tenant ON treatments(tenant_id, cattle_id);
CREATE INDEX IF NOT EXISTS idx_vet_visits_tenant ON vet_visits(tenant_id, cattle_id);
