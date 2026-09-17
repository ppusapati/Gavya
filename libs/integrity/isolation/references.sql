-- The references the schema names and does not enforce.
--
-- 153 columns in this schema end in _id. A reference-shaped column with no
-- foreign key is guaranteed by nothing: a milk_record could name a cattle_id
-- that was never issued, or one that belonged to another tenant, or one that was
-- deleted last year, and the database would take it.
--
-- Every one of them now has a decision recorded below — 101 decisions, 31
-- enforced and 70 not, and every one of the 70 for a stated reason rather than by
-- omission. Two of them name no _id at all and no view will ever ask about
-- them; they are recorded because a comment in the schema says they are.
--
-- Those figures are a snapshot and will drift. The views are not:
-- gavya_unconstrained_reference_report is every reference-shaped column nothing
-- enforces, and gavya_undecided_references and gavya_unguessable_references are
-- the ones with no decision. When this comment and those views disagree, the
-- views are right.
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
            'a payable is what one producer takes home from one cycle'),

        -- -------------------------------------------------------------------
        -- The sixty-three the guess could not reach
        -- -------------------------------------------------------------------
        --
        -- gavya_undecided_references only asks about a column whose name
        -- matches a table name, and that is the minority. These are the rest,
        -- decided one at a time. Five are enforced. The other fifty-eight are
        -- not, and the reasons fall into a small number of shapes that are
        -- worth naming, because each shape is a thing about this platform
        -- rather than an omission.

        -- SHAPE 1: an identifier in somebody else's system.
        --
        -- There is no table here to reference and there never will be. A
        -- foreign key would require this platform to hold a registry of every
        -- row in every system it imports from, which is the opposite of what
        -- importing is.
        ('audit_logs', 'trace_id', '', false,
            'a distributed trace, not a row: it correlates log lines across services and outlives none of them'),
        ('capture_sessions', 'external_session_id', '', false,
            'the session id the vendor device issued, which is how a re-imported capture is recognised'),
        ('captured_records', 'external_session_id', '', false,
            'the vendor device session a record arrived in'),
        ('quarantined_records', 'external_session_id', '', false,
            'a quarantined record names the session it failed in, and that session may itself be unregistered'),
        ('external_identities', 'external_id', '', false,
            'the identifier in the other system: this table exists to map it, so enforcing it would be circular'),
        ('external_settlement_assertions', 'external_settlement_id', '', false,
            'the incumbent settlement being recomputed, identified as the incumbent identifies it'),
        ('external_settlement_assertions', 'source_record_id', '', false,
            'the line of the file this arrived on'),
        ('observations', 'source_record_id', '', false,
            'the line of the file this arrived on'),
        ('priced_collections', 'source_record_id', '', false,
            'the line of the file this arrived on'),
        ('producer_milk', 'origin_source_record_id', '', false,
            'the line of the file this arrived on'),
        ('verification_certificates', 'source_record_id', '', false,
            'the line of the file this arrived on'),
        ('observations', 'uncertainty_model_id', '', false,
            'the version of a model that produced an uncertainty budget, which is a released artefact and not a row'),

        -- SHAPE 2: a registry this platform does not keep.
        --
        -- source_system_id and import_batch_id name things that exist — an
        -- incumbent system, a run of an importer — and there is no table for
        -- either. That is a gap worth closing rather than a decision to be
        -- pleased with: an import batch nobody can look up is an import batch
        -- nobody can re-run or attribute. Recorded as not-enforced so it is
        -- visible, not so it is settled.
        ('external_identities', 'source_system_id', '', false,
            'no registry of source systems exists yet; this is a gap rather than a decision'),
        ('external_settlement_assertions', 'source_system_id', '', false,
            'no registry of source systems exists yet'),
        ('observations', 'source_system_id', '', false,
            'no registry of source systems exists yet'),
        ('priced_collections', 'source_system_id', '', false,
            'no registry of source systems exists yet'),
        ('producer_milk', 'origin_source_system_id', '', false,
            'no registry of source systems exists yet'),
        ('verification_certificates', 'source_system_id', '', false,
            'no registry of source systems exists yet'),
        ('external_settlement_assertions', 'import_batch_id', '', false,
            'no registry of import batches exists yet; an unlookupable batch cannot be re-run or attributed'),
        ('observations', 'import_batch_id', '', false,
            'no registry of import batches exists yet'),
        ('priced_collections', 'import_batch_id', '', false,
            'no registry of import batches exists yet'),
        ('producer_milk', 'origin_import_batch_id', '', false,
            'no registry of import batches exists yet'),
        ('verification_certificates', 'import_batch_id', '', false,
            'no registry of import batches exists yet'),

        -- SHAPE 3: a derivation, which is a computation rather than a record.
        --
        -- A derived value names the computation that produced it. Those are not
        -- stored as rows today. Same status as shape 2: a gap, recorded.
        ('observations', 'derivation_id', '', false,
            'no registry of derivations exists yet; a derived value naming an unlookupable computation cannot be re-derived'),
        ('pool_valuations', 'origin_derivation_id', '', false,
            'no registry of derivations exists yet'),
        ('producer_economic_events', 'origin_derivation_id', '', false,
            'no registry of derivations exists yet'),
        ('producer_milk', 'origin_derivation_id', '', false,
            'no registry of derivations exists yet'),
        ('shadow_settlement_computations', 'derivation_id', '', false,
            'no registry of derivations exists yet'),
        ('verification_certificates', 'derivation_id', '', false,
            'no registry of derivations exists yet'),

        -- SHAPE 4: polymorphic — the target is chosen by a sibling column.
        --
        -- A foreign key names one table. These name whichever table the
        -- accompanying kind column says, so no single key can express them and
        -- adding one would be picking a favourite target and refusing the rest.
        ('audit_logs', 'resource_id', '', false,
            'the target is whatever resource_type says, and a foreign key names one table'),
        ('file_records', 'entity_id', '', false,
            'the target is whatever entity_type says'),
        ('external_identities', 'entity_id', '', false,
            'the target is whatever entity_kind says: producer, cattle, route, centre, device or settlement'),
        ('invoices', 'reference_id', '', false,
            'the target is whatever reference_type says: an order, a subscription, or nothing'),
        ('notifications', 'reference_id', '', false,
            'the target is whatever the notification is about'),
        ('stock_movements', 'reference_id', '', false,
            'the target is whatever caused the movement'),

        -- SHAPE 5: a subject with no registry.
        --
        -- An observation is about a producer, a route or a tanker, and the
        -- platform has a table for none of them. These are the society's own
        -- numbering, resolved through canonical-service rather than held here.
        -- Enforcing would require inventing registries the product has not
        -- decided the shape of.
        ('observations', 'producer_id', '', false,
            'the society''s own member numbering; there is no producer table and canonical-service is what resolves these'),
        ('observations', 'route_id', '', false,
            'the society''s own route numbering; there is no route table'),
        ('observations', 'tanker_id', '', false,
            'the society''s own tanker numbering; there is no tanker table'),
        ('inseminations', 'semen_batch_id', '', false,
            'a straw from a semen station, identified as the station identifies it; there is no semen batch table'),

        -- SHAPE 6: the balance network, where the empty string means something.
        --
        -- from_node_id and to_node_id are empty at the system boundary — milk
        -- arriving from outside the network being balanced, or leaving it. A
        -- foreign key would refuse exactly the flows that cross the boundary,
        -- which are the ones a transit-loss calculation is about.
        ('flow_measurements', 'from_node_id', '', false,
            'the empty node id is the system boundary, and a key would refuse every flow that crosses it'),
        ('flow_measurements', 'to_node_id', '', false,
            'the empty node id is the system boundary'),
        ('flow_measurements', 'flow_id', '', false,
            'the flow''s own identity, shared by the measurements of it, rather than a row elsewhere'),
        ('reconciled_flows', 'flow_id', '', false,
            'the flow''s own identity, shared with the measurements it reconciles'),

        -- SHAPE 7: a person, and this platform has no register of people.
        --
        -- users exists, and it is login accounts. A cattle owner, a
        -- veterinarian, a buyer at a market, a customer being invoiced — most
        -- of them never log in, and requiring an account before a vet can be
        -- recorded as having visited would be the software dictating the world.
        --
        -- So none of these are enforced, and that is the largest single gap in
        -- this list. The answer is a party register distinct from login
        -- accounts, which is a product decision rather than a schema one. Until
        -- there is one, a mistyped owner id names nobody and nothing objects.
        ('audit_logs', 'actor_id', 'users', false,
            'an actor may be a person or a service identity, and services have no users row'),
        ('cattle', 'owner_id', 'users', false,
            'an owner is a person, and most of them never log in; there is no party register'),
        ('cattle_ownership', 'owner_id', 'users', false,
            'an owner is a person, and there is no party register distinct from login accounts'),
        ('cattle_bids', 'bidder_id', 'users', false,
            'a bidder at a cattle market is not necessarily a platform user'),
        ('cattle_listings', 'seller_id', 'users', false,
            'a seller is not necessarily a platform user'),
        ('cattle_sales', 'buyer_id', 'users', false,
            'a buyer is not necessarily a platform user'),
        ('cattle_sales', 'seller_id', 'users', false,
            'a seller is not necessarily a platform user'),
        ('farms', 'manager_id', 'users', false,
            'a farm manager may be recorded before they have an account, or may never have one'),
        ('warehouses', 'manager_id', 'users', false,
            'a warehouse manager may be recorded before they have an account'),
        ('invoices', 'customer_id', 'users', false,
            'a customer being invoiced is not a login account'),
        ('orders', 'customer_id', 'users', false,
            'a customer placing an order is not a login account'),
        ('notifications', 'recipient_id', 'users', false,
            'a recipient may be reached by phone number alone, with no account behind it; and when recipient_type is role it is a role name, for everyone in the tenant who holds it'),
        ('notification_outbox', 'recipient_id', '', false,
            'a role name today (see settlement''s domain.RecipientsFor); a person when somebody links a producer_ref to one'),
        ('notification_outbox', 'reference_id', '', false,
            'the target is whatever reference_type says: a producer_payable or a payment_cycle'),
        ('vaccinations', 'veterinarian_id', 'users', false,
            'requiring an account before a vet can be recorded as having vaccinated would be the software dictating the world'),
        ('vet_visits', 'veterinarian_id', 'users', false,
            'a visiting vet is not a platform user'),

        -- SHAPE 8: a target that exists but is the wrong one.
        --
        -- Worth writing down, because the name invites the mistake. batches is
        -- a warehouse batch — a SKU, a quantity, an expiry — and an
        -- observation''s batch is a batch of milk. Enforcing this would join two
        -- unrelated things and refuse every milk observation.
        ('observations', 'batch_id', '', false,
            'batches is a warehouse SKU batch; an observation''s batch is milk, and they are not the same thing'),
        ('inseminations', 'bull_id', '', false,
            'the method column defaults to AI: the bull is at a semen station and is not this society''s animal'),

        -- -------------------------------------------------------------------
        -- The four that are enforced
        -- -------------------------------------------------------------------
        --
        -- Four out of sixty-three, and the ratio is the finding rather than a
        -- disappointment. Most reference-shaped columns in this schema point at
        -- something outside it — another system's row, a person with no account,
        -- a registry the platform has not built — and a key cannot be added to
        -- any of them by trying harder.
        --
        -- Each of these four names one table that exists, holds one kind of row,
        -- and is nullable, so the record can be written before the thing it
        -- points at is known and has to be real once it is.
        ('calving_records', 'calf_id', 'cattle', true,
            'a calf that has been registered is an animal; null until it is, and then it has to be one that exists'),
        ('cattle_ownership', 'sale_id', 'cattle_sales', true,
            'ownership acquired by purchase names the sale it came from; null for a birth or a gift'),
        ('categories', 'parent_id', 'categories', true,
            'a product category sits under another, and a parent that does not exist is a branch of the tree nothing can reach'),
        ('producer_payables', 'adjusts_payable_id', 'producer_payables', true,
            'a correction names the payment it corrects, and one naming a payment that does not exist is money moved for a reason nobody can check'),

        -- -------------------------------------------------------------------
        -- The masters schema
        -- -------------------------------------------------------------------
        --
        -- Five columns on tables that live in masters rather than public, and
        -- that are declared stubs: gen_ulid_polyfill.sql creates them so that
        -- views written against the coming masters and metasearch services have
        -- something to resolve against, and says so.
        --
        -- One thing stops these being enforced, and it used to be two.
        --
        -- The reason that has gone: gavya_enforce_references took one schema and
        -- the deployment ran it for public, so a decision marked enforce here
        -- would have recorded a guarantee nothing went on to make. It now finds
        -- each table wherever it lives, so that is no longer an obstacle — and
        -- these five are the reason it was noticed before twenty-seven services
        -- moved out of public and hit the same thing at scale.
        --
        -- The reason that remains: they are stubs. The references on
        -- columns_metadata.table_id and tables_metadata.schema_id are real and
        -- belong on the tables that replace these, not on the placeholders. A
        -- key added to a placeholder is a key somebody has to remember to move.
        --
        -- Worth noting separately: a file named gen_ulid_polyfill.sql, whose
        -- stated purpose is to provide one function, also creates six business
        -- tables in another schema. That is how they were missed by an audit
        -- that counted tenant-owned tables in public and found sixty-nine when
        -- there were seventy-five.
        ('items', 'company_id', '', false,
            'no company registry exists; this is a stub table in masters awaiting the masters service'),
        ('items', 'branch_id', '', false,
            'no branch registry exists; stub table in masters'),
        ('chart_of_accounts', 'company_id', '', false,
            'no company registry exists; stub table in masters'),
        ('tables_metadata', 'schema_id', 'schemas_metadata', false,
            'a real reference on a stub table in masters, which enforcement does not reach: it '
            'belongs on the table that replaces this one'),
        ('columns_metadata', 'table_id', 'tables_metadata', false,
            'a real reference on a stub table in masters, which enforcement does not reach: it '
            'belongs on the table that replaces this one'),

        -- -------------------------------------------------------------------
        -- Recorded although nothing demanded it
        -- -------------------------------------------------------------------
        --
        -- lab_samples.source_ref names a collection, a movement, a node or a
        -- batch, chosen by source_kind beside it. It is as much a reference as
        -- anything above and neither view will ever ask about it, because the
        -- report only considers columns ending in _id and this one does not.
        --
        -- That is the naming blind spot again, one level further out: the check
        -- that was silent about sixty-three columns whose targets could not be
        -- guessed is also silent about every reference that is not spelled _id.
        -- Widening it to every column would be guessing at what a reference is,
        -- which is the thing this list exists to avoid.
        --
        -- So this entry is here because the schema comment beside the column
        -- says the decision is recorded here, and a comment that says where to
        -- look should be right.
        ('lab_samples', 'source_ref', '', false,
            'the target is whatever source_kind says: a collection, a movement, a node or a '
            'batch, in four different services'),
        ('lab_results', 'instrument_ref', '', false,
            'the laboratory does not hold the certificate register; the reference is to an '
            'instrument recorded elsewhere, and the calibration it depends on is copied onto '
            'the row rather than joined to'),
        -- production_batches.source_ref is the same shape as lab_samples.source_ref
        -- and recorded for the same reason: the schema comment beside it says the
        -- decision is here, and a comment that says where to look should be right.
        --
        -- The two _id columns on production_inputs are not in this list because
        -- they are not unconstrained: both carry a composite foreign key against
        -- (tenant_id, id), which is what keeps one tenant's batch out of another
        -- tenant's genealogy.
        ('production_batches', 'source_ref', '', false,
            'raw milk points back out of the plant at whatever source_kind says — a movement or '
            'a node, both in material-service — which is where a recall keeps going past the '
            'plant gate'),
        ('production_batches', 'product_ref', '', false,
            'what the plant calls the thing it made, in its own words; deliberately not a '
            'reference, because a closed list here would be this platform deciding what a dairy '
            'is allowed to make')
    ) AS t(table_name, column_name, references_table, enforce, reason);
