-- Settlement: the period a society pays for, and what each producer takes home.
--
-- Procurement says what milk was worth. This says what was actually handed over,
-- which is not the same number and differs by everything the producer owed.
--
-- Several constraints here duplicate a check the Go code also makes. That is
-- deliberate. Every one of them guards an error that is invisible in totals — a
-- producer paid twice for one delivery, a debt recovered past its principal, a
-- payable whose parts do not add up — and the code is one import script away
-- from not being the thing that wrote the row.

CREATE EXTENSION IF NOT EXISTS btree_gist;

-- ---------------------------------------------------------------------------
-- Payment cycles
-- ---------------------------------------------------------------------------

-- One period a society settles: a fortnight, or a month.
--
-- deduction_policy has no DEFAULT. What happens when a producer owes more than
-- their milk earned is a decision each society makes and neither answer is
-- obviously right: capping at earnings never hands anyone a bill, and not
-- capping is how a society actually recovers a large advance. Choosing here
-- would be choosing on their behalf, invisibly, and the producer would find out
-- at the window.
CREATE TABLE IF NOT EXISTS payment_cycles (
    id            VARCHAR(26) PRIMARY KEY,
    tenant_id     VARCHAR(26) NOT NULL,
    society_code  VARCHAR(64) NOT NULL,
    name          VARCHAR(120) NOT NULL,

    -- Both ends inclusive. A fortnight is spoken about as "the 1st to the 15th",
    -- and an exclusive end would quietly put the 15th's milk in the next one.
    period_start  DATE NOT NULL,
    period_end    DATE NOT NULL,
    CONSTRAINT payment_cycles_period_is_forwards CHECK (period_end >= period_start),

    currency      CHAR(3) NOT NULL,
    amount_scale  SMALLINT NOT NULL CHECK (amount_scale BETWEEN 0 AND 9),

    deduction_policy VARCHAR(20) NOT NULL
                     CHECK (deduction_policy IN ('CAP_AT_EARNINGS', 'ALLOW_NEGATIVE')),

    status        VARCHAR(16) NOT NULL
                  CHECK (status IN ('OPEN', 'GATHERED', 'APPROVED', 'PAID', 'ABANDONED')),

    gathered_at   TIMESTAMPTZ,
    approved_at   TIMESTAMPTZ,
    approved_by   VARCHAR(26),
    paid_at       TIMESTAMPTZ,

    -- A cycle cannot be approved without having gathered anything, and cannot be
    -- paid without having been approved. Recorded as a constraint rather than
    -- left to the service, because the sequence is what makes "approved" mean
    -- something a person did to figures that existed.
    CONSTRAINT payment_cycles_approved_after_gathering CHECK (
        approved_at IS NULL OR gathered_at IS NOT NULL),
    CONSTRAINT payment_cycles_paid_after_approval CHECK (
        paid_at IS NULL OR approved_at IS NOT NULL),
    CONSTRAINT payment_cycles_approval_is_attributed CHECK (
        (approved_at IS NULL) = (approved_by IS NULL)),

    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by    VARCHAR(26) NOT NULL,
    updated_by    VARCHAR(26) NOT NULL,
    deleted_at    TIMESTAMPTZ
);

-- Two live cycles covering the same day for the same society is not a conflict
-- to resolve when gathering, it is a conflict to prevent.
--
-- Resolved later — by taking the newest, say — a day's milk lands in one of them
-- and nobody can say which, so the same fortnight settles to two different
-- numbers depending on which cycle a query reached first. An abandoned cycle is
-- excluded because abandoning one is exactly how a society corrects the mistake
-- of having declared the wrong period.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'payment_cycles_one_per_period') THEN
        ALTER TABLE payment_cycles ADD CONSTRAINT payment_cycles_one_per_period
            EXCLUDE USING gist (
                tenant_id WITH =,
                society_code WITH =,
                daterange(period_start, period_end, '[]') WITH &&
            ) WHERE (deleted_at IS NULL AND status <> 'ABANDONED');
    END IF;
END
$$;

-- The composite key every child reaches through, so a line, a deduction or a
-- payable cannot point at another tenant's cycle. A single-column reference
-- would allow it: a foreign key is checked by the system rather than by the
-- querying role, so the row-level policies do not stop one.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'payment_cycles_tenant_id_key') THEN
        ALTER TABLE payment_cycles ADD CONSTRAINT payment_cycles_tenant_id_key UNIQUE (tenant_id, id);
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS payment_cycles_by_period
    ON payment_cycles (tenant_id, society_code, period_start DESC) WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- Cycle lines
