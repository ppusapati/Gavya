-- Buying milk from producers: what it is worth, and what was paid for it.
--
-- This is the ledger a society's relationship with its members rests on. A
-- farmer brings milk twice a day for a fortnight and is paid a number at the end
-- of it, and everything here exists so that number can be explained, recomputed,
-- and defended a year later.

-- ---------------------------------------------------------------------------
-- Rate cards
-- ---------------------------------------------------------------------------

-- The policy that turns milk into money.
--
-- Four of these columns record decisions the platform refuses to make on a
-- society's behalf, because each is a few paise on every collection every day
-- and all four are invisible once made:
--
--   basis           whether the rate is per litre or per kilogram, which differ
--                   by about three per cent
--   between_points  what a chart does with a reading between its rows
--   outside_chart   what it does with a reading off the chart entirely
--   rounding        which way a half-paisa goes
--
-- There is no DEFAULT on any of them. A card that has not decided is a card
-- that cannot price, and finding that out at deployment costs one conversation.
CREATE TABLE IF NOT EXISTS rate_cards (
    id            VARCHAR(26) PRIMARY KEY,
    tenant_id     VARCHAR(26) NOT NULL,
    name          VARCHAR(120) NOT NULL,

    kind          VARCHAR(16) NOT NULL CHECK (kind IN ('CHART', 'FORMULA')),
    currency      CHAR(3) NOT NULL,
    amount_scale  SMALLINT NOT NULL CHECK (amount_scale BETWEEN 0 AND 9),

    -- Only a chart has these. A formula prices per kilogram of each component
    -- and has no single basis, no points to fall between, and no edges.
    basis           VARCHAR(16) CHECK (basis IN ('PER_LITRE', 'PER_KG')),
    between_points  VARCHAR(16) CHECK (between_points IN ('BAND', 'INTERPOLATE', 'EXACT_ONLY')),
    outside_chart   VARCHAR(16) CHECK (outside_chart IN ('REFUSE', 'CLAMP')),

    rounding      VARCHAR(16) NOT NULL
                  CHECK (rounding IN ('HALF_UP', 'HALF_EVEN', 'DOWN', 'UP', 'HALF_DOWN')),

    CONSTRAINT rate_cards_chart_is_complete CHECK (
        kind <> 'CHART' OR
        (basis IS NOT NULL AND between_points IS NOT NULL AND outside_chart IS NOT NULL)
    ),

    -- When this card prices milk. A settlement recomputed a year later has to
    -- reach for the card that was on the wall then, not the one there now.
    valid_from    TIMESTAMPTZ NOT NULL,
    valid_to      TIMESTAMPTZ,
    CONSTRAINT rate_cards_period_is_forwards CHECK (valid_to IS NULL OR valid_to > valid_from),

    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by    VARCHAR(26) NOT NULL,
    updated_by    VARCHAR(26) NOT NULL,
    deleted_at    TIMESTAMPTZ
);

-- Two cards in force at the same moment is not a conflict to resolve at pricing
-- time, it is a conflict to prevent. Resolved later — by taking the newest, say
-- — the choice is invisible and the society finds out when a producer compares
-- two statements.
--
-- The range is half-open so a card ending on the first of March and one starting
-- there do not overlap, which is how a society actually replaces a card.
CREATE EXTENSION IF NOT EXISTS btree_gist;
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'rate_cards_one_at_a_time') THEN
        ALTER TABLE rate_cards ADD CONSTRAINT rate_cards_one_at_a_time
            EXCLUDE USING gist (
                tenant_id WITH =,
                tstzrange(valid_from, valid_to, '[)') WITH &&
            ) WHERE (deleted_at IS NULL);
    END IF;
END
$$;

-- The card's own composite key, so a cell, a term or a priced collection cannot
-- point at another tenant's card. A single-column reference would allow it: a
-- foreign key is checked by the system rather than by the querying role, so the
-- isolation policies do not stop one, and the consequence here is milk priced
-- from somebody else's chart.
--
-- Declared before the tables that reference it, because a composite reference
-- needs its target key to exist already.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'rate_cards_tenant_id_key') THEN
        ALTER TABLE rate_cards ADD CONSTRAINT rate_cards_tenant_id_key UNIQUE (tenant_id, id);
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS rate_cards_in_force
    ON rate_cards (tenant_id, valid_from DESC) WHERE deleted_at IS NULL;

