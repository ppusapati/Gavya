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

-- ---------------------------------------------------------------------------
-- Which day a reading falls on
--
-- A daily yield asks "what did this animal give today", and today is a local
-- fact. recorded_at is a TIMESTAMPTZ, and casting one to a date uses whatever
-- timezone the database session happens to be in — so a collection at one in
-- the morning Indian time falls on the 11th if the database runs in Kolkata and
-- the 10th if it runs in UTC or Chicago. Measured, not assumed. The figure a
-- fortnight's settlement is drawn from would then depend on a setting nobody
-- involved chose.
--
-- tenant-service already records a timezone per tenant, and nothing read it.
-- This pins the same fact where the reading is, on the same shape as
-- tenant_currency: fixed on first use, refused if it ever changes. Reading it
-- from a pin rather than from tenant-service means recording milk does not
-- depend on another service being reachable, which is the argument the currency
-- pin already makes.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS tenant_timezone (
    tenant_id VARCHAR(26) PRIMARY KEY,
    -- An IANA name, as in Asia/Kolkata. Validated by the service against its
    -- own tzdata before it gets here, because a zone the database accepts and
    -- the code cannot load is a row nothing can read.
    timezone  TEXT NOT NULL,
    pinned_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_milk_records_recorded ON milk_records(tenant_id, cattle_id, recorded_at);
