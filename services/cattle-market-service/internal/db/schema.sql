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
    -- The read path now takes these out as decimal literals and holds them in
    -- libs/integrity/money, which is exact to the full width of the column, so
    -- the ceiling would refuse figures the service handles correctly.
    --
    -- Named, not looped. information_schema.columns lists views as well as
    -- tables, and ALTER TABLE ... DROP CONSTRAINT on a view aborts everything
    -- after it in this file — which is how e2e/migration_test.go caught the
    -- first attempt at this in product-catalog-service.
    ALTER TABLE cattle_listings DROP CONSTRAINT IF EXISTS cattle_listings_asking_price_carriable;
    ALTER TABLE cattle_bids     DROP CONSTRAINT IF EXISTS cattle_bids_bid_amount_carriable;
    ALTER TABLE cattle_sales    DROP CONSTRAINT IF EXISTS cattle_sales_sale_price_carriable;
END
$$;

CREATE TABLE IF NOT EXISTS tenant_currency (
    tenant_id      VARCHAR(26) PRIMARY KEY,
    currency       CHAR(3) NOT NULL,
    currency_scale SMALLINT NOT NULL CHECK (currency_scale BETWEEN 0 AND 4),
    pinned_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