-- ---------------------------------------------------------------------------

-- One collection, as it stood when the cycle gathered it.
--
-- The amount is copied from procurement rather than joined to it. A collection
-- re-priced after a producer has been paid must not change what they were paid;
-- if it should change, that is a correction with its own record, and not a
-- number that quietly stops matching the slip in somebody's pocket.
CREATE TABLE IF NOT EXISTS cycle_lines (
    id            VARCHAR(26) PRIMARY KEY,
    tenant_id     VARCHAR(26) NOT NULL,
    cycle_id      VARCHAR(26) NOT NULL,

    producer_ref  VARCHAR(64) NOT NULL,
    collection_id VARCHAR(26) NOT NULL,

    collected_on  DATE NOT NULL,
    shift         VARCHAR(16) NOT NULL CHECK (shift IN ('MORNING', 'EVENING')),

    quantity      VARCHAR(32) NOT NULL,
    quantity_unit VARCHAR(16) NOT NULL,
    rate          VARCHAR(32),

    currency           CHAR(3) NOT NULL,
    amount_scale       SMALLINT NOT NULL CHECK (amount_scale BETWEEN 0 AND 9),
    amount_minor_units BIGINT NOT NULL CHECK (amount_minor_units >= 0),

    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    FOREIGN KEY (tenant_id, cycle_id) REFERENCES payment_cycles (tenant_id, id)
);

-- The constraint this table exists for.
--
-- A collection belongs to exactly one cycle, ever. The failure it prevents is
-- specific and it has a shape: a fortnight is gathered, somebody notices a
-- problem, the cycle is re-gathered without the first attempt being cleared, and
-- every producer is paid twice for the same milk. The totals look larger, which
-- is what a good fortnight also looks like.
--
-- Unique across all cycles rather than within one, which is the whole point.
-- Abandoning a cycle deletes its lines, so the collections it held are free to
-- be gathered again by a cycle that is not a mistake.
CREATE UNIQUE INDEX IF NOT EXISTS cycle_lines_one_cycle_per_collection
    ON cycle_lines (tenant_id, collection_id);

CREATE INDEX IF NOT EXISTS cycle_lines_by_producer
    ON cycle_lines (tenant_id, cycle_id, producer_ref, collected_on);

-- ---------------------------------------------------------------------------
-- Recoveries
-- ---------------------------------------------------------------------------

-- Something a producer owes the society, recovered out of milk.
CREATE TABLE IF NOT EXISTS recoveries (
    id            VARCHAR(26) PRIMARY KEY,
    tenant_id     VARCHAR(26) NOT NULL,
    producer_ref  VARCHAR(64) NOT NULL,

    kind          VARCHAR(20) NOT NULL
                  CHECK (kind IN ('ADVANCE', 'FEED_CREDIT', 'SOCIETY_DUES', 'LOAN', 'OTHER')),
    reference     VARCHAR(120),

    currency      CHAR(3) NOT NULL,
    amount_scale  SMALLINT NOT NULL CHECK (amount_scale BETWEEN 0 AND 9),

    principal_minor_units BIGINT NOT NULL CHECK (principal_minor_units > 0),
    recovered_minor_units BIGINT NOT NULL DEFAULT 0 CHECK (recovered_minor_units >= 0),

    -- The most that may be taken in any one period. Zero means there is no
    -- instalment and the whole outstanding balance is recovered as soon as there
    -- is milk to recover it from, which is what dues on a single fortnight look
    -- like.
    instalment_minor_units BIGINT NOT NULL CHECK (instalment_minor_units >= 0),

    -- Which debt is served first when the milk will not cover everything. No
    -- DEFAULT: leaving it to the order rows come back in makes a decision about
    -- money into a decision nobody made, and it can differ between two runs of
    -- the same settlement.
    priority      SMALLINT NOT NULL CHECK (priority > 0),

    status        VARCHAR(16) NOT NULL CHECK (status IN ('OUTSTANDING', 'SETTLED', 'WAIVED')),
    opened_on     DATE NOT NULL,

    -- The constraint that makes over-recovery impossible rather than unlikely.
    --
    -- Taking more from a producer than they owe is invisible in every total the
    -- society looks at: the books balance, the payout is a plausible number, and
    -- the only person who can tell is the producer, a year later, if they kept
    -- the slips. The arithmetic checks this too; this is here because the
    -- arithmetic is one import script away from not being what wrote the row.
    CONSTRAINT recoveries_never_over_recovered
        CHECK (recovered_minor_units <= principal_minor_units),

    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by    VARCHAR(26) NOT NULL,
    updated_by    VARCHAR(26) NOT NULL,
    deleted_at    TIMESTAMPTZ
);

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'recoveries_tenant_id_key') THEN
        ALTER TABLE recoveries ADD CONSTRAINT recoveries_tenant_id_key UNIQUE (tenant_id, id);
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS recoveries_outstanding_by_producer
    ON recoveries (tenant_id, producer_ref, priority, opened_on, id)
    WHERE deleted_at IS NULL AND status = 'OUTSTANDING';

