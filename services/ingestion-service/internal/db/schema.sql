-- Transport idempotency: device generations, capture sessions, and the
-- sequence space that makes ingestion replayable.
--
-- The invariant this schema exists to enforce: redelivering a record any
-- number of times admits it exactly once, and a record whose identity is
-- ambiguous is held rather than dropped.

CREATE TABLE IF NOT EXISTS devices (
    id                 VARCHAR(26) PRIMARY KEY,
    tenant_id          VARCHAR(26) NOT NULL,
    serial             VARCHAR(128) NOT NULL,
    kind               VARCHAR(32) NOT NULL CHECK (kind IN (
                           'WEIGHBRIDGE','MILK_ANALYSER','PLATFORM_SCALE','MOBILE_APP','MANUAL_ENTRY')),
    label              VARCHAR(256) NOT NULL DEFAULT '',
    -- The epoch new records must arrive under. Rolled whenever the device's
    -- own sequence counter restarts.
    current_generation BIGINT NOT NULL DEFAULT 1 CHECK (current_generation >= 1),

    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by         VARCHAR(26) NOT NULL,
    updated_by         VARCHAR(26) NOT NULL,
    deleted_at         TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_device_serial
    ON devices (tenant_id, serial) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS device_generations (
    id          VARCHAR(26) PRIMARY KEY,
    tenant_id   VARCHAR(26) NOT NULL,
    device_id   VARCHAR(26) NOT NULL REFERENCES devices(id),
    generation  BIGINT NOT NULL CHECK (generation >= 1),
    reason      VARCHAR(32) NOT NULL CHECK (reason IN (
                    'INITIAL_PROVISIONING','FACTORY_RESET','FIRMWARE_REFLASH',
                    'APP_REINSTALL','CLOCK_RESET','SUSPECTED_TAMPERING','OPERATOR_REQUEST')),
    opened_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_at   TIMESTAMPTZ,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(26) NOT NULL,

    CONSTRAINT generation_closes_after_opening CHECK (closed_at IS NULL OR closed_at >= opened_at)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_device_generation
    ON device_generations (tenant_id, device_id, generation);
-- A device has at most one open generation: two would make "the current epoch"
-- ambiguous, and every admission rule depends on it being unambiguous.
CREATE UNIQUE INDEX IF NOT EXISTS uq_device_open_generation
    ON device_generations (tenant_id, device_id) WHERE closed_at IS NULL;

CREATE TABLE IF NOT EXISTS capture_sessions (
    id                  VARCHAR(26) PRIMARY KEY,
    tenant_id           VARCHAR(26) NOT NULL,
    device_id           VARCHAR(26) NOT NULL REFERENCES devices(id),
    generation          BIGINT NOT NULL CHECK (generation >= 1),
    -- Supplied by the device, unique only within a generation.
    external_session_id VARCHAR(128) NOT NULL,
    operator_ref        VARCHAR(64) NOT NULL DEFAULT '',

    status              VARCHAR(16) NOT NULL DEFAULT 'OPEN'
                        CHECK (status IN ('OPEN','CLOSED','ABANDONED','QUARANTINED')),
    opened_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_at           TIMESTAMPTZ,

    -- Highest sequence admitted so far. A record at or below this that is not
    -- a replay of a known record is a regression and is quarantined.
    last_sequence       BIGINT NOT NULL DEFAULT 0 CHECK (last_sequence >= 0),
    record_count        BIGINT NOT NULL DEFAULT 0 CHECK (record_count >= 0),

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by          VARCHAR(26) NOT NULL,
    updated_by          VARCHAR(26) NOT NULL,

    CONSTRAINT closed_session_has_a_close_time CHECK (status <> 'CLOSED' OR closed_at IS NOT NULL)
);

-- A session id reused under a new generation is a different session, so the
-- generation is part of the key.
CREATE UNIQUE INDEX IF NOT EXISTS uq_capture_session
    ON capture_sessions (tenant_id, device_id, generation, external_session_id);
CREATE INDEX IF NOT EXISTS idx_capture_session_open
    ON capture_sessions (tenant_id, status, opened_at) WHERE status = 'OPEN';

CREATE TABLE IF NOT EXISTS captured_records (
    id                  VARCHAR(26) PRIMARY KEY,
    tenant_id           VARCHAR(26) NOT NULL,
    device_id           VARCHAR(26) NOT NULL REFERENCES devices(id),
    generation          BIGINT NOT NULL CHECK (generation >= 1),
    session_id          VARCHAR(26) NOT NULL REFERENCES capture_sessions(id),
    external_session_id VARCHAR(128) NOT NULL,
    sequence            BIGINT NOT NULL CHECK (sequence >= 1),

    -- Distinguishes a benign replay from a conflict. Same slot, same hash is
    -- one record delivered twice; same slot, different hash is two records
    -- claiming one identity.
    payload_hash        VARCHAR(80) NOT NULL,
    payload             JSONB NOT NULL,

    -- The device's clock and the platform's. They differ by days when a device
    -- has been offline, and neither substitutes for the other.
    captured_at         TIMESTAMPTZ NOT NULL,
    received_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by          VARCHAR(26) NOT NULL
);

-- The idempotency key. This index is what makes redelivery safe: a second
-- insert for the same slot cannot succeed, so concurrent deliveries of the
-- same record resolve to one row without any application-level locking.
CREATE UNIQUE INDEX IF NOT EXISTS uq_captured_record_slot
    ON captured_records (tenant_id, device_id, generation, external_session_id, sequence);

CREATE INDEX IF NOT EXISTS idx_captured_record_session
    ON captured_records (tenant_id, session_id, sequence);
CREATE INDEX IF NOT EXISTS idx_captured_record_received
    ON captured_records (tenant_id, received_at DESC);

CREATE TABLE IF NOT EXISTS quarantined_records (
    id                    VARCHAR(26) PRIMARY KEY,
    tenant_id             VARCHAR(26) NOT NULL,
    reason                VARCHAR(40) NOT NULL CHECK (reason IN (
                              'TRANSPORT_IDENTITY_CONFLICT','SEQUENCE_REGRESSION',
                              'UNTRUSTED_SESSION_IDENTITY','STALE_GENERATION','SESSION_NOT_ACCEPTING')),
    detail                TEXT NOT NULL,

    device_id             VARCHAR(26) NOT NULL,
    generation            BIGINT NOT NULL,
    external_session_id   VARCHAR(128) NOT NULL,
    sequence              BIGINT NOT NULL,

    payload_hash          VARCHAR(80) NOT NULL,
    -- The payload is kept in full. Quarantine holds a record for a human; it
    -- never discards one, because dropping a producer's collection over a
    -- sequence-number dispute is the worst available outcome.
    payload               JSONB NOT NULL,
    conflicting_record_id VARCHAR(26) REFERENCES captured_records(id),

    captured_at           TIMESTAMPTZ NOT NULL,
    received_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    resolved_at           TIMESTAMPTZ,
    resolved_by           VARCHAR(26),
    resolution            TEXT,
    released_record_id    VARCHAR(26) REFERENCES captured_records(id),

    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by            VARCHAR(26) NOT NULL,

    CONSTRAINT resolution_is_attributed CHECK (
        (resolved_at IS NULL AND resolved_by IS NULL) OR
        (resolved_at IS NOT NULL AND resolved_by IS NOT NULL))
);

CREATE INDEX IF NOT EXISTS idx_quarantine_open
    ON quarantined_records (tenant_id, reason, received_at DESC) WHERE resolved_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_quarantine_device
    ON quarantined_records (tenant_id, device_id, generation);