-- One cell of a chart: a fat reading, an SNF reading, and the rate where they
-- meet.
--
-- Readings are scaled integers rather than numerics with a fixed scale, because
-- a society's chart may be written to one decimal place and another's to two,
-- and rewriting one into the other's resolution changes which cell a reading
-- falls in.
CREATE TABLE IF NOT EXISTS rate_card_cells (
    id             VARCHAR(26) PRIMARY KEY,
    tenant_id      VARCHAR(26) NOT NULL,
    rate_card_id   VARCHAR(26) NOT NULL,

    fat_value      BIGINT NOT NULL,
    fat_scale      SMALLINT NOT NULL CHECK (fat_scale BETWEEN 0 AND 6),
    snf_value      BIGINT NOT NULL,
    snf_scale      SMALLINT NOT NULL CHECK (snf_scale BETWEEN 0 AND 6),

    rate_numerator BIGINT NOT NULL CHECK (rate_numerator >= 0),
    rate_scale     SMALLINT NOT NULL CHECK (rate_scale BETWEEN 0 AND 9),

    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by     VARCHAR(26) NOT NULL,

    FOREIGN KEY (tenant_id, rate_card_id) REFERENCES rate_cards (tenant_id, id),
    -- One rate where a row and a column meet. Two would mean the chart says two
    -- things and pricing would depend on which row was read first.
    UNIQUE (tenant_id, rate_card_id, fat_value, fat_scale, snf_value, snf_scale)
);

CREATE INDEX IF NOT EXISTS rate_card_cells_by_card ON rate_card_cells (tenant_id, rate_card_id);

-- One term of a formula: a component and what a kilogram of it is worth.
CREATE TABLE IF NOT EXISTS rate_card_terms (
    id             VARCHAR(26) PRIMARY KEY,
    tenant_id      VARCHAR(26) NOT NULL,
    rate_card_id   VARCHAR(26) NOT NULL,

    component      VARCHAR(24) NOT NULL,
    rate_numerator BIGINT NOT NULL,
    rate_scale     SMALLINT NOT NULL CHECK (rate_scale BETWEEN 0 AND 9),

    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by     VARCHAR(26) NOT NULL,

    FOREIGN KEY (tenant_id, rate_card_id) REFERENCES rate_cards (tenant_id, id),
    UNIQUE (tenant_id, rate_card_id, component)
);

-- ---------------------------------------------------------------------------
-- Priced collections
-- ---------------------------------------------------------------------------

-- One delivery of milk, and what it came to.
--
-- The rate and the card are stored on the row rather than looked up when the
-- statement is printed. A card corrected next week must not silently change what
-- a producer was told they earned last week; if it should change, that is a
-- correction and leaves its own record.
CREATE TABLE IF NOT EXISTS priced_collections (
    id            VARCHAR(26) PRIMARY KEY,
    tenant_id     VARCHAR(26) NOT NULL,

    producer_ref  VARCHAR(64) NOT NULL,
    society_code  VARCHAR(64),

    collected_on  DATE NOT NULL,
    shift         VARCHAR(16) NOT NULL CHECK (shift IN ('MORNING', 'EVENING')),

    quantity_value BIGINT NOT NULL CHECK (quantity_value > 0),
    quantity_scale SMALLINT NOT NULL CHECK (quantity_scale BETWEEN 0 AND 6),
    quantity_unit  VARCHAR(16) NOT NULL CHECK (quantity_unit IN ('PER_LITRE', 'PER_KG')),

    fat_value     BIGINT,
    fat_scale     SMALLINT,
    snf_value     BIGINT,
    snf_scale     SMALLINT,

    -- What it was priced at, and by what.
    rate_card_id   VARCHAR(26) NOT NULL,
    rate_numerator BIGINT,
    rate_scale     SMALLINT,

    currency           CHAR(3) NOT NULL,
    amount_scale       SMALLINT NOT NULL CHECK (amount_scale BETWEEN 0 AND 9),
    amount_minor_units BIGINT NOT NULL CHECK (amount_minor_units >= 0),

    -- How the number was reached, in words. A producer disputing a payment is
    -- owed the reasoning, and reconstructing it later from a card that has since
    -- been replaced is not the same thing as having kept it.
    explanation   TEXT NOT NULL,

    -- Where the collection came from: entered here, or imported from a society's
    -- own system.
    origin_kind      VARCHAR(16) NOT NULL DEFAULT 'NATIVE'
                     CHECK (origin_kind IN ('NATIVE', 'IMPORTED')),
    source_system_id VARCHAR(64),
    import_batch_id  VARCHAR(64),
    source_record_id VARCHAR(128),

    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by    VARCHAR(26) NOT NULL,
    updated_by    VARCHAR(26) NOT NULL,
    deleted_at    TIMESTAMPTZ,

    FOREIGN KEY (tenant_id, rate_card_id) REFERENCES rate_cards (tenant_id, id)
);

-- A producer delivers once in the morning and once in the evening. The same
-- producer, day and shift arriving twice is the single commonest defect in
-- imported dairy data and it means somebody is paid twice for one delivery.
--
-- Enforced here rather than checked at import, because an import is not the only
-- way a row arrives and a check is only as good as the paths that run it.
CREATE UNIQUE INDEX IF NOT EXISTS priced_collections_one_per_shift
    ON priced_collections (tenant_id, producer_ref, collected_on, shift)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS priced_collections_by_producer
    ON priced_collections (tenant_id, producer_ref, collected_on DESC);
CREATE INDEX IF NOT EXISTS priced_collections_by_day
    ON priced_collections (tenant_id, collected_on DESC);
