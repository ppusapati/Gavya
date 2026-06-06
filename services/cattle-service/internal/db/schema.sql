CREATE TABLE IF NOT EXISTS breeds (
    id          VARCHAR(26) PRIMARY KEY,
    tenant_id   VARCHAR(26) NOT NULL,
    name        VARCHAR(100) NOT NULL,
    origin      VARCHAR(100),
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(26) NOT NULL,
    updated_by  VARCHAR(26) NOT NULL,
    deleted_at  TIMESTAMPTZ,
    UNIQUE(tenant_id, name)
);

CREATE TABLE IF NOT EXISTS cattle (
    id            VARCHAR(26) PRIMARY KEY,
    tenant_id     VARCHAR(26) NOT NULL,
    tag_number    VARCHAR(50) NOT NULL,
    name          VARCHAR(100),
    breed_id      VARCHAR(26) REFERENCES breeds(id),
    date_of_birth DATE,
    gender        VARCHAR(1) NOT NULL CHECK (gender IN ('M','F')),
    status        VARCHAR(20) NOT NULL DEFAULT 'active',
    weight        NUMERIC(8,2),
    color         VARCHAR(50),
    owner_id      VARCHAR(26),
    farm_id       VARCHAR(26),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by    VARCHAR(26) NOT NULL,
    updated_by    VARCHAR(26) NOT NULL,
    deleted_at    TIMESTAMPTZ,
    UNIQUE(tenant_id, tag_number)
);

CREATE TABLE IF NOT EXISTS cattle_lineage (
    id        VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    cattle_id VARCHAR(26) NOT NULL REFERENCES cattle(id),
    sire_id   VARCHAR(26) REFERENCES cattle(id),
    dam_id    VARCHAR(26) REFERENCES cattle(id)
);

CREATE INDEX IF NOT EXISTS idx_cattle_tenant ON cattle(tenant_id);
CREATE INDEX IF NOT EXISTS idx_cattle_status ON cattle(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_breeds_tenant ON breeds(tenant_id);
