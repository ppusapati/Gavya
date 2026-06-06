CREATE TABLE IF NOT EXISTS milk_sessions (
    id           VARCHAR(26) PRIMARY KEY,
    tenant_id    VARCHAR(26) NOT NULL,
    cattle_id    VARCHAR(26) NOT NULL,
    session_date DATE NOT NULL,
    shift_type   VARCHAR(20) NOT NULL CHECK (shift_type IN ('morning','afternoon','evening')),
    status       VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by   VARCHAR(26) NOT NULL,
    updated_by   VARCHAR(26) NOT NULL,
    deleted_at   TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS milk_records (
    id               VARCHAR(26) PRIMARY KEY,
    tenant_id        VARCHAR(26) NOT NULL,
    session_id       VARCHAR(26) NOT NULL REFERENCES milk_sessions(id),
    cattle_id        VARCHAR(26) NOT NULL,
    quantity_liters  NUMERIC(8,3) NOT NULL,
    recorded_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    recorded_by      VARCHAR(26) NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by       VARCHAR(26) NOT NULL,
    updated_by       VARCHAR(26) NOT NULL,
    deleted_at       TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS milk_quality (
    id          VARCHAR(26) PRIMARY KEY,
    tenant_id   VARCHAR(26) NOT NULL,
    record_id   VARCHAR(26) NOT NULL REFERENCES milk_records(id),
    fat_percent NUMERIC(5,2),
    snf_percent NUMERIC(5,2),
    lactose     NUMERIC(5,2),
    tested_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(26) NOT NULL,
    updated_by  VARCHAR(26) NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_milk_sessions_tenant   ON milk_sessions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_milk_sessions_cattle   ON milk_sessions(tenant_id, cattle_id);
CREATE INDEX IF NOT EXISTS idx_milk_records_session   ON milk_records(session_id);
CREATE INDEX IF NOT EXISTS idx_milk_records_tenant    ON milk_records(tenant_id);
