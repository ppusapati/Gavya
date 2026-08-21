-- Canonicalisation: mapping an incumbent system's identifiers onto platform
-- entities, and deciding which record is the authoritative collection for a
-- given producer, date and shift.

-- btree_gist lets an exclusion constraint mix equality on plain columns with
-- overlap on a range, which is what enforces the identity rule below.
CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE IF NOT EXISTS external_identities (
    id               VARCHAR(26) PRIMARY KEY,
    tenant_id        VARCHAR(26) NOT NULL,
    source_system_id VARCHAR(26) NOT NULL,
    entity_kind      VARCHAR(16) NOT NULL CHECK (entity_kind IN (
                         'PRODUCER','CATTLE','ROUTE','CENTRE','DEVICE','SETTLEMENT')),
    external_id      VARCHAR(256) NOT NULL,
    entity_id        VARCHAR(26) NOT NULL,

    method           VARCHAR(16) NOT NULL CHECK (method IN ('EXACT','MANUAL','INFERRED')),
    -- Meaningful only for INFERRED mappings; an exact or manual mapping is not
    -- a probability.
    confidence       NUMERIC(4,3) CHECK (confidence IS NULL OR (confidence >= 0 AND confidence <= 1)),
    note             TEXT NOT NULL DEFAULT '',

    valid_from       TIMESTAMPTZ NOT NULL,
    valid_to         TIMESTAMPTZ NOT NULL DEFAULT '9999-12-31 23:59:59+00',

    recorded_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    superseded_at    TIMESTAMPTZ,
    superseded_by    VARCHAR(26),

    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by       VARCHAR(26) NOT NULL,

    CONSTRAINT identity_interval_ordered CHECK (valid_to > valid_from),
    CONSTRAINT inferred_mappings_state_confidence CHECK (method <> 'INFERRED' OR confidence IS NOT NULL)
);

-- One external identifier resolves to at most one entity at any instant.
--
-- Not a plain unique index: external identifiers are reused, so a cooperative
-- may legitimately retire member number P-001 and reissue it years later. What
-- must never happen is two mappings for the same identifier whose valid
-- intervals overlap, because then a record from that period resolves to two
-- different producers and there is no way to know which was paid.
ALTER TABLE external_identities DROP CONSTRAINT IF EXISTS no_overlapping_identity;
ALTER TABLE external_identities ADD CONSTRAINT no_overlapping_identity
    EXCLUDE USING gist (
        tenant_id WITH =,
        source_system_id WITH =,
        entity_kind WITH =,
        external_id WITH =,
        tstzrange(valid_from, valid_to) WITH &&
    ) WHERE (superseded_at IS NULL);

CREATE INDEX IF NOT EXISTS idx_identity_lookup
    ON external_identities (tenant_id, source_system_id, entity_kind, external_id)
    WHERE superseded_at IS NULL;
-- Serves the reverse question: which external identifiers does this entity
-- carry, which is what the data-mapping workspace shows.
CREATE INDEX IF NOT EXISTS idx_identity_reverse
    ON external_identities (tenant_id, entity_kind, entity_id)
    WHERE superseded_at IS NULL;

CREATE TABLE IF NOT EXISTS collection_identity_policies (
    id             VARCHAR(26) PRIMARY KEY,
    tenant_id      VARCHAR(26) NOT NULL,
    name           VARCHAR(128) NOT NULL,
    -- Ordered, and the order is part of the policy's identity: the slot key is
    -- built from it, so reordering yields different keys and must be a new
    -- version rather than an edit.
    dimensions     TEXT[] NOT NULL CHECK (array_length(dimensions, 1) >= 1),
    resolution     VARCHAR(24) NOT NULL CHECK (resolution IN (
                       'FIRST_WINS','LAST_WINS','HIGHEST_QUALITY','MANUAL')),
    version        INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),

    effective_from TIMESTAMPTZ NOT NULL,
    effective_to   TIMESTAMPTZ,

    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by     VARCHAR(26) NOT NULL,

    CONSTRAINT policy_window_ordered CHECK (effective_to IS NULL OR effective_to > effective_from),
    -- Every other dimension qualifies whose milk it was, so a policy without a
    -- producer cannot identify a collection at all.
    CONSTRAINT policy_identifies_a_producer CHECK ('PRODUCER' = ANY (dimensions))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_policy_version
    ON collection_identity_policies (tenant_id, name, version);

-- At most one policy in force at a time: two would make the slot key for a
-- collection ambiguous.
ALTER TABLE collection_identity_policies DROP CONSTRAINT IF EXISTS one_policy_in_force;
ALTER TABLE collection_identity_policies ADD CONSTRAINT one_policy_in_force
    EXCLUDE USING gist (
        tenant_id WITH =,
        tstzrange(effective_from, COALESCE(effective_to, 'infinity'::timestamptz)) WITH &&
    );

CREATE TABLE IF NOT EXISTS authoritative_collection_slots (
    id                    VARCHAR(26) PRIMARY KEY,
    tenant_id             VARCHAR(26) NOT NULL,
    slot_key              VARCHAR(80) NOT NULL,
    -- Part of the slot's identity. In shadow mode the platform holds an
    -- imported collection and its own recomputed one side by side; they are not
    -- competing claims, and forcing them into one slot would report the entire
    -- import as a conflict. Comparing them is the shadow settlement's job.
    origin_kind           VARCHAR(16) NOT NULL CHECK (origin_kind IN ('NATIVE','IMPORTED','DERIVED')),

    policy_id             VARCHAR(26) NOT NULL REFERENCES collection_identity_policies(id),
    policy_version        INTEGER NOT NULL,

    -- Empty while the slot is in conflict, because a conflicted slot has no
    -- authoritative answer.
    authoritative_ref     VARCHAR(64) NOT NULL DEFAULT '',
    status                VARCHAR(16) NOT NULL DEFAULT 'SETTLED'
                          CHECK (status IN ('SETTLED','CONFLICT')),

    -- The holding claim's own attributes, so a later claim can be compared
    -- against it without reloading the record it came from.
    incumbent_recorded_at TIMESTAMPTZ NOT NULL,
    incumbent_quality     INTEGER NOT NULL DEFAULT 0,

    -- Claims that did not win, kept so a reviewer can see what was set aside
    -- and on what grounds.
    contenders            JSONB NOT NULL DEFAULT '[]'::jsonb,
    values                JSONB NOT NULL DEFAULT '{}'::jsonb,

    resolved_at           TIMESTAMPTZ,
    resolved_by           VARCHAR(26),
    resolution            TEXT,

    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by            VARCHAR(26) NOT NULL,
    updated_by            VARCHAR(26) NOT NULL,

    CONSTRAINT settled_slot_has_a_holder CHECK (status <> 'SETTLED' OR authoritative_ref <> ''),
    CONSTRAINT conflicted_slot_has_no_holder CHECK (status <> 'CONFLICT' OR authoritative_ref = ''),
    CONSTRAINT resolution_is_attributed CHECK (
        (resolved_at IS NULL AND resolved_by IS NULL) OR
        (resolved_at IS NOT NULL AND resolved_by IS NOT NULL))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_slot_key
    ON authoritative_collection_slots (tenant_id, slot_key, origin_kind);

-- Drives the integrity workspace's conflict queue.
CREATE INDEX IF NOT EXISTS idx_slot_conflicts
    ON authoritative_collection_slots (tenant_id, updated_at DESC)
    WHERE status = 'CONFLICT' AND resolved_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_slot_by_policy
    ON authoritative_collection_slots (tenant_id, policy_id, policy_version);
