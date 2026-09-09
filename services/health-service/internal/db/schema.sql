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

    -- The ceiling on money columns is gone, because the reason for it is.
    --
    -- vet_visits.cost is NUMERIC(18,4) so one schema serves a yen deployment and
    -- a dinar one. This service used to read it into float64, and float64
    -- carries a four-decimal value exactly only up to about 10^11 — measured,
    -- not assumed: 200,000 values below 10^11 round-tripped through
    -- NUMERIC(18,4) -> float64 -> JSON -> float64 without losing a digit, and
    -- above 10^12 more than three quarters of them did. A figure of
    -- 6791947779410.3551 came back as 6791947779410.3555.
    --
    -- So a CHECK refused what the code would have mangled. That was a limit of
    -- the Go read path written into the database, which made a fact about
    -- float64 look like a fact about money. The read path now takes the cost out
    -- as a decimal literal and holds it in libs/integrity/money, which is exact
    -- to the full width of the column.
    --
    -- Named, not looped: information_schema.columns lists views as well as
    -- tables, and ALTER TABLE ... DROP CONSTRAINT on a view aborts everything
    -- after it in this file.
    ALTER TABLE vet_visits DROP CONSTRAINT IF EXISTS vet_visits_cost_carriable;
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