END
$fn$ LANGUAGE plpgsql IMMUTABLE;

COMMENT ON FUNCTION gavya_reference_decisions() IS
    'Which reference-shaped columns are references, decided by a person rather than derived.';

-- ---------------------------------------------------------------------------
-- Applying them
-- ---------------------------------------------------------------------------

-- It finds each table rather than being told where to look.
--
-- It used to take a schema and default to 'public', and the deployment called it
-- with no argument. Everything else in this layer sweeps every non-system schema
-- — the policies, the grants, the reference report — and this was the one piece
-- that did not, which had two consequences.
--
-- The one that was already true: five reference-shaped columns on the stub
-- tables in `masters` could not be enforced, and the decisions above say so.
-- Recording them as enforced would have been a guarantee nothing went on to
-- make.
--
-- The one that was about to be: giving each service a schema of its own moves
-- twenty-seven services' tables out of `public`, and this would then have
-- enforced nothing for any of them. It would not have failed — it returns
-- 'skipped: ... does not exist here' and the deployment only reads REFUSED — so
-- every declared reference in the platform would have quietly stopped being a
-- key, and the check beside it would have gone on passing.
--
-- A table in two schemas is refused rather than guessed at. That is the
-- collision case tools/dbadmin's coexist check exists for, and enforcing a
-- reference on whichever copy was found first is how one of the two silently
-- gets a constraint the other does not.
DROP FUNCTION IF EXISTS gavya_enforce_references(text);
DROP FUNCTION IF EXISTS gavya_enforce_references();
CREATE FUNCTION gavya_enforce_references()
RETURNS TABLE(constraint_name text, outcome text) AS $fn$
DECLARE
    d         record;
    v_name    text;
    v_orphans bigint;
    p_schema  text;
    v_homes   text[];
