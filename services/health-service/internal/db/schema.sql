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

-- ---------------------------------------------------------------------------
-- Multi-currency
--
-- The DEFAULT 'INR' is gone: a default is how a deployment outside India ends
-- up silently recording rupees with nothing to notice. Money columns widen to
-- four decimals, the most any ISO 4217 currency has, so one schema serves a yen
-- deployment and a dinar one; the tenant's scale says how many are real.
-- ---------------------------------------------------------------------------
DO $$
DECLARE
    col RECORD;
BEGIN
    FOR col IN
        SELECT table_name, column_name
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND data_type = 'numeric'
          AND numeric_scale = 2
          AND table_name IN ('vet_visits')
    LOOP
        EXECUTE format('ALTER TABLE %I ALTER COLUMN %I TYPE NUMERIC(18,4)',
                       col.table_name, col.column_name);
    END LOOP;

    FOR col IN
        SELECT table_name, column_name
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND column_name = 'currency'
          AND column_default IS NOT NULL
    LOOP
        EXECUTE format('ALTER TABLE %I ALTER COLUMN %I DROP DEFAULT',
                       col.table_name, col.column_name);
    END LOOP;

    -- Bound every money column to what the Go read path can carry exactly.
    --
    -- These columns are NUMERIC(18,4) so one schema serves a yen deployment and
    -- a dinar one. The services read them into float64, and float64 carries a
    -- four-decimal value exactly only up to about 10^11 — measured, not
    -- assumed: 200,000 values below 10^11 round-tripped through
    -- NUMERIC(18,4) -> float64 -> JSON -> float64 without losing a digit, and
    -- above 10^12 more than three quarters of them did.
    --
    -- So the column can hold values the code cannot carry, and nothing said so.
    -- A price of 6791947779410.3551 came back as 6791947779410.3555.
    --
    -- Refused here rather than rounded on the way out. A figure silently
    -- changed between the database and the reply is the worst version of this:
    -- both ends believe they agree. The ceiling is far above any real price —
    -- a hundred billion of any currency — so this refuses nothing a dairy does
    -- and catches the case the type quietly permits.
    --
    -- The real fix is to read these as exact decimals, as the integrity
    -- services do with libs/integrity/money. Until then the database refuses
    -- what the code would mangle.
    FOR col IN
        SELECT c.table_name, c.column_name
        FROM information_schema.columns c
        WHERE c.table_schema = current_schema()
          AND c.data_type = 'numeric'
          AND c.numeric_scale = 4
          AND NOT EXISTS (
              SELECT 1 FROM pg_constraint k
              WHERE k.conname = c.table_name || '_' || c.column_name || '_carriable'
          )
    LOOP
        EXECUTE format(
            'ALTER TABLE %I ADD CONSTRAINT %I CHECK (%I IS NULL OR abs(%I) < 100000000000)',
            col.table_name, col.table_name || '_' || col.column_name || '_carriable',
            col.column_name, col.column_name);
    END LOOP;
END
$$;

CREATE TABLE IF NOT EXISTS tenant_currency (
    tenant_id      VARCHAR(26) PRIMARY KEY,
    currency       CHAR(3) NOT NULL,
    currency_scale SMALLINT NOT NULL CHECK (currency_scale BETWEEN 0 AND 4),
    pinned_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- A vet visit's cost is money, and a money record that does not say what
-- currency it is in is a number. The column is added rather than assumed.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_name='vet_visits' AND column_name='currency') THEN
        ALTER TABLE vet_visits ADD COLUMN currency CHAR(3);
        -- Existing rows were written when the platform only did rupees.
        UPDATE vet_visits SET currency = 'INR' WHERE currency IS NULL;
        ALTER TABLE vet_visits ALTER COLUMN currency SET NOT NULL;
    END IF;
END
$$;
