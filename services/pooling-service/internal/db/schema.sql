-- Pooling: aggregating a marketing period's milk, valuing it by what it was
-- used for, and sharing that value out to the producers who delivered it.
--
-- Every amount is held as an integer count of minor units alongside its
-- currency and scale. A binary float cannot represent a rupee exactly and a
-- bare NUMERIC leaves the scale implicit, so two rows that look equal could
-- differ; the pool's defining invariant is that producers' totals sum to the
-- pool's value exactly, and that is only checkable in exact integers.

-- btree_gist lets an exclusion constraint mix equality on plain columns with
-- overlap on a range, which is what enforces the policy rule below.
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE IF NOT EXISTS pools (
    id             VARCHAR(26) PRIMARY KEY,
    tenant_id      VARCHAR(26) NOT NULL,
    name           VARCHAR(128) NOT NULL,
    period_start   TIMESTAMPTZ NOT NULL,
    period_end     TIMESTAMPTZ NOT NULL,
    unit           VARCHAR(16) NOT NULL,

    currency       CHAR(3) NOT NULL,
    amount_scale   SMALLINT NOT NULL CHECK (amount_scale BETWEEN 0 AND 9),

    -- The lifecycle is a vocabulary rather than free text because settlement
    -- reads it: a pool spelled 'valued' would never be settled, and a pool
    -- spelled 'settled' that was never valued would pay from nothing.
    status         VARCHAR(16) NOT NULL DEFAULT 'OPEN'
                   CHECK (status IN ('OPEN','VALUED','SETTLED','REOPENED')),

    -- Fix the rules the pool was valued under, so a revaluation can be told
    -- from a rule change.
    rate_card_id   VARCHAR(26) NOT NULL DEFAULT '',
    policy_version VARCHAR(32) NOT NULL DEFAULT '',

    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by     VARCHAR(26) NOT NULL,
    updated_by     VARCHAR(26) NOT NULL,

    CONSTRAINT pool_period_ordered CHECK (period_end > period_start)
);

-- Serves the retroactivity question: which pools closed inside the lookback
-- window a late correction may still reach.
CREATE INDEX IF NOT EXISTS idx_pools_by_period
    ON pools (tenant_id, period_end DESC, period_start DESC);

CREATE TABLE IF NOT EXISTS producer_milk (
    id                         VARCHAR(26) PRIMARY KEY,
    tenant_id                  VARCHAR(26) NOT NULL,
    pool_id                    VARCHAR(26) NOT NULL REFERENCES pools(id),
    producer_ref               VARCHAR(64) NOT NULL,

    -- Three decimals resolves a gram in a kilogram, which is finer than any
    -- dairy scale in the field and is the scale the domain weights shares at.
    quantity                   NUMERIC(18,3) NOT NULL CHECK (quantity >= 0),
    components                 JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- The authoritative collection slots this aggregate was built from, so a
    -- producer's pooled quantity can be traced back without re-deriving it.
    slot_refs                  TEXT[] NOT NULL DEFAULT '{}',

    origin_kind                VARCHAR(16) NOT NULL DEFAULT 'DERIVED'
                               CHECK (origin_kind IN ('NATIVE','IMPORTED','DERIVED')),
    origin_source_system_id    VARCHAR(26) NOT NULL DEFAULT '',
    origin_import_batch_id     VARCHAR(26) NOT NULL DEFAULT '',
    origin_source_record_id    VARCHAR(256) NOT NULL DEFAULT '',
    origin_source_payload_hash VARCHAR(80) NOT NULL DEFAULT '',
    origin_derivation_id       VARCHAR(26) NOT NULL DEFAULT '',

    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by                 VARCHAR(26) NOT NULL
);

-- A producer's pooled quantity is the weight their slice of the residual fund
-- is computed from. A second row for the same producer would double that
-- weight and take money from every other producer in the pool without anyone
-- seeing an error.
CREATE UNIQUE INDEX IF NOT EXISTS uq_producer_per_pool
    ON producer_milk (tenant_id, pool_id, producer_ref);

CREATE INDEX IF NOT EXISTS idx_producer_milk_by_pool
    ON producer_milk (tenant_id, pool_id, producer_ref);

