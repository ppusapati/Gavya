-- The laboratory: what was sampled, who held it, and what it read.
--
-- Fat and SNF decide what a producer is paid. A result that priced a fortnight
-- and cannot be traced to a sealed sample, held by known hands, read on an
-- instrument somebody had certified, is a number a society cannot defend when a
-- member asks about it — and the member is entitled to ask.
--
-- So this records three things and joins them: the sample, its custody, and the
-- result. The join is the product. Any one of them alone is a row in a book.

-- ---------------------------------------------------------------------------
-- Samples
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS lab_samples (
    id          VARCHAR(26) PRIMARY KEY,
    tenant_id   VARCHAR(26) NOT NULL,

    -- What is written on the bottle. A person at a bench reads this, not an
    -- identifier they have never seen.
    sample_code VARCHAR(64) NOT NULL,

    -- What it was drawn from, and its identifier in whichever service owns it.
    --
    -- Polymorphic on purpose: a sample is drawn from a producer's can, from a
    -- tanker at the dock, from a silo, from a batch of paneer. A foreign key
    -- names one table and these name four, so the reference is recorded and not
    -- enforced — and that decision is written down in
    -- libs/integrity/isolation/references.sql rather than left to be noticed.
    source_kind VARCHAR(16) NOT NULL CHECK (source_kind IN (
                    'COLLECTION', 'MOVEMENT', 'NODE', 'BATCH')),
    source_ref  VARCHAR(64) NOT NULL,

    drawn_at    TIMESTAMPTZ NOT NULL,
    drawn_by    VARCHAR(26) NOT NULL,

    -- The seal.
    --
    -- A sample that was never sealed, or whose seal was broken before it reached
    -- the bench, is not evidence. It may still be analysed — a society running a
    -- process check does not seal anything — but a result from it must not be
    -- what a payment dispute turns on, and the only way to know afterwards is to
    -- have recorded it at the time.
    seal_number VARCHAR(64),
    seal_broken_at TIMESTAMPTZ,
    seal_broken_by VARCHAR(26),
    seal_broken_reason TEXT,
    CONSTRAINT lab_samples_seal_break_is_whole CHECK (
        (seal_broken_at IS NULL AND seal_broken_by IS NULL AND seal_broken_reason IS NULL) OR
        (seal_broken_at IS NOT NULL AND seal_broken_by IS NOT NULL
            AND seal_broken_reason IS NOT NULL AND seal_broken_reason <> '')),
    -- A seal cannot be broken on a sample that never had one.
    CONSTRAINT lab_samples_unsealed_has_no_break CHECK (
        seal_number IS NOT NULL OR seal_broken_at IS NULL),
    CONSTRAINT lab_samples_seal_broken_after_drawing CHECK (
        seal_broken_at IS NULL OR seal_broken_at >= drawn_at),

    -- Why the sample was taken. A routine payment sample, a duplicate drawn to
    -- cross-check one, a sample taken because somebody disputed a figure: the
    -- three carry different weight in an argument and look identical afterwards
    -- unless it was written down.
    purpose     VARCHAR(20) NOT NULL CHECK (purpose IN (
                    'PAYMENT', 'DUPLICATE', 'DISPUTE', 'PROCESS_CHECK', 'REGULATORY')),
    -- The sample this one duplicates, where it is a duplicate.
    duplicates_sample_id VARCHAR(26),
    CONSTRAINT lab_samples_duplicate_names_its_original CHECK (
        (purpose = 'DUPLICATE') = (duplicates_sample_id IS NOT NULL)),

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(26) NOT NULL,
    updated_by  VARCHAR(26) NOT NULL,
    deleted_at  TIMESTAMPTZ
);

-- One sample per code. Two bottles labelled the same is a result attached to the
-- wrong producer, and nobody finds out.
CREATE UNIQUE INDEX IF NOT EXISTS lab_samples_one_per_code
    ON lab_samples (tenant_id, sample_code) WHERE deleted_at IS NULL;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'lab_samples_tenant_id_key') THEN
        ALTER TABLE lab_samples ADD CONSTRAINT lab_samples_tenant_id_key UNIQUE (tenant_id, id);
    END IF;
END
$$;

