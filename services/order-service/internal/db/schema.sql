CREATE TABLE IF NOT EXISTS orders (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    customer_id VARCHAR(26) NOT NULL,
    order_number VARCHAR(50) NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'draft',
    sub_total NUMERIC(12,2) NOT NULL DEFAULT 0,
    tax_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    total_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    currency VARCHAR(3) NOT NULL DEFAULT 'INR',
    shipping_address TEXT,
    notes TEXT,
    ordered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ,
    UNIQUE(tenant_id, order_number)
);

CREATE TABLE IF NOT EXISTS order_items (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    order_id VARCHAR(26) NOT NULL REFERENCES orders(id),
    sku_id VARCHAR(26) NOT NULL,
    product_id VARCHAR(26) NOT NULL,
    quantity NUMERIC(10,3) NOT NULL,
    unit_price NUMERIC(12,2) NOT NULL,
    total_price NUMERIC(12,2) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL
);

CREATE TABLE IF NOT EXISTS invoices (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    order_id VARCHAR(26) NOT NULL REFERENCES orders(id),
    invoice_number VARCHAR(50) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'draft',
    sub_total NUMERIC(12,2) NOT NULL,
    tax_amount NUMERIC(12,2) NOT NULL,
    total_amount NUMERIC(12,2) NOT NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'INR',
    issued_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    due_at TIMESTAMPTZ NOT NULL,
    paid_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS returns (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    order_id VARCHAR(26) NOT NULL REFERENCES orders(id),
    reason TEXT,
    status VARCHAR(20) NOT NULL DEFAULT 'requested',
    refund_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
    currency VARCHAR(3) NOT NULL DEFAULT 'INR',
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_orders_tenant ON orders(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_order_items_order ON order_items(order_id);

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
          -- Named explicitly rather than "every numeric column", because
          -- order_items.quantity is a quantity in litres or kilos and has
          -- nothing to do with a currency's minor unit.
          --
          -- This list said 'order_invoices', which is not a table in this
          -- schema — it is 'invoices' — and it omitted 'returns' altogether.
          -- So a deployment recording a three-decimal currency could place an
          -- order at 1.234 and have the invoice raised from it silently rounded
          -- to 1.23 by a NUMERIC(12,2) column, with the invoice then
          -- disagreeing with the order it came from and nothing saying why.
          -- The loop matched nothing for a name that does not exist and
          -- reported success, which is what a control that does nothing looks
          -- like. Found by listing the columns of a live database rather than
          -- reading this file.
          AND table_name IN ('orders','order_items','invoices','returns')
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

    -- The ceiling on money columns is gone, because the reason for it is.
    --
    -- These columns are NUMERIC(18,4) so one schema serves a yen deployment and
    -- a dinar one. This service used to read them into float64, and float64
    -- carries a four-decimal value exactly only up to about 10^11 — measured,
    -- not assumed: 200,000 values below 10^11 round-tripped through
    -- NUMERIC(18,4) -> float64 -> JSON -> float64 without losing a digit, and
    -- above 10^12 more than three quarters of them did.
    --
    -- So a CHECK refused what the code would have mangled. That was a limit of
    -- the Go read path written into the database, which made a fact about
    -- float64 look like a fact about money. The read path now takes these out
    -- as decimal literals and holds them in libs/integrity/money, which is
    -- exact to the full width of the column.
    --
    -- Named, not looped: information_schema.columns lists views as well as
    -- tables, and ALTER TABLE ... DROP CONSTRAINT on a view aborts everything
    -- after it in this file.
    ALTER TABLE orders      DROP CONSTRAINT IF EXISTS orders_sub_total_carriable;
    ALTER TABLE orders      DROP CONSTRAINT IF EXISTS orders_tax_amount_carriable;
    ALTER TABLE orders      DROP CONSTRAINT IF EXISTS orders_total_amount_carriable;
    ALTER TABLE order_items DROP CONSTRAINT IF EXISTS order_items_unit_price_carriable;
    ALTER TABLE order_items DROP CONSTRAINT IF EXISTS order_items_total_price_carriable;
    ALTER TABLE invoices    DROP CONSTRAINT IF EXISTS invoices_sub_total_carriable;
    ALTER TABLE invoices    DROP CONSTRAINT IF EXISTS invoices_tax_amount_carriable;
    ALTER TABLE invoices    DROP CONSTRAINT IF EXISTS invoices_total_amount_carriable;
    ALTER TABLE returns     DROP CONSTRAINT IF EXISTS returns_refund_amount_carriable;
END
$$;

-- ---------------------------------------------------------------------------
-- Tax
--
-- One rate applied to every line of every order cannot be right for a catalogue
-- that mixes rated and exempt goods, and it certainly cannot be right in two
-- countries at once. The rate belongs to the line.
--
-- tax_inclusive says which way round the quoted price works. Much of Europe,
-- the UK and Indian retail quote a price that already contains the tax; the
-- United States quotes one that does not. Both are ordinary, and a system that
-- can only do one of them can only be deployed in half the world.
-- ---------------------------------------------------------------------------
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_name='order_items' AND column_name='tax_rate') THEN
        ALTER TABLE order_items ADD COLUMN tax_rate NUMERIC(6,3) NOT NULL DEFAULT 0
            CHECK (tax_rate >= 0 AND tax_rate < 1000);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_name='orders' AND column_name='tax_inclusive') THEN
        ALTER TABLE orders ADD COLUMN tax_inclusive BOOLEAN NOT NULL DEFAULT FALSE;
    END IF;
END
$$;

-- ---------------------------------------------------------------------------
-- Tax backfill
--
-- Every order written before this change was taxed at a hardcoded 18%, and
-- the per-line rate did not exist or was ignored. Left alone, recomputing one
-- of those orders would now find every rate at zero and quietly restate a
-- figure a customer has already been given.
--
-- So the historical rate is written onto the lines that actually carried it:
-- those belonging to a order whose tax_amount is above zero, which is the
-- evidence that tax was charged. A order that was never taxed keeps its zero,
-- and anything created from now on states its own rate.
--
-- This runs once. The marker row is what stops a second run from overwriting
-- rates an operator has since corrected by hand.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS schema_migrations (
    name       TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM schema_migrations WHERE name = 'tax_rate_backfill_18pc') THEN
        UPDATE order_items i
        SET    tax_rate = 18.000
        FROM   orders d
        WHERE  d.id = i.order_id
          AND  d.tax_amount > 0
          AND  i.tax_rate = 0;

        INSERT INTO schema_migrations (name) VALUES ('tax_rate_backfill_18pc');
    END IF;
END
$$;

-- ---------------------------------------------------------------------------
-- Returns
--
-- The table and domain.Return existed from the beginning with no code path at
-- all — no repository method, no service method, no endpoint. Refunds did not
-- exist, and a table called `returns` made it look as though they did, which is
-- worse than their absence would have been.
--
-- What is wired now is the state machine the type already declared in a comment:
-- requested, then approved or rejected, then completed. Nothing beyond it was
-- invented. In particular, completing a return does not touch the invoice or the
-- order's totals: what that should do to a customer's account is an accounting
-- decision nobody here has made, and guessing it would put a model in place that
-- somebody works around forever.
-- ---------------------------------------------------------------------------
ALTER TABLE returns DROP CONSTRAINT IF EXISTS returns_status_is_known;
ALTER TABLE returns ADD CONSTRAINT returns_status_is_known
    CHECK (status IN ('requested','approved','rejected','completed'));

-- A refund cannot exceed what was charged.
--
-- This is arithmetic rather than policy — refunding more than was taken is wrong
-- under any refund policy — so it belongs in the database, where no code path can
-- route around it. Only approved and completed returns count against the total; a
-- request is not money until somebody agrees to it, and refusing to record an
-- optimistic request would lose the fact that it was made.
CREATE OR REPLACE FUNCTION gavya_return_does_not_exceed_the_order()
RETURNS TRIGGER AS $fn$
DECLARE
    committed NUMERIC;
    charged   NUMERIC;
BEGIN
    IF NEW.status NOT IN ('approved','completed') THEN
        RETURN NEW;
    END IF;

    -- The lock comes first, and the order of these two statements is the whole
    -- of the concurrency argument.
    --
    -- FOR UPDATE on the order serialises approvals against it, so the second
    -- transaction waits for the first. A sum taken before that wait is a sum of
    -- the world as it was before the other approval existed, and using it
    -- afterwards is the same as not having waited. Written the other way round
    -- this let two concurrent approvals of 60 both through against an order of
    -- 100 — measured, not reasoned about, and the reason these two lines are in
    -- this order rather than the one that reads more naturally.
    SELECT total_amount INTO charged
      FROM orders WHERE id = NEW.order_id AND tenant_id = NEW.tenant_id
      FOR UPDATE;
    IF charged IS NULL THEN
        RAISE EXCEPTION 'return % is against order %, which does not exist',
            NEW.id, NEW.order_id;
    END IF;

    SELECT COALESCE(SUM(refund_amount), 0) INTO committed
      FROM returns
     WHERE order_id = NEW.order_id AND tenant_id = NEW.tenant_id
       AND id <> NEW.id AND deleted_at IS NULL
       AND status IN ('approved','completed');

    IF committed + NEW.refund_amount > charged THEN
        RAISE EXCEPTION 'refunding % on order % would take the total refunded to %, and only % was charged',
            NEW.refund_amount, NEW.order_id, committed + NEW.refund_amount, charged
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$fn$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS gavya_return_does_not_exceed_the_order ON returns;
CREATE TRIGGER gavya_return_does_not_exceed_the_order
    BEFORE INSERT OR UPDATE ON returns
    FOR EACH ROW EXECUTE FUNCTION gavya_return_does_not_exceed_the_order();

CREATE INDEX IF NOT EXISTS idx_returns_order ON returns(tenant_id, order_id);
