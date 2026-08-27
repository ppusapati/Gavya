CREATE TABLE IF NOT EXISTS cattle_listings (
    id           VARCHAR(26) PRIMARY KEY,
    tenant_id    VARCHAR(26) NOT NULL,
    cattle_id    VARCHAR(26) NOT NULL,
    seller_id    VARCHAR(26) NOT NULL,
    title        VARCHAR(200) NOT NULL,
    description  TEXT,
    asking_price NUMERIC(12,2) NOT NULL,
    currency     VARCHAR(3) NOT NULL DEFAULT 'INR',
    listing_type VARCHAR(20) NOT NULL CHECK (listing_type IN ('fixed','auction','negotiable')),
    status       VARCHAR(20) NOT NULL DEFAULT 'active',
    expires_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by   VARCHAR(26) NOT NULL,
    updated_by   VARCHAR(26) NOT NULL,
    deleted_at   TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS cattle_bids (
    id          VARCHAR(26) PRIMARY KEY,
    tenant_id   VARCHAR(26) NOT NULL,
    listing_id  VARCHAR(26) NOT NULL REFERENCES cattle_listings(id),
    bidder_id   VARCHAR(26) NOT NULL,
    bid_amount  NUMERIC(12,2) NOT NULL,
    currency    VARCHAR(3) NOT NULL DEFAULT 'INR',
    status      VARCHAR(20) NOT NULL DEFAULT 'pending',
    message     TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(26) NOT NULL,
    updated_by  VARCHAR(26) NOT NULL,
    deleted_at  TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS cattle_sales (
    id            VARCHAR(26) PRIMARY KEY,
    tenant_id     VARCHAR(26) NOT NULL,
    listing_id    VARCHAR(26) NOT NULL REFERENCES cattle_listings(id),
    seller_id     VARCHAR(26) NOT NULL,
    buyer_id      VARCHAR(26) NOT NULL,
    cattle_id     VARCHAR(26) NOT NULL,
    sale_price    NUMERIC(12,2) NOT NULL,
    currency      VARCHAR(3) NOT NULL DEFAULT 'INR',
    sale_date     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    transfer_date TIMESTAMPTZ,
    status        VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by    VARCHAR(26) NOT NULL,
    updated_by    VARCHAR(26) NOT NULL,
    deleted_at    TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS cattle_ownership (
    id               VARCHAR(26) PRIMARY KEY,
    tenant_id        VARCHAR(26) NOT NULL,
    cattle_id        VARCHAR(26) NOT NULL,
    owner_id         VARCHAR(26) NOT NULL,
    acquired_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    released_at      TIMESTAMPTZ,
    acquisition_type VARCHAR(20) NOT NULL,
    sale_id          VARCHAR(26),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by       VARCHAR(26) NOT NULL,
    updated_by       VARCHAR(26) NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_listings_tenant ON cattle_listings(tenant_id);
CREATE INDEX IF NOT EXISTS idx_listings_status ON cattle_listings(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_bids_listing    ON cattle_bids(listing_id);
CREATE INDEX IF NOT EXISTS idx_ownership_cattle ON cattle_ownership(tenant_id, cattle_id);

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
          AND table_name IN ('cattle_listings','cattle_bids','cattle_sales')
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