-- The duplicate reference is added here rather than inline, because it points at
-- this same table and a composite reference needs its target key to exist
-- already — which, inside the CREATE TABLE that is declaring it, it does not.
--
-- This is the third table in this repository to be written the wrong way round
-- and refused on first application. The rule is worth stating plainly: a
-- composite foreign key, self-referencing or not, comes after the UNIQUE that
-- backs it.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'lab_samples_duplicates_fkey') THEN
        ALTER TABLE lab_samples ADD CONSTRAINT lab_samples_duplicates_fkey
            FOREIGN KEY (tenant_id, duplicates_sample_id) REFERENCES lab_samples (tenant_id, id);
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS lab_samples_by_source
    ON lab_samples (tenant_id, source_kind, source_ref) WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- Custody
-- ---------------------------------------------------------------------------

-- One handover.
--
-- Modelled as handovers rather than as holding periods, because a handover is
-- the event somebody witnesses and a holding period is an inference from two of
-- them. The chain is intact when each handover's receiver is the next one's
-- giver — which is checkable, and which a set of overlapping periods is not.
CREATE TABLE IF NOT EXISTS lab_custody (
    id          VARCHAR(26) PRIMARY KEY,
    tenant_id   VARCHAR(26) NOT NULL,
    sample_id   VARCHAR(26) NOT NULL,

    -- Position in the chain, from 1. Explicit rather than derived from the
    -- timestamp: two handovers in the same minute are ordinary at a dock, and
    -- ordering by time would leave which came first to the query plan.
    sequence    SMALLINT NOT NULL CHECK (sequence > 0),

    at          TIMESTAMPTZ NOT NULL,
    from_holder VARCHAR(64) NOT NULL,
    to_holder   VARCHAR(64) NOT NULL,
    CONSTRAINT lab_custody_hands_it_to_somebody_else CHECK (from_holder <> to_holder),
    note        TEXT,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(26) NOT NULL,

    FOREIGN KEY (tenant_id, sample_id) REFERENCES lab_samples (tenant_id, id),
    UNIQUE (tenant_id, sample_id, sequence)
);

CREATE INDEX IF NOT EXISTS lab_custody_by_sample
    ON lab_custody (tenant_id, sample_id, sequence);

-- A handover already recorded is not rewritten.
--
-- The chain of custody is the thing a disputed result rests on. A link edited
-- afterwards is a chain nobody can rely on, and the edit is invisible: the
-- result is the same, the sample is the same, and only the story of who held it
-- has changed.
CREATE OR REPLACE FUNCTION gavya_custody_is_append_only()
RETURNS TRIGGER AS $fn$
BEGIN
    RAISE EXCEPTION 'custody link % on sample % cannot be % : the chain a disputed result rests '
                    'on is written once. A handover recorded in error is corrected by recording '
                    'the correction, not by editing the record of what was witnessed',
        OLD.id, OLD.sample_id, lower(TG_OP)
        USING ERRCODE = '23514';
END
$fn$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS lab_custody_append_only ON lab_custody;
CREATE TRIGGER lab_custody_append_only
    BEFORE UPDATE OR DELETE ON lab_custody
    FOR EACH ROW EXECUTE FUNCTION gavya_custody_is_append_only();

-- ---------------------------------------------------------------------------
-- Results
-- ---------------------------------------------------------------------------

