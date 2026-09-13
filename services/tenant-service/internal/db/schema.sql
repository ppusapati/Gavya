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
    -- No default. A tenant's currency decides how every amount recorded against
    -- it is read, and a default is how a deployment outside India ends up
    -- silently recording rupees.
    currency CHAR(3) NOT NULL,
    -- Stored rather than looked up, so a change to the currency table can never
    -- retroactively change what an amount already written means.
    currency_scale SMALLINT NOT NULL CHECK (currency_scale BETWEEN 0 AND 4),
    max_users INT NOT NULL DEFAULT 5,
    max_cattle INT NOT NULL DEFAULT 100,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

-- Existing deployments predate the currency columns. Adding them by ALTER as
-- well as in the CREATE above is what lets a database that already has the
-- table pick them up; the backfill assumes the rupee those rows were written in.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_name='tenants' AND column_name='currency_scale') THEN
        ALTER TABLE tenants ADD COLUMN currency_scale SMALLINT;
        UPDATE tenants SET currency_scale = 2 WHERE currency_scale IS NULL;
        ALTER TABLE tenants ALTER COLUMN currency_scale SET NOT NULL;
        ALTER TABLE tenants ADD CONSTRAINT tenants_currency_scale_range
            CHECK (currency_scale BETWEEN 0 AND 4);
    END IF;
    -- A default on currency would let a tenant be created without one being
    -- chosen, which is the thing this is here to prevent.
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name='tenants' AND column_name='currency'
                 AND column_default IS NOT NULL) THEN
        ALTER TABLE tenants ALTER COLUMN currency DROP DEFAULT;
    END IF;
    -- And its type. The first version declared currency VARCHAR(3); the CREATE
    -- above says CHAR(3), and nothing changed a column that already existed.
    -- The two compare differently — CHAR pads, VARCHAR does not — so a database
    -- upgraded from the first version held a subtly different column from a
    -- fresh one, for the value every amount in the platform is read against.
    -- Every value is a three-letter ISO code, so the conversion loses nothing.
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name='tenants' AND column_name='currency'
                 AND data_type='character varying') THEN
        ALTER TABLE tenants ALTER COLUMN currency TYPE CHAR(3);
    END IF;
END
$$;

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
