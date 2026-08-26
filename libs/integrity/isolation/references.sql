-- The references the schema names and does not enforce.
--
-- 136 columns in this schema are named for a reference. 46 had a foreign key.
-- The other 90 point at a real table and are guaranteed by nothing: a
-- milk_record could name a cattle_id that was never issued, or one that belonged
-- to another tenant, or one that was deleted last year, and the database would
-- take it.
--
-- That matters more here than in most systems. A settlement is recomputed from
-- collections joined to animals and producers; a collection whose animal does
-- not exist does not fail loudly, it drops out of the join and quietly reduces
-- somebody's payment. The claim this platform makes is that its numbers can be
-- recomputed and defended, and a join that silently loses rows is the shortest
-- path to a number nobody can explain.
--
--
-- WHY THIS ONE IS A LIST
--
-- Almost everything else in this directory is derived from the catalogue rather
-- than written out, because a list is a thing somebody has to remember to update
-- and forgetting is silent. This is the exception, and it is worth saying why.
--
-- Whether a column named for a reference is one cannot be derived. Some are not:
-- a trace identifier, an identifier from somebody else's system, an identifier
-- of a row that is not supposed to exist. Deriving would either add constraints
-- that are wrong or skip ones that are needed, and both are worse than a
-- decision somebody made on purpose.
--
-- So the decisions are written down, and the list is made self-checking instead:
-- gavya_check_reference_decisions fails if a column in
-- gavya_unconstrained_reference_report has no entry here. A new unconstrained
-- reference then forces a decision rather than defaulting to silence, which is
-- the property the derivation was giving us elsewhere.
--
--
-- WHY THE CONSTRAINTS ARE COMPOSITE
--
-- (tenant_id, cattle_id) rather than (cattle_id), for the reason in
-- foreignkeys.sql: a foreign key is checked by the system rather than by the
-- querying role, so row-level security does not stop one tenant referencing
-- another's row, and the constraint then answers whether an identifier exists in
-- another tenant, one probe at a time.

-- ---------------------------------------------------------------------------
-- The decisions
-- ---------------------------------------------------------------------------