-- ---------------------------------------------------------------------------
-- Deductions
-- ---------------------------------------------------------------------------

-- One recovery served from one cycle's earnings.
CREATE TABLE IF NOT EXISTS cycle_deductions (
    id            VARCHAR(26) PRIMARY KEY,
    tenant_id     VARCHAR(26) NOT NULL,
    cycle_id      VARCHAR(26) NOT NULL,
    recovery_id   VARCHAR(26) NOT NULL,

    producer_ref  VARCHAR(64) NOT NULL,
    kind          VARCHAR(20) NOT NULL,
    reference     VARCHAR(120),

    currency           CHAR(3) NOT NULL,
    amount_scale       SMALLINT NOT NULL CHECK (amount_scale BETWEEN 0 AND 9),
    -- A deduction of zero is not a deduction. It is a line on the one document a
    -- producer actually reads, saying nothing happened.
    amount_minor_units BIGINT NOT NULL CHECK (amount_minor_units > 0),

    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    FOREIGN KEY (tenant_id, cycle_id) REFERENCES payment_cycles (tenant_id, id),
    FOREIGN KEY (tenant_id, recovery_id) REFERENCES recoveries (tenant_id, id),

    -- One deduction per recovery per cycle. Two would mean the same debt was
    -- served twice out of one fortnight, which is the same over-recovery as
    -- before arriving by a different route.
    UNIQUE (tenant_id, cycle_id, recovery_id)
);

CREATE INDEX IF NOT EXISTS cycle_deductions_by_producer
    ON cycle_deductions (tenant_id, cycle_id, producer_ref);

-- ---------------------------------------------------------------------------
-- Producer payables
-- ---------------------------------------------------------------------------

