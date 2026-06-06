CREATE TABLE IF NOT EXISTS feed_types (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    name VARCHAR(200) NOT NULL,
    category VARCHAR(100),
    unit VARCHAR(50) NOT NULL DEFAULT 'kg',
    nutritional_info TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS nutrition_plans (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    cattle_id VARCHAR(26) NOT NULL,
    feed_type_id VARCHAR(26) NOT NULL REFERENCES feed_types(id),
    daily_quantity_kg NUMERIC(8,3) NOT NULL,
    start_date TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    end_date TIMESTAMPTZ,
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS feed_consumption (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    cattle_id VARCHAR(26) NOT NULL,
    feed_type_id VARCHAR(26) NOT NULL REFERENCES feed_types(id),
    quantity_kg NUMERIC(8,3) NOT NULL,
    fed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    fed_by VARCHAR(26),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_feed_types_tenant ON feed_types(tenant_id);
CREATE INDEX IF NOT EXISTS idx_nutrition_plans_cattle ON nutrition_plans(tenant_id, cattle_id);
CREATE INDEX IF NOT EXISTS idx_feed_consumption_cattle ON feed_consumption(tenant_id, cattle_id, fed_at);