-- The views come down before the function they read, or this file cannot be
-- applied a second time.
--
-- It could not. DROP FUNCTION refuses while a view depends on it, so every
-- re-run after the first failed on this line and left everything below it — the
-- decisions, the enforcer, the check — as it was on the first run. A schema
-- change to this file would have applied cleanly to a fresh database and been
-- silently ignored on every existing one, which is the worst way round for a
-- migration to be wrong.
DROP VIEW IF EXISTS gavya_undecided_references;
DROP VIEW IF EXISTS gavya_unguessable_references;
DROP FUNCTION IF EXISTS gavya_reference_decisions();
CREATE FUNCTION gavya_reference_decisions()
RETURNS TABLE(table_name text, column_name text, references_table text, enforce boolean, reason text)
AS $fn$
BEGIN
    RETURN QUERY
    SELECT * FROM (VALUES
        -- Animals. Every one of these is a real reference and every one of them
        -- feeds a settlement or a health record.
        ('breeding_cycles',   'cattle_id',  'cattle',    true,  'a cycle is about an animal'),
        ('calving_records',   'cattle_id',  'cattle',    true,  'a calving is about an animal'),
        ('cattle_listings',   'cattle_id',  'cattle',    true,  'a listing offers an animal'),
        ('cattle_ownership',  'cattle_id',  'cattle',    true,  'ownership is of an animal'),
        ('cattle_sales',      'cattle_id',  'cattle',    true,  'a sale transfers an animal'),
        ('feed_consumption',  'cattle_id',  'cattle',    true,  'feed is consumed by an animal'),
        ('inseminations',     'cattle_id',  'cattle',    true,  'an insemination is of an animal'),
        ('milk_records',      'cattle_id',  'cattle',    true,  'a collection is from an animal'),
        ('milk_sessions',     'cattle_id',  'cattle',    true,  'a session is for an animal'),
        ('nutrition_plans',   'cattle_id',  'cattle',    true,  'a plan is for an animal'),
        ('pregnancies',       'cattle_id',  'cattle',    true,  'a pregnancy is of an animal'),
        ('treatments',        'cattle_id',  'cattle',    true,  'a treatment is of an animal'),
        ('vaccinations',      'cattle_id',  'cattle',    true,  'a vaccination is of an animal'),
        ('vet_visits',        'cattle_id',  'cattle',    true,  'a visit is about an animal'),

        -- An observation may be about something that is not an animal — a tank,
        -- a consignment — so the column is nullable and the reference applies
        -- only when it is set.
        ('observations',      'cattle_id',  'cattle',    true,  'when set, an observation is about an animal'),

        ('cattle',            'farm_id',    'farms',     true,  'an animal is kept at a farm, when one is recorded'),

        -- Stock.
        ('batches',           'sku_id',       'skus',       true, 'a batch is of a stock item'),
        ('batches',           'warehouse_id', 'warehouses', true, 'a batch is held somewhere'),
        ('inventory_items',   'sku_id',       'skus',       true, 'an inventory line is of a stock item'),
        ('stock_movements',   'sku_id',       'skus',       true, 'a movement moves a stock item'),
        ('stock_movements',   'warehouse_id', 'warehouses', true, 'a movement moves it somewhere'),
        ('order_items',       'sku_id',       'skus',       true, 'an order line is for a stock item'),
        ('order_items',       'product_id',   'products',   true, 'an order line is for a product'),

        -- The two that name a rate card and are not references to one, at least
        -- not yet. Both became visible only when procurement-service introduced
        -- a rate_cards table for them to point at, which is this check doing its
        -- job: a new table turned two columns into apparent references and made
        -- somebody decide.
        --
        -- pools.rate_card_id is NOT NULL DEFAULT '', so most pools carry an
        -- empty string. A pool is valued from component prices handed in at the
        -- time; the column records which card those came from when there was
        -- one. Enforcing it would reject every pool that names none, and the fix
        -- is to make the column nullable first -- a change to pooling's schema,
        -- not a constraint to add here.
        ('pools', 'rate_card_id', 'rate_cards', false,
            'NOT NULL DEFAULT empty string, so most pools name no card and a reference would '
            'reject them; make the column nullable before enforcing this'),

        -- shadow_settlement_computations.rate_card_id records which card a
        -- recomputation used. In shadow mode that is frequently the incumbent's
        -- own card, which by definition is not in this platform's registry --
        -- that is the point of shadow mode. A reference here would refuse to
        -- record exactly the comparisons the product exists to make.
        ('shadow_settlement_computations', 'rate_card_id', 'rate_cards', false,
            'a shadow recomputation often cites the incumbent own card, which is not in this '
            'platform registry and is not supposed to be'),

        -- And the one that must not be enforced.
        --
        -- quarantined_records holds what failed validation on the way in, and
        -- the reasons include UNTRUSTED_SESSION_IDENTITY and STALE_GENERATION —
        -- which is to say, data from a device that may not be registered at all.
        -- A foreign key here would refuse to quarantine exactly the records most
        -- worth quarantining, and the data would be dropped instead of held
        -- where somebody can look at it.
        ('quarantined_records', 'device_id', 'devices', false,
            'a quarantined record is one that failed validation, and may name a device that was '
            'never registered — that is often why it was quarantined'),

        -- Settlement.
        --
        -- cycle_lines.collection_id names a row in procurement's
        -- priced_collections. Under the deployment this platform actually runs —
        -- one database, all services — the table is right there and the key
        -- could be enforced. It is not, and the reason is what the column is for.
        --
        -- A cycle line is a copy of what a collection said at the moment the
        -- fortnight was gathered, kept precisely so that a collection corrected
        -- afterwards does not change what a producer was already paid. The id is
        -- the trail back to the source, not a live dependency on it. Enforcing
        -- the key would make the settlement record hostage to a row it has
        -- deliberately stopped reading: a collection deleted for being a
        -- duplicate would take with it the line proving somebody was paid for
        -- it, and the payable would no longer add up.
        --
        -- The uniqueness of collection_id is enforced, which is the property
        -- that matters — one collection reaches one cycle. Its existence is not.
        ('cycle_lines', 'collection_id', 'priced_collections', false,
            'a cycle line is a copy of what a collection said when the fortnight was gathered, '
            'kept so a later correction cannot change what a producer was already paid; the id '
            'is a trail back to the source, not a live dependency on it'),

        -- The rest are enforced. Each is a composite key inside settlement''s own
        -- tables and each is declared in its schema, so these entries record the
        -- decision rather than create the constraint.
        ('cycle_lines', 'cycle_id', 'payment_cycles', true,
            'a line belongs to the cycle that gathered it'),
        ('cycle_deductions', 'cycle_id', 'payment_cycles', true,
            'a deduction was taken in a cycle'),
        ('cycle_deductions', 'recovery_id', 'recoveries', true,
            'a deduction is money taken against a specific debt, and one that names a debt that '
            'does not exist is money taken for no reason anybody can find'),
        ('producer_payables', 'cycle_id', 'payment_cycles', true,
            'a payable is what one producer takes home from one cycle')
    ) AS t(table_name, column_name, references_table, enforce, reason);
END
$fn$ LANGUAGE plpgsql IMMUTABLE;

COMMENT ON FUNCTION gavya_reference_decisions() IS
    'Which reference-shaped columns are references, decided by a person rather than derived.';

-- ---------------------------------------------------------------------------
-- Applying them
-- ---------------------------------------------------------------------------

DROP FUNCTION IF EXISTS gavya_enforce_references(text);
CREATE FUNCTION gavya_enforce_references(p_schema text DEFAULT 'public')
RETURNS TABLE(constraint_name text, outcome text) AS $fn$
DECLARE
    d         record;
    v_name    text;
    v_orphans bigint;
