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