-- What one producer takes home from one cycle.
CREATE TABLE IF NOT EXISTS producer_payables (
    id            VARCHAR(26) PRIMARY KEY,
    tenant_id     VARCHAR(26) NOT NULL,
    cycle_id      VARCHAR(26) NOT NULL,
    producer_ref  VARCHAR(64) NOT NULL,

    currency      CHAR(3) NOT NULL,
    amount_scale  SMALLINT NOT NULL CHECK (amount_scale BETWEEN 0 AND 9),

    gross_minor_units    BIGINT NOT NULL,
    deducted_minor_units BIGINT NOT NULL CHECK (deducted_minor_units >= 0),
    -- Net may be negative: a society whose policy allows it hands the producer a
    -- bill rather than a payment.
    net_minor_units      BIGINT NOT NULL,
    carried_forward_minor_units BIGINT NOT NULL DEFAULT 0
                                CHECK (carried_forward_minor_units >= 0),

    -- The three figures are stored together and forced to agree, so a statement
    -- does not have to be trusted to subtract and a repair script cannot leave
    -- them disagreeing. A payable whose parts do not add up is a document a
    -- producer can hold up in a room and be right about.
    CONSTRAINT producer_payables_adds_up
        CHECK (net_minor_units = gross_minor_units - deducted_minor_units),

    status        VARCHAR(16) NOT NULL
                  CHECK (status IN ('PAYABLE', 'APPROVED', 'PAID', 'HELD')),

    -- What kind of money this is.
    --
    -- SETTLEMENT is the fortnight itself, computed by gathering. ADJUSTMENT is
    -- money that turned out to be owed after that fortnight was paid: a
    -- collection corrected, a recovery taken in error, a figure disputed and
    -- found wrong.
    --
    -- The trigger below refuses to edit a payable that has been paid, and its
    -- message says a payment that turned out to be wrong is corrected by a
    -- further payment rather than by editing the record of the one that
    -- happened. This column is that further payment. Without it the refusal
    -- names a remedy the platform does not have, and the only way to fix a
    -- wrong payment is the one thing the database will not allow.
    kind          VARCHAR(16) NOT NULL DEFAULT 'SETTLEMENT'
                  CHECK (kind IN ('SETTLEMENT', 'ADJUSTMENT')),

    -- Which payment this one corrects, where it corrects a specific one.
    adjusts_payable_id VARCHAR(26),

    -- Why an adjustment exists. Required for one, because an unexplained
    -- payment to a producer outside the settlement that computed it is the
    -- single row in this schema most worth explaining.
    reason        TEXT,
    CONSTRAINT producer_payables_adjustment_has_a_reason CHECK (
        kind <> 'ADJUSTMENT' OR (reason IS NOT NULL AND reason <> '')),
    CONSTRAINT producer_payables_only_adjustments_adjust CHECK (
        adjusts_payable_id IS NULL OR kind = 'ADJUSTMENT'),
    -- An adjustment of zero moves no money and puts a line on a producer's
    -- statement saying nothing happened.
    CONSTRAINT producer_payables_adjustment_is_not_zero CHECK (
        kind <> 'ADJUSTMENT' OR net_minor_units <> 0),

    -- A fortnight cannot earn a negative amount of milk money, and a settlement
    -- row whose gross is below zero means the gathering arithmetic went wrong.
    --
    -- An adjustment can. Corrections run both ways: a fat reading restated
    -- downwards means the producer was overpaid, and the money comes back. A
    -- blanket "gross is never negative" reads as prudent and makes half of what
    -- adjustments are for impossible — which is what it did here until an
    -- overpayment was tried against it.
    CONSTRAINT producer_payables_settlement_gross_is_not_negative CHECK (
        kind <> 'SETTLEMENT' OR gross_minor_units >= 0),

    -- An adjustment is a bare amount. Recovering a debt out of one would hide a
    -- deduction on a line whose reason says something else; a debt is recovered
    -- through a recovery, in the settlement that serves it.
    CONSTRAINT producer_payables_adjustment_deducts_nothing CHECK (
        kind <> 'ADJUSTMENT' OR deducted_minor_units = 0),

    approved_at   TIMESTAMPTZ,
    approved_by   VARCHAR(26),
    paid_at       TIMESTAMPTZ,
    paid_by       VARCHAR(26),
    payment_reference VARCHAR(120),
    held_reason   TEXT,

    CONSTRAINT producer_payables_paid_after_approval CHECK (
        paid_at IS NULL OR approved_at IS NOT NULL),
    CONSTRAINT producer_payables_approval_is_attributed CHECK (
        (approved_at IS NULL) = (approved_by IS NULL)),
    CONSTRAINT producer_payables_payment_is_attributed CHECK (
        (paid_at IS NULL) = (paid_by IS NULL)),
    -- Holding a payment is a thing done to a person, so it says why.
    CONSTRAINT producer_payables_hold_has_a_reason CHECK (
        status <> 'HELD' OR (held_reason IS NOT NULL AND held_reason <> '')),

    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    FOREIGN KEY (tenant_id, cycle_id) REFERENCES payment_cycles (tenant_id, id)
);

DO $$
BEGIN
    ALTER TABLE producer_payables ADD COLUMN IF NOT EXISTS kind VARCHAR(16) NOT NULL DEFAULT 'SETTLEMENT';
    ALTER TABLE producer_payables ADD COLUMN IF NOT EXISTS adjusts_payable_id VARCHAR(26);
    ALTER TABLE producer_payables ADD COLUMN IF NOT EXISTS reason TEXT;

    -- The rules on those columns, which the CREATE TABLE above declares inline
    -- and which therefore never reached a table that already existed. A
    -- database upgraded from the first version had the columns and none of the
    -- six constraints: an adjustment with no reason, a settlement row with a
    -- negative gross, a kind nothing recognises — all accepted, on exactly the
    -- databases that had been running longest. Found by comparing a database
    -- that lived through every version against a fresh one; every version had
    -- applied without error.
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'producer_payables_kind_check') THEN
        ALTER TABLE producer_payables ADD CONSTRAINT producer_payables_kind_check
            CHECK (kind IN ('SETTLEMENT', 'ADJUSTMENT'));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'producer_payables_adjustment_has_a_reason') THEN
        ALTER TABLE producer_payables ADD CONSTRAINT producer_payables_adjustment_has_a_reason
            CHECK (kind <> 'ADJUSTMENT' OR (reason IS NOT NULL AND reason <> ''));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'producer_payables_only_adjustments_adjust') THEN
        ALTER TABLE producer_payables ADD CONSTRAINT producer_payables_only_adjustments_adjust
            CHECK (adjusts_payable_id IS NULL OR kind = 'ADJUSTMENT');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'producer_payables_adjustment_is_not_zero') THEN
        ALTER TABLE producer_payables ADD CONSTRAINT producer_payables_adjustment_is_not_zero
            CHECK (kind <> 'ADJUSTMENT' OR net_minor_units <> 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'producer_payables_settlement_gross_is_not_negative') THEN
        ALTER TABLE producer_payables ADD CONSTRAINT producer_payables_settlement_gross_is_not_negative
            CHECK (kind <> 'SETTLEMENT' OR gross_minor_units >= 0);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'producer_payables_adjustment_deducts_nothing') THEN
        ALTER TABLE producer_payables ADD CONSTRAINT producer_payables_adjustment_deducts_nothing
            CHECK (kind <> 'ADJUSTMENT' OR deducted_minor_units = 0);
    END IF;
    -- Dropped by its generated name: it forbids the negative adjustment that
    -- an overpayment has to be recorded as.
    ALTER TABLE producer_payables DROP CONSTRAINT IF EXISTS producer_payables_gross_minor_units_check;
