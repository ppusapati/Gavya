-- ============================================================================
-- gen_ulid() SQL polyfill — matches packages/ulid format
-- ============================================================================
-- Location: packages/database/schema/gen_ulid_polyfill.sql
-- Purpose : Provide a gen_ulid() function for DEFAULT clauses in migrations
--           when the third-party pg_ulid extension is unavailable.
--
-- The implementation matches Go's packages/ulid exactly:
--   - 26-char Crockford Base32 string
--   - Alphabet: 0123456789ABCDEFGHJKMNPQRSTVWXYZ  (no I, L, O, U)
--   - 48-bit millisecond Unix timestamp in the first 10 chars
--   - 80-bit cryptographic randomness in the last 16 chars
--
-- ULIDs produced here are bit-compatible with those produced by
-- packages/ulid.New().String() — parseable, sortable, and indistinguishable
-- at the column level.
--
-- Dependencies: pgcrypto (ships with every mainstream Postgres distribution,
-- including Postgres 18.3 on Windows; no third-party install needed).
-- ============================================================================

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ----------------------------------------------------------------------------
-- _ulid_crockford_encode(b bytea) — internal helper
-- ----------------------------------------------------------------------------
-- Encodes a 16-byte payload (128 bits) into a 26-character Crockford Base32
-- string. 26 * 5 = 130 bits; the top 2 bits of the first 5-bit group are
-- zero-padded — this matches the canonical ULID layout.
--
-- Implementation: walks the 128-bit integer packed from the 16 bytes and
-- extracts 5-bit groups from the most-significant end. Uses numeric (not
-- bit()) to avoid Postgres-version quirks with bit-string casts on bytea.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION _ulid_crockford_encode(b bytea)
RETURNS char(26)
LANGUAGE plpgsql
IMMUTABLE
PARALLEL SAFE
AS $$
DECLARE
    alphabet CONSTANT text := '0123456789ABCDEFGHJKMNPQRSTVWXYZ';
    hi      bigint;    -- top 64 bits of the payload
    lo      bigint;    -- bottom 64 bits of the payload (unsigned-interpreted)
    out     text := '';
    i       int;
    bitpos  int;
    byteidx int;
    bitshift int;
    nibble  int;
BEGIN
    IF length(b) <> 16 THEN
        RAISE EXCEPTION 'gen_ulid payload must be 16 bytes, got %', length(b);
    END IF;

    -- Walk 5 bits at a time from the most significant end of the 128-bit
    -- payload. Total positions: 0..127 spanning the 16-byte array; the
    -- top 2 bits of the 130-bit "word" are implicit zero (the first
    -- 5-bit group only reads 3 bits of real data from byte 0 and pads).
    FOR i IN 0..25 LOOP
        -- Bit offset from MSB of the 130-bit word; first group's offset
        -- would be -2 (the 2 zero-pad bits), so shift by +2.
        bitpos := i * 5 - 2;
        nibble := 0;
        -- For each of the 5 bits in this group
        FOR j IN 0..4 LOOP
            IF bitpos + j >= 0 AND bitpos + j < 128 THEN
                byteidx := (bitpos + j) / 8;
                bitshift := 7 - ((bitpos + j) % 8);
                nibble := nibble * 2 +
                          ((get_byte(b, byteidx) >> bitshift) & 1);
            ELSE
                nibble := nibble * 2; -- zero-pad bit
            END IF;
        END LOOP;
        out := out || substr(alphabet, nibble + 1, 1);
    END LOOP;
    RETURN out;
END;
$$;

-- ----------------------------------------------------------------------------
-- gen_ulid() — the DEFAULT-clause target
-- ----------------------------------------------------------------------------
-- Returns a fresh CHAR(26) ULID. Thread-safe; each call generates fresh
-- randomness via pgcrypto's gen_random_bytes().
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION gen_ulid()
RETURNS char(26)
LANGUAGE plpgsql
VOLATILE
PARALLEL RESTRICTED
AS $$
DECLARE
    ts_ms    bigint;
    ts_bytes bytea;
    rnd_bytes bytea;
    payload  bytea;
BEGIN
    -- 48-bit millisecond Unix timestamp, big-endian.
    ts_ms := (extract(epoch from clock_timestamp()) * 1000)::bigint;
    ts_bytes :=
        set_byte(set_byte(set_byte(set_byte(set_byte(set_byte(
            '\x000000000000'::bytea,
            0, ((ts_ms >> 40) & 255)::int),
            1, ((ts_ms >> 32) & 255)::int),
            2, ((ts_ms >> 24) & 255)::int),
            3, ((ts_ms >> 16) & 255)::int),
            4, ((ts_ms >>  8) & 255)::int),
            5, ( ts_ms        & 255)::int);

    -- 80-bit cryptographic randomness.
    rnd_bytes := gen_random_bytes(10);

    payload := ts_bytes || rnd_bytes;
    RETURN _ulid_crockford_encode(payload);
