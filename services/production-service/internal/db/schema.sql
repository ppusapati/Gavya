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

    -- What this batch was expected to yield is not here. It is a property of the
    -- recipe, not of the vat, and it lives on the formulation the batch points
    -- at further down this file. A figure typed per batch is a figure retyped a
    -- hundred times a month by whoever is on shift, and it drifts silently.
    --
    -- There is still no table of standard yields anywhere in this platform. A
    -- society's paneer yield is a property of its milk, its process and its
    -- equipment. What the formulations section adds is not a default but the
    -- means to find the number out: a plant's own observed history, summarised,
    -- from which it declares its own expectation.

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

-- ---------------------------------------------------------------------------
-- Formulations: what a plant says it makes a thing out of, and what it expects
-- to get.
--
-- The expectation was on the batch, one number typed per vat. That works and it
-- drifts: the figure is retyped a hundred times a month by whoever is on shift,
-- nobody compares this month's typing with last month's, and by the time the
-- variance report is wrong nobody can say when it started being wrong. It
-- belongs in one place that a batch points at.
--
--
-- THIS PLATFORM STILL DOES NOT KNOW YOUR YIELD
--
-- There is no table of standard yields here and this file does not add one. A
-- society's paneer yield is a property of its milk, its process and its
-- equipment; a figure invented here would be a number nobody measured sitting
-- in a variance report that somebody is asked to explain.
--
-- What this does add is the way to find it out. gavya_formulation_observed_yield
-- gathers what every batch that followed a formulation actually gave, and
-- reports the count, the range, the median and the quartiles. A plant reads its
-- own history and declares its own expectation from it. That is the honest
-- answer to a question this platform cannot answer for anybody: not a default,
-- and not a shrug either.
--
-- The declared expectation stays optional after all that. A plant that has run
-- a process four times has no business declaring an expectation from four
-- numbers, and the report says how many it is looking at so the reader can see
-- that for themselves.
--
--
-- WHY A RECIPE IS VERSIONED
--
-- A recipe changes: a coagulant is switched, a standardisation target moves, a
-- new separator goes in. Editing the row in place would change the variance on
-- every batch already made under the old recipe, retroactively, and the report
-- that was signed off last month would say something different this month.
--
-- So a formulation is in force over a period, one version at a time per code,
-- and a batch records the exact version it followed. Superseding is how a
-- recipe changes, which is the same thing rate cards do in procurement and for
-- the same reason.

CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TABLE IF NOT EXISTS production_formulations (
    id          VARCHAR(26) PRIMARY KEY,
    tenant_id   VARCHAR(26) NOT NULL,

    -- What the plant calls this recipe. Stable across versions: PANEER-STD is
    -- the same recipe in March and in September, at two versions.
    code        VARCHAR(64) NOT NULL,
    name        VARCHAR(200) NOT NULL,

    -- What it makes, in the plant's own words, matching the product_ref a batch
    -- carries. Free text for the same reason it is free text there.
    output_product_ref VARCHAR(64) NOT NULL,
    output_unit        VARCHAR(16) NOT NULL CHECK (output_unit IN ('LITRES', 'KILOGRAMS')),

    -- What the plant expects, in parts per million of what it consumes.
    -- Optional, and left optional deliberately: see the note at the top.
    expected_yield_ppm BIGINT CHECK (expected_yield_ppm IS NULL OR expected_yield_ppm > 0),
    -- Where the expectation came from. A figure a plant derived from its own
    -- observed history is a different kind of claim from one somebody read off
    -- a supplier's leaflet, and a variance report that treats them alike
    -- invites the same argument every month.
    expectation_basis TEXT,
    CONSTRAINT production_formulations_expectation_says_where_from CHECK (
        expected_yield_ppm IS NULL OR (expectation_basis IS NOT NULL AND expectation_basis <> '')),

    -- When this version of the recipe is the one in force.
    valid_from  TIMESTAMPTZ NOT NULL,
    valid_to    TIMESTAMPTZ,
    CONSTRAINT production_formulations_period_is_forwards CHECK (
        valid_to IS NULL OR valid_to > valid_from),

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(26) NOT NULL,
    updated_by  VARCHAR(26) NOT NULL,
    deleted_at  TIMESTAMPTZ
);