END
$$;

-- One SETTLEMENT payable per producer per cycle. Two is one producer paid twice
-- for one fortnight.
--
-- A partial unique index rather than a table constraint, because adjustments
-- share the table and there may legitimately be several of them: a fortnight
-- can be wrong more than once. Restricting the rule to the settlement row keeps
-- the guarantee that matters — the fortnight itself is computed once — without
-- forbidding the corrections to it.
--
-- The table constraint it replaces is dropped by name, because a UNIQUE
-- constraint covering every kind would refuse the first adjustment ever raised.
ALTER TABLE producer_payables
    DROP CONSTRAINT IF EXISTS producer_payables_tenant_id_cycle_id_producer_ref_key;
CREATE UNIQUE INDEX IF NOT EXISTS producer_payables_one_settlement_per_producer
    ON producer_payables (tenant_id, cycle_id, producer_ref)
    WHERE kind = 'SETTLEMENT';

CREATE INDEX IF NOT EXISTS producer_payables_by_cycle
    ON producer_payables (tenant_id, cycle_id, producer_ref);
CREATE INDEX IF NOT EXISTS producer_payables_unpaid
    ON producer_payables (tenant_id, cycle_id) WHERE status IN ('PAYABLE', 'APPROVED');

-- Money that has been handed over does not change afterwards.
--
-- Not a status check, which a service can be talked out of, and not a revoked
-- UPDATE grant, which would stop the rows that legitimately still move. A
-- trigger, so the rule holds for whatever reaches the table: the service, a
-- migration, a repair script somebody ran at eleven at night against production
-- because a total looked wrong.
--
-- The only thing a paid payable will accept is a payment reference being filled
-- in — a cheque number that arrived after the payment did.
CREATE OR REPLACE FUNCTION gavya_payable_is_final_once_paid()
RETURNS TRIGGER AS $fn$
BEGIN
    IF OLD.paid_at IS NULL THEN
        RETURN NEW;
    END IF;
    IF NEW.paid_at IS DISTINCT FROM OLD.paid_at
       OR NEW.paid_by IS DISTINCT FROM OLD.paid_by
       OR NEW.net_minor_units IS DISTINCT FROM OLD.net_minor_units
       OR NEW.gross_minor_units IS DISTINCT FROM OLD.gross_minor_units
       OR NEW.deducted_minor_units IS DISTINCT FROM OLD.deducted_minor_units
       OR NEW.status IS DISTINCT FROM OLD.status
       OR NEW.producer_ref IS DISTINCT FROM OLD.producer_ref
       OR NEW.cycle_id IS DISTINCT FROM OLD.cycle_id THEN
        RAISE EXCEPTION 'payable % was paid on % and cannot be changed; a payment that '
                        'turned out to be wrong is corrected by a further payment, not by '
                        'editing the record of the one that happened',
            OLD.id, OLD.paid_at
            USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END
$fn$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS producer_payables_final_once_paid ON producer_payables;
CREATE TRIGGER producer_payables_final_once_paid
    BEFORE UPDATE ON producer_payables
    FOR EACH ROW EXECUTE FUNCTION gavya_payable_is_final_once_paid();