BEGIN
    FOR d IN SELECT * FROM gavya_reference_decisions() WHERE enforce ORDER BY table_name, column_name
    LOOP
        v_name := left(d.table_name || '_' || d.column_name || '_fkey', 63);

        SELECT array_agg(n.nspname ORDER BY n.nspname) INTO v_homes
          FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
         WHERE c.relkind = 'r' AND c.relname = d.table_name
           AND n.nspname NOT IN ('pg_catalog', 'information_schema')
           AND n.nspname NOT LIKE 'pg_%';

        IF v_homes IS NULL THEN
            constraint_name := v_name;
            outcome := format('skipped: no table called %I in any schema', d.table_name);
            RETURN NEXT;
            CONTINUE;
        END IF;
        IF array_length(v_homes, 1) > 1 THEN
            constraint_name := v_name;
            outcome := format('REFUSED: %I exists in %s schemas (%s), so there is no one table '
                              'this reference belongs on. Two services defining a table of the '
                              'same name is what tools/dbadmin''s coexist check reports.',
                              d.table_name, array_length(v_homes, 1), array_to_string(v_homes, ', '));
            RETURN NEXT;
            CONTINUE;
        END IF;
        p_schema := v_homes[1];

        -- The target has to be in the same schema. A reference across two
        -- services' namespaces is a coupling nobody declared, and adding it here
        -- would make it permanent before anyone had noticed.
        IF NOT EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
                       WHERE c.relkind = 'r' AND n.nspname = p_schema
                         AND c.relname = d.references_table) THEN
            constraint_name := v_name;
            outcome := format('skipped: %I.%I names %I, which is not in that schema',
                              p_schema, d.table_name, d.references_table);
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

COMMENT ON FUNCTION gavya_enforce_references() IS
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