-- One version of a recipe in force at a time. Two would not be a conflict to
-- resolve when a batch looks one up — resolved then, by taking the newest say,
-- the choice is invisible, and the plant finds out when two vats of the same
-- product report variances against different targets.
--
-- Half-open, so a recipe ending on the first of March and its replacement
-- starting there do not overlap. That is how a plant actually changes one.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                   WHERE conname = 'production_formulations_one_version_at_a_time') THEN
        ALTER TABLE production_formulations
            ADD CONSTRAINT production_formulations_one_version_at_a_time
            EXCLUDE USING gist (
                tenant_id WITH =,
                code WITH =,
                tstzrange(valid_from, valid_to, '[)') WITH &&
            ) WHERE (deleted_at IS NULL);
    END IF;
END
$$;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                   WHERE conname = 'production_formulations_tenant_id_key') THEN
        ALTER TABLE production_formulations ADD CONSTRAINT production_formulations_tenant_id_key
            UNIQUE (tenant_id, id);
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS production_formulations_by_output
    ON production_formulations (tenant_id, output_product_ref) WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- What goes into it
-- ---------------------------------------------------------------------------

-- One expected ingredient of a recipe, by what the plant calls it.
--
-- By product_ref rather than by batch, because a recipe names milk and rennet;
-- which lot of milk is a fact about Tuesday, not about the recipe.
CREATE TABLE IF NOT EXISTS production_formulation_inputs (
    id             VARCHAR(26) PRIMARY KEY,
    tenant_id      VARCHAR(26) NOT NULL,
    formulation_id VARCHAR(26) NOT NULL,

    product_ref VARCHAR(64) NOT NULL,

    -- What share of the total input this ingredient is expected to be, in parts
    -- per million. Optional: a plant that knows its proportions declares them
    -- and gets them checked; one that does not still gets its ingredients
    -- checked, which is the part that catches paneer made with no milk in it.
    --
    -- Not required to sum to a million. A recipe that names its two main
    -- ingredients and leaves the salt undeclared is an ordinary recipe, and
    -- refusing it would push the plant into inventing a figure for the salt.
    expected_share_ppm BIGINT CHECK (
        expected_share_ppm IS NULL OR (expected_share_ppm > 0 AND expected_share_ppm <= 1000000)),

    -- How far from that share a batch may be before it is worth mentioning.
    -- Only meaningful beside a declared share, and required in order to get a
    -- finding at all.
    --
    -- A real vat never hits a declared proportion exactly, so comparing for
    -- equality would report every batch a plant ever made and the report would
    -- stop being read within a month. How close is close enough is a question
    -- about this plant's process, its scales and what it is trying to control.
    -- A figure invented here would be the platform's opinion sitting in a
    -- report with the plant's name on it.
    --
    -- Where none is declared the observed and declared shares are still both
    -- reported. The numbers are shown; the judgement is not made.
    share_tolerance_ppm BIGINT CHECK (
        share_tolerance_ppm IS NULL OR (share_tolerance_ppm >= 0 AND share_tolerance_ppm <= 1000000)),
    CONSTRAINT production_formulation_inputs_tolerance_needs_a_share CHECK (
        share_tolerance_ppm IS NULL OR expected_share_ppm IS NOT NULL),

    -- Whether a batch that does not contain this is wrong, or merely unusual.
    -- Paneer without milk is wrong. Paneer without the optional culture is a
    -- Tuesday.
    required BOOLEAN NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,

    FOREIGN KEY (tenant_id, formulation_id)
        REFERENCES production_formulations (tenant_id, id) ON DELETE CASCADE,

    UNIQUE (tenant_id, formulation_id, product_ref)
);