END;
$$;

-- ----------------------------------------------------------------------------
-- update_updated_at / update_updated_at_column — standard row-updated trigger
-- ----------------------------------------------------------------------------
-- Canonical "set updated_at = NOW() on every row UPDATE" trigger function.
-- Migrations + service-side triggers reference this under two names
-- (historical split); both are defined identically so existing DDL resolves
-- regardless of which convention a given migration/trigger file used.
-- Callers: `public.update_updated_at` (migrations 000009/000012/000013/000063)
-- and `public.update_updated_at_column` (all *db/schema/003_triggers.sql).
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION public.update_updated_at()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION public.update_updated_at_column()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$;

-- Third historical alias: 002/003 layout files in finance/inventory/purchase/
-- fulfillment/identity reference public.set_updated_at(). Same body.
CREATE OR REPLACE FUNCTION public.set_updated_at()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$;

-- ----------------------------------------------------------------------------
-- masters.items stub — referenced cross-schema by manufacturing/inventory/
-- finance migrations that need an FK target before the masters service has
-- shipped its own migration. The full masters service (when it ships) uses
-- CREATE TABLE IF NOT EXISTS, so the stub is a forward-compatible placeholder.
-- The original location was a stub buried in 000049 (manufacturing/shopfloor)
-- but earlier migrations 000045+ already need it. Promoting to bootstrap.
-- ----------------------------------------------------------------------------

CREATE SCHEMA IF NOT EXISTS masters;

-- Stubs use the column names downstream views/queries reference
-- (item_code, item_name, uom_code, account_code, account_name).
-- The original 000049 stub used `code`/`name` which broke the bom view.
CREATE TABLE IF NOT EXISTS masters.items (
    id              CHAR(26) PRIMARY KEY,
    tenant_id       CHAR(26) NOT NULL,
    company_id      CHAR(26) NOT NULL,
    branch_id       CHAR(26) NOT NULL,
    item_code       VARCHAR(50),
    item_name       VARCHAR(200),
    deleted_at      TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS masters.uoms (
    id              CHAR(26) PRIMARY KEY,
    tenant_id       CHAR(26) NOT NULL,
    uom_code        VARCHAR(20),
    uom_name        VARCHAR(100),
    deleted_at      TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS masters.chart_of_accounts (
    id              CHAR(26) PRIMARY KEY,
    tenant_id       CHAR(26) NOT NULL,
    company_id      CHAR(26) NOT NULL,
    code            VARCHAR(50),
    name            VARCHAR(200),
    deleted_at      TIMESTAMPTZ
);

-- Metadata tables consumed by insights (S3.T10/dataset) for schema discovery
-- across the multi-schema Postgres. Stubs only — full versions land with the
-- masters/metasearch services.
CREATE TABLE IF NOT EXISTS masters.schemas_metadata (
    id              CHAR(26) PRIMARY KEY,
    tenant_id       CHAR(26) NOT NULL,
    schema_name     VARCHAR(100),
    deleted_at      TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS masters.tables_metadata (
    id              CHAR(26) PRIMARY KEY,
    tenant_id       CHAR(26) NOT NULL,
    schema_id       CHAR(26),
    table_name      VARCHAR(100),
    deleted_at      TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS masters.columns_metadata (
    id              CHAR(26) PRIMARY KEY,
    tenant_id       CHAR(26) NOT NULL,
    table_id        CHAR(26),
    column_name     VARCHAR(100),
    deleted_at      TIMESTAMPTZ
);

-- ----------------------------------------------------------------------------
-- generate_ulid() — synonym for gen_ulid() to satisfy historical naming drift
-- ----------------------------------------------------------------------------
-- Two of the 72 migrations (000019_create_finance_ledger_schema.up.sql,
-- 000020_create_finance_journal_schema.up.sql) reference
-- `public.generate_ulid()` instead of the canonical `public.gen_ulid()`.
-- Rather than diverging two migration files for one alias, we expose
-- generate_ulid() as a thin call-through. Both names produce identical
-- output (Crockford Base32, 26 char) and are interchangeable.
-- ----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION public.generate_ulid()
RETURNS char(26)
LANGUAGE sql
VOLATILE
PARALLEL RESTRICTED
AS $$
    SELECT public.gen_ulid();
$$;

-- ----------------------------------------------------------------------------
-- Self-test — fail loudly if anything is miswired.
-- ----------------------------------------------------------------------------
DO $$
DECLARE
    u char(26);
BEGIN
    u := gen_ulid();
    IF length(u) <> 26 THEN
        RAISE EXCEPTION 'gen_ulid() returned wrong length: %', length(u);
    END IF;
    IF u !~ '^[0-9A-HJKMNP-TV-Z]{26}$' THEN
        RAISE EXCEPTION 'gen_ulid() returned non-Crockford chars: %', u;
    END IF;
END;
$$;