-- One analyte, read once.
CREATE TABLE IF NOT EXISTS lab_results (
    id          VARCHAR(26) PRIMARY KEY,
    tenant_id   VARCHAR(26) NOT NULL,
    sample_id   VARCHAR(26) NOT NULL,

    analyte     VARCHAR(20) NOT NULL CHECK (analyte IN (
                    'FAT', 'SNF', 'PROTEIN', 'LACTOSE', 'ADDED_WATER',
                    'ACIDITY', 'MBRT', 'TEMPERATURE')),

    -- Scaled integers, like every other measured figure in this platform. A
    -- society's chart may be written to one decimal place and another's to two,
    -- and rewriting one into the other's resolution changes which cell a reading
    -- falls in — which is money.
    value_numerator BIGINT NOT NULL,
    value_scale     SMALLINT NOT NULL CHECK (value_scale BETWEEN 0 AND 6),

    -- How it was read, and on what.
    method        VARCHAR(40) NOT NULL,
    instrument_ref VARCHAR(64) NOT NULL,
    -- The instrument's calibration, as it stood on the day of the analysis.
    -- Copied rather than joined: a certificate renewed next month must not
    -- retroactively make last month's result eligible.
    instrument_valid_until DATE,
    instrument_certificate VARCHAR(120),

    analysed_at TIMESTAMPTZ NOT NULL,
    analysed_by VARCHAR(64) NOT NULL,

    -- Whether this result is fit to price milk, and why.
    --
    -- The same three words observation-service uses for measurement eligibility,
    -- deliberately: a platform with two vocabularies for the same idea is one
    -- where somebody eventually maps the wrong one onto the other.
    --
    -- Stored rather than recomputed on read, because the certificate register
    -- changes and an auditor needs the verdict that was reached when the payment
    -- was made.
    eligibility VARCHAR(16) NOT NULL CHECK (eligibility IN (
                    'ELIGIBLE', 'NOT_ELIGIBLE', 'UNKNOWN')),
    eligibility_reason TEXT NOT NULL,
    CONSTRAINT lab_results_ineligible_says_why CHECK (
        eligibility = 'ELIGIBLE' OR eligibility_reason <> ''),

    -- Corrections, the same shape as everywhere else: a new row, the prior
    -- version keeping the instant it was believed, both readable.
    superseded_at TIMESTAMPTZ,
    superseded_by VARCHAR(26),
    supersedes    VARCHAR(26),
    correction_reason TEXT,
    CONSTRAINT lab_results_supersession_is_attributed CHECK (
        (superseded_at IS NULL AND superseded_by IS NULL) OR
        (superseded_at IS NOT NULL AND superseded_by IS NOT NULL)),
    CONSTRAINT lab_results_correction_has_a_reason CHECK (
        supersedes IS NULL OR (correction_reason IS NOT NULL AND correction_reason <> '')),

    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  VARCHAR(26) NOT NULL,
    updated_by  VARCHAR(26) NOT NULL,
    deleted_at  TIMESTAMPTZ,

    FOREIGN KEY (tenant_id, sample_id) REFERENCES lab_samples (tenant_id, id)
);

-- One live result per sample per analyte per instrument.
--
-- Per instrument, not per analyte, and the difference is the point: a laboratory
-- running the same sample on two machines is doing a cross-check, and that is
-- worth having. Two results from the same machine for the same analyte is one
-- of them being entered twice.
CREATE UNIQUE INDEX IF NOT EXISTS lab_results_one_per_instrument
    ON lab_results (tenant_id, sample_id, analyte, instrument_ref)
    WHERE deleted_at IS NULL AND superseded_at IS NULL;

CREATE INDEX IF NOT EXISTS lab_results_by_sample
    ON lab_results (tenant_id, sample_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS lab_results_ineligible
    ON lab_results (tenant_id, analysed_at DESC)
    WHERE deleted_at IS NULL AND superseded_at IS NULL AND eligibility <> 'ELIGIBLE';

-- A result cannot be read before its sample was drawn.
--
-- A cross-table rule, so a trigger rather than a CHECK. It catches the
-- commonest date-entry error in a laboratory book — yesterday's date typed on
-- this morning's analysis — and that error puts a result against a sample that
-- did not exist, which no total will ever reveal.
CREATE OR REPLACE FUNCTION gavya_result_follows_its_sample()
RETURNS TRIGGER AS $fn$
DECLARE
    v_drawn TIMESTAMPTZ;
BEGIN
    SELECT drawn_at INTO v_drawn FROM lab_samples
        WHERE tenant_id = NEW.tenant_id AND id = NEW.sample_id;
    IF v_drawn IS NULL THEN
        RETURN NEW;
    END IF;
    IF NEW.analysed_at < v_drawn THEN
        RAISE EXCEPTION 'result % is timed % and its sample was drawn at %: a sample cannot be '
                        'read before it exists',
            NEW.id, NEW.analysed_at, v_drawn
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$fn$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS lab_results_follow_their_sample ON lab_results;
CREATE TRIGGER lab_results_follow_their_sample
    BEFORE INSERT OR UPDATE ON lab_results
    FOR EACH ROW EXECUTE FUNCTION gavya_result_follows_its_sample();
