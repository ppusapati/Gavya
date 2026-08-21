-- Mass-balance reconciliation: closing a period for a route by finding the
-- smallest uncertainty-weighted set of adjustments that makes every interior
-- node balance, and recording which flow most likely carries a gross error.

CREATE TABLE IF NOT EXISTS balance_windows (
    id           VARCHAR(26) PRIMARY KEY,
    tenant_id    VARCHAR(26) NOT NULL,
    route_ref    VARCHAR(64) NOT NULL,

    period_start TIMESTAMPTZ NOT NULL,
    period_end   TIMESTAMPTZ NOT NULL,

    -- Litres and kilograms do not balance against each other, so the unit is a
    -- property of the whole window rather than of each measurement.
    unit         VARCHAR(8) NOT NULL CHECK (unit IN ('LITRES','KG')),
    status       VARCHAR(16) NOT NULL DEFAULT 'OPEN'
                 CHECK (status IN ('OPEN','RECONCILED','ACCEPTED')),

    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by   VARCHAR(26) NOT NULL,
    updated_by   VARCHAR(26) NOT NULL,

    CONSTRAINT window_period_ordered CHECK (period_end > period_start)
);

-- One window per route and period. Two would give the same milk two closes,
-- and a dispute could be argued from whichever one suited.
CREATE UNIQUE INDEX IF NOT EXISTS uq_window_period
    ON balance_windows (tenant_id, route_ref, period_start, period_end);

CREATE INDEX IF NOT EXISTS idx_windows_by_status
    ON balance_windows (tenant_id, status, period_start DESC);

CREATE TABLE IF NOT EXISTS flow_measurements (
    id                   VARCHAR(26) PRIMARY KEY,
    tenant_id            VARCHAR(26) NOT NULL,
    window_id            VARCHAR(26) NOT NULL REFERENCES balance_windows(id),
    flow_id              VARCHAR(64) NOT NULL,

    -- The empty node id is the system boundary: an external source or sink,
    -- which is not a point whose inflow has to equal its outflow. It carries no
    -- kind because it is not a place milk is held.
    from_node_id         VARCHAR(64) NOT NULL DEFAULT '',
    from_node_kind       VARCHAR(24) NOT NULL DEFAULT '',
    to_node_id           VARCHAR(64) NOT NULL DEFAULT '',
    to_node_kind         VARCHAR(24) NOT NULL DEFAULT '',

    -- Quantities are exact decimals. A settlement argued from these numbers has
    -- to reproduce them digit for digit, which binary floating point cannot
    -- promise.
    measured             NUMERIC(18,3) NOT NULL,
    standard_uncertainty NUMERIC(18,3),
    unmeasured           BOOLEAN NOT NULL DEFAULT FALSE,

    -- Ties the measurement back to the observation it was read from, so a
    -- nominated gross error can be traced to an instrument and a shift.
    observation_ref      VARCHAR(26) NOT NULL DEFAULT '',

    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by           VARCHAR(26) NOT NULL,

    CONSTRAINT from_node_kind_is_known CHECK (
        (from_node_id = '' AND from_node_kind = '') OR
        from_node_kind IN ('COLLECTION_CENTRE','TANKER','CHILLING_UNIT','PLANT')),
    CONSTRAINT to_node_kind_is_known CHECK (
        (to_node_id = '' AND to_node_kind = '') OR
        to_node_kind IN ('COLLECTION_CENTRE','TANKER','CHILLING_UNIT','PLANT')),

    -- A flow between the boundary and itself never enters the network, so it
    -- constrains nothing and only inflates the flow count.
    CONSTRAINT flow_touches_the_network CHECK (from_node_id <> '' OR to_node_id <> ''),
    -- A flow leaving and entering one node cancels in that node's balance,
    -- which would hide a real recirculation loss rather than measure it.
    CONSTRAINT flow_is_not_a_self_loop CHECK (from_node_id <> to_node_id),

    -- A zero or negative uncertainty weights the stream infinitely and lets one
    -- instrument dictate the whole solution.
    CONSTRAINT measured_flow_is_weighted CHECK (
        unmeasured OR (standard_uncertainty IS NOT NULL AND standard_uncertainty > 0))
);