BEGIN
    FOR d IN SELECT * FROM gavya_reference_decisions() WHERE enforce ORDER BY table_name, column_name
    LOOP
        v_name := left(d.table_name || '_' || d.column_name || '_fkey', 63);

        IF NOT EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
                       WHERE n.nspname = p_schema AND c.relname = d.table_name) THEN
            constraint_name := v_name;
            outcome := format('skipped: %I.%I does not exist here', p_schema, d.table_name);
            RETURN NEXT;
            CONTINUE;
        END IF;

        IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = v_name) THEN
            constraint_name := v_name;
            outcome := 'already enforced';
            RETURN NEXT;
            CONTINUE;
        END IF;

        -- Counted before the constraint is attempted. ALTER TABLE would fail on
        -- the first violating row and name only that one, which tells an
        -- operator nothing about whether this is one bad import or half the
        -- table.
        EXECUTE format(
            'SELECT count(*) FROM %I.%I s WHERE s.%I IS NOT NULL AND NOT EXISTS ('
            '  SELECT 1 FROM %I.%I t WHERE t.tenant_id = s.tenant_id AND t.id = s.%I)',
            p_schema, d.table_name, d.column_name,
            p_schema, d.references_table, d.column_name)
        INTO v_orphans;

        IF v_orphans > 0 THEN
            constraint_name := v_name;
            outcome := format('REFUSED: %s row(s) in %I.%I name a %I that does not exist in their '
                              'tenant. Those rows are already wrong; adding the constraint would '
                              'hide them behind a migration failure instead of showing them.',
                              v_orphans, p_schema, d.table_name, d.references_table);
            RETURN NEXT;
            CONTINUE;
        END IF;

        -- The target needs a unique key on (tenant_id, id) to point at. The
        -- foreign-key converter adds these, so this is usually a no-op.
        BEGIN
            EXECUTE format('ALTER TABLE %I.%I ADD CONSTRAINT %I UNIQUE (tenant_id, id)',
                           p_schema, d.references_table,
                           left(d.references_table || '_tenant_id_key', 63));
        EXCEPTION WHEN duplicate_table OR duplicate_object THEN
            NULL;
        END;

        EXECUTE format(
            'ALTER TABLE %I.%I ADD CONSTRAINT %I FOREIGN KEY (tenant_id, %I) '
            'REFERENCES %I.%I (tenant_id, id) ON UPDATE NO ACTION ON DELETE NO ACTION',
            p_schema, d.table_name, v_name, d.column_name,
            p_schema, d.references_table);

        constraint_name := v_name;
        outcome := format('now (tenant_id, %I) -> %I (tenant_id, id)', d.column_name, d.references_table);
        RETURN NEXT;
    END LOOP;
END
$fn$ LANGUAGE plpgsql;

COMMENT ON FUNCTION gavya_enforce_references(text) IS
    'Adds the tenant-safe foreign keys the decisions call for, refusing where the data already violates them.';

-- ---------------------------------------------------------------------------
-- Keeping the list honest
-- ---------------------------------------------------------------------------

-- gavya_check_reference_decisions lists reference-shaped columns that nobody has
-- decided about.
--
-- This is what stops the list rotting. Everything else in this directory is
-- derived so that a new table cannot be missed; here the decision cannot be
-- derived, so instead a missing decision is made loud. A column that turns up
-- here is not necessarily a problem — it may well not be a reference — but it is
-- a question somebody has to answer rather than one that answers itself.
CREATE VIEW gavya_undecided_references AS
SELECT r.schema_name, r.table_name, r.column_name, r.probably_references
FROM gavya_unconstrained_reference_report r
WHERE r.probably_references IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM gavya_reference_decisions() d
      WHERE d.table_name = r.table_name AND d.column_name = r.column_name);

-- The blind spot in the view above, made countable.
--
-- gavya_undecided_references only considers a column whose name matches a table
-- name, because that is the only case where the target can be guessed. That
-- leaves it silent about every reference-shaped column whose target is named
-- irregularly — and irregular naming is the norm: cattle.owner_id points at no
-- table called "owners", and cycle_lines.collection_id points at
-- priced_collections.
--
-- At the time of writing that is 63 columns against 26, so the check that exists
-- to make a missing decision loud was quiet about seventy per cent of them. Many
-- of the 63 are genuinely not references — trace_id, source_record_id,
-- external_id — but that is the argument for somebody deciding, not for the
-- question never being asked.
--
-- This is a report rather than a failure. Deciding all of them is a piece of
-- work across every service in the platform, and a deployment that refused to
-- finish until it was done would simply be switched off. What it must not do is
-- report a number that understates the gap by seventy per cent.
CREATE VIEW gavya_unguessable_references AS
SELECT r.schema_name, r.table_name, r.column_name
FROM gavya_unconstrained_reference_report r
WHERE r.probably_references IS NULL
  AND NOT EXISTS (
      SELECT 1 FROM gavya_reference_decisions() d
      WHERE d.table_name = r.table_name AND d.column_name = r.column_name);