-- A paid payable is not deleted either. Deleting one loses the record that money
-- left the society, which is the single row an auditor most wants.
CREATE OR REPLACE FUNCTION gavya_payable_is_not_deleted_once_paid()
RETURNS TRIGGER AS $fn$
BEGIN
    IF OLD.paid_at IS NOT NULL THEN
        RAISE EXCEPTION 'payable % was paid on % and cannot be deleted',
            OLD.id, OLD.paid_at USING ERRCODE = '23514';
    END IF;
    RETURN OLD;
END
$fn$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS producer_payables_not_deleted_once_paid ON producer_payables;
CREATE TRIGGER producer_payables_not_deleted_once_paid
    BEFORE DELETE ON producer_payables
    FOR EACH ROW EXECUTE FUNCTION gavya_payable_is_not_deleted_once_paid();

-- ---------------------------------------------------------------------------
-- Notifications owed and not yet delivered
-- ---------------------------------------------------------------------------

-- What somebody is owed being told, kept until it has been.
--
-- notification-service holds the inbox; this holds the debt. A row is written
-- in the same transaction as the change it describes — a hold, an approval, a
-- payment — so the message exists exactly when the change does. It is delivered
-- afterwards by a sweep in this service, and stays here until notification-
-- service has accepted it.
--
-- Written here rather than sent from the handler because a send from the handler
-- is a message that is lost whenever notification-service is down, and is lost
-- silently: the hold succeeds, the caller sees success, and nobody is told. An
-- outbox turns that outage into a delay.
CREATE TABLE IF NOT EXISTS notification_outbox (
    id             VARCHAR(26) PRIMARY KEY,
    tenant_id      VARCHAR(26) NOT NULL,
    event          VARCHAR(40) NOT NULL,

    -- A role, today. See RecipientsFor in the domain package for why a role and
    -- not a person: nothing links a producer_ref to anyone who can sign in.
    recipient_type VARCHAR(30) NOT NULL,
    recipient_id   VARCHAR(26) NOT NULL,

    title          VARCHAR(300) NOT NULL,
    body           TEXT NOT NULL,
    reference_type VARCHAR(50) NOT NULL,
    reference_id   VARCHAR(26) NOT NULL,

    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by     VARCHAR(26) NOT NULL,

    -- The delivery record. attempts counts every try; last_error is why the
    -- most recent one failed; delivered_at is set once, when it succeeded.
    attempts       INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error     TEXT,
    last_attempt_at TIMESTAMPTZ,
    delivered_at   TIMESTAMPTZ,

    -- A row cannot be delivered without having been tried. The failure this
    -- refuses is a repair script marking things delivered to make a queue look
    -- empty.
    CONSTRAINT notification_outbox_delivery_was_attempted
        CHECK (delivered_at IS NULL OR attempts > 0)
);

CREATE INDEX IF NOT EXISTS notification_outbox_undelivered
    ON notification_outbox (created_at)
    WHERE delivered_at IS NULL;

-- The sweep reads across every tenant, and the tenant isolation policies
-- refuse exactly that: a connection with no tenant set sees nothing, which is
-- the whole point of them. So the sweep asks through a definer-rights function,
-- the way identity-service's pre-authentication lookups do. It returns the rows
-- due for a try — never tried, or tried long enough ago — oldest first, and
-- nothing else about the table is reachable this way. Marking a row delivered
-- or failed happens under that row's tenant, through the ordinary policies.
--
-- The wait between tries grows with the count of failures, ten seconds a time
-- and capped at ten, so a notification-service that is down for an hour is
-- asked every hundred seconds rather than every sweep, and one that is back is
-- caught up with quickly.
CREATE OR REPLACE FUNCTION gavya_settlement_notifications_to_deliver(p_limit integer)
RETURNS SETOF notification_outbox AS $fn$
BEGIN
    RETURN QUERY
        SELECT * FROM notification_outbox
         WHERE delivered_at IS NULL
           AND (last_attempt_at IS NULL
                OR last_attempt_at + (LEAST(attempts, 10) * interval '10 seconds') <= NOW())
         ORDER BY created_at
         LIMIT p_limit;
END
$fn$ LANGUAGE plpgsql STABLE SECURITY DEFINER SET search_path = public;

-- Granted to the application role where that role exists. It exists wherever
-- libs/integrity/isolation has been applied — every deployment — and not in a
-- test database built from this file alone, where the schema-applying user runs
-- the sweep itself.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'gavya_app') THEN
        REVOKE ALL ON FUNCTION gavya_settlement_notifications_to_deliver(integer) FROM PUBLIC;
        GRANT EXECUTE ON FUNCTION gavya_settlement_notifications_to_deliver(integer) TO gavya_app;
    END IF;
END
$$;