CREATE INDEX IF NOT EXISTS production_formulation_inputs_by_formulation
    ON production_formulation_inputs (tenant_id, formulation_id);

-- A recipe that makes a thing out of itself has no first ingredient, and a
-- plant following it would never start.
CREATE OR REPLACE FUNCTION gavya_formulation_input_is_not_its_own_output()
RETURNS TRIGGER AS $fn$
DECLARE
    v_output TEXT;
    v_code   TEXT;
BEGIN
    SELECT output_product_ref, code INTO v_output, v_code
      FROM production_formulations
     WHERE tenant_id = NEW.tenant_id AND id = NEW.formulation_id;

    IF v_output = NEW.product_ref THEN
        RAISE EXCEPTION 'recipe % makes % and this line says it is made from %; a recipe whose '
                        'ingredient is its own product has no first ingredient',
            v_code, v_output, NEW.product_ref
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$fn$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS production_formulation_inputs_not_the_output
    ON production_formulation_inputs;
CREATE TRIGGER production_formulation_inputs_not_the_output
    BEFORE INSERT OR UPDATE ON production_formulation_inputs
    FOR EACH ROW EXECUTE FUNCTION gavya_formulation_input_is_not_its_own_output();

-- ---------------------------------------------------------------------------
-- A batch says which recipe it followed
-- ---------------------------------------------------------------------------

ALTER TABLE production_batches
    ADD COLUMN IF NOT EXISTS formulation_id VARCHAR(26);

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint
                   WHERE conname = 'production_batches_formulation_fkey') THEN
        ALTER TABLE production_batches ADD CONSTRAINT production_batches_formulation_fkey
            FOREIGN KEY (tenant_id, formulation_id)
            REFERENCES production_formulations (tenant_id, id);
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS production_batches_by_formulation
    ON production_batches (tenant_id, formulation_id) WHERE formulation_id IS NOT NULL;

-- The recipe a batch followed has to be the recipe for what the batch is, and
-- it has to have been in force when the batch was made.
--
-- Both halves matter. A paneer vat pointed at the ghee recipe reports a
-- variance against a target for a different product, and the number looks
-- alarming rather than meaningless. A vat pointed at a version of its own
-- recipe that came into force a month later reports against a target nobody
-- was working to on the day — which is the retroactive problem versioning
-- exists to prevent, arriving through the other door.
CREATE OR REPLACE FUNCTION gavya_batch_follows_a_recipe_for_what_it_is()
RETURNS TRIGGER AS $fn$
DECLARE
    v_output TEXT;
    v_code   TEXT;
    v_from   TIMESTAMPTZ;
    v_to     TIMESTAMPTZ;
BEGIN
    IF NEW.formulation_id IS NULL THEN
        RETURN NEW;
    END IF;

    SELECT output_product_ref, code, valid_from, valid_to
      INTO v_output, v_code, v_from, v_to
      FROM production_formulations
     WHERE tenant_id = NEW.tenant_id AND id = NEW.formulation_id;

    IF v_output IS NULL THEN
        RETURN NEW;   -- the foreign key reports this one, and reports it better
    END IF;

    IF v_output IS DISTINCT FROM NEW.product_ref THEN
        RAISE EXCEPTION 'batch % is % and recipe % makes %; a batch measured against the recipe '
                        'for a different product reports a variance that means nothing and '
                        'looks alarming',
            NEW.batch_code, NEW.product_ref, v_code, v_output
            USING ERRCODE = '23514';
    END IF;

    IF NEW.produced_at < v_from OR (v_to IS NOT NULL AND NEW.produced_at >= v_to) THEN
        RAISE EXCEPTION 'batch % was made at % and version % of recipe % was in force from % '
                        'to %; measuring it against a version nobody was working to on the day '
                        'is the retroactive problem versioning exists to prevent',
            NEW.batch_code, NEW.produced_at, NEW.formulation_id, v_code, v_from,
            COALESCE(v_to::text, 'now')
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$fn$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS production_batches_recipe_matches ON production_batches;
CREATE TRIGGER production_batches_recipe_matches
    BEFORE INSERT OR UPDATE ON production_batches
    FOR EACH ROW EXECUTE FUNCTION gavya_batch_follows_a_recipe_for_what_it_is();

