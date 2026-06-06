CREATE TABLE IF NOT EXISTS breeding_cycles (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    cattle_id VARCHAR(26) NOT NULL,
    heat_date TIMESTAMPTZ NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'heat',
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS inseminations (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    cycle_id VARCHAR(26) NOT NULL REFERENCES breeding_cycles(id),
    cattle_id VARCHAR(26) NOT NULL,
    bull_id VARCHAR(26),
    semen_batch_id VARCHAR(26),
    inseminated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    method VARCHAR(20) NOT NULL DEFAULT 'AI',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS pregnancies (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    cattle_id VARCHAR(26) NOT NULL,
    insemination_id VARCHAR(26) NOT NULL REFERENCES inseminations(id),
    confirmed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expected_calving_date TIMESTAMPTZ NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS calving_records (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    pregnancy_id VARCHAR(26) NOT NULL REFERENCES pregnancies(id),
    cattle_id VARCHAR(26) NOT NULL,
    calf_id VARCHAR(26),
    calving_date TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    calf_gender VARCHAR(1),
    calf_weight NUMERIC(6,2),
    complications TEXT,
    status VARCHAR(20) NOT NULL DEFAULT 'normal',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_cycles_tenant ON breeding_cycles(tenant_id, cattle_id);
CREATE INDEX IF NOT EXISTS idx_pregnancies_status ON pregnancies(tenant_id, status);
