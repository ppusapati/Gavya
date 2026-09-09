CREATE TABLE IF NOT EXISTS categories (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    name VARCHAR(200) NOT NULL,
    slug VARCHAR(200) NOT NULL,
    parent_id VARCHAR(26),
    description TEXT,
    sort_order INT DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ,
    UNIQUE(tenant_id, slug)
);

CREATE TABLE IF NOT EXISTS brands (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    name VARCHAR(200) NOT NULL,
    slug VARCHAR(200) NOT NULL,
    logo_url TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ,
    UNIQUE(tenant_id, slug)
);

CREATE TABLE IF NOT EXISTS products (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    category_id VARCHAR(26) REFERENCES categories(id),
    brand_id VARCHAR(26) REFERENCES brands(id),
    name VARCHAR(300) NOT NULL,
    slug VARCHAR(300) NOT NULL,
    description TEXT,
    product_type VARCHAR(30) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ,
    UNIQUE(tenant_id, slug)
);

CREATE TABLE IF NOT EXISTS skus (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    product_id VARCHAR(26) NOT NULL REFERENCES products(id),
    code VARCHAR(100) NOT NULL,
    name VARCHAR(200) NOT NULL,
    price NUMERIC(12,2) NOT NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'INR',
    unit VARCHAR(20) NOT NULL,
    unit_size NUMERIC(10,3),
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ,
    UNIQUE(tenant_id, code)
);

CREATE INDEX IF NOT EXISTS idx_products_tenant ON products(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_skus_product ON skus(product_id);

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
          AND table_name IN ('skus')
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
    -- These columns are NUMERIC(18,4) so one schema serves a yen deployment and
    -- a dinar one. This service used to read them into float64, and float64
    -- carries a four-decimal value exactly only up to about 10^11 — measured,
    -- not assumed: 200,000 values below 10^11 round-tripped through
    -- NUMERIC(18,4) -> float64 -> JSON -> float64 without losing a digit, and
    -- above 10^12 more than three quarters of them did. A price of
    -- 6791947779410.3551 came back as 6791947779410.3555.
    --
    -- So the column could hold values the code could not carry, and a CHECK
    -- refused them here rather than let the service mangle them on the way out.
    -- That was a limit of the read path written into the database, which is the
    -- wrong place for it: it made a fact about Go look like a fact about money.
    --
    -- The read path now takes the price out as a decimal literal and holds it
    -- in libs/integrity/money, which is exact to the full width of the column.
    -- The ceiling would now refuse figures the service handles correctly, so it
    -- is dropped. It stays in the schemas of the services that still read
    -- float64, where it is still true.
    --
    -- Named, not looped. The loop that added these had to search for columns it
    -- did not know; undoing it does know, and the search version was worse than
    -- unnecessary — information_schema.columns lists views as well as tables,
    -- and ALTER TABLE ... DROP CONSTRAINT on a view is an error that aborts
    -- everything after it in this file. That is the third time in this
    -- repository a schema statement has failed silently far from where it was
    -- written, and e2e/migration_test.go is what caught this one.
    ALTER TABLE skus DROP CONSTRAINT IF EXISTS skus_price_carriable;
END
$$;

CREATE TABLE IF NOT EXISTS tenant_currency (
    tenant_id      VARCHAR(26) PRIMARY KEY,
    currency       CHAR(3) NOT NULL,
    currency_scale SMALLINT NOT NULL CHECK (currency_scale BETWEEN 0 AND 4),
    pinned_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
