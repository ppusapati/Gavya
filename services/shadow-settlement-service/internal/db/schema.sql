-- Shadow settlement: what the incumbent system asserts, what this platform
-- independently computes, and every difference between the two.
--
-- Nothing here is ever updated in place except to mark a row superseded. A
-- correction arrives as a new version with a later recorded_at, which is what
-- lets a settlement be replayed as it was known on any past date.

CREATE TABLE IF NOT EXISTS external_settlement_assertions (
    id                     VARCHAR(26) PRIMARY KEY,
    tenant_id              VARCHAR(26) NOT NULL,
    source_system_id       VARCHAR(26) NOT NULL,
    external_settlement_id VARCHAR(128) NOT NULL,
    producer_ref           VARCHAR(64) NOT NULL,
    period_start           DATE NOT NULL,
    period_end             DATE NOT NULL,

    currency               CHAR(3) NOT NULL,
    amount_scale           SMALLINT NOT NULL CHECK (amount_scale BETWEEN 0 AND 9),
    total_minor_units      BIGINT NOT NULL,
    components             JSONB NOT NULL DEFAULT '[]'::jsonb,
    asserted_at            TIMESTAMPTZ NOT NULL,

    -- Record origin. An assertion is imported by definition, so every one of
    -- these is mandatory: without the payload hash a re-delivery of the same
    -- record cannot be told from a genuine amendment.
    origin_kind            VARCHAR(16) NOT NULL DEFAULT 'IMPORTED'
                           CHECK (origin_kind = 'IMPORTED'),
    import_batch_id        VARCHAR(26) NOT NULL,
    source_record_id       VARCHAR(256) NOT NULL,
    source_payload_hash    VARCHAR(80) NOT NULL,

    -- Bitemporality: valid_from/valid_to is when the assertion is true of the
    -- world; recorded_at/superseded_at is when the platform believed it.
    valid_from             TIMESTAMPTZ NOT NULL,
    valid_to               TIMESTAMPTZ NOT NULL DEFAULT '9999-12-31 23:59:59+00',
    recorded_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    superseded_at          TIMESTAMPTZ,
    superseded_by          VARCHAR(26),

    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by             VARCHAR(26) NOT NULL,

    CONSTRAINT valid_interval_ordered CHECK (valid_to >= valid_from)
);

-- Replay idempotency: the same source record with the same payload delivered
-- twice is one assertion, not two. A changed payload for the same source
-- record is a genuine amendment and is admitted as a new version.
CREATE UNIQUE INDEX IF NOT EXISTS uq_assertion_source_payload
    ON external_settlement_assertions (tenant_id, source_system_id, source_record_id, source_payload_hash);

-- At most one live version of a given external settlement at a time.
CREATE UNIQUE INDEX IF NOT EXISTS uq_assertion_current_version
    ON external_settlement_assertions (tenant_id, source_system_id, external_settlement_id)
    WHERE superseded_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_assertion_producer_period
    ON external_settlement_assertions (tenant_id, producer_ref, period_start, period_end);
CREATE INDEX IF NOT EXISTS idx_assertion_batch
    ON external_settlement_assertions (tenant_id, import_batch_id);

