-- Observations: the measured facts every settlement is computed from, the
-- instruments that produced them, and the legal-metrology certificates that say
-- whether those instruments were fit to determine a payment.
--
-- Nothing here is ever updated in place except to mark a row superseded and to
-- attach the two advisory estimates once. A correction arrives as a new row, so
-- a payment can always be re-justified against the number as it stood when it
-- was made.

CREATE TABLE IF NOT EXISTS instruments (
    id          VARCHAR(26) PRIMARY KEY,
    tenant_id   VARCHAR(26) NOT NULL,
    serial      VARCHAR(128) NOT NULL,
    kind        VARCHAR(32) NOT NULL CHECK (kind IN (
                    'WEIGHBRIDGE','MILK_ANALYSER','PLATFORM_SCALE',
                    'FLOW_METER','THERMOMETER','MANUAL_ENTRY')),
    label       VARCHAR(256) NOT NULL DEFAULT '',
    make        VARCHAR(128) NOT NULL DEFAULT '',
    model       VARCHAR(128) NOT NULL DEFAULT '',

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(26) NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_instrument_serial
    ON instruments (tenant_id, serial);

CREATE TABLE IF NOT EXISTS verification_certificates (
    id                  VARCHAR(26) PRIMARY KEY,
    tenant_id           VARCHAR(26) NOT NULL,
    instrument_id       VARCHAR(26) NOT NULL REFERENCES instruments(id),

    -- Stored exactly as the source stated them, blanks included. A certificate
    -- imported from an incumbent system with no authority or number recorded is
    -- what makes the eligibility verdict UNKNOWN; refusing the row here would
    -- instead make it look as though no certificate ever existed.
    certificate_number  VARCHAR(128) NOT NULL DEFAULT '',
    verifying_authority VARCHAR(256) NOT NULL DEFAULT '',
    issued_at           TIMESTAMPTZ NOT NULL,
    expires_at          TIMESTAMPTZ NOT NULL,

    origin_kind         VARCHAR(16) NOT NULL DEFAULT 'NATIVE'
                        CHECK (origin_kind IN ('NATIVE','IMPORTED','DERIVED')),
    source_system_id    VARCHAR(26),
    import_batch_id     VARCHAR(26),
    source_record_id    VARCHAR(256),
    source_payload_hash VARCHAR(80),
    derivation_id       VARCHAR(26),

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by          VARCHAR(26) NOT NULL,

    CONSTRAINT certificate_origin_is_complete CHECK (
        origin_kind = 'NATIVE' OR
        (origin_kind = 'IMPORTED' AND source_system_id IS NOT NULL AND import_batch_id IS NOT NULL
            AND source_record_id IS NOT NULL AND source_payload_hash IS NOT NULL) OR
        (origin_kind = 'DERIVED' AND derivation_id IS NOT NULL))
);

-- One row per stamping. The authority is part of the key because certificate
-- numbers are only unique within the state that issued them.
CREATE UNIQUE INDEX IF NOT EXISTS uq_certificate_number
    ON verification_certificates (tenant_id, verifying_authority, certificate_number)
    WHERE certificate_number <> '';

-- Serves the lookup on the recording path: the certificate governing one
-- instrument at one instant.
CREATE INDEX IF NOT EXISTS idx_certificate_instrument_window
    ON verification_certificates (tenant_id, instrument_id, issued_at DESC, expires_at DESC);

CREATE TABLE IF NOT EXISTS observations (
    id                  VARCHAR(26) PRIMARY KEY,
    tenant_id           VARCHAR(26) NOT NULL,

    -- One typed column per subject kind rather than a (subject_type,
    -- subject_id) pair. Each of these can carry a foreign key to the table that
    -- owns it and can be type checked by the database; a polymorphic pair can
    -- do neither, so a wrong type string would join to nothing and never be
    -- noticed. Exactly one is non-null, enforced below.
    cattle_id           VARCHAR(26),
    producer_id         VARCHAR(26),
    route_id            VARCHAR(26),
    tanker_id           VARCHAR(26),
    batch_id            VARCHAR(26),

    quantity_kind       VARCHAR(32) NOT NULL CHECK (quantity_kind IN (
                            'VOLUME_LITRES','MASS_KG','FAT_PERCENT','SNF_PERCENT',
                            'LACTOSE_PERCENT','PROTEIN_PERCENT','TEMPERATURE_C',
                            'SOMATIC_CELL_COUNT','ADULTERATION_INDEX')),
    value               NUMERIC(20,6) NOT NULL,
    unit                VARCHAR(16) NOT NULL,

    instrument_id       VARCHAR(26) REFERENCES instruments(id),
    -- The ingestion capture session the reading arrived in. A reference and not
    -- a foreign key: sessions belong to ingestion-service.
    session_ref         VARCHAR(128) NOT NULL DEFAULT '',
    observed_by         VARCHAR(64) NOT NULL DEFAULT '',

    -- Record origin. Unlike a settlement assertion an observation may be any of
    -- the three kinds: captured by our own devices, imported from an incumbent,
    -- or derived from other observations.
    origin_kind         VARCHAR(16) NOT NULL CHECK (origin_kind IN ('NATIVE','IMPORTED','DERIVED')),
    source_system_id    VARCHAR(26),
    import_batch_id     VARCHAR(26),
    source_record_id    VARCHAR(256),
    source_payload_hash VARCHAR(80),
    derivation_id       VARCHAR(26),

    -- Bitemporality: valid_from/valid_to is when the fact is true of the world;
    -- recorded_at/superseded_at is when the platform believed it.
    valid_from          TIMESTAMPTZ NOT NULL,
    valid_to            TIMESTAMPTZ NOT NULL DEFAULT '9999-12-31 23:59:59+00',
    recorded_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    superseded_at       TIMESTAMPTZ,
    superseded_by       VARCHAR(26),
    supersedes          VARCHAR(26) REFERENCES observations(id),

    -- The legal-metrology finding, stored rather than recomputed on read: the
    -- certificate register changes, and an auditor needs the verdict that was
    -- reached when the payment was made.
    eligibility_verdict VARCHAR(16) NOT NULL CHECK (eligibility_verdict IN (
                            'ELIGIBLE','NOT_ELIGIBLE','UNKNOWN')),
    eligibility_reason  TEXT NOT NULL,
    eligibility_certificate_id VARCHAR(26) REFERENCES verification_certificates(id),

    uncertainty_model_id      VARCHAR(64) NOT NULL DEFAULT '',
    -- True until an estimate is attached. It defaults true because an
    -- observation is recorded before the estimate is attempted: a settlement
    -- must never be blocked because a model was unreachable, and a null
    -- uncertainty must not be mistaken for a perfect measurement.
    uncertainty_missing       BOOLEAN NOT NULL DEFAULT TRUE,
    standard_uncertainty      NUMERIC(20,9),
    expanded_uncertainty      NUMERIC(20,9),
    coverage_factor           NUMERIC(8,4),
    coverage_probability      NUMERIC(6,5),
    uncertainty_model_version VARCHAR(64),
    uncertainty_estimated_at  TIMESTAMPTZ,

    -- Advisory. A flag marks an observation for review and never rejects it, so
    -- nothing in the settlement path reads these columns.
    anomaly_score         DOUBLE PRECISION,
    anomaly_flagged       BOOLEAN NOT NULL DEFAULT FALSE,
    anomaly_method        VARCHAR(64),
    anomaly_model_version VARCHAR(64),
    -- Null bounds mean the scorer established no baseline and the tolerance
    -- band is unbounded. Zero would read as an infinitely tight band.
    anomaly_lower_bound   DOUBLE PRECISION,
    anomaly_upper_bound   DOUBLE PRECISION,
    anomaly_explanation   TEXT,
    anomaly_scored_at     TIMESTAMPTZ,

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by          VARCHAR(26) NOT NULL,

    CONSTRAINT exactly_one_subject CHECK (
        (cattle_id IS NOT NULL)::int + (producer_id IS NOT NULL)::int +
        (route_id IS NOT NULL)::int + (tanker_id IS NOT NULL)::int +
        (batch_id IS NOT NULL)::int = 1),

    CONSTRAINT valid_interval_ordered CHECK (valid_to > valid_from),

    CONSTRAINT supersession_is_attributed CHECK (
        (superseded_at IS NULL AND superseded_by IS NULL) OR
        (superseded_at IS NOT NULL AND superseded_by IS NOT NULL)),

    CONSTRAINT observation_origin_is_complete CHECK (
        origin_kind = 'NATIVE' OR
        (origin_kind = 'IMPORTED' AND source_system_id IS NOT NULL AND import_batch_id IS NOT NULL
            AND source_record_id IS NOT NULL AND source_payload_hash IS NOT NULL) OR
        (origin_kind = 'DERIVED' AND derivation_id IS NOT NULL)),

    -- A half-populated budget is worse than none: an expanded uncertainty
    -- without the coverage factor it was expanded by cannot be interpreted.
    CONSTRAINT uncertainty_is_all_or_nothing CHECK (
        (uncertainty_missing AND standard_uncertainty IS NULL AND expanded_uncertainty IS NULL
            AND coverage_factor IS NULL AND coverage_probability IS NULL) OR
        (NOT uncertainty_missing AND standard_uncertainty IS NOT NULL AND expanded_uncertainty IS NOT NULL
            AND coverage_factor IS NOT NULL AND coverage_probability IS NOT NULL)),

    CONSTRAINT anomaly_flag_has_a_score CHECK (NOT anomaly_flagged OR anomaly_scored_at IS NOT NULL)
);

-- At most one live version of a subject's quantity at a given valid_from. A
-- correction supersedes the prior version before the new one lands, so this
-- index is what stops two contradictory live answers to the same question.
-- NULLS NOT DISTINCT is required because four of the five subject columns are
-- null in every row.
CREATE UNIQUE INDEX IF NOT EXISTS uq_observation_live_version
    ON observations (tenant_id, quantity_kind, valid_from,
                     cattle_id, producer_id, route_id, tanker_id, batch_id)
    NULLS NOT DISTINCT
    WHERE superseded_at IS NULL;

-- One index per subject kind, which is the second thing the typed columns buy:
-- each is dense, covers the valid-time ordering, and skips every row about a
-- different kind of subject.
CREATE INDEX IF NOT EXISTS idx_observation_cattle
    ON observations (tenant_id, cattle_id, quantity_kind, valid_from DESC)
    WHERE cattle_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_observation_producer
    ON observations (tenant_id, producer_id, quantity_kind, valid_from DESC)
    WHERE producer_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_observation_route
    ON observations (tenant_id, route_id, quantity_kind, valid_from DESC)
    WHERE route_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_observation_tanker
    ON observations (tenant_id, tanker_id, quantity_kind, valid_from DESC)
    WHERE tanker_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_observation_batch
    ON observations (tenant_id, batch_id, quantity_kind, valid_from DESC)
    WHERE batch_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_observation_session
    ON observations (tenant_id, session_ref, valid_from)
    WHERE session_ref <> '';

-- The review queue. A flag is cleared by correcting the observation, which
-- supersedes it, so the live flagged rows are exactly the outstanding work.
CREATE INDEX IF NOT EXISTS idx_observation_flagged
    ON observations (tenant_id, anomaly_score DESC, valid_from DESC)
    WHERE anomaly_flagged AND superseded_at IS NULL;

-- Drives the audit question the eligibility verdict exists to answer: which
-- payments rested on instruments that were not verified.
CREATE INDEX IF NOT EXISTS idx_observation_ineligible
    ON observations (tenant_id, eligibility_verdict, valid_from DESC)
    WHERE eligibility_verdict <> 'ELIGIBLE' AND superseded_at IS NULL;
