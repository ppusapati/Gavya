-- Batch genealogy: what a batch was made from, and what was made from it.
--
-- The question this exists to answer is a recall. A tanker of milk turns out to
-- have been contaminated; which cartons of paneer contain it, and where did they
-- go. Or the other way: a carton is returned; what went into it and what else
-- came out of the same silo.
--
-- Both answers have to be complete. A partial list of the cartons containing a
-- contaminated lot is worse than no list, because it is acted on: the ones named
-- are recalled and the ones missed stay on shelves with somebody's confidence
-- behind them.
--
-- Everything here is a batch, including raw milk. A tanker's delivery into a
-- silo is a batch of raw milk with a reference back to the movement it came
-- from. Modelling raw milk as something other than a batch would mean the
-- genealogy stops at the plant gate, and the plant gate is exactly where a
-- recall needs to keep going.

-- ---------------------------------------------------------------------------
-- Batches
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS production_batches (
    id          VARCHAR(26) PRIMARY KEY,
    tenant_id   VARCHAR(26) NOT NULL,

    -- What is written on the vessel or the carton. A person reads this.
    batch_code  VARCHAR(64) NOT NULL,

    kind        VARCHAR(16) NOT NULL CHECK (kind IN ('RAW', 'INTERMEDIATE', 'FINISHED')),

    -- What it is, in the plant's own words: RAW_MILK, CREAM, PANEER, GHEE,
    -- SMP. Free text on purpose. A closed set here would be this platform
    -- deciding what a dairy is allowed to make, and the list would be wrong for
    -- the first plant that makes something regional.
    product_ref VARCHAR(64) NOT NULL,

    produced_value BIGINT NOT NULL CHECK (produced_value > 0),
    produced_unit  VARCHAR(16) NOT NULL CHECK (produced_unit IN ('LITRES', 'KILOGRAMS')),

    produced_at TIMESTAMPTZ NOT NULL,
    produced_by VARCHAR(64) NOT NULL,

    -- Raw milk says where it came from: a movement that arrived, or a node it
    -- was drawn from. Anything else is made from other batches and says so
    -- through production_inputs.
    -- source_ref points out of this service: at a movement or a node, both in
    -- material-service, chosen by source_kind beside it. No foreign key names
    -- two tables, and the decision not to constrain it is recorded in
    -- libs/integrity/isolation/references.sql rather than left to be inferred.
    source_kind VARCHAR(16) CHECK (source_kind IS NULL OR source_kind IN ('MOVEMENT', 'NODE')),
    source_ref  VARCHAR(64),
    CONSTRAINT production_batches_raw_says_where_from CHECK (
        (kind = 'RAW') = (source_kind IS NOT NULL AND source_ref IS NOT NULL)),

    -- What the plant expected this process to yield, in parts per million of the
    -- input it consumed. One kilogram of paneer from six of milk is 166667.
    --
    -- Optional, and there is no table of standard yields in this platform. A
    -- society's paneer yield is a property of its milk, its process and its
    -- equipment; a figure invented here would be a number nobody measured
    -- sitting in a variance report that somebody is asked to explain. Where a
    -- plant supplies one, the variance against it is reported; where none is
    -- supplied, the observed yield is reported and the report says no
    -- expectation was declared.
    --
    -- It sits on the batch rather than in a formulation registry because there
    -- is not one yet. That is where it belongs and where it should move.
    expected_yield_ppm BIGINT CHECK (expected_yield_ppm IS NULL OR expected_yield_ppm > 0),

    status      VARCHAR(16) NOT NULL CHECK (status IN (
                    'OPEN', 'RELEASED', 'QUARANTINED', 'RECALLED', 'DISPOSED')),
    -- Why a batch was held back or called in. A status alone does not carry a
    -- reason and a recall without one cannot be explained to whoever receives it.
    status_reason TEXT,
    CONSTRAINT production_batches_hold_has_a_reason CHECK (
        status IN ('OPEN', 'RELEASED') OR (status_reason IS NOT NULL AND status_reason <> '')),

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(26) NOT NULL,
    updated_by  VARCHAR(26) NOT NULL,
    deleted_at  TIMESTAMPTZ
);