-- ---------------------------------------------------------------------------
-- What the plant actually got
-- ---------------------------------------------------------------------------

-- Every batch that followed a recipe, and what it yielded.
--
-- Only where the batch and its inputs are in the same unit. Where they are not,
-- the yield needs a density, a density is the caller's to supply, and a view
-- has no caller — so those batches are absent rather than converted on an
-- assumption. gavya_formulation_observed_yield says how many were left out, so
-- a reader can tell a recipe with a short history from one whose history is
-- mostly unconvertible.
CREATE OR REPLACE VIEW gavya_batch_observed_yield AS
SELECT b.tenant_id,
       b.formulation_id,
       b.id AS batch_id,
       b.batch_code,
       b.produced_at,
       b.produced_value,
       b.produced_unit,
       agg.consumed_value,
       -- Integer division, deliberately, and the cast is what makes it so.
       -- SUM() over a BIGINT column returns NUMERIC, and numeric division here
       -- would give 166666.666666666667 where the service computing the same
       -- yield in Go gives 166666. Two figures for one vat, differing in a
       -- place nobody looks, is how a variance report and the screen it was
       -- read off stop agreeing.
       CASE WHEN agg.consumed_value > 0
             AND agg.units_used = 1
             AND agg.a_unit = b.produced_unit
            THEN b.produced_value * 1000000 / agg.consumed_value::bigint
       END AS observed_ppm,
       (agg.units_used > 1 OR agg.a_unit IS DISTINCT FROM b.produced_unit) AS needs_a_density
  FROM production_batches b
  JOIN (
      SELECT tenant_id,
             output_batch_id,
             SUM(consumed_value)           AS consumed_value,
             MIN(consumed_unit)            AS a_unit,
             COUNT(DISTINCT consumed_unit) AS units_used
        FROM production_inputs
       GROUP BY tenant_id, output_batch_id
  ) agg ON agg.tenant_id = b.tenant_id AND agg.output_batch_id = b.id
 WHERE b.deleted_at IS NULL AND b.formulation_id IS NOT NULL;

-- What a plant needs in order to declare its own expectation.
--
-- The median and the quartiles are percentile_disc rather than percentile_cont:
-- they pick an observation that actually happened rather than interpolating
-- between two that did. A plant setting a target off this list should be
-- looking at figures its own vats produced.
--
-- batches_counted and batches_needing_a_density are both reported. A recipe
-- with three usable observations and forty unconvertible ones is not a recipe
-- anybody should be setting a target from, and a summary that showed only the
-- three would not say so.
CREATE OR REPLACE VIEW gavya_formulation_observed_yield AS
SELECT f.tenant_id,
       f.id AS formulation_id,
       f.code,
       f.output_product_ref,
       f.expected_yield_ppm,
       COUNT(y.observed_ppm)                                   AS batches_counted,
       COUNT(*) FILTER (WHERE y.needs_a_density)                AS batches_needing_a_density,
       MIN(y.observed_ppm)                                      AS lowest_ppm,
       PERCENTILE_DISC(0.25) WITHIN GROUP (ORDER BY y.observed_ppm) AS lower_quartile_ppm,
       PERCENTILE_DISC(0.5)  WITHIN GROUP (ORDER BY y.observed_ppm) AS median_ppm,
       PERCENTILE_DISC(0.75) WITHIN GROUP (ORDER BY y.observed_ppm) AS upper_quartile_ppm,
       MAX(y.observed_ppm)                                      AS highest_ppm
  FROM production_formulations f
  LEFT JOIN gavya_batch_observed_yield y
         ON y.tenant_id = f.tenant_id AND y.formulation_id = f.id
 WHERE f.deleted_at IS NULL
 GROUP BY f.tenant_id, f.id, f.code, f.output_product_ref, f.expected_yield_ppm;
