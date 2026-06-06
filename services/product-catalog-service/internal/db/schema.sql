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