-- One batch per code. Two vessels labelled the same is a recall that reaches
-- the wrong one, and the wrong one is on a lorry.
CREATE UNIQUE INDEX IF NOT EXISTS production_batches_one_per_code
    ON production_batches (tenant_id, batch_code) WHERE deleted_at IS NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'production_batches_tenant_id_key') THEN
        ALTER TABLE production_batches ADD CONSTRAINT production_batches_tenant_id_key
            UNIQUE (tenant_id, id);
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS production_batches_by_product
    ON production_batches (tenant_id, product_ref, produced_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS production_batches_by_source
    ON production_batches (tenant_id, source_kind, source_ref) WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- Inputs
-- ---------------------------------------------------------------------------

-- What went into a batch, and how much of it.
CREATE TABLE IF NOT EXISTS production_inputs (
    id          VARCHAR(26) PRIMARY KEY,
    tenant_id   VARCHAR(26) NOT NULL,

    output_batch_id VARCHAR(26) NOT NULL,
    input_batch_id  VARCHAR(26) NOT NULL,
    CONSTRAINT production_inputs_not_itself CHECK (output_batch_id <> input_batch_id),

    consumed_value BIGINT NOT NULL CHECK (consumed_value > 0),
    consumed_unit  VARCHAR(16) NOT NULL CHECK (consumed_unit IN ('LITRES', 'KILOGRAMS')),

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(26) NOT NULL,

    FOREIGN KEY (tenant_id, output_batch_id) REFERENCES production_batches (tenant_id, id),
    FOREIGN KEY (tenant_id, input_batch_id) REFERENCES production_batches (tenant_id, id),

    -- One line per input per output. Two would mean the same lot recorded twice
    -- into one vessel, which double-counts it in the yield and in every recall
    -- that traverses it.
    UNIQUE (tenant_id, output_batch_id, input_batch_id)
);

CREATE INDEX IF NOT EXISTS production_inputs_by_output
    ON production_inputs (tenant_id, output_batch_id);
CREATE INDEX IF NOT EXISTS production_inputs_by_input
    ON production_inputs (tenant_id, input_batch_id);

-- A batch cannot be its own ancestor.
--
-- Without this a genealogy is a graph with a loop in it, and every traversal of
-- it either runs forever or stops at an arbitrary depth and reports a partial
-- answer as a complete one. The second is worse: a recall that walked into a
-- cycle, gave up at ten hops and returned what it had found would look exactly
-- like a recall that finished.
--
-- Checked with a recursive descent rather than by remembering ancestors on the
-- row, because the ancestor set of a silo that took forty tankers is large and
-- the check happens once per input line.
CREATE OR REPLACE FUNCTION gavya_batch_is_not_its_own_ancestor()
RETURNS TRIGGER AS $fn$
DECLARE
    v_cycle boolean;
BEGIN
    WITH RECURSIVE ancestry AS (
        SELECT input_batch_id AS id FROM production_inputs
            WHERE tenant_id = NEW.tenant_id AND output_batch_id = NEW.input_batch_id
        UNION
        SELECT i.input_batch_id FROM production_inputs i
            JOIN ancestry a ON i.output_batch_id = a.id
            WHERE i.tenant_id = NEW.tenant_id
    )
    SELECT EXISTS (SELECT 1 FROM ancestry WHERE id = NEW.output_batch_id) INTO v_cycle;

    IF v_cycle THEN
        RAISE EXCEPTION 'batch % is already somewhere upstream of batch %, so making one an '
                        'input of the other would put a loop in the genealogy — and a recall '
                        'that walks into a loop reports a partial answer as a complete one',
            NEW.output_batch_id, NEW.input_batch_id
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$fn$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS production_inputs_no_cycles ON production_inputs;
CREATE TRIGGER production_inputs_no_cycles
    BEFORE INSERT OR UPDATE ON production_inputs
    FOR EACH ROW EXECUTE FUNCTION gavya_batch_is_not_its_own_ancestor();

-- A batch cannot give up more than it holds.
--
-- Five hundred kilograms drawn from a four hundred kilogram lot is milk from
-- nowhere, and it balances: the output looks larger and every yield computed
-- from it looks better. The consuming end is where it has to be caught, because
-- by the time somebody totals the silo the extra hundred kilograms have already
-- become product.
--
-- Units have to match to compare. A litre lot consumed in kilograms is refused
-- here rather than converted, because converting needs a density and this
-- trigger has none — the service supplies one where it is needed, and a
-- conversion invented in a trigger would be the three per cent this platform
-- spends its time refusing to invent.
CREATE OR REPLACE FUNCTION gavya_batch_does_not_overdraw()
RETURNS TRIGGER AS $fn$
DECLARE
    v_produced BIGINT;
    v_unit     TEXT;
    v_code     TEXT;
    v_consumed BIGINT;
BEGIN
    SELECT produced_value, produced_unit, batch_code
      INTO v_produced, v_unit, v_code
      FROM production_batches
     WHERE tenant_id = NEW.tenant_id AND id = NEW.input_batch_id
     FOR UPDATE;

    IF v_produced IS NULL THEN
        RETURN NEW;
    END IF;
    IF v_unit IS DISTINCT FROM NEW.consumed_unit THEN
        RAISE EXCEPTION 'batch % holds % and this line consumes %; they are different units and '
                        'converting between them needs a density nobody has supplied',
            v_code, v_unit, NEW.consumed_unit
            USING ERRCODE = '23514';
    END IF;

    SELECT COALESCE(SUM(consumed_value), 0) INTO v_consumed
      FROM production_inputs
     WHERE tenant_id = NEW.tenant_id AND input_batch_id = NEW.input_batch_id
       AND id <> NEW.id;

    IF v_consumed + NEW.consumed_value > v_produced THEN
        RAISE EXCEPTION 'batch % holds %, % of it is already consumed, and this line takes a '
                        'further %: a lot cannot give up more than it holds',
            v_code, v_produced, v_consumed, NEW.consumed_value
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$fn$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS production_inputs_no_overdraw ON production_inputs;
CREATE TRIGGER production_inputs_no_overdraw
    BEFORE INSERT OR UPDATE ON production_inputs
    FOR EACH ROW EXECUTE FUNCTION gavya_batch_does_not_overdraw();

-- A batch under hold cannot be fed into anything.
--
-- This is the control that makes a recall worth running. Finding the cartons is
-- half the job; the other half is that the lot stops moving. Without this, a
-- silo quarantined at nine o'clock is churned into butter at ten, the butter is
-- a new batch with a clean status, and the quarantine has achieved nothing
-- except a row in a table.
--
-- The hold is on the *input* being consumed, checked at the moment of
-- consumption. Lines recorded before the hold are left alone — they are history,
-- and the recall traversal is what deals with them. Deleting them would erase
-- the evidence of where the contaminated milk went, which is the one thing the
-- recall needs.
--
-- There is a way out and it is deliberate: lift the hold. That is a status
-- change on the batch, it is written to the audit chain like every other change
-- in this platform, and it has a person's name on it. A control with no way out
-- gets worked around outside the database, where nothing is recorded.
CREATE OR REPLACE FUNCTION gavya_held_batch_is_not_consumed()
RETURNS TRIGGER AS $fn$
DECLARE
    v_status TEXT;
    v_reason TEXT;
    v_code   TEXT;
BEGIN
    SELECT status, status_reason, batch_code
      INTO v_status, v_reason, v_code
      FROM production_batches
     WHERE tenant_id = NEW.tenant_id AND id = NEW.input_batch_id;

    IF v_status IN ('QUARANTINED', 'RECALLED', 'DISPOSED') THEN
        RAISE EXCEPTION 'batch % is % (%) and cannot be consumed; lift the hold first if it '
                        'should not be, because a hold that does not stop the lot moving is '
                        'not a hold',
            v_code, v_status, v_reason
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$fn$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS production_inputs_no_held_input ON production_inputs;
CREATE TRIGGER production_inputs_no_held_input
    BEFORE INSERT OR UPDATE ON production_inputs
    FOR EACH ROW EXECUTE FUNCTION gavya_held_batch_is_not_consumed();

-- Not enforced here: that an input was produced before the batch it went into.
--
-- It looks like an obvious rule and it is not one. A silo's produced_at is the
-- moment the plant chooses to stamp — when it opened, when it closed, when the
-- last tanker discharged — and plants differ. Under one of those readings a
-- perfectly ordinary batch drawn from a silo that is still filling has an input
-- stamped later than the output. Refusing it would be a false refusal on
-- routine work, and a control that refuses routine work is answered by fudging
-- the timestamp — which corrupts the exact field a recall reads.
--
-- So it is reported rather than refused: the service surfaces out-of-order
-- pairs, and a person decides whether it is a stamping convention or a mistake.

-- What a batch has left, and what it gave up.
--
-- A view rather than a column, because a stored remaining quantity is a number
-- that drifts from the lines it is supposed to summarise, and the drift is
-- silent.
CREATE OR REPLACE VIEW gavya_batch_balance AS
SELECT b.tenant_id,
       b.id,
       b.batch_code,
       b.produced_value,
       b.produced_unit,
       COALESCE(c.consumed, 0) AS consumed_value,
       b.produced_value - COALESCE(c.consumed, 0) AS remaining_value
  FROM production_batches b
  LEFT JOIN (
      SELECT tenant_id, input_batch_id, SUM(consumed_value) AS consumed
        FROM production_inputs
       GROUP BY tenant_id, input_batch_id
  ) c ON c.tenant_id = b.tenant_id AND c.input_batch_id = b.id
 WHERE b.deleted_at IS NULL;