CREATE TABLE IF NOT EXISTS shadow_settlement_computations (
    id                  VARCHAR(26) PRIMARY KEY,
    tenant_id           VARCHAR(26) NOT NULL,
    assertion_id        VARCHAR(26) REFERENCES external_settlement_assertions(id),
    producer_ref        VARCHAR(64) NOT NULL,
    period_start        DATE NOT NULL,
    period_end          DATE NOT NULL,

    currency            CHAR(3) NOT NULL,
    amount_scale        SMALLINT NOT NULL CHECK (amount_scale BETWEEN 0 AND 9),
    total_minor_units   BIGINT NOT NULL,
    components          JSONB NOT NULL DEFAULT '[]'::jsonb,

    policy_version      VARCHAR(64) NOT NULL,
    rate_card_id        VARCHAR(26) NOT NULL,
    -- Every precision-losing step, in order, so a reviewer can reproduce the
    -- total by hand rather than take it on trust.
    rounding_trail      JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- Fixes which observations were consumed. A replay that yields a different
    -- total under the same digest is a defect; under a different digest it
    -- merely consumed different inputs.
    input_digest        VARCHAR(80) NOT NULL,
    -- The transaction time inputs were read at. Recomputing at the same as_of
    -- must reproduce the total exactly.
    as_of               TIMESTAMPTZ NOT NULL,

    origin_kind         VARCHAR(16) NOT NULL DEFAULT 'DERIVED'
                        CHECK (origin_kind = 'DERIVED'),
    derivation_id       VARCHAR(26) NOT NULL,

    computed_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    recorded_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    superseded_at       TIMESTAMPTZ,
    superseded_by       VARCHAR(26),

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by          VARCHAR(26) NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_shadow_current_version
    ON shadow_settlement_computations (tenant_id, producer_ref, period_start, period_end)
    WHERE superseded_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_shadow_assertion
    ON shadow_settlement_computations (tenant_id, assertion_id);
CREATE INDEX IF NOT EXISTS idx_shadow_producer_period
    ON shadow_settlement_computations (tenant_id, producer_ref, period_start, period_end);

CREATE TABLE IF NOT EXISTS settlement_divergences (
    id                VARCHAR(26) PRIMARY KEY,
    tenant_id         VARCHAR(26) NOT NULL,
    assertion_id      VARCHAR(26) NOT NULL REFERENCES external_settlement_assertions(id),
    computation_id    VARCHAR(26) NOT NULL REFERENCES shadow_settlement_computations(id),
    producer_ref      VARCHAR(64) NOT NULL,

    currency          CHAR(3) NOT NULL,
    amount_scale      SMALLINT NOT NULL CHECK (amount_scale BETWEEN 0 AND 9),
    delta_minor_units BIGINT NOT NULL,

    classification    VARCHAR(32) NOT NULL CHECK (classification IN (
                          'MATCH',
                          'ROUNDING_DIFFERENCE',
                          'INPUT_DIFFERENCE',
                          'POLICY_DIFFERENCE',
                          'RECOVERY_DIFFERENCE',
                          'UNEXPLAINED',
                          'INSUFFICIENT_EVIDENCE')),
    rationale         TEXT NOT NULL,
    evidence          JSONB NOT NULL DEFAULT '[]'::jsonb,

    -- Advisory only, and only ever populated for UNEXPLAINED. The column is
    -- deliberately separate from classification so a model can never be
    -- mistaken for the authoritative verdict.
    ml_hypotheses     JSONB NOT NULL DEFAULT '[]'::jsonb,

    status            VARCHAR(32) NOT NULL DEFAULT 'OPEN' CHECK (status IN (
                          'OPEN',
                          'UNDER_REVIEW',
                          'ACCEPTED',
                          'EXTERNAL_CONFIRMED',
                          'SHADOW_CONFIRMED',
                          'RESOLVED')),
    resolution        TEXT,
    resolved_at       TIMESTAMPTZ,
    resolved_by       VARCHAR(26),

    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by        VARCHAR(26) NOT NULL,
    updated_by        VARCHAR(26) NOT NULL,
    deleted_at        TIMESTAMPTZ,

    CONSTRAINT match_has_no_delta CHECK (classification <> 'MATCH' OR delta_minor_units = 0),
    CONSTRAINT resolution_is_attributed CHECK (
        (resolved_at IS NULL AND resolved_by IS NULL) OR
        (resolved_at IS NOT NULL AND resolved_by IS NOT NULL))
);

-- One divergence per assertion/computation pair.
CREATE UNIQUE INDEX IF NOT EXISTS uq_divergence_pair
    ON settlement_divergences (tenant_id, assertion_id, computation_id)
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_divergence_triage
    ON settlement_divergences (tenant_id, status, classification)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_divergence_producer
    ON settlement_divergences (tenant_id, producer_ref)
    WHERE deleted_at IS NULL;
-- Drives the integrity workspace's "largest unexplained exposure" view.
CREATE INDEX IF NOT EXISTS idx_divergence_magnitude
    ON settlement_divergences (tenant_id, abs(delta_minor_units) DESC)
    WHERE deleted_at IS NULL AND status = 'OPEN';