CREATE TABLE IF NOT EXISTS classified_utilisations (
    id              VARCHAR(26) PRIMARY KEY,
    tenant_id       VARCHAR(26) NOT NULL,
    pool_id         VARCHAR(26) NOT NULL REFERENCES pools(id),

    -- The class vocabulary is closed because the blend price is only
    -- comparable between pools when every pool's milk was classified the same
    -- way; a spelling variant would silently create a fifth class that no rate
    -- card prices and that no reviewer would notice was missing.
    class           VARCHAR(16) NOT NULL CHECK (class IN (
                        'CLASS_I','CLASS_II','CLASS_III','CLASS_IV')),
    quantity        NUMERIC(18,3) NOT NULL CHECK (quantity >= 0),

    -- Price per unit of quantity, as an exact numerator over 10^scale.
    price_numerator BIGINT NOT NULL,
    price_scale     SMALLINT NOT NULL CHECK (price_scale BETWEEN 0 AND 9),

    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by      VARCHAR(26) NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_utilisations_by_pool
    ON classified_utilisations (tenant_id, pool_id, class);

CREATE TABLE IF NOT EXISTS pool_valuations (
    id                                   VARCHAR(26) PRIMARY KEY,
    tenant_id                            VARCHAR(26) NOT NULL,
    pool_id                              VARCHAR(26) NOT NULL REFERENCES pools(id),

    currency                             CHAR(3) NOT NULL,
    amount_scale                         SMALLINT NOT NULL CHECK (amount_scale BETWEEN 0 AND 9),
    classified_value_minor_units         BIGINT NOT NULL,
    component_value_minor_units          BIGINT NOT NULL,
    producer_settlement_fund_minor_units BIGINT NOT NULL,

    total_quantity                       NUMERIC(18,3) NOT NULL,
    blend_price_numerator                BIGINT NOT NULL,
    blend_price_scale                    SMALLINT NOT NULL CHECK (blend_price_scale BETWEEN 0 AND 9),

    -- Every precision-losing step of the computation, kept so a producer's
    -- payment can be re-derived years later without rerunning the code.
    rounding_trail                       JSONB NOT NULL DEFAULT '[]'::jsonb,

    origin_kind                          VARCHAR(16) NOT NULL DEFAULT 'DERIVED'
                                         CHECK (origin_kind = 'DERIVED'),
    origin_derivation_id                 VARCHAR(26) NOT NULL DEFAULT '',

    computed_at                          TIMESTAMPTZ NOT NULL,
    created_at                           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by                           VARCHAR(26) NOT NULL,
    superseded_at                        TIMESTAMPTZ,

    -- The fund is the residual by construction. Storing all three and checking
    -- the relation means a valuation whose parts disagree cannot reach the
    -- table at all, whatever wrote it.
    CONSTRAINT fund_is_the_residual CHECK (
        producer_settlement_fund_minor_units
            = classified_value_minor_units - component_value_minor_units)
);

-- A pool has one answer at a time. Two live valuations would leave settlement
-- free to pay producers from either, and nothing in the data would say which
-- was meant. Superseded valuations stay readable because a payment made under
-- one must remain explainable.
CREATE UNIQUE INDEX IF NOT EXISTS uq_live_valuation_per_pool
    ON pool_valuations (tenant_id, pool_id)
    WHERE superseded_at IS NULL;

CREATE TABLE IF NOT EXISTS allocations (
    id                          VARCHAR(26) PRIMARY KEY,
    tenant_id                   VARCHAR(26) NOT NULL,
    pool_id                     VARCHAR(26) NOT NULL REFERENCES pools(id),
    valuation_id                VARCHAR(26) NOT NULL REFERENCES pool_valuations(id),
    producer_ref                VARCHAR(64) NOT NULL,

    currency                    CHAR(3) NOT NULL,
    amount_scale                SMALLINT NOT NULL CHECK (amount_scale BETWEEN 0 AND 9),
    component_value_minor_units BIGINT NOT NULL,
    fund_share_minor_units      BIGINT NOT NULL,
    total_minor_units           BIGINT NOT NULL,

    -- The share basis, kept so the split can be checked by hand.
    weight                      BIGINT NOT NULL CHECK (weight >= 0),

    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by                  VARCHAR(26) NOT NULL,

    -- The total is what becomes payable, and the two parts are what explain it
    -- to the producer. If they can drift apart then the explanation on the
    -- payment advice is not the amount that was paid.
    CONSTRAINT allocation_total_is_its_parts CHECK (
        total_minor_units = component_value_minor_units + fund_share_minor_units)
);

CREATE INDEX IF NOT EXISTS idx_allocations_by_pool
    ON allocations (tenant_id, pool_id, valuation_id);
-- Serves a producer's own statement: every pool they were paid from.
CREATE INDEX IF NOT EXISTS idx_allocations_by_producer
    ON allocations (tenant_id, producer_ref, created_at DESC);

CREATE TABLE IF NOT EXISTS producer_economic_events (
    id                   VARCHAR(26) PRIMARY KEY,
    tenant_id            VARCHAR(26) NOT NULL,
    pool_id              VARCHAR(26) NOT NULL REFERENCES pools(id),
    allocation_id        VARCHAR(26) NOT NULL REFERENCES allocations(id),
    producer_ref         VARCHAR(64) NOT NULL,

    currency             CHAR(3) NOT NULL,
    amount_scale         SMALLINT NOT NULL CHECK (amount_scale BETWEEN 0 AND 9),
    amount_minor_units   BIGINT NOT NULL,

    kind                 VARCHAR(16) NOT NULL
                         CHECK (kind IN ('ORIGINAL','INCREMENTAL','RESTATEMENT')),
    supersedes_event_id  VARCHAR(26) REFERENCES producer_economic_events(id),

    origin_kind          VARCHAR(16) NOT NULL DEFAULT 'DERIVED'
                         CHECK (origin_kind = 'DERIVED'),
    origin_derivation_id VARCHAR(26) NOT NULL DEFAULT '',

    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by           VARCHAR(26) NOT NULL,

    -- A correction is only meaningful against something. An INCREMENTAL that
    -- names nothing is a difference from an unknown amount, and an ORIGINAL
    -- that names something is claiming to be both the first payable and a
    -- correction of an earlier one. Either would let a producer be paid twice
    -- with no row saying so.
    CONSTRAINT correction_names_what_it_supersedes CHECK (
        (kind = 'ORIGINAL' AND supersedes_event_id IS NULL) OR
        (kind IN ('INCREMENTAL','RESTATEMENT') AND supersedes_event_id IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS idx_events_by_producer
    ON producer_economic_events (tenant_id, producer_ref, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_events_by_pool
    ON producer_economic_events (tenant_id, pool_id, producer_ref);

CREATE TABLE IF NOT EXISTS recovery_retroactivity_policies (
    id                             VARCHAR(26) PRIMARY KEY,
    tenant_id                      VARCHAR(26) NOT NULL,
    name                           VARCHAR(128) NOT NULL,
    mode                           VARCHAR(24) NOT NULL CHECK (mode IN (
                                       'DO_NOT_REOPEN','RECALCULATE','APPLY_INCREMENTAL','CUSTOM')),
    -- Zero means settled pools are closed for good, whatever the mode says.
    max_lookback_days              INTEGER NOT NULL DEFAULT 0 CHECK (max_lookback_days >= 0),

    currency                       CHAR(3) NOT NULL,
    amount_scale                   SMALLINT NOT NULL CHECK (amount_scale BETWEEN 0 AND 9),
    minimum_adjustment_minor_units BIGINT NOT NULL DEFAULT 0
                                   CHECK (minimum_adjustment_minor_units >= 0),

    effective_from                 TIMESTAMPTZ NOT NULL,
    effective_to                   TIMESTAMPTZ,

    created_at                     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by                     VARCHAR(26) NOT NULL,

    CONSTRAINT retro_policy_window_ordered CHECK (
        effective_to IS NULL OR effective_to > effective_from)
);

-- At most one policy in force per tenant at a time. Two overlapping policies
-- would give a late correction two different answers about whether it may
-- reopen a settled pool, and the one that ran would be whichever the query
-- happened to return first.
ALTER TABLE recovery_retroactivity_policies
    DROP CONSTRAINT IF EXISTS one_retroactivity_policy_in_force;
ALTER TABLE recovery_retroactivity_policies
    ADD CONSTRAINT one_retroactivity_policy_in_force
    EXCLUDE USING gist (
        tenant_id WITH =,
        tstzrange(effective_from, COALESCE(effective_to, 'infinity'::timestamptz)) WITH &&
    );
