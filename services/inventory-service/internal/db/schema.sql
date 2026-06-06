CREATE TABLE IF NOT EXISTS warehouses (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    name VARCHAR(200) NOT NULL,
    code VARCHAR(50) NOT NULL,
    address TEXT,
    manager_id VARCHAR(26),
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ,
    UNIQUE(tenant_id, code)
);

CREATE TABLE IF NOT EXISTS inventory_items (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    warehouse_id VARCHAR(26) NOT NULL REFERENCES warehouses(id),
    sku_id VARCHAR(26) NOT NULL,
    quantity_on_hand NUMERIC(12,3) NOT NULL DEFAULT 0,
    quantity_reserved NUMERIC(12,3) NOT NULL DEFAULT 0,
    reorder_point NUMERIC(12,3) NOT NULL DEFAULT 0,
    max_stock NUMERIC(12,3) NOT NULL DEFAULT 0,
    last_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    UNIQUE(tenant_id, warehouse_id, sku_id)
);

CREATE TABLE IF NOT EXISTS stock_movements (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    warehouse_id VARCHAR(26) NOT NULL,
    sku_id VARCHAR(26) NOT NULL,
    movement_type VARCHAR(20) NOT NULL CHECK (movement_type IN ('in','out','adjustment','transfer')),
    quantity NUMERIC(12,3) NOT NULL,
    reference_id VARCHAR(26),
    reference_type VARCHAR(50),
    notes TEXT,
    moved_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    moved_by VARCHAR(26) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL
);

CREATE TABLE IF NOT EXISTS batches (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    warehouse_id VARCHAR(26) NOT NULL,
    sku_id VARCHAR(26) NOT NULL,
    batch_number VARCHAR(100) NOT NULL,
    quantity NUMERIC(12,3) NOT NULL,
    manufactured_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    status VARCHAR(20) NOT NULL DEFAULT 'available',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_inventory_tenant ON inventory_items(tenant_id);
CREATE INDEX IF NOT EXISTS idx_movements_tenant ON stock_movements(tenant_id);
CREATE INDEX IF NOT EXISTS idx_batches_expiry ON batches(tenant_id, expires_at) WHERE status='available';
