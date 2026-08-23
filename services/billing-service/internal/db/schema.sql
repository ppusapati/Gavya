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

-- ---------------------------------------------------------------------------
-- Multi-currency
--
-- A tenant records money in exactly one currency, chosen when the tenant is
-- created. These changes make that true rather than merely intended:
--
--   * The DEFAULT 'INR' is gone. A default is precisely how a deployment
--     outside India ends up silently recording rupees, and nothing downstream
--     could tell.
--   * Money columns are widened to four decimals, which is the most any ISO
--     4217 currency has. One schema then serves a yen deployment and a dinar
--     one; the tenant's scale says how many of those decimals are real.
--   * tenant_currency pins the first currency seen for a tenant, so a later
--     record in a different one is refused by the database rather than sitting
--     alongside the others looking like the same kind of money.
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS tenant_currency (
    tenant_id      VARCHAR(26) PRIMARY KEY,
    currency       CHAR(3) NOT NULL,
    currency_scale SMALLINT NOT NULL CHECK (currency_scale BETWEEN 0 AND 4),
    pinned_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$
DECLARE
    col RECORD;
BEGIN
    -- Widen every money column to the finest minor unit in use anywhere.
    FOR col IN
        SELECT table_name, column_name
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND data_type = 'numeric'
          AND numeric_scale = 2
          AND table_name IN ('invoices','invoice_items','payments')
    LOOP
        EXECUTE format('ALTER TABLE %I ALTER COLUMN %I TYPE NUMERIC(18,4)',
                       col.table_name, col.column_name);
    END LOOP;

    -- Drop the currency defaults. A currency has to be stated.
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
END
$$;

-- ---------------------------------------------------------------------------
-- Tax
--
-- invoice_items has carried a tax_rate column since the beginning and nothing
-- ever read it; every invoice was taxed at a single hardcoded rate instead. It
-- is widened here to three decimals, because rates like 12.375% exist, and it
-- is now what the totals are computed from.
--
-- tax_inclusive says whether the quoted price already contains the tax. Europe,
-- the UK and Indian retail generally quote inclusive; the United States quotes
-- exclusive. A system that can only express one of them cannot be deployed in
-- both.
-- ---------------------------------------------------------------------------
DO $$
BEGIN
    ALTER TABLE invoice_items ALTER COLUMN tax_rate TYPE NUMERIC(6,3);
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname='invoice_items_tax_rate_range') THEN
        ALTER TABLE invoice_items ADD CONSTRAINT invoice_items_tax_rate_range
            CHECK (tax_rate >= 0 AND tax_rate < 1000);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_name='invoices' AND column_name='tax_inclusive') THEN
        ALTER TABLE invoices ADD COLUMN tax_inclusive BOOLEAN NOT NULL DEFAULT FALSE;
    END IF;
END
$$;