-- The flow id is what the reconciler's answer is keyed by, so a repeat within
-- one window would make two streams share a single verdict.
CREATE UNIQUE INDEX IF NOT EXISTS uq_flow_in_window
    ON flow_measurements (tenant_id, window_id, flow_id);

CREATE INDEX IF NOT EXISTS idx_flows_by_observation
    ON flow_measurements (tenant_id, observation_ref)
    WHERE observation_ref <> '';

CREATE TABLE IF NOT EXISTS reconciliation_runs (
    id                    VARCHAR(26) PRIMARY KEY,
    tenant_id             VARCHAR(26) NOT NULL,
    window_id             VARCHAR(26) NOT NULL REFERENCES balance_windows(id),

    -- False when the network was under-determined, or when no model answered.
    -- Such runs are kept rather than discarded: a leg with no instrument
    -- anywhere around it is a real operational state, and the imbalance below
    -- is evidence whether or not anything converged.
    converged             BOOLEAN NOT NULL,

    -- Computed by this service from the measured values, so a window can still
    -- be reported as failing to close when the model tier is unreachable.
    residual_before       NUMERIC(18,3) NOT NULL CHECK (residual_before >= 0),
    -- Null when no model answered: there is no reconciled state to measure
    -- against, and repeating residual_before here would read as a closed window.
    residual_after        NUMERIC(18,3) CHECK (residual_after IS NULL OR residual_after >= 0),

    model_version         VARCHAR(64) NOT NULL DEFAULT '',
    gross_error_threshold DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (gross_error_threshold >= 0),
    reason                TEXT NOT NULL DEFAULT '',

    accepted_at           TIMESTAMPTZ,
    accepted_by           VARCHAR(26),

    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by            VARCHAR(26) NOT NULL,

    -- A reconciled state that names no model could not be reproduced later.
    CONSTRAINT model_answer_names_its_model CHECK (residual_after IS NULL OR model_version <> ''),
    -- A run with no model answer has to say why, or a window that was never
    -- reconciled is indistinguishable from one the reconciler declined.
    CONSTRAINT unanswered_run_states_why CHECK (residual_after IS NOT NULL OR reason <> ''),
    CONSTRAINT acceptance_is_attributed CHECK (
        (accepted_at IS NULL AND accepted_by IS NULL) OR
        (accepted_at IS NOT NULL AND accepted_by IS NOT NULL))
);

-- A period closes once. A second accepted run would leave two answers to what
-- the route delivered, with nothing to say which was paid against.
CREATE UNIQUE INDEX IF NOT EXISTS uq_accepted_run_per_window
    ON reconciliation_runs (tenant_id, window_id)
    WHERE accepted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_runs_by_window
    ON reconciliation_runs (tenant_id, window_id, created_at DESC);

CREATE TABLE IF NOT EXISTS reconciled_flows (
    run_id         VARCHAR(26) NOT NULL REFERENCES reconciliation_runs(id) ON DELETE CASCADE,
    tenant_id      VARCHAR(26) NOT NULL,
    flow_id        VARCHAR(64) NOT NULL,

    measured       NUMERIC(18,3) NOT NULL,
    reconciled     NUMERIC(18,3) NOT NULL,
    adjustment     NUMERIC(18,3) NOT NULL,

    -- How many of its own standard uncertainties the stream had to move. A
    -- ratio rather than a quantity, so it is not fixed-point.
    test_statistic DOUBLE PRECISION NOT NULL DEFAULT 0 CHECK (test_statistic >= 0),
    gross_error    BOOLEAN NOT NULL DEFAULT FALSE,
    -- Unmeasured legs are solved for rather than adjusted: their reconciled
    -- value is the network's answer for a stream nobody metered.
    unmeasured     BOOLEAN NOT NULL DEFAULT FALSE,

    -- One verdict per flow per run.
    PRIMARY KEY (run_id, flow_id),

    -- An unmeasured flow has no measurement to fail, so it can never be the
    -- stream that carries the gross error.
    CONSTRAINT unmeasured_flow_is_never_a_gross_error CHECK (NOT (gross_error AND unmeasured))
);

CREATE INDEX IF NOT EXISTS idx_gross_errors
    ON reconciled_flows (tenant_id, flow_id)
    WHERE gross_error;
