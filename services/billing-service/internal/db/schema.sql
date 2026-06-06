CREATE TABLE IF NOT EXISTS invoices (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    customer_id VARCHAR(26) NOT NULL,
    invoice_number VARCHAR(50) NOT NULL,
    reference_id VARCHAR(26),
    reference_type VARCHAR(30),
    status VARCHAR(20) NOT NULL DEFAULT 'draft',
    sub_total NUMERIC(12,2) NOT NULL DEFAULT 0,
    tax_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    total_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    currency VARCHAR(3) NOT NULL DEFAULT 'INR',
    issued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    due_at TIMESTAMPTZ NOT NULL,
    paid_at TIMESTAMPTZ,
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ,
    UNIQUE(tenant_id, invoice_number)
);

CREATE TABLE IF NOT EXISTS invoice_items (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    invoice_id VARCHAR(26) NOT NULL REFERENCES invoices(id),
    description TEXT NOT NULL,
    quantity NUMERIC(10,3) NOT NULL,
    unit_price NUMERIC(12,2) NOT NULL,
    total_price NUMERIC(12,2) NOT NULL,
    tax_rate NUMERIC(5,2) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL
);

CREATE TABLE IF NOT EXISTS payments (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    invoice_id VARCHAR(26) NOT NULL REFERENCES invoices(id),
    amount NUMERIC(12,2) NOT NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'INR',
    payment_method VARCHAR(30) NOT NULL,
    reference_no VARCHAR(100),
    paid_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_invoices_tenant ON invoices(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_payments_invoice ON payments(invoice_id);
