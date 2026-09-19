-- A dairy, in full, for a database nobody is being paid by.
--
-- One tenant's worth of coherent data in every table the platform has, so that a
-- fresh database can be opened, clicked through, queried and demonstrated
-- without anybody first having to invent a co-operative.
--
--
-- THIS IS NOT A MIGRATION, AND IT IS DELIBERATELY NOT IN THE MIGRATION PATH
--
-- tools/dbadmin/internal/schema.Plan is the order the platform's SQL is applied
-- in, and this file is not in it. deploy/postgres-init does not run it either.
-- Both of those reach production, and test data that can reach production is a
-- loaded gun: the failure is not that somebody runs it by accident, it is that
-- it runs by default on the one database where "Valley Dairy Co-operative" is
-- not a joke.
--
-- It is applied on purpose, by a command that refuses unless it is told twice:
--
--     go run ./tools/dbadmin/cmd/seed -dsn "$DATABASE_URL" -i-am-not-in-production
--
-- or, if you would rather not go through the command, by psql directly:
--
--     psql -v ON_ERROR_STOP=1 -f deploy/seed/seed.sql "$DATABASE_URL"
--
-- The command exists for the refusals, not for the SQL. There are no psql
-- meta-commands in this file — no \set, no \i — so that the same bytes can be
-- sent by a driver; ON_ERROR_STOP is passed on the command line above rather
-- than set in the file for exactly that reason. It is one transaction either
-- way, so a failure halfway leaves nothing behind.
--
--
-- IT IS RE-RUNNABLE, LIKE EVERY OTHER SQL FILE HERE
--
-- Every insert is ON CONFLICT DO NOTHING against a fixed identifier, so running
-- it twice changes nothing the first run did not already do. The schemas in this
-- platform are re-runnable by design and a seed that was not would be the one
-- file somebody had to think about before running.
--
--
-- THE IDENTIFIERS ARE READABLE, AND THAT IS A CHOICE
--
-- The platform generates ULIDs. These are not ULIDs: they are readable labels
-- padded to the twenty-six characters the columns hold, following the convention
-- already in tools/dbadmin/internal/backup/backup_test.go. It is safe because
-- nothing in the platform parses an identifier it reads back — checked, not
-- assumed — and it is worth doing because the whole point of seed data is that a
-- person can see what they are looking at. CAT_LAKSHMI_00000000000001 in a query
-- result says what 01HGW2NBXP9VTQK8KXGWGZ3JXC does not.
--
--
-- WHAT IT IS AND IS NOT
--
-- It is shaped-valid: every row satisfies the column types, the CHECK
-- constraints, the foreign keys — including the twenty-two that cross services —
-- and row-level security. The database accepted all of it, which is a stronger
-- statement than it sounds, because the database is where this platform's
-- invariants actually live.
--
-- It is not service-produced. A settlement here was written by this file, not
-- computed by settlement-service from these collections, so the arithmetic
-- between tables is hand-made to agree rather than derived. Where a figure is
-- carried from one table to another the comment says so. Read it as a plausible
-- database, not as a recording of the platform running: for the latter, run
-- e2e, which drives the real procedures.
--
--
-- THE SECOND TENANT IS THE POINT OF THE SECOND TENANT
--
-- Hill Creamery exists so that a query which forgets its tenant returns a
-- visibly wrong answer instead of a plausible one. A single-tenant seed cannot
-- distinguish working isolation from absent isolation, which is the failure this
-- platform has already met twice in hand-written queries.

BEGIN;

-- Row-level security is FORCE on every table but three, so the session has to
-- say which tenant it is acting as, the same way every service connection does.
--
-- Everything from here to the Hill Creamery block at the foot belongs to Valley
-- Dairy. That is not decoration: a row inserted under the wrong setting is
-- rejected rather than misfiled, so the block boundary is enforced rather than
-- observed.
--
-- This file is applied on a superuser connection, which is the same connection
-- tools/dbadmin/cmd/migrate already requires, and a superuser bypasses row-level
-- security entirely. The setting is here anyway because it documents which
-- tenant each block writes, and because the identifiers then read correctly if
-- anybody re-runs a statement by hand on a restricted connection.
SET LOCAL app.tenant_id = 'TEN_VALLEY_DAIRY_000000001';

-- ---------------------------------------------------------------------------
-- The co-operative itself
-- ---------------------------------------------------------------------------

SET LOCAL search_path = tenant_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO tenant_service.tenants
    (id, name, slug, plan, status, contact_email, contact_phone, address,
     country, timezone, currency, currency_scale, max_users, max_cattle,
     created_by, updated_by)
VALUES
    ('TEN_VALLEY_DAIRY_000000001', 'Valley Dairy Co-operative', 'valley-dairy',
     'growth', 'active', 'office@valley-dairy.example', '+91 20 5550 0101',
     'Plot 4, MIDC Road, Baramati, Maharashtra', 'India', 'Asia/Kolkata',
     'INR', 2, 40, 500, 'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO tenant_service.tenant_settings
    (id, tenant_id, key, value, data_type, created_by, updated_by)
VALUES
    ('TST_COLLECTION_SHIFTS_0001', 'TEN_VALLEY_DAIRY_000000001',
     'collection.shifts', 'morning,evening', 'string',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('TST_PAYMENT_FORTNIGHT_001', 'TEN_VALLEY_DAIRY_000000001',
     'settlement.cycle_days', '15', 'number',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('TST_SHADOW_MODE_00000001', 'TEN_VALLEY_DAIRY_000000001',
     'settlement.shadow_mode', 'true', 'boolean',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

-- The currency and timezone pins. Each is fixed on first use and refused if it
-- ever changes, so a seed that set them inconsistently would produce a database
-- the services then refuse to write to. They agree with tenants.currency and
-- tenants.timezone above, which is the whole point of a pin.

SET LOCAL search_path = billing_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO billing_service.tenant_currency (tenant_id, currency, currency_scale)
VALUES ('TEN_VALLEY_DAIRY_000000001', 'INR', 2)
ON CONFLICT (tenant_id) DO NOTHING;

SET LOCAL search_path = order_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO order_service.tenant_currency (tenant_id, currency, currency_scale)
VALUES ('TEN_VALLEY_DAIRY_000000001', 'INR', 2)
ON CONFLICT (tenant_id) DO NOTHING;

SET LOCAL search_path = product_catalog_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO product_catalog_service.tenant_currency (tenant_id, currency, currency_scale)
VALUES ('TEN_VALLEY_DAIRY_000000001', 'INR', 2)
ON CONFLICT (tenant_id) DO NOTHING;

SET LOCAL search_path = cattle_market_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO cattle_market_service.tenant_currency (tenant_id, currency, currency_scale)
VALUES ('TEN_VALLEY_DAIRY_000000001', 'INR', 2)
ON CONFLICT (tenant_id) DO NOTHING;

SET LOCAL search_path = health_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO health_service.tenant_currency (tenant_id, currency, currency_scale)
VALUES ('TEN_VALLEY_DAIRY_000000001', 'INR', 2)
ON CONFLICT (tenant_id) DO NOTHING;

SET LOCAL search_path = milk_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO milk_service.tenant_timezone (tenant_id, timezone)
VALUES ('TEN_VALLEY_DAIRY_000000001', 'Asia/Kolkata')
ON CONFLICT (tenant_id) DO NOTHING;


-- ---------------------------------------------------------------------------
-- Who works here
--
-- The roles carry the names libs/integrity/authz knows — collector, clerk,
-- supervisor, accountant, auditor, admin — because a session's permissions are
-- looked up from authz.Roles() by that name. The permissions column stays empty
-- on purpose: it is not what the platform reads, and filling it with a list that
-- nothing consults would be a second answer to the same question, drifting from
-- the first the moment a permission is added.
-- ---------------------------------------------------------------------------

SET LOCAL search_path = identity_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO identity_service.roles
    (id, tenant_id, name, description, is_builtin, created_by, updated_by)
VALUES
    ('ROL_COLLECTOR_00000000001', 'TEN_VALLEY_DAIRY_000000001', 'collector',
     'Records collections at the booth.', true,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('ROL_CLERK_000000000000001', 'TEN_VALLEY_DAIRY_000000001', 'clerk',
     'The society office.', true,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('ROL_SUPERVISOR_0000000001', 'TEN_VALLEY_DAIRY_000000001', 'supervisor',
     'Signs off what the booth recorded.', true,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('ROL_ACCOUNTANT_0000000001', 'TEN_VALLEY_DAIRY_000000001', 'accountant',
     'Approves and pays a settlement.', true,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('ROL_AUDITOR_00000000000001', 'TEN_VALLEY_DAIRY_000000001', 'auditor',
     'Reads everything and changes nothing.', true,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('ROL_ADMIN_000000000000001', 'TEN_VALLEY_DAIRY_000000001', 'admin',
     'Administers the tenant.', true,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

-- Every one of these signs in with the password 'not-a-real-password'.
--
-- A real argon2id hash of that phrase, produced by libs/integrity/credential, so
-- these accounts actually work rather than looking as though they do. The phrase
-- says what it is: a seeded account that could be signed into with a password
-- somebody might use elsewhere is a worse thing to leave lying about than one
-- nobody can sign into at all.
INSERT INTO identity_service.users
    (id, email, email_normalised, full_name, password_hash, status,
     last_login_at, created_by, updated_by)
VALUES
    ('USR_SEED_OPERATOR_00000001', 'seed@valley-dairy.example', 'seed@valley-dairy.example',
     'Seed Operator', NULL, 'active', NULL,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('USR_ANITA_MANAGER_00000001', 'anita@valley-dairy.example', 'anita@valley-dairy.example',
     'Anita Deshmukh', '$argon2id$v=19$m=65536,t=3,p=4$r+ZbPZHwnO3iNxZR+M/hDg$336rA5aZBHLddl/k+64E/iPaiRLKYE2lE1w6zFg0Ix0',
     'active', '2026-09-15 06:05:00+05:30',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('USR_RAVI_COLLECTOR_000001', 'ravi@valley-dairy.example', 'ravi@valley-dairy.example',
     'Ravi Pawar', '$argon2id$v=19$m=65536,t=3,p=4$r+ZbPZHwnO3iNxZR+M/hDg$336rA5aZBHLddl/k+64E/iPaiRLKYE2lE1w6zFg0Ix0',
     'active', '2026-09-15 05:40:00+05:30',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('USR_MEENA_VET_00000000001', 'meena@valley-dairy.example', 'meena@valley-dairy.example',
     'Dr Meena Kulkarni', '$argon2id$v=19$m=65536,t=3,p=4$r+ZbPZHwnO3iNxZR+M/hDg$336rA5aZBHLddl/k+64E/iPaiRLKYE2lE1w6zFg0Ix0',
     'active', '2026-09-12 10:20:00+05:30',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('USR_SUNIL_ACCOUNTS_000001', 'sunil@valley-dairy.example', 'sunil@valley-dairy.example',
     'Sunil Jadhav', '$argon2id$v=19$m=65536,t=3,p=4$r+ZbPZHwnO3iNxZR+M/hDg$336rA5aZBHLddl/k+64E/iPaiRLKYE2lE1w6zFg0Ix0',
     'active', '2026-09-16 09:00:00+05:30',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    -- Suspended, so a list of users has something other than 'active' in it and
    -- the status column is exercised by data rather than by a comment.
    ('USR_PRIYA_LEAVER_0000001', 'priya@valley-dairy.example', 'priya@valley-dairy.example',
     'Priya Shinde', '$argon2id$v=19$m=65536,t=3,p=4$r+ZbPZHwnO3iNxZR+M/hDg$336rA5aZBHLddl/k+64E/iPaiRLKYE2lE1w6zFg0Ix0',
     'suspended', '2026-08-30 08:00:00+05:30',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO identity_service.tenant_memberships
    (id, tenant_id, user_id, role_id, status, is_default, created_by, updated_by)
VALUES
    ('MEM_SEED_OPERATOR_0000001', 'TEN_VALLEY_DAIRY_000000001', 'USR_SEED_OPERATOR_00000001',
     'ROL_ADMIN_000000000000001', 'active', true,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('MEM_ANITA_000000000000001', 'TEN_VALLEY_DAIRY_000000001', 'USR_ANITA_MANAGER_00000001',
     'ROL_SUPERVISOR_0000000001', 'active', true,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('MEM_RAVI_0000000000000001', 'TEN_VALLEY_DAIRY_000000001', 'USR_RAVI_COLLECTOR_000001',
     'ROL_COLLECTOR_00000000001', 'active', true,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('MEM_MEENA_000000000000001', 'TEN_VALLEY_DAIRY_000000001', 'USR_MEENA_VET_00000000001',
     'ROL_CLERK_000000000000001', 'active', true,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('MEM_SUNIL_000000000000001', 'TEN_VALLEY_DAIRY_000000001', 'USR_SUNIL_ACCOUNTS_000001',
     'ROL_ACCOUNTANT_0000000001', 'active', true,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('MEM_PRIYA_000000000000001', 'TEN_VALLEY_DAIRY_000000001', 'USR_PRIYA_LEAVER_0000001',
     'ROL_AUDITOR_00000000000001', 'suspended', true,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

-- The service identity the ingestion path authenticates as. Its secret is a real
-- hash of 'not-a-real-secret', for the same reason the passwords are real.
INSERT INTO identity_service.service_identities
    (id, tenant_id, name, description, secret_hash, status, expires_at,
     last_used_at, created_by, updated_by)
VALUES
    ('SVC_BOOTH_TABLET_0000001', 'TEN_VALLEY_DAIRY_000000001', 'valley-booth-tablet',
     'The tablet at the Baramati booth, posting captures.',
     '$argon2id$v=19$m=65536,t=3,p=4$NOfyNgGFVXkKBhsS9hIxpQ$81J/fjSquWhIz7YkyhYrZt9Lt8kZKv/LZSv0pUCuCVE',
     'active', '2027-09-01 00:00:00+05:30', '2026-09-15 05:38:00+05:30',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

-- One live session per subject kind. auth_sessions_one_subject refuses a row
-- that names both or neither, so these are two rows rather than one.
INSERT INTO identity_service.auth_sessions
    (id, tenant_id, user_id, service_identity_id, issued_at, expires_at,
     ip_address, user_agent, last_seen_at, created_by, updated_by)
VALUES
    ('SES_ANITA_LIVE_000000001', 'TEN_VALLEY_DAIRY_000000001', 'USR_ANITA_MANAGER_00000001',
     NULL, '2026-09-15 06:05:00+05:30', '2027-09-15 06:05:00+05:30',
     '203.0.113.17', 'Gavya Console/1.0', '2026-09-15 06:40:00+05:30',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('SES_BOOTH_LIVE_000000001', 'TEN_VALLEY_DAIRY_000000001', NULL,
     'SVC_BOOTH_TABLET_0000001', '2026-09-15 05:38:00+05:30', '2027-09-15 05:38:00+05:30',
     '198.51.100.9', 'Gavya Booth/2.3 (Android)', '2026-09-15 06:55:00+05:30',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    -- Revoked, so the revocation columns carry something and a session list is
    -- not uniformly live.
    ('SES_PRIYA_REVOKED_000001', 'TEN_VALLEY_DAIRY_000000001', 'USR_PRIYA_LEAVER_0000001',
     NULL, '2026-08-30 08:00:00+05:30', '2027-08-30 08:00:00+05:30',
     '203.0.113.40', 'Gavya Console/1.0', '2026-08-30 17:10:00+05:30',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

UPDATE identity_service.auth_sessions
   SET revoked_at = '2026-08-31 09:00:00+05:30', revoked_reason = 'membership suspended'
 WHERE id = 'SES_PRIYA_REVOKED_000001' AND revoked_at IS NULL;

INSERT INTO identity_service.authentication_attempts
    (id, email_normalised, user_id, tenant_id, succeeded, failure_reason,
     ip_address, user_agent, attempted_at)
VALUES
    ('ATT_ANITA_OK_00000000001', 'anita@valley-dairy.example', 'USR_ANITA_MANAGER_00000001',
     'TEN_VALLEY_DAIRY_000000001', true, NULL,
     '203.0.113.17', 'Gavya Console/1.0', '2026-09-15 06:05:00+05:30'),
    ('ATT_ANITA_TYPO_000000001', 'anita@valley-dairy.example', 'USR_ANITA_MANAGER_00000001',
     'TEN_VALLEY_DAIRY_000000001', false, 'password mismatch',
     '203.0.113.17', 'Gavya Console/1.0', '2026-09-15 06:04:31+05:30'),
    -- An attempt against an address nobody has. user_id is null because there is
    -- no user to point at, which is the case the column is nullable for.
    ('ATT_UNKNOWN_00000000001', 'nobody@example.invalid', NULL,
     'TEN_VALLEY_DAIRY_000000001', false, 'no such user',
     '198.51.100.200', 'curl/8.4.0', '2026-09-14 02:11:09+05:30')
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- The reference tables
--
-- masters is six tables of shared reference data and metadata about the schema
-- itself. Its identifier columns are CHAR(26) rather than VARCHAR(26), so
-- PostgreSQL blank-pads what goes in; the padding is invisible in a comparison
-- and startling in a query result, which is worth knowing before it startles
-- somebody.
-- ---------------------------------------------------------------------------

SET LOCAL search_path = masters, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO masters.uoms (id, tenant_id, uom_code, uom_name) VALUES
    ('UOM_LITRE_00000000000001', 'TEN_VALLEY_DAIRY_000000001', 'L',   'Litre'),
    ('UOM_KILOGRAM_00000000001', 'TEN_VALLEY_DAIRY_000000001', 'KG',  'Kilogram'),
    ('UOM_EACH_000000000000001', 'TEN_VALLEY_DAIRY_000000001', 'EA',  'Each'),
    ('UOM_CRATE_00000000000001', 'TEN_VALLEY_DAIRY_000000001', 'CRT', 'Crate of twelve')
ON CONFLICT (id) DO NOTHING;

INSERT INTO masters.chart_of_accounts (id, tenant_id, company_id, code, name) VALUES
    ('COA_MILK_PURCHASES_00001', 'TEN_VALLEY_DAIRY_000000001', 'CMP_VALLEY_000000000001',
     '5100', 'Milk purchases'),
    ('COA_PRODUCER_PAYABLE_001', 'TEN_VALLEY_DAIRY_000000001', 'CMP_VALLEY_000000000001',
     '2100', 'Producer payable'),
    ('COA_SALES_DAIRY_0000001', 'TEN_VALLEY_DAIRY_000000001', 'CMP_VALLEY_000000000001',
     '4100', 'Dairy sales'),
    ('COA_FEED_RECOVERY_000001', 'TEN_VALLEY_DAIRY_000000001', 'CMP_VALLEY_000000000001',
     '1300', 'Feed credit recoverable')
ON CONFLICT (id) DO NOTHING;

INSERT INTO masters.items (id, tenant_id, company_id, branch_id, item_code, item_name) VALUES
    ('ITM_RAW_MILK_000000001', 'TEN_VALLEY_DAIRY_000000001', 'CMP_VALLEY_000000000001',
     'BRN_BARAMATI_00000000001', 'RAW-MILK', 'Raw milk, pooled'),
    ('ITM_CATTLE_FEED_0000001', 'TEN_VALLEY_DAIRY_000000001', 'CMP_VALLEY_000000000001',
     'BRN_BARAMATI_00000000001', 'FEED-CONC', 'Concentrate feed, 50kg'),
    ('ITM_PACKED_MILK_0000001', 'TEN_VALLEY_DAIRY_000000001', 'CMP_VALLEY_000000000001',
     'BRN_BARAMATI_00000000001', 'PKD-TONED', 'Toned milk, packed')
ON CONFLICT (id) DO NOTHING;

-- The three metadata tables describe the database to whatever reads them. They
-- are seeded consistently with each other — a schema, a table in it, a column in
-- that table — rather than with three unrelated rows, because a chain that does
-- not join is the kind of fixture that makes a bug look like data.
INSERT INTO masters.schemas_metadata (id, tenant_id, schema_name) VALUES
    ('SCM_MILK_SERVICE_0000001', 'TEN_VALLEY_DAIRY_000000001', 'milk_service'),
    ('SCM_CATTLE_SERVICE_00001', 'TEN_VALLEY_DAIRY_000000001', 'cattle_service')
ON CONFLICT (id) DO NOTHING;

INSERT INTO masters.tables_metadata (id, tenant_id, schema_id, table_name) VALUES
    ('TBL_MILK_RECORDS_0000001', 'TEN_VALLEY_DAIRY_000000001',
     'SCM_MILK_SERVICE_0000001', 'milk_records'),
    ('TBL_CATTLE_0000000000001', 'TEN_VALLEY_DAIRY_000000001',
     'SCM_CATTLE_SERVICE_00001', 'cattle')
ON CONFLICT (id) DO NOTHING;

INSERT INTO masters.columns_metadata (id, tenant_id, table_id, column_name) VALUES
    ('CLM_QUANTITY_LITERS_0001', 'TEN_VALLEY_DAIRY_000000001',
     'TBL_MILK_RECORDS_0000001', 'quantity_liters'),
    ('CLM_TAG_NUMBER_000000001', 'TEN_VALLEY_DAIRY_000000001',
     'TBL_CATTLE_0000000000001', 'tag_number')
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- The farm and the herd
-- ---------------------------------------------------------------------------

SET LOCAL search_path = farm_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO farm_service.farms
    (id, tenant_id, name, code, address, city, state, country, capacity,
     manager_id, status, created_by, updated_by)
VALUES
    ('FRM_BARAMATI_00000000001', 'TEN_VALLEY_DAIRY_000000001', 'Baramati Farm', 'BAR-01',
     'Survey 118, Baramati Taluka', 'Baramati', 'Maharashtra', 'India', 240,
     'USR_ANITA_MANAGER_00000001', 'active',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('FRM_INDAPUR_000000000001', 'TEN_VALLEY_DAIRY_000000001', 'Indapur Farm', 'IND-01',
     'Survey 42, Indapur Taluka', 'Indapur', 'Maharashtra', 'India', 90,
     'USR_ANITA_MANAGER_00000001', 'active',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO farm_service.farm_sections
    (id, tenant_id, farm_id, name, section_type, capacity, current_occupancy,
     created_by, updated_by)
VALUES
    ('SEC_BAR_MILKING_0000001', 'TEN_VALLEY_DAIRY_000000001', 'FRM_BARAMATI_00000000001',
     'Milking shed A', 'milking', 120, 96,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('SEC_BAR_DRY_00000000001', 'TEN_VALLEY_DAIRY_000000001', 'FRM_BARAMATI_00000000001',
     'Dry stock pen', 'dry', 60, 28,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('SEC_IND_CALF_000000001', 'TEN_VALLEY_DAIRY_000000001', 'FRM_INDAPUR_000000000001',
     'Calf pen', 'calf', 40, 11,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

SET LOCAL search_path = cattle_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO cattle_service.breeds
    (id, tenant_id, name, origin, description, created_by, updated_by)
VALUES
    ('BRD_GIR_00000000000001', 'TEN_VALLEY_DAIRY_000000001', 'Gir', 'Gujarat, India',
     'Indigenous zebu, heat tolerant, moderate yield at high solids.',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('BRD_HF_CROSS_0000000001', 'TEN_VALLEY_DAIRY_000000001', 'HF Cross', 'Crossbred',
     'Holstein Friesian cross. Higher yield, lower fat.',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('BRD_SAHIWAL_000000000001', 'TEN_VALLEY_DAIRY_000000001', 'Sahiwal', 'Punjab',
     'Indigenous, hardy, good persistency.',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO cattle_service.cattle
    (id, tenant_id, tag_number, name, breed_id, date_of_birth, gender, status,
     weight, color, owner_id, farm_id, created_by, updated_by)
VALUES
    ('CAT_LAKSHMI_000000000001', 'TEN_VALLEY_DAIRY_000000001', 'VD-0001', 'Lakshmi',
     'BRD_GIR_00000000000001', '2020-03-14', 'F', 'active', 412.50, 'Red',
     'USR_ANITA_MANAGER_00000001', 'FRM_BARAMATI_00000000001',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('CAT_GANGA_00000000000001', 'TEN_VALLEY_DAIRY_000000001', 'VD-0002', 'Ganga',
     'BRD_HF_CROSS_0000000001', '2019-11-02', 'F', 'active', 486.00, 'Black and white',
     'USR_ANITA_MANAGER_00000001', 'FRM_BARAMATI_00000000001',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('CAT_KAVERI_0000000000001', 'TEN_VALLEY_DAIRY_000000001', 'VD-0003', 'Kaveri',
     'BRD_SAHIWAL_000000000001', '2021-07-21', 'F', 'active', 395.25, 'Brown',
     'USR_ANITA_MANAGER_00000001', 'FRM_BARAMATI_00000000001',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    -- Dry rather than active: a herd where every animal is milking is a herd
    -- nobody has ever kept.
    ('CAT_TULSI_00000000000001', 'TEN_VALLEY_DAIRY_000000001', 'VD-0004', 'Tulsi',
     'BRD_GIR_00000000000001', '2018-05-30', 'F', 'dry', 430.00, 'Red',
     'USR_ANITA_MANAGER_00000001', 'FRM_INDAPUR_000000000001',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('CAT_BHIMA_BULL_000000001', 'TEN_VALLEY_DAIRY_000000001', 'VD-0005', 'Bhima',
     'BRD_GIR_00000000000001', '2017-01-09', 'M', 'active', 620.00, 'Red',
     'USR_ANITA_MANAGER_00000001', 'FRM_INDAPUR_000000000001',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    -- Lakshmi's heifer calf, born in the calving record further down.
    ('CAT_CALF_ARYA_000000001', 'TEN_VALLEY_DAIRY_000000001', 'VD-0006', 'Arya',
     'BRD_GIR_00000000000001', '2026-06-18', 'F', 'active', 78.40, 'Red',
     'USR_ANITA_MANAGER_00000001', 'FRM_INDAPUR_000000000001',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO cattle_service.cattle_lineage (id, tenant_id, cattle_id, sire_id, dam_id) VALUES
    ('LIN_ARYA_0000000000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_CALF_ARYA_000000001',
     'CAT_BHIMA_BULL_000000001', 'CAT_LAKSHMI_000000000001'),
    -- A dam nobody recorded a sire for, which is the ordinary case for an animal
    -- bought in rather than born here.
    ('LIN_KAVERI_00000000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_KAVERI_0000000000001',
     NULL, NULL)
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- The morning, and the evening
--
-- 15 September 2026, which is the last day of the settlement fortnight further
-- down. The quantities here are the ones the settlement is built from, so
-- changing one without changing the other makes the seed disagree with itself.
-- ---------------------------------------------------------------------------

SET LOCAL search_path = milk_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO milk_service.milk_sessions
    (id, tenant_id, cattle_id, session_date, shift_type, status, created_by, updated_by)
VALUES
    ('MSS_LAKSHMI_AM_00000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_LAKSHMI_000000000001',
     '2026-09-15', 'morning', 'closed',
     'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001'),
    ('MSS_GANGA_AM_000000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_GANGA_00000000000001',
     '2026-09-15', 'morning', 'closed',
     'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001'),
    ('MSS_KAVERI_AM_00000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_KAVERI_0000000000001',
     '2026-09-15', 'morning', 'closed',
     'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001'),
    ('MSS_LAKSHMI_PM_00000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_LAKSHMI_000000000001',
     '2026-09-15', 'evening', 'pending',
     'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO milk_service.milk_records
    (id, tenant_id, session_id, cattle_id, quantity_liters, recorded_at,
     recorded_by, created_by, updated_by)
VALUES
    ('MRC_LAKSHMI_AM_00000001', 'TEN_VALLEY_DAIRY_000000001', 'MSS_LAKSHMI_AM_00000001',
     'CAT_LAKSHMI_000000000001', 6.250, '2026-09-15 06:12:00+05:30',
     'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001'),
    ('MRC_GANGA_AM_000000001', 'TEN_VALLEY_DAIRY_000000001', 'MSS_GANGA_AM_000000001',
     'CAT_GANGA_00000000000001', 9.400, '2026-09-15 06:19:00+05:30',
     'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001'),
    ('MRC_KAVERI_AM_00000001', 'TEN_VALLEY_DAIRY_000000001', 'MSS_KAVERI_AM_00000001',
     'CAT_KAVERI_0000000000001', 5.100, '2026-09-15 06:27:00+05:30',
     'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001'),
    ('MRC_LAKSHMI_PM_00000001', 'TEN_VALLEY_DAIRY_000000001', 'MSS_LAKSHMI_PM_00000001',
     'CAT_LAKSHMI_000000000001', 4.800, '2026-09-15 17:45:00+05:30',
     'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO milk_service.milk_quality
    (id, tenant_id, record_id, fat_percent, snf_percent, lactose, tested_at,
     created_by, updated_by)
VALUES
    ('MQL_LAKSHMI_AM_00000001', 'TEN_VALLEY_DAIRY_000000001', 'MRC_LAKSHMI_AM_00000001',
     4.60, 8.70, 4.90, '2026-09-15 07:05:00+05:30',
     'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001'),
    ('MQL_GANGA_AM_000000001', 'TEN_VALLEY_DAIRY_000000001', 'MRC_GANGA_AM_000000001',
     3.80, 8.40, 4.80, '2026-09-15 07:06:00+05:30',
     'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001'),
    -- Lactose not measured: the analyser at this booth does not report it, and a
    -- zero would be a reading nobody took.
    ('MQL_KAVERI_AM_00000001', 'TEN_VALLEY_DAIRY_000000001', 'MRC_KAVERI_AM_00000001',
     4.90, 8.90, NULL, '2026-09-15 07:07:00+05:30',
     'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001')
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Breeding: one cycle carried all the way to a calf that exists in the herd
-- ---------------------------------------------------------------------------

SET LOCAL search_path = breeding_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO breeding_service.breeding_cycles
    (id, tenant_id, cattle_id, heat_date, status, notes, created_by, updated_by)
VALUES
    ('BCY_LAKSHMI_000000000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_LAKSHMI_000000000001',
     '2025-09-06 07:00:00+05:30', 'calved', 'Standing heat observed at the morning milking.',
     'USR_MEENA_VET_00000000001', 'USR_MEENA_VET_00000000001'),
    ('BCY_KAVERI_00000000000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_KAVERI_0000000000001',
     '2026-09-02 06:30:00+05:30', 'heat', 'First observed heat this season.',
     'USR_MEENA_VET_00000000001', 'USR_MEENA_VET_00000000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO breeding_service.inseminations
    (id, tenant_id, cycle_id, cattle_id, bull_id, semen_batch_id, inseminated_at,
     method, created_by, updated_by)
VALUES
    ('INS_LAKSHMI_000000000001', 'TEN_VALLEY_DAIRY_000000001', 'BCY_LAKSHMI_000000000001',
     'CAT_LAKSHMI_000000000001', 'CAT_BHIMA_BULL_000000001', 'SEM_GIR_2025_0000000001',
     '2025-09-07 09:15:00+05:30', 'AI',
     'USR_MEENA_VET_00000000001', 'USR_MEENA_VET_00000000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO breeding_service.pregnancies
    (id, tenant_id, cattle_id, insemination_id, confirmed_at, expected_calving_date,
     status, created_by, updated_by)
VALUES
    ('PRG_LAKSHMI_000000000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_LAKSHMI_000000000001',
     'INS_LAKSHMI_000000000001', '2025-10-19 11:00:00+05:30', '2026-06-16 00:00:00+05:30',
     'calved', 'USR_MEENA_VET_00000000001', 'USR_MEENA_VET_00000000001')
ON CONFLICT (id) DO NOTHING;

-- calf_id points at Arya in cattle_service, which is one of the twenty-two
-- references that cross a service boundary.
INSERT INTO breeding_service.calving_records
    (id, tenant_id, pregnancy_id, cattle_id, calf_id, calving_date, calf_gender,
     calf_weight, complications, status, created_by, updated_by)
VALUES
    ('CLV_LAKSHMI_000000000001', 'TEN_VALLEY_DAIRY_000000001', 'PRG_LAKSHMI_000000000001',
     'CAT_LAKSHMI_000000000001', 'CAT_CALF_ARYA_000000001', '2026-06-18 04:40:00+05:30',
     'F', 27.60, NULL, 'normal',
     'USR_MEENA_VET_00000000001', 'USR_MEENA_VET_00000000001')
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Health
-- ---------------------------------------------------------------------------

SET LOCAL search_path = health_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO health_service.vet_visits
    (id, tenant_id, cattle_id, veterinarian_id, visit_date, purpose, notes, cost,
     currency, created_by, updated_by)
VALUES
    ('VST_LAKSHMI_000000000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_LAKSHMI_000000000001',
     'USR_MEENA_VET_00000000001', '2026-06-18 06:00:00+05:30',
     'Post-calving check', 'Uterine involution normal. No retained placenta.',
     450.0000, 'INR', 'USR_MEENA_VET_00000000001', 'USR_MEENA_VET_00000000001'),
    ('VST_KAVERI_00000000000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_KAVERI_0000000000001',
     'USR_MEENA_VET_00000000001', '2026-09-10 16:30:00+05:30',
     'Lameness, left hind', 'Sole ulcer. Trimmed and dressed; recheck in ten days.',
     620.0000, 'INR', 'USR_MEENA_VET_00000000001', 'USR_MEENA_VET_00000000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO health_service.vaccinations
    (id, tenant_id, cattle_id, vaccine_name, batch_number, administered_at,
     next_due_date, veterinarian_id, dosage, created_by, updated_by)
VALUES
    ('VAC_LAKSHMI_FMD_0000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_LAKSHMI_000000000001',
     'Foot and Mouth Disease', 'FMD-2026-114', '2026-04-02 09:00:00+05:30',
     '2026-10-02 00:00:00+05:30', 'USR_MEENA_VET_00000000001', '2 ml subcutaneous',
     'USR_MEENA_VET_00000000001', 'USR_MEENA_VET_00000000001'),
    ('VAC_GANGA_FMD_00000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_GANGA_00000000000001',
     'Foot and Mouth Disease', 'FMD-2026-114', '2026-04-02 09:12:00+05:30',
     '2026-10-02 00:00:00+05:30', 'USR_MEENA_VET_00000000001', '2 ml subcutaneous',
     'USR_MEENA_VET_00000000001', 'USR_MEENA_VET_00000000001'),
    ('VAC_ARYA_BQ_0000000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_CALF_ARYA_000000001',
     'Black Quarter', 'BQ-2026-031', '2026-08-20 08:30:00+05:30',
     '2027-08-20 00:00:00+05:30', 'USR_MEENA_VET_00000000001', '5 ml subcutaneous',
     'USR_MEENA_VET_00000000001', 'USR_MEENA_VET_00000000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO health_service.treatments
    (id, tenant_id, cattle_id, diagnosis_code, diagnosis, medicine_name, dosage,
     treated_at, treated_by, follow_up_date, status, created_by, updated_by)
VALUES
    ('TRT_KAVERI_LAME_000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_KAVERI_0000000000001',
     'LAME-SOLE', 'Sole ulcer, left hind claw.', 'Oxytetracycline spray',
     'Topical, once daily', '2026-09-10 16:45:00+05:30', 'USR_MEENA_VET_00000000001',
     '2026-09-20 00:00:00+05:30', 'ongoing',
     'USR_MEENA_VET_00000000001', 'USR_MEENA_VET_00000000001'),
    ('TRT_GANGA_MASTITIS_0001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_GANGA_00000000000001',
     'MAST-SUB', 'Subclinical mastitis, right fore quarter.', 'Intramammary cephalosporin',
     '1 tube per quarter, three days', '2026-08-28 18:00:00+05:30',
     'USR_MEENA_VET_00000000001', NULL, 'resolved',
     'USR_MEENA_VET_00000000001', 'USR_MEENA_VET_00000000001')
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Feed
-- ---------------------------------------------------------------------------

SET LOCAL search_path = feed_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO feed_service.feed_types
    (id, tenant_id, name, category, unit, nutritional_info, created_by, updated_by)
VALUES
    ('FDT_CONCENTRATE_000001', 'TEN_VALLEY_DAIRY_000000001', 'Dairy concentrate',
     'concentrate', 'kg', 'CP 22%, TDN 72%',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('FDT_GREEN_FODDER_00001', 'TEN_VALLEY_DAIRY_000000001', 'Napier grass',
     'green fodder', 'kg', 'CP 9%, DM 20%',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('FDT_DRY_FODDER_000001', 'TEN_VALLEY_DAIRY_000000001', 'Wheat straw',
     'dry fodder', 'kg', 'CP 3.5%, DM 90%',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO feed_service.nutrition_plans
    (id, tenant_id, cattle_id, feed_type_id, daily_quantity_kg, start_date, end_date,
     notes, created_by, updated_by)
VALUES
    ('NPL_LAKSHMI_CONC_00001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_LAKSHMI_000000000001',
     'FDT_CONCENTRATE_000001', 4.500, '2026-06-20 00:00:00+05:30', NULL,
     'Lactation ration, reviewed fortnightly.',
     'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001'),
    ('NPL_GANGA_CONC_0000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_GANGA_00000000000001',
     'FDT_CONCENTRATE_000001', 6.000, '2026-05-01 00:00:00+05:30', NULL,
     'Higher yield, higher ration.',
     'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001'),
    -- Ended: a plan that stopped, so the end_date column is not uniformly null.
    ('NPL_TULSI_DRY_00000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_TULSI_00000000000001',
     'FDT_DRY_FODDER_000001', 8.000, '2026-07-01 00:00:00+05:30',
     '2026-09-01 00:00:00+05:30', 'Dry period ration; ended at drying off.',
     'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO feed_service.feed_consumption
    (id, tenant_id, cattle_id, feed_type_id, quantity_kg, fed_at, fed_by,
     created_by, updated_by)
VALUES
    ('FCN_LAKSHMI_20260915_1', 'TEN_VALLEY_DAIRY_000000001', 'CAT_LAKSHMI_000000000001',
     'FDT_CONCENTRATE_000001', 4.500, '2026-09-15 05:30:00+05:30',
     'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001'),
    ('FCN_GANGA_20260915_001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_GANGA_00000000000001',
     'FDT_CONCENTRATE_000001', 6.000, '2026-09-15 05:34:00+05:30',
     'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001'),
    ('FCN_KAVERI_20260915_01', 'TEN_VALLEY_DAIRY_000000001', 'CAT_KAVERI_0000000000001',
     'FDT_GREEN_FODDER_00001', 22.000, '2026-09-15 05:40:00+05:30',
     'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001')
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- What the society sells
-- ---------------------------------------------------------------------------

SET LOCAL search_path = product_catalog_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO product_catalog_service.brands
    (id, tenant_id, name, slug, logo_url, created_by, updated_by)
VALUES
    ('BRN_VALLEY_FRESH_00001', 'TEN_VALLEY_DAIRY_000000001', 'Valley Fresh', 'valley-fresh',
     NULL, 'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

-- A parent and a child, so a category tree has an edge in it rather than being
-- a flat list that happens to have a parent_id column.
INSERT INTO product_catalog_service.categories
    (id, tenant_id, name, slug, parent_id, description, sort_order, created_by, updated_by)
VALUES
    ('CTG_DAIRY_00000000000001', 'TEN_VALLEY_DAIRY_000000001', 'Dairy', 'dairy', NULL,
     'Everything made from the day''s milk.', 1,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('CTG_LIQUID_MILK_000001', 'TEN_VALLEY_DAIRY_000000001', 'Liquid milk', 'liquid-milk',
     'CTG_DAIRY_00000000000001', 'Packed milk by fat grade.', 1,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('CTG_FATS_0000000000001', 'TEN_VALLEY_DAIRY_000000001', 'Fats', 'fats',
     'CTG_DAIRY_00000000000001', 'Ghee and butter.', 2,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO product_catalog_service.products
    (id, tenant_id, category_id, brand_id, name, slug, description, product_type,
     status, created_by, updated_by)
VALUES
    ('PRD_TONED_MILK_000001', 'TEN_VALLEY_DAIRY_000000001', 'CTG_LIQUID_MILK_000001',
     'BRN_VALLEY_FRESH_00001', 'Toned milk', 'toned-milk',
     'Standardised to 3.0 per cent fat, 8.5 SNF.', 'simple', 'active',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('PRD_COW_GHEE_00000001', 'TEN_VALLEY_DAIRY_000000001', 'CTG_FATS_0000000000001',
     'BRN_VALLEY_FRESH_00001', 'Cow ghee', 'cow-ghee',
     'Clarified from the society''s own cream.', 'simple', 'active',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    -- Discontinued, so status is a column with more than one value in it.
    ('PRD_FLAVOURED_MILK_001', 'TEN_VALLEY_DAIRY_000000001', 'CTG_LIQUID_MILK_000001',
     'BRN_VALLEY_FRESH_00001', 'Rose flavoured milk', 'rose-flavoured-milk',
     'Withdrawn after the 2025 season.', 'simple', 'discontinued',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO product_catalog_service.skus
    (id, tenant_id, product_id, code, name, price, currency, unit, unit_size,
     status, created_by, updated_by)
VALUES
    ('SKU_TONED_500ML_000001', 'TEN_VALLEY_DAIRY_000000001', 'PRD_TONED_MILK_000001',
     'VF-TM-500', 'Toned milk 500 ml', 27.0000, 'INR', 'ml', 500.000, 'active',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('SKU_TONED_1L_00000001', 'TEN_VALLEY_DAIRY_000000001', 'PRD_TONED_MILK_000001',
     'VF-TM-1000', 'Toned milk 1 litre', 52.0000, 'INR', 'ml', 1000.000, 'active',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('SKU_GHEE_500ML_000001', 'TEN_VALLEY_DAIRY_000000001', 'PRD_COW_GHEE_00000001',
     'VF-GH-500', 'Cow ghee 500 ml', 410.0000, 'INR', 'ml', 500.000, 'active',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Where it is kept
-- ---------------------------------------------------------------------------

SET LOCAL search_path = inventory_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO inventory_service.warehouses
    (id, tenant_id, name, code, address, manager_id, status, created_by, updated_by)
VALUES
    ('WHS_BARAMATI_COLD_0001', 'TEN_VALLEY_DAIRY_000000001', 'Baramati cold store', 'WH-BAR',
     'Plot 4, MIDC Road, Baramati', 'USR_ANITA_MANAGER_00000001', 'active',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('WHS_PUNE_DEPOT_000001', 'TEN_VALLEY_DAIRY_000000001', 'Pune depot', 'WH-PUN',
     'Unit 7, Hadapsar Industrial Estate, Pune', 'USR_ANITA_MANAGER_00000001', 'active',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO inventory_service.inventory_items
    (id, tenant_id, warehouse_id, sku_id, quantity_on_hand, quantity_reserved,
     reorder_point, max_stock, created_by, updated_by)
VALUES
    ('INV_BAR_TONED500_00001', 'TEN_VALLEY_DAIRY_000000001', 'WHS_BARAMATI_COLD_0001',
     'SKU_TONED_500ML_000001', 1840.000, 100.000, 400.000, 4000.000,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('INV_BAR_GHEE500_000001', 'TEN_VALLEY_DAIRY_000000001', 'WHS_BARAMATI_COLD_0001',
     'SKU_GHEE_500ML_000001', 212.000, 20.000, 60.000, 600.000,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    -- Below its reorder point, so a low-stock report has something to find.
    ('INV_PUN_TONED1L_000001', 'TEN_VALLEY_DAIRY_000000001', 'WHS_PUNE_DEPOT_000001',
     'SKU_TONED_1L_00000001', 96.000, 0.000, 250.000, 2500.000,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO inventory_service.batches
    (id, tenant_id, warehouse_id, sku_id, batch_number, quantity, manufactured_at,
     expires_at, status, created_by, updated_by)
VALUES
    ('BAT_TONED_20260915_01', 'TEN_VALLEY_DAIRY_000000001', 'WHS_BARAMATI_COLD_0001',
     'SKU_TONED_500ML_000001', 'TM-20260915-A', 1840.000,
     '2026-09-15 09:00:00+05:30', '2026-09-18 09:00:00+05:30', 'available',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('BAT_GHEE_20260901_01', 'TEN_VALLEY_DAIRY_000000001', 'WHS_BARAMATI_COLD_0001',
     'SKU_GHEE_500ML_000001', 'GH-20260901-A', 212.000,
     '2026-09-01 11:00:00+05:30', '2027-03-01 11:00:00+05:30', 'available',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    -- Expired stock that nobody has written off yet, which is the state a stock
    -- report exists to surface.
    ('BAT_TONED_20260908_01', 'TEN_VALLEY_DAIRY_000000001', 'WHS_PUNE_DEPOT_000001',
     'SKU_TONED_1L_00000001', 'TM-20260908-B', 96.000,
     '2026-09-08 09:00:00+05:30', '2026-09-11 09:00:00+05:30', 'expired',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO inventory_service.stock_movements
    (id, tenant_id, warehouse_id, sku_id, movement_type, quantity, reference_id,
     reference_type, notes, moved_at, moved_by, created_by, updated_by)
VALUES
    ('STM_TONED_IN_0000001', 'TEN_VALLEY_DAIRY_000000001', 'WHS_BARAMATI_COLD_0001',
     'SKU_TONED_500ML_000001', 'in', 1840.000, 'BAT_TONED_20260915_01', 'batch',
     'Morning packing run.', '2026-09-15 09:30:00+05:30',
     'USR_ANITA_MANAGER_00000001', 'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('STM_GHEE_OUT_000001', 'TEN_VALLEY_DAIRY_000000001', 'WHS_BARAMATI_COLD_0001',
     'SKU_GHEE_500ML_000001', 'out', 20.000, 'ORD_2026_00000000000001', 'order',
     'Picked for the Pune order.', '2026-09-16 10:15:00+05:30',
     'USR_ANITA_MANAGER_00000001', 'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('STM_TONED_XFER_00001', 'TEN_VALLEY_DAIRY_000000001', 'WHS_PUNE_DEPOT_000001',
     'SKU_TONED_1L_00000001', 'transfer', 96.000, 'WHS_BARAMATI_COLD_0001', 'warehouse',
     'Depot replenishment.', '2026-09-08 14:00:00+05:30',
     'USR_ANITA_MANAGER_00000001', 'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    -- An adjustment states the count after a stocktake and replaces the running
    -- total, rather than moving it by a delta — see the switch in
    -- inventory-service's repository/movement.go. So this is 96, the figure
    -- somebody counted on the shelf, and it is the figure in inventory_items
    -- above. A seed that wrote the four missing packs here instead would look
    -- right and would set the depot's stock to four.
    ('STM_TONED_ADJ_000001', 'TEN_VALLEY_DAIRY_000000001', 'WHS_PUNE_DEPOT_000001',
     'SKU_TONED_1L_00000001', 'adjustment', 96.000, NULL, NULL,
     'Stocktake: four packs damaged in handling, counted 96.', '2026-09-09 08:20:00+05:30',
     'USR_ANITA_MANAGER_00000001', 'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- The order book
--
-- The arithmetic is made to agree by hand: each item's total_price is quantity
-- times unit_price, the order's sub_total is the sum of them, tax_amount is each
-- item's line total at its own rate, and total_amount is the two added. Nothing
-- computed these — this file wrote them — so they are checked here by being
-- written once and referenced, rather than restated.
--
--   100 x toned 500 ml @ 27.0000 = 2700.0000, at 5 per cent  =  135.0000
--    20 x ghee  500 ml @ 410.0000 = 8200.0000, at 12 per cent =  984.0000
--                       sub-total  10900.0000   tax 1119.0000   total 12019.0000
-- ---------------------------------------------------------------------------

SET LOCAL search_path = order_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO order_service.orders
    (id, tenant_id, customer_id, order_number, status, sub_total, tax_amount,
     total_amount, currency, shipping_address, notes, ordered_at, delivered_at,
     tax_inclusive, created_by, updated_by)
VALUES
    ('ORD_2026_00000000000001', 'TEN_VALLEY_DAIRY_000000001', 'CUS_PUNE_RETAIL_000001',
     'ORD-2026-0001', 'delivered', 10900.0000, 1119.0000, 12019.0000, 'INR',
     'Shop 3, Hadapsar Market, Pune', 'Standing weekly order.',
     '2026-09-16 08:00:00+05:30', '2026-09-16 15:30:00+05:30', false,
     'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001'),
    ('ORD_2026_00000000000002', 'TEN_VALLEY_DAIRY_000000001', 'CUS_BARAMATI_CAFE_0001',
     'ORD-2026-0002', 'draft', 0.0000, 0.0000, 0.0000, 'INR',
     'Café Sahyadri, Station Road, Baramati', 'Quote requested, not yet confirmed.',
     '2026-09-17 11:00:00+05:30', NULL, false,
     'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO order_service.order_items
    (id, tenant_id, order_id, sku_id, product_id, quantity, unit_price, total_price,
     status, tax_rate, created_by, updated_by)
VALUES
    ('OIT_2026_0001_000001', 'TEN_VALLEY_DAIRY_000000001', 'ORD_2026_00000000000001',
     'SKU_TONED_500ML_000001', 'PRD_TONED_MILK_000001', 100.000, 27.0000, 2700.0000,
     'delivered', 5.000, 'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001'),
    ('OIT_2026_0001_000002', 'TEN_VALLEY_DAIRY_000000001', 'ORD_2026_00000000000001',
     'SKU_GHEE_500ML_000001', 'PRD_COW_GHEE_00000001', 20.000, 410.0000, 8200.0000,
     'delivered', 12.000, 'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO order_service.invoices
    (id, tenant_id, order_id, invoice_number, status, sub_total, tax_amount,
     total_amount, currency, issued_at, due_at, paid_at, created_by, updated_by)
VALUES
    ('OIN_2026_0001_00000001', 'TEN_VALLEY_DAIRY_000000001', 'ORD_2026_00000000000001',
     'INV-ORD-2026-0001', 'paid', 10900.0000, 1119.0000, 12019.0000, 'INR',
     '2026-09-16 16:00:00+05:30', '2026-09-30 16:00:00+05:30', '2026-09-17 09:12:00+05:30',
     'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

-- Two ghee packs went back. 2 x 410.0000 = 820.0000 plus 12 per cent = 918.4000.
INSERT INTO order_service.returns
    (id, tenant_id, order_id, reason, status, refund_amount, currency,
     requested_at, processed_at, created_by, updated_by)
VALUES
    ('RET_2026_0001_0000001', 'TEN_VALLEY_DAIRY_000000001', 'ORD_2026_00000000000001',
     'Two ghee tins with a damaged seal.', 'completed', 918.4000, 'INR',
     '2026-09-17 10:00:00+05:30', '2026-09-17 14:20:00+05:30',
     'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Billing, which is the other side of the house: feed sold to a member
--
--   2 x 50 kg concentrate @ 1250.0000 = 2500.0000, at 5 per cent = 125.0000
--                                        total 2625.0000, paid in full
-- ---------------------------------------------------------------------------

SET LOCAL search_path = billing_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO billing_service.invoices
    (id, tenant_id, customer_id, invoice_number, reference_id, reference_type,
     status, sub_total, tax_amount, total_amount, currency, issued_at, due_at,
     paid_at, notes, tax_inclusive, created_by, updated_by)
VALUES
    ('BIN_FEED_2026_0001_01', 'TEN_VALLEY_DAIRY_000000001', 'USR_ANITA_MANAGER_00000001',
     'INV-FEED-2026-0001', NULL, NULL, 'paid', 2500.0000, 125.0000, 2625.0000, 'INR',
     '2026-09-05 10:00:00+05:30', '2026-09-20 10:00:00+05:30', '2026-09-08 11:30:00+05:30',
     'Concentrate against the member''s feed credit.', false,
     'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001'),
    -- Unpaid and past its due date, so an ageing report has a row.
    ('BIN_FEED_2026_0002_01', 'TEN_VALLEY_DAIRY_000000001', 'USR_RAVI_COLLECTOR_000001',
     'INV-FEED-2026-0002', NULL, NULL, 'issued', 1200.0000, 60.0000, 1260.0000, 'INR',
     '2026-08-20 10:00:00+05:30', '2026-09-04 10:00:00+05:30', NULL,
     'Outstanding past due.', false,
     'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO billing_service.invoice_items
    (id, tenant_id, invoice_id, description, quantity, unit_price, total_price,
     tax_rate, created_by, updated_by)
VALUES
    ('BII_FEED_0001_000001', 'TEN_VALLEY_DAIRY_000000001', 'BIN_FEED_2026_0001_01',
     'Dairy concentrate, 50 kg bag', 2.000, 1250.0000, 2500.0000, 5.000,
     'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001'),
    ('BII_FEED_0002_000001', 'TEN_VALLEY_DAIRY_000000001', 'BIN_FEED_2026_0002_01',
     'Wheat straw, 100 kg', 1.000, 1200.0000, 1200.0000, 5.000,
     'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO billing_service.payments
    (id, tenant_id, invoice_id, amount, currency, payment_method, reference_no,
     paid_at, notes, created_by, updated_by)
VALUES
    ('PAY_FEED_0001_000001', 'TEN_VALLEY_DAIRY_000000001', 'BIN_FEED_2026_0001_01',
     2625.0000, 'INR', 'upi', 'UPI/2026/0908/44311', '2026-09-08 11:30:00+05:30',
     'Paid in full.', 'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- The cattle market
-- ---------------------------------------------------------------------------

SET LOCAL search_path = cattle_market_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO cattle_market_service.cattle_listings
    (id, tenant_id, cattle_id, seller_id, title, description, asking_price,
     currency, listing_type, status, expires_at, created_by, updated_by)
VALUES
    ('LST_TULSI_0000000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_TULSI_00000000000001',
     'USR_ANITA_MANAGER_00000001', 'Gir cow, dry, third lactation',
     'Sound feet, good temperament. Dried off 1 September.',
     48000.0000, 'INR', 'negotiable', 'active', '2026-10-15 00:00:00+05:30',
     'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001'),
    ('LST_BHIMA_0000000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_BHIMA_BULL_000000001',
     'USR_ANITA_MANAGER_00000001', 'Gir bull, proven',
     'Sire of the 2026 heifer crop. Offered at auction.',
     92000.0000, 'INR', 'auction', 'active', '2026-10-01 00:00:00+05:30',
     'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO cattle_market_service.cattle_bids
    (id, tenant_id, listing_id, bidder_id, bid_amount, currency, status, message,
     created_by, updated_by)
VALUES
    ('BID_TULSI_00000000001', 'TEN_VALLEY_DAIRY_000000001', 'LST_TULSI_0000000001',
     'USR_RAVI_COLLECTOR_000001', 44000.0000, 'INR', 'accepted',
     'Can collect on Saturday.', 'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001'),
    ('BID_TULSI_00000000002', 'TEN_VALLEY_DAIRY_000000001', 'LST_TULSI_0000000001',
     'USR_SUNIL_ACCOUNTS_000001', 41000.0000, 'INR', 'rejected',
     'Below the reserve.', 'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001'),
    ('BID_BHIMA_00000000001', 'TEN_VALLEY_DAIRY_000000001', 'LST_BHIMA_0000000001',
     'USR_RAVI_COLLECTOR_000001', 88000.0000, 'INR', 'pending',
     'Subject to a vet check.', 'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001')
ON CONFLICT (id) DO NOTHING;

-- The accepted bid became a sale, at the bid price rather than the asking price.
INSERT INTO cattle_market_service.cattle_sales
    (id, tenant_id, listing_id, seller_id, buyer_id, cattle_id, sale_price,
     currency, sale_date, transfer_date, status, created_by, updated_by)
VALUES
    ('SAL_TULSI_00000000001', 'TEN_VALLEY_DAIRY_000000001', 'LST_TULSI_0000000001',
     'USR_ANITA_MANAGER_00000001', 'USR_RAVI_COLLECTOR_000001', 'CAT_TULSI_00000000000001',
     44000.0000, 'INR', '2026-09-17 12:00:00+05:30', NULL, 'pending',
     'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO cattle_market_service.cattle_ownership
    (id, tenant_id, cattle_id, owner_id, acquired_at, released_at, acquisition_type,
     sale_id, created_by, updated_by)
VALUES
    ('OWN_LAKSHMI_00000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_LAKSHMI_000000000001',
     'USR_ANITA_MANAGER_00000001', '2020-03-14 00:00:00+05:30', NULL, 'born', NULL,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('OWN_GANGA_0000000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_GANGA_00000000000001',
     'USR_ANITA_MANAGER_00000001', '2021-02-08 00:00:00+05:30', NULL, 'purchased', NULL,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    -- Tulsi's ownership has not been released, because the sale above has not
    -- transferred: transfer_date is null and status is pending. The two tables
    -- say the same thing, which is the point of seeding them together.
    ('OWN_TULSI_0000000001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_TULSI_00000000000001',
     'USR_ANITA_MANAGER_00000001', '2018-05-30 00:00:00+05:30', NULL, 'born',
     'SAL_TULSI_00000000001', 'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Telling people, reporting, and the files
-- ---------------------------------------------------------------------------

SET LOCAL search_path = notification_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO notification_service.notification_templates
    (id, tenant_id, event_type, channel, title, body_template, is_active,
     created_by, updated_by)
VALUES
    ('NTP_PAYABLE_APPROVED_1', 'TEN_VALLEY_DAIRY_000000001', 'settlement.payable.approved',
     'sms', 'Payment approved',
     'Your payment of {{amount}} for {{period}} has been approved.', true,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('NTP_PAYABLE_HELD_00001', 'TEN_VALLEY_DAIRY_000000001', 'settlement.payable.held',
     'sms', 'Payment held',
     'Your payment for {{period}} is held: {{reason}}. Please see the society office.',
     true, 'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('NTP_PAYABLE_PAID_00001', 'TEN_VALLEY_DAIRY_000000001', 'settlement.payable.paid',
     'in_app', 'Payment sent',
     'Your payment of {{amount}} was sent on {{date}}, reference {{reference}}.',
     true, 'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO notification_service.notifications
    (id, tenant_id, recipient_id, recipient_type, channel, title, body, status,
     priority, reference_id, reference_type, sent_at, read_at, created_by, updated_by)
VALUES
    ('NOT_RAVI_APPROVED_0001', 'TEN_VALLEY_DAIRY_000000001', 'USR_RAVI_COLLECTOR_000001',
     'user', 'sms', 'Payment approved',
     'Your payment of INR 693.60 for 1-15 September has been approved.',
     'sent', 'normal', 'PBL_RAVI_SEP1_0001', 'producer_payable',
     '2026-09-17 09:30:00+05:30', NULL,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('NOT_ANITA_HELD_0000001', 'TEN_VALLEY_DAIRY_000000001', 'USR_ANITA_MANAGER_00000001',
     'user', 'in_app', 'Payment held',
     'A payment for 1-15 September is held pending a lab dispute.',
     'sent', 'high', 'PBL_MEENA_SEP1_001', 'producer_payable',
     '2026-09-17 09:31:00+05:30', '2026-09-17 09:48:00+05:30',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    -- Queued and not yet sent, which is the state the outbox sweep exists for.
    ('NOT_SUNIL_PENDING_0001', 'TEN_VALLEY_DAIRY_000000001', 'USR_SUNIL_ACCOUNTS_000001',
     'user', 'email', 'Fortnight closed',
     'The 1-15 September cycle is gathered and awaiting approval.',
     'pending', 'normal', 'CYC_SEP_FIRST_00001', 'payment_cycle', NULL, NULL,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

SET LOCAL search_path = reporting_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO reporting_service.reports
    (id, tenant_id, name, report_type, parameters, status, file_path, file_format,
     requested_by, started_at, completed_at, created_by, updated_by)
VALUES
    ('RPT_YIELD_SEP_0000001', 'TEN_VALLEY_DAIRY_000000001', 'Daily yield, September',
     'daily_yield', '{"from":"2026-09-01","to":"2026-09-15"}'::jsonb, 'completed',
     '/reports/valley/daily-yield-2026-09.csv', 'csv', 'USR_ANITA_MANAGER_00000001',
     '2026-09-16 07:00:00+05:30', '2026-09-16 07:00:12+05:30',
     'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001'),
    -- Failed, with no file. A reports table where everything succeeded is one
    -- nobody has run in anger.
    ('RPT_SETTLEMENT_FAIL_01', 'TEN_VALLEY_DAIRY_000000001', 'Settlement summary',
     'settlement_summary', '{"cycle":"CYC_SEP_FIRST_00001"}'::jsonb, 'failed',
     NULL, NULL, 'USR_SUNIL_ACCOUNTS_000001',
     '2026-09-17 08:00:00+05:30', '2026-09-17 08:00:03+05:30',
     'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO reporting_service.report_schedules
    (id, tenant_id, report_type, schedule, parameters, is_active, last_run_at,
     next_run_at, created_by, updated_by)
VALUES
    ('RSC_YIELD_DAILY_00001', 'TEN_VALLEY_DAIRY_000000001', 'daily_yield', '0 7 * * *',
     '{"window":"yesterday"}'::jsonb, true,
     '2026-09-18 07:00:00+05:30', '2026-09-19 07:00:00+05:30',
     'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001'),
    ('RSC_SETTLE_FORTNIGHT1', 'TEN_VALLEY_DAIRY_000000001', 'settlement_summary',
     '0 8 1,16 * *', '{"window":"last_cycle"}'::jsonb, false, NULL,
     '2026-10-01 08:00:00+05:30',
     'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

SET LOCAL search_path = file_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO file_service.file_records
    (id, tenant_id, original_name, stored_name, content_type, size_bytes,
     storage_path, storage_provider, entity_type, entity_id, uploaded_by,
     is_public, created_by, updated_by)
VALUES
    ('FIL_LAB_CERT_0000001', 'TEN_VALLEY_DAIRY_000000001', 'analyser-cert-2026.pdf',
     '01JA7K2QF9-analyser-cert-2026.pdf', 'application/pdf', 184320,
     'valley/certificates/01JA7K2QF9-analyser-cert-2026.pdf', 's3',
     'instrument', 'INST_ANALYSER_0000001', 'USR_ANITA_MANAGER_00000001', false,
     'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001'),
    ('FIL_LAKSHMI_PHOTO_001', 'TEN_VALLEY_DAIRY_000000001', 'lakshmi.jpg',
     '01JA7K3RB2-lakshmi.jpg', 'image/jpeg', 512044,
     'valley/cattle/01JA7K3RB2-lakshmi.jpg', 's3',
     'cattle', 'CAT_LAKSHMI_000000000001', 'USR_RAVI_COLLECTOR_000001', true,
     'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001'),
    -- Attached to nothing: an upload that happened before anybody said what it
    -- was for, which is why entity_type and entity_id are nullable.
    ('FIL_SCAN_UNFILED_0001', 'TEN_VALLEY_DAIRY_000000001', 'scan-0042.pdf',
     '01JA7K4TC5-scan-0042.pdf', 'application/pdf', 96010,
     'valley/inbox/01JA7K4TC5-scan-0042.pdf', 'local', NULL, NULL,
     'USR_SUNIL_ACCOUNTS_000001', false,
     'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- The integrity spine, part one: what was observed, and by what
--
-- An observation carries where it came from, whether the instrument that took it
-- was eligible at the time, and what is known about its uncertainty. The
-- uncertainty columns are deliberately left unfilled on two of these three and
-- uncertainty_missing stays true, because instrument uncertainty by measurement
-- method is an empirical property of a specific instrument in a specific plant
-- and this repository does not invent those. Seeding a confident figure here
-- would be inventing exactly the thing it has refused to invent everywhere else.
-- ---------------------------------------------------------------------------

SET LOCAL search_path = observation_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO observation_service.instruments
    (id, tenant_id, serial, kind, label, make, model, created_by)
VALUES
    ('INST_ANALYSER_0000001', 'TEN_VALLEY_DAIRY_000000001', 'EKO-2291-A', 'MILK_ANALYSER',
     'Baramati booth analyser', 'Ekomilk', 'Total M',
     'USR_SEED_OPERATOR_00000001'),
    ('INST_WEIGHBRIDGE_0001', 'TEN_VALLEY_DAIRY_000000001', 'WB-BAR-01', 'WEIGHBRIDGE',
     'Plant weighbridge', 'Avery', 'ZM303',
     'USR_SEED_OPERATOR_00000001'),
    ('INST_THERMOMETER_0001', 'TEN_VALLEY_DAIRY_000000001', 'TH-BAR-07', 'THERMOMETER',
     'Bulk cooler probe', 'Testo', '104',
     'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO observation_service.verification_certificates
    (id, tenant_id, instrument_id, certificate_number, verifying_authority,
     issued_at, expires_at, origin_kind, created_by)
VALUES
    ('VCT_ANALYSER_00000001', 'TEN_VALLEY_DAIRY_000000001', 'INST_ANALYSER_0000001',
     'LM/MH/2026/11842', 'Legal Metrology, Maharashtra',
     '2026-01-15 00:00:00+05:30', '2027-01-14 23:59:59+05:30', 'NATIVE',
     'USR_SEED_OPERATOR_00000001'),
    -- Expired eleven days before the collections below, which is why one of them
    -- is NOT_ELIGIBLE rather than merely unverified.
    ('VCT_WEIGHBRIDGE_00001', 'TEN_VALLEY_DAIRY_000000001', 'INST_WEIGHBRIDGE_0001',
     'LM/MH/2025/09117', 'Legal Metrology, Maharashtra',
     '2025-09-04 00:00:00+05:30', '2026-09-04 23:59:59+05:30', 'NATIVE',
     'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO observation_service.observations
    (id, tenant_id, cattle_id, tanker_id, batch_id, quantity_kind, value, unit,
     instrument_id, session_ref, observed_by, origin_kind, valid_from,
     eligibility_verdict, eligibility_reason, eligibility_certificate_id,
     uncertainty_missing, created_by)
VALUES
    -- exactly_one_subject: a reading is about one thing. An animal, or a tanker,
    -- or a batch, or a producer, or a route — never two. The constraint refuses
    -- a row that names an animal and a producer both, which is what a first
    -- draft of this file did.
    ('OBS_LAKSHMI_VOL_00001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_LAKSHMI_000000000001',
     NULL, NULL, 'VOLUME_LITRES', 6.250000, 'L',
     'INST_ANALYSER_0000001', 'CSN_BOOTH_20260915_01', 'USR_RAVI_COLLECTOR_000001',
     'NATIVE', '2026-09-15 06:12:00+05:30',
     'ELIGIBLE', 'Analyser verification valid until 14 January 2027.',
     'VCT_ANALYSER_00000001', true, 'USR_RAVI_COLLECTOR_000001'),
    ('OBS_LAKSHMI_FAT_00001', 'TEN_VALLEY_DAIRY_000000001', 'CAT_LAKSHMI_000000000001',
     NULL, NULL, 'FAT_PERCENT', 4.600000, 'PCT',
     'INST_ANALYSER_0000001', 'CSN_BOOTH_20260915_01', 'USR_RAVI_COLLECTOR_000001',
     'NATIVE', '2026-09-15 06:12:00+05:30',
     'ELIGIBLE', 'Analyser verification valid until 14 January 2027.',
     'VCT_ANALYSER_00000001', true, 'USR_RAVI_COLLECTOR_000001'),
    -- Taken on the weighbridge whose certificate expired on 4 September. The
    -- reading exists and is recorded; what it is not is eligible to be paid on.
    ('OBS_TANKER_MASS_00001', 'TEN_VALLEY_DAIRY_000000001', NULL,
     'TNK_MH12_AB_4471_0001', NULL, 'MASS_KG', 4182.400000, 'KG',
     'INST_WEIGHBRIDGE_0001', 'CSN_BOOTH_20260915_01', 'USR_ANITA_MANAGER_00000001',
     'NATIVE', '2026-09-15 08:40:00+05:30',
     'NOT_ELIGIBLE', 'Weighbridge verification expired on 4 September 2026.',
     'VCT_WEIGHBRIDGE_00001', true, 'USR_ANITA_MANAGER_00000001'),
    -- A cooler temperature, which is a reading and not a quantity: four degrees
    -- is the ordinary case and a cold chain runs below zero, which is why the
    -- rule that refuses a negative quantity does not reach this quantity kind.
    ('OBS_COOLER_TEMP_00001', 'TEN_VALLEY_DAIRY_000000001', NULL,
     NULL, 'BCH_BULK_20260915_01', 'TEMPERATURE_C', 3.800000, 'C',
     'INST_THERMOMETER_0001', 'CSN_BOOTH_20260915_01', 'USR_RAVI_COLLECTOR_000001',
     'NATIVE', '2026-09-15 07:30:00+05:30',
     'ELIGIBLE', 'Probe within calibration.', NULL, true, 'USR_RAVI_COLLECTOR_000001')
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- The integrity spine, part two: what the booth actually sent
--
-- A device has a generation, which goes up whenever the device's identity could
-- have been reset underneath it; a session belongs to a device and a generation;
-- a record belongs to a session and carries a sequence. A record that arrives
-- out of that order is not accepted and corrected, it is quarantined, and the
-- quarantined row below is a real one of those rather than an empty table.
-- ---------------------------------------------------------------------------

SET LOCAL search_path = ingestion_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO ingestion_service.devices
    (id, tenant_id, serial, kind, label, current_generation, created_by, updated_by)
VALUES
    ('DEV_BOOTH_TABLET_0001', 'TEN_VALLEY_DAIRY_000000001', 'TAB-BAR-0001', 'MOBILE_APP',
     'Baramati booth tablet', 2,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('DEV_ANALYSER_0000001', 'TEN_VALLEY_DAIRY_000000001', 'EKO-2291-A', 'MILK_ANALYSER',
     'Baramati booth analyser', 1,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

-- Generation 1 was closed and generation 2 opened when the tablet was
-- reinstalled. The closed generation is kept: it is what makes a record arriving
-- late from the old install identifiable as stale rather than merely odd.
INSERT INTO ingestion_service.device_generations
    (id, tenant_id, device_id, generation, reason, opened_at, closed_at, created_by)
VALUES
    ('GEN_TABLET_1_0000001', 'TEN_VALLEY_DAIRY_000000001', 'DEV_BOOTH_TABLET_0001', 1,
     'INITIAL_PROVISIONING', '2026-01-04 09:00:00+05:30', '2026-08-11 16:20:00+05:30',
     'USR_SEED_OPERATOR_00000001'),
    ('GEN_TABLET_2_0000001', 'TEN_VALLEY_DAIRY_000000001', 'DEV_BOOTH_TABLET_0001', 2,
     'APP_REINSTALL', '2026-08-11 16:40:00+05:30', NULL,
     'USR_SEED_OPERATOR_00000001'),
    ('GEN_ANALYSER_1_00001', 'TEN_VALLEY_DAIRY_000000001', 'DEV_ANALYSER_0000001', 1,
     'INITIAL_PROVISIONING', '2026-01-04 09:05:00+05:30', NULL,
     'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO ingestion_service.capture_sessions
    (id, tenant_id, device_id, generation, external_session_id, operator_ref, status,
     opened_at, closed_at, last_sequence, record_count, created_by, updated_by)
VALUES
    ('CSN_BOOTH_20260915_1', 'TEN_VALLEY_DAIRY_000000001', 'DEV_BOOTH_TABLET_0001', 2,
     'CSN_BOOTH_20260915_01', 'USR_RAVI_COLLECTOR_000001', 'CLOSED',
     '2026-09-15 05:38:00+05:30', '2026-09-15 07:20:00+05:30', 3, 3,
     'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001'),
    ('CSN_BOOTH_20260915_2', 'TEN_VALLEY_DAIRY_000000001', 'DEV_BOOTH_TABLET_0001', 2,
     'CSN_BOOTH_20260915_02', 'USR_RAVI_COLLECTOR_000001', 'OPEN',
     '2026-09-15 17:30:00+05:30', NULL, 0, 0,
     'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO ingestion_service.captured_records
    (id, tenant_id, device_id, generation, session_id, external_session_id, sequence,
     payload_hash, payload, captured_at, created_by)
VALUES
    ('CRC_20260915_000001', 'TEN_VALLEY_DAIRY_000000001', 'DEV_BOOTH_TABLET_0001', 2,
     'CSN_BOOTH_20260915_1', 'CSN_BOOTH_20260915_01', 1,
     'sha256:2f1c0b8a4d5e6f7081920a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f',
     '{"producer":"PRD_RAVI_HOUSEHOLD_0001","litres":"6.250","fat":"4.60","snf":"8.70"}'::jsonb,
     '2026-09-15 06:12:00+05:30', 'USR_RAVI_COLLECTOR_000001'),
    ('CRC_20260915_000002', 'TEN_VALLEY_DAIRY_000000001', 'DEV_BOOTH_TABLET_0001', 2,
     'CSN_BOOTH_20260915_1', 'CSN_BOOTH_20260915_01', 2,
     'sha256:3a2b1c0d9e8f7060514233221100ffeeddccbbaa99887766554433221100ffee',
     '{"producer":"PRD_MEENA_HOUSE_000001","litres":"9.400","fat":"3.80","snf":"8.40"}'::jsonb,
     '2026-09-15 06:19:00+05:30', 'USR_RAVI_COLLECTOR_000001'),
    ('CRC_20260915_000003', 'TEN_VALLEY_DAIRY_000000001', 'DEV_BOOTH_TABLET_0001', 2,
     'CSN_BOOTH_20260915_1', 'CSN_BOOTH_20260915_01', 3,
     'sha256:44556677889900aabbccddeeff00112233445566778899aabbccddeeff001122',
     '{"producer":"PRD_SUNIL_HOUSE_000001","litres":"5.100","fat":"4.90","snf":"8.90"}'::jsonb,
     '2026-09-15 06:27:00+05:30', 'USR_RAVI_COLLECTOR_000001')
ON CONFLICT (id) DO NOTHING;

-- A record from the tablet's first install, arriving a month after that
-- generation was closed. It is not a duplicate and not corrupt: it is a reading
-- from an identity the platform no longer trusts, and the honest thing to do
-- with it is hold it where somebody can look at it.
INSERT INTO ingestion_service.quarantined_records
    (id, tenant_id, reason, detail, device_id, generation, external_session_id,
     sequence, payload_hash, payload, captured_at, created_by)
VALUES
    ('QRC_STALE_GEN_000001', 'TEN_VALLEY_DAIRY_000000001', 'STALE_GENERATION',
     'Generation 1 was closed on 11 August 2026 when the app was reinstalled. '
     'This record claims generation 1 and was received on 15 September.',
     'DEV_BOOTH_TABLET_0001', 1, 'CSN_BOOTH_20260810_04', 7,
     'sha256:aabbccddeeff00112233445566778899aabbccddeeff001122334455667788990',
     '{"producer":"PRD_RAVI_HOUSEHOLD_0001","litres":"7.100"}'::jsonb,
     '2026-08-10 06:05:00+05:30', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- The laboratory
--
-- A sample is drawn, sealed, handed along a chain of custody, and analysed. The
-- result is held as an exact scaled integer — 460 at scale 2 is 4.60 per cent —
-- because a payment is made from it, and a figure a member can dispute on the
-- third decimal is one nobody can defend.
-- ---------------------------------------------------------------------------

SET LOCAL search_path = laboratory_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO laboratory_service.lab_samples
    (id, tenant_id, sample_code, source_kind, source_ref, drawn_at, drawn_by,
     seal_number, purpose, duplicates_sample_id, created_by, updated_by)
VALUES
    ('LSM_20260915_000001', 'TEN_VALLEY_DAIRY_000000001', 'VD/LAB/2026/0915/01',
     'COLLECTION', 'CRC_20260915_000001', '2026-09-15 06:15:00+05:30',
     'USR_RAVI_COLLECTOR_000001', 'SEAL-0091142', 'PAYMENT', NULL,
     'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001'),
    -- The duplicate drawn at the same time, which is what a dispute is settled
    -- against.
    ('LSM_20260915_000002', 'TEN_VALLEY_DAIRY_000000001', 'VD/LAB/2026/0915/01-D',
     'COLLECTION', 'CRC_20260915_000001', '2026-09-15 06:15:00+05:30',
     'USR_RAVI_COLLECTOR_000001', 'SEAL-0091143', 'DUPLICATE', 'LSM_20260915_000001',
     'USR_RAVI_COLLECTOR_000001', 'USR_RAVI_COLLECTOR_000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO laboratory_service.lab_custody
    (id, tenant_id, sample_id, sequence, at, from_holder, to_holder, note, created_by)
VALUES
    ('LCU_20260915_00000001', 'TEN_VALLEY_DAIRY_000000001', 'LSM_20260915_000001', 1,
     '2026-09-15 06:15:00+05:30', 'booth:baramati', 'collector:ravi',
     'Drawn and sealed at the booth.', 'USR_RAVI_COLLECTOR_000001'),
    ('LCU_20260915_00000002', 'TEN_VALLEY_DAIRY_000000001', 'LSM_20260915_000001', 2,
     '2026-09-15 08:05:00+05:30', 'collector:ravi', 'lab:baramati',
     'Handed in at the plant laboratory; seal intact.', 'USR_RAVI_COLLECTOR_000001'),
    ('LCU_20260915_00000003', 'TEN_VALLEY_DAIRY_000000001', 'LSM_20260915_000001', 3,
     '2026-09-15 09:40:00+05:30', 'lab:baramati', 'analyst:meena',
     'Seal broken for analysis.', 'USR_MEENA_VET_00000000001')
ON CONFLICT (id) DO NOTHING;

UPDATE laboratory_service.lab_samples
   SET seal_broken_at = '2026-09-15 09:40:00+05:30',
       seal_broken_by = 'USR_MEENA_VET_00000000001',
       seal_broken_reason = 'Opened for scheduled payment analysis.'
 WHERE id = 'LSM_20260915_000001' AND seal_broken_at IS NULL;

INSERT INTO laboratory_service.lab_results
    (id, tenant_id, sample_id, analyte, value_numerator, value_scale, method,
     instrument_ref, instrument_valid_until, instrument_certificate, analysed_at,
     analysed_by, eligibility, eligibility_reason, created_by, updated_by)
VALUES
    ('LRS_20260915_FAT_001', 'TEN_VALLEY_DAIRY_000000001', 'LSM_20260915_000001',
     'FAT', 460, 2, 'GERBER', 'INST_ANALYSER_0000001', '2027-01-14',
     'LM/MH/2026/11842', '2026-09-15 09:55:00+05:30', 'analyst:meena',
     'ELIGIBLE', 'Instrument verification valid on the date of analysis.',
     'USR_MEENA_VET_00000000001', 'USR_MEENA_VET_00000000001'),
    ('LRS_20260915_SNF_001', 'TEN_VALLEY_DAIRY_000000001', 'LSM_20260915_000001',
     'SNF', 870, 2, 'LACTOMETER', 'INST_ANALYSER_0000001', '2027-01-14',
     'LM/MH/2026/11842', '2026-09-15 09:56:00+05:30', 'analyst:meena',
     'ELIGIBLE', 'Instrument verification valid on the date of analysis.',
     'USR_MEENA_VET_00000000001', 'USR_MEENA_VET_00000000001'),
    -- An MBRT with no instrument certificate on file. It is recorded and it is
    -- not eligible: the reading is real, what is missing is the basis for
    -- treating it as payable.
    ('LRS_20260915_MBRT_01', 'TEN_VALLEY_DAIRY_000000001', 'LSM_20260915_000001',
     'MBRT', 240, 0, 'MBRT', 'INST_LAB_BATH_0000001', NULL, NULL,
     '2026-09-15 14:00:00+05:30', 'analyst:meena',
     'NOT_ELIGIBLE', 'No verification certificate recorded for the water bath.',
     'USR_MEENA_VET_00000000001', 'USR_MEENA_VET_00000000001')
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Material: where the milk physically went
-- ---------------------------------------------------------------------------

SET LOCAL search_path = material_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO material_service.material_nodes
    (id, tenant_id, code, name, kind, capacity_value, capacity_unit, active,
     created_by, updated_by)
VALUES
    ('MND_BOOTH_BARAMATI_01', 'TEN_VALLEY_DAIRY_000000001', 'CC-BAR-01',
     'Baramati collection centre', 'COLLECTION_CENTRE', 6000, 'LITRES', true,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('MND_COOLER_BAR_0001', 'TEN_VALLEY_DAIRY_000000001', 'BC-BAR-01',
     'Baramati bulk cooler', 'BULK_COOLER', 5000, 'LITRES', true,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('MND_TANKER_MH12_0001', 'TEN_VALLEY_DAIRY_000000001', 'TK-MH12-AB-4471',
     'Tanker MH12 AB 4471', 'TANKER', 12000, 'LITRES', true,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('MND_PLANT_BARAMATI_1', 'TEN_VALLEY_DAIRY_000000001', 'PL-BAR-01',
     'Baramati processing plant', 'PLANT', NULL, NULL, true,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

-- material_instruments_one_kind_of_uncertainty: an instrument states its
-- uncertainty as a relative figure or as an absolute one, never both and never
-- neither. These are the manufacturers' published figures for instruments of
-- this class and are illustrative — the repository's standing position is that
-- instrument uncertainty by measurement method is a property of a specific
-- instrument in a specific plant and is not derivable from a table. Seed data is
-- where an illustrative figure is honest; a rate card is not.
INSERT INTO material_service.material_instruments
    (id, tenant_id, node_id, method, label, relative_ppm, absolute_value,
     absolute_unit, certificate_ref, calibrated_on, valid_until,
     created_by, updated_by)
VALUES
    ('MIN_COOLER_DIP_00001', 'TEN_VALLEY_DAIRY_000000001', 'MND_COOLER_BAR_0001',
     'DIP', 'Bulk cooler dipstick', NULL, 15, 'LITRES',
     'LM/MH/2026/11901', '2026-02-01', '2027-01-31',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    ('MIN_PLANT_FLOW_00001', 'TEN_VALLEY_DAIRY_000000001', 'MND_PLANT_BARAMATI_1',
     'FLOWMETER', 'Plant intake flowmeter', 2000, NULL, NULL,
     'LM/MH/2026/11955', '2026-03-12', '2027-03-11',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO material_service.material_movements
    (id, tenant_id, from_node_id, to_node_id, dispatched_at, dispatched_value,
     dispatched_unit, dispatch_method, dispatched_by, received_at, received_value,
     received_unit, receipt_method, received_by, variance_value, variance_unit,
     status, abandoned_reason, created_by, updated_by)
VALUES
    -- Received, and twelve litres short. The variance is recorded rather than
    -- absorbed: material_movements_unexplained_variance refuses a received
    -- movement that neither states its variance nor says why it cannot.
    ('MMV_BOOTH_COOLER_001', 'TEN_VALLEY_DAIRY_000000001', 'MND_BOOTH_BARAMATI_01',
     'MND_COOLER_BAR_0001', '2026-09-15 07:40:00+05:30', 4182, 'LITRES', 'DIP',
     'USR_RAVI_COLLECTOR_000001', '2026-09-15 08:05:00+05:30', 4170, 'LITRES',
     'DIP', 'USR_ANITA_MANAGER_00000001', 12, 'LITRES', 'RECEIVED', NULL,
     'USR_RAVI_COLLECTOR_000001', 'USR_ANITA_MANAGER_00000001'),
    -- On the road: no receipt yet, and therefore no variance to state.
    ('MMV_COOLER_TANKER_01', 'TEN_VALLEY_DAIRY_000000001', 'MND_COOLER_BAR_0001',
     'MND_TANKER_MH12_0001', '2026-09-15 09:15:00+05:30', 4170, 'LITRES', 'DIP',
     'USR_ANITA_MANAGER_00000001', NULL, NULL, NULL, NULL, NULL, NULL, NULL,
     'IN_TRANSIT', NULL,
     'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001'),
    ('MMV_TANKER_PLANT_01', 'TEN_VALLEY_DAIRY_000000001', 'MND_TANKER_MH12_0001',
     'MND_PLANT_BARAMATI_1', '2026-09-14 09:30:00+05:30', 3980, 'LITRES', 'DIP',
     'USR_ANITA_MANAGER_00000001', NULL, NULL, NULL, NULL, NULL, NULL, NULL,
     'ABANDONED', 'Tanker turned back with a failed seal; load returned to the cooler.',
     'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001')
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Production
--
-- expected_yield_ppm is left null on every formulation here, and that is
-- deliberate rather than lazy. A process yield is an empirical property of a
-- particular plant's line, and this repository has refused to invent those from
-- the start; the column is nullable precisely so a plant that has not measured
-- its own yield is not forced to state one. The constraint agrees: a stated
-- expectation must also say where it came from.
-- ---------------------------------------------------------------------------

SET LOCAL search_path = production_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO production_service.production_formulations
    (id, tenant_id, code, name, output_product_ref, output_unit, expected_yield_ppm,
     expectation_basis, status, approved_by, approved_at, approval_note,
     withdrawn_reason, valid_from, created_by, updated_by)
VALUES
    ('FRM_TONED_STD_00001', 'TEN_VALLEY_DAIRY_000000001', 'TONED-STD',
     'Toned milk, standard', 'PRD_TONED_MILK_000001', 'LITRES', NULL, NULL,
     'APPROVED', 'USR_ANITA_MANAGER_00000001', '2026-04-01 10:00:00+05:30',
     'Approved for the 2026 season.', NULL, '2026-04-01 00:00:00+05:30',
     'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001'),
    ('FRM_GHEE_STD_000001', 'TEN_VALLEY_DAIRY_000000001', 'GHEE-STD',
     'Cow ghee, standard', 'PRD_COW_GHEE_00000001', 'KILOGRAMS', NULL, NULL,
     'DRAFT', NULL, NULL, NULL, NULL, '2026-09-01 00:00:00+05:30',
     'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001'),
    ('FRM_FLAVOURED_00001', 'TEN_VALLEY_DAIRY_000000001', 'FLAV-ROSE',
     'Rose flavoured milk', 'PRD_FLAVOURED_MILK_001', 'LITRES', NULL, NULL,
     'WITHDRAWN', NULL, NULL, NULL,
     'Product discontinued after the 2025 season.', '2025-03-01 00:00:00+05:30',
     'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001')
ON CONFLICT (id) DO NOTHING;

-- A recipe, by contrast, is a decision the plant makes rather than a property
-- somebody has to measure, so these shares are stated.
INSERT INTO production_service.production_formulation_inputs
    (id, tenant_id, formulation_id, product_ref, expected_share_ppm,
     share_tolerance_ppm, required, created_by)
VALUES
    ('FIN_TONED_WHOLE_0001', 'TEN_VALLEY_DAIRY_000000001', 'FRM_TONED_STD_00001',
     'RAW-MILK-WHOLE', 620000, 20000, true, 'USR_ANITA_MANAGER_00000001'),
    ('FIN_TONED_SKIM_00001', 'TEN_VALLEY_DAIRY_000000001', 'FRM_TONED_STD_00001',
     'RAW-MILK-SKIM', 380000, 20000, true, 'USR_ANITA_MANAGER_00000001'),
    ('FIN_GHEE_CREAM_00001', 'TEN_VALLEY_DAIRY_000000001', 'FRM_GHEE_STD_000001',
     'CREAM-40', 1000000, NULL, true, 'USR_ANITA_MANAGER_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO production_service.production_batches
    (id, tenant_id, batch_code, kind, product_ref, produced_value, produced_unit,
     produced_at, produced_by, source_kind, source_ref, status, status_reason,
     formulation_id, created_by, updated_by)
VALUES
    -- production_batches_raw_says_where_from: a RAW batch names its source and
    -- nothing else may. The raw intake is the movement that arrived above.
    ('PBT_RAW_20260915_01', 'TEN_VALLEY_DAIRY_000000001', 'RAW-20260915-01', 'RAW',
     'RAW-MILK-WHOLE', 4170, 'LITRES', '2026-09-15 08:10:00+05:30', 'plant:baramati',
     'MOVEMENT', 'MMV_BOOTH_COOLER_001', 'RELEASED', NULL, NULL,
     'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001'),
    ('PBT_TONED_20260915_1', 'TEN_VALLEY_DAIRY_000000001', 'TM-20260915-A', 'FINISHED',
     'PRD_TONED_MILK_000001', 4000, 'LITRES', '2026-09-15 09:00:00+05:30',
     'plant:baramati', NULL, NULL, 'RELEASED', NULL, 'FRM_TONED_STD_00001',
     'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001'),
    -- Quarantined, which has to say why.
    ('PBT_CREAM_20260915_1', 'TEN_VALLEY_DAIRY_000000001', 'CR-20260915-A', 'INTERMEDIATE',
     'CREAM-40', 160, 'LITRES', '2026-09-15 09:05:00+05:30', 'plant:baramati',
     NULL, NULL, 'QUARANTINED', 'Awaiting the MBRT result on sample VD/LAB/2026/0915/01.',
     NULL, 'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO production_service.production_inputs
    (id, tenant_id, output_batch_id, input_batch_id, consumed_value, consumed_unit,
     created_by)
VALUES
    ('PIN_TONED_FROM_RAW_1', 'TEN_VALLEY_DAIRY_000000001', 'PBT_TONED_20260915_1',
     'PBT_RAW_20260915_01', 4010, 'LITRES', 'USR_ANITA_MANAGER_00000001'),
    ('PIN_CREAM_FROM_RAW_1', 'TEN_VALLEY_DAIRY_000000001', 'PBT_CREAM_20260915_1',
     'PBT_RAW_20260915_01', 160, 'LITRES', 'USR_ANITA_MANAGER_00000001')
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Canonical identity: which producer a collection belongs to
--
-- A mapping is never edited. It is closed and a new one opened, so the question
-- "who was this attributed to on the fourteenth" has an answer after somebody
-- corrects it — which is the whole reason a settlement can be explained.
-- ---------------------------------------------------------------------------

SET LOCAL search_path = canonical_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO canonical_service.collection_identity_policies
    (id, tenant_id, name, dimensions, resolution, version, effective_from, created_by)
VALUES
    ('CIP_VALLEY_V1_000001', 'TEN_VALLEY_DAIRY_000000001', 'Producer, date and shift',
     ARRAY['PRODUCER','DATE','SHIFT'], 'LAST_WINS', 1, '2026-01-01 00:00:00+05:30',
     'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO canonical_service.external_identities
    (id, tenant_id, source_system_id, entity_kind, external_id, entity_id, method,
     confidence, note, valid_from, valid_to, created_by)
VALUES
    -- The mapping in force. Exact, so it states no confidence: a number here
    -- would imply somebody guessed when nobody did.
    ('XID_RAVI_CURRENT_001', 'TEN_VALLEY_DAIRY_000000001', 'SRC_LEGACY_AMCU_0001',
     'PRODUCER', 'AMCU-0417', 'PRD_RAVI_HOUSEHOLD_0001', 'EXACT', NULL,
     'Matched on the society register.', '2026-02-01 00:00:00+05:30',
     '9999-12-31 23:59:59+00', 'USR_SEED_OPERATOR_00000001'),
    -- Inferred from a near-match on the name, and therefore obliged to say how
    -- confident: inferred_mappings_state_confidence refuses one that does not.
    ('XID_MEENA_INFER_0001', 'TEN_VALLEY_DAIRY_000000001', 'SRC_LEGACY_AMCU_0001',
     'PRODUCER', 'AMCU-0418', 'PRD_MEENA_HOUSE_000001', 'INFERRED', 0.870,
     'Name match with one transposed digit; to be confirmed at the office.',
     '2026-02-01 00:00:00+05:30', '9999-12-31 23:59:59+00',
     'USR_SEED_OPERATOR_00000001'),
    -- The retired mapping. It is closed rather than deleted, which is what makes
    -- a settlement from January still explainable.
    ('XID_RAVI_RETIRED_001', 'TEN_VALLEY_DAIRY_000000001', 'SRC_LEGACY_AMCU_0001',
     'PRODUCER', 'AMCU-0417', 'PRD_RAVI_OLD_HOUSE_001', 'MANUAL', NULL,
     'Household split; AMCU-0417 reassigned to the son.',
     '2025-04-01 00:00:00+05:30', '2026-02-01 00:00:00+05:30',
     'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

UPDATE canonical_service.external_identities
   SET superseded_at = '2026-02-01 00:00:00+05:30', superseded_by = 'XID_RAVI_CURRENT_001'
 WHERE id = 'XID_RAVI_RETIRED_001' AND superseded_at IS NULL;

INSERT INTO canonical_service.authoritative_collection_slots
    (id, tenant_id, slot_key, origin_kind, policy_id, policy_version,
     authoritative_ref, status, incumbent_recorded_at, incumbent_quality,
     contenders, values, created_by, updated_by)
VALUES
    ('SLT_RAVI_0915_AM_001', 'TEN_VALLEY_DAIRY_000000001',
     'PRD_RAVI_HOUSEHOLD_0001|2026-09-15|MORNING', 'NATIVE', 'CIP_VALLEY_V1_000001', 1,
     'CRC_20260915_000001', 'SETTLED', '2026-09-15 06:12:00+05:30', 90,
     '[]'::jsonb, '{"litres":"6.250","fat":"4.60","snf":"8.70"}'::jsonb,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    -- Two devices claimed the same producer, date and shift. Nothing is
    -- authoritative until somebody decides, so authoritative_ref is empty and
    -- conflicted_slot_has_no_holder enforces that it stays empty.
    ('SLT_MEENA_0915_AM_01', 'TEN_VALLEY_DAIRY_000000001',
     'PRD_MEENA_HOUSE_000001|2026-09-15|MORNING', 'NATIVE', 'CIP_VALLEY_V1_000001', 1,
     '', 'CONFLICT', '2026-09-15 06:19:00+05:30', 70,
     '[{"ref":"CRC_20260915_000002","quality":70},{"ref":"QRC_STALE_GEN_000001","quality":10}]'::jsonb,
     '{"litres":"9.400"}'::jsonb,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Procurement: what each collection was worth, and why
--
-- Every figure is an exact scaled integer. 6250 at scale 3 is 6.250 litres; 3450
-- at scale 2 is 34.50 rupees a litre; 21563 at scale 2 is 215.63 rupees. The
-- arithmetic, at HALF_UP as the card says:
--
--   6.250 x 34.50 = 215.625 -> 215.63   C1, 14 Sep morning
--   5.800 x 34.50 = 200.10  -> 200.10   C5, the correction of C2
--   6.250 x 34.50 = 215.625 -> 215.63   C3, 15 Sep morning
--   4.800 x 33.80 = 162.24  -> 162.24   C4, 15 Sep evening
--                                         gross 793.60 = 79360 minor units
-- ---------------------------------------------------------------------------

SET LOCAL search_path = procurement_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO procurement_service.rate_cards
    (id, tenant_id, name, kind, currency, amount_scale, basis, between_points,
     outside_chart, rounding, valid_from, valid_to, created_by, updated_by)
VALUES
    -- rate_cards_chart_is_complete: a CHART card that does not say its basis,
    -- what happens between its points and what happens outside it is a card
    -- whose answer depends on whoever reads it.
    ('RTC_CHART_SEP2026_01', 'TEN_VALLEY_DAIRY_000000001', 'Fat/SNF chart, September 2026',
     'CHART', 'INR', 2, 'PER_LITRE', 'INTERPOLATE', 'CLAMP', 'HALF_UP',
     '2026-09-01 00:00:00+05:30', NULL,
     'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001'),
    ('RTC_FORMULA_2026_001', 'TEN_VALLEY_DAIRY_000000001', 'Two-axis formula, 2026',
     'FORMULA', 'INR', 2, NULL, NULL, NULL, 'HALF_EVEN',
     '2026-01-01 00:00:00+05:30', '2026-09-01 00:00:00+05:30',
     'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO procurement_service.rate_card_cells
    (id, tenant_id, rate_card_id, fat_value, fat_scale, snf_value, snf_scale,
     rate_numerator, rate_scale, created_by)
VALUES
    ('RCC_SEP_38_84_00001', 'TEN_VALLEY_DAIRY_000000001', 'RTC_CHART_SEP2026_01',
     380, 2, 840, 2, 3380, 2, 'USR_SUNIL_ACCOUNTS_000001'),
    ('RCC_SEP_40_85_00001', 'TEN_VALLEY_DAIRY_000000001', 'RTC_CHART_SEP2026_01',
     400, 2, 850, 2, 3450, 2, 'USR_SUNIL_ACCOUNTS_000001'),
    ('RCC_SEP_46_87_00001', 'TEN_VALLEY_DAIRY_000000001', 'RTC_CHART_SEP2026_01',
     460, 2, 870, 2, 3450, 2, 'USR_SUNIL_ACCOUNTS_000001'),
    ('RCC_SEP_50_90_00001', 'TEN_VALLEY_DAIRY_000000001', 'RTC_CHART_SEP2026_01',
     500, 2, 900, 2, 3620, 2, 'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO procurement_service.rate_card_terms
    (id, tenant_id, rate_card_id, component, rate_numerator, rate_scale, created_by)
VALUES
    ('RCT_FAT_0000000000001', 'TEN_VALLEY_DAIRY_000000001', 'RTC_FORMULA_2026_001',
     'FAT', 46000, 4, 'USR_SUNIL_ACCOUNTS_000001'),
    ('RCT_SNF_0000000000001', 'TEN_VALLEY_DAIRY_000000001', 'RTC_FORMULA_2026_001',
     'SNF', 21000, 4, 'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO procurement_service.priced_collections
    (id, tenant_id, producer_ref, society_code, collected_on, shift, quantity_value,
     quantity_scale, quantity_unit, fat_value, fat_scale, snf_value, snf_scale,
     rate_card_id, rate_numerator, rate_scale, currency, amount_scale,
     amount_minor_units, explanation, origin_kind, created_by, updated_by)
VALUES
    ('PCL_RAVI_0914_AM_01', 'TEN_VALLEY_DAIRY_000000001', 'PRD_RAVI_HOUSEHOLD_0001',
     'SOC-BAR-01', '2026-09-14', 'MORNING', 6250, 3, 'PER_LITRE', 460, 2, 870, 2,
     'RTC_CHART_SEP2026_01', 3450, 2, 'INR', 2, 21563,
     '6.250 L at 34.50/L on the 4.60/8.70 cell; 215.625 rounded HALF_UP to 215.63.',
     'NATIVE', 'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001'),
    -- Superseded below, after the evening sample was re-analysed.
    ('PCL_RAVI_0914_PM_01', 'TEN_VALLEY_DAIRY_000000001', 'PRD_RAVI_HOUSEHOLD_0001',
     'SOC-BAR-01', '2026-09-14', 'EVENING', 5800, 3, 'PER_LITRE', 380, 2, 840, 2,
     'RTC_CHART_SEP2026_01', 3380, 2, 'INR', 2, 19604,
     '5.800 L at 33.80/L on the 3.80/8.40 cell; 196.04 exactly.',
     'NATIVE', 'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001'),
    ('PCL_RAVI_0915_AM_01', 'TEN_VALLEY_DAIRY_000000001', 'PRD_RAVI_HOUSEHOLD_0001',
     'SOC-BAR-01', '2026-09-15', 'MORNING', 6250, 3, 'PER_LITRE', 460, 2, 870, 2,
     'RTC_CHART_SEP2026_01', 3450, 2, 'INR', 2, 21563,
     '6.250 L at 34.50/L on the 4.60/8.70 cell; 215.625 rounded HALF_UP to 215.63.',
     'NATIVE', 'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001'),
    ('PCL_RAVI_0915_PM_01', 'TEN_VALLEY_DAIRY_000000001', 'PRD_RAVI_HOUSEHOLD_0001',
     'SOC-BAR-01', '2026-09-15', 'EVENING', 4800, 3, 'PER_LITRE', 380, 2, 840, 2,
     'RTC_CHART_SEP2026_01', 3380, 2, 'INR', 2, 16224,
     '4.800 L at 33.80/L on the 3.80/8.40 cell; 162.24 exactly.',
     'NATIVE', 'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

-- Closed before the correction is written, not after.
--
-- priced_collections_one_per_shift is a UNIQUE index over the live rows only —
-- (tenant, producer, date, shift) WHERE superseded_at IS NULL — so one shift has
-- exactly one collection anybody would pay on, and the corrected row and its
-- correction cannot both be live for an instant. Writing the correction first
-- fails, which is the index doing its job.

UPDATE procurement_service.priced_collections
   SET superseded_at = '2026-09-16 11:00:00+05:30', superseded_by = 'PCL_RAVI_0914_PM_02'
 WHERE id = 'PCL_RAVI_0914_PM_01' AND superseded_at IS NULL;

-- The correction. It supersedes rather than edits, and it has to say why:
-- collection_correction_has_a_reason refuses a correction with a blank reason,
-- because "the figure changed and nobody wrote down why" is the state this
-- platform exists to make impossible.
INSERT INTO procurement_service.priced_collections
    (id, tenant_id, producer_ref, society_code, collected_on, shift, quantity_value,
     quantity_scale, quantity_unit, fat_value, fat_scale, snf_value, snf_scale,
     rate_card_id, rate_numerator, rate_scale, currency, amount_scale,
     amount_minor_units, explanation, origin_kind, supersedes, correction_reason,
     created_by, updated_by)
VALUES
    ('PCL_RAVI_0914_PM_02', 'TEN_VALLEY_DAIRY_000000001', 'PRD_RAVI_HOUSEHOLD_0001',
     'SOC-BAR-01', '2026-09-14', 'EVENING', 5800, 3, 'PER_LITRE', 460, 2, 870, 2,
     'RTC_CHART_SEP2026_01', 3450, 2, 'INR', 2, 20010,
     '5.800 L at 34.50/L on the 4.60/8.70 cell; 200.10 exactly.',
     'NATIVE', 'PCL_RAVI_0914_PM_01',
     'Duplicate sample re-analysed on 16 September: fat 4.60, not 3.80. The '
     'original reading was taken before the analyser was rinsed.',
     'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;


-- ---------------------------------------------------------------------------
-- Pooling: the fortnight valued as a pool
--
-- Two of these tables enforce their own arithmetic and the seed has to satisfy
-- it rather than assert it:
--
--   fund_is_the_residual      fund = classified value - component value
--   allocation_total_is_its_parts   total = component + fund share
--
-- So: the classified utilisations come to 845.40, the components to 793.60 —
-- which is the same 79360 the collections priced to — and the producer
-- settlement fund is what is left, 51.80.
--
--   CLASS_I   15.000 L @ 38.00 = 570.00
--   CLASS_II   8.100 L @ 34.00 = 275.40
--                       total  = 845.40 = 84540 minor units
--   less components                793.60 = 79360
--   producer settlement fund        51.80 =  5180
-- ---------------------------------------------------------------------------

SET LOCAL search_path = pooling_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO pooling_service.pools
    (id, tenant_id, name, period_start, period_end, unit, currency, amount_scale,
     status, rate_card_id, policy_version, created_by, updated_by)
VALUES
    ('POL_SEP_FIRST_00001', 'TEN_VALLEY_DAIRY_000000001', 'September, first fortnight',
     '2026-09-01 00:00:00+05:30', '2026-09-16 00:00:00+05:30', 'LITRES', 'INR', 2,
     'VALUED', 'RTC_CHART_SEP2026_01', 'v3',
     'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO pooling_service.producer_milk
    (id, tenant_id, pool_id, producer_ref, quantity, components, slot_refs,
     origin_kind, origin_derivation_id, created_by)
VALUES
    ('PML_RAVI_SEP1_00001', 'TEN_VALLEY_DAIRY_000000001', 'POL_SEP_FIRST_00001',
     'PRD_RAVI_HOUSEHOLD_0001', 23.100,
     '{"fat_kg":"1.036","snf_kg":"1.994"}'::jsonb,
     ARRAY['SLT_RAVI_0915_AM_001'], 'DERIVED', 'DRV_POOL_SEP1_000001',
     'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO pooling_service.classified_utilisations
    (id, tenant_id, pool_id, class, quantity, price_numerator, price_scale, created_by)
VALUES
    ('CUT_SEP1_CLASS_I_01', 'TEN_VALLEY_DAIRY_000000001', 'POL_SEP_FIRST_00001',
     'CLASS_I', 15.000, 3800, 2, 'USR_SUNIL_ACCOUNTS_000001'),
    ('CUT_SEP1_CLASS_II_1', 'TEN_VALLEY_DAIRY_000000001', 'POL_SEP_FIRST_00001',
     'CLASS_II', 8.100, 3400, 2, 'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO pooling_service.pool_valuations
    (id, tenant_id, pool_id, currency, amount_scale, classified_value_minor_units,
     component_value_minor_units, producer_settlement_fund_minor_units,
     total_quantity, blend_price_numerator, blend_price_scale, rounding_trail,
     origin_kind, origin_derivation_id, computed_at, created_by)
VALUES
    ('PVL_SEP1_0000000001', 'TEN_VALLEY_DAIRY_000000001', 'POL_SEP_FIRST_00001',
     'INR', 2, 84540, 79360, 5180, 23.100, 3660, 2,
     '[{"step":"blend_price","of":"84540/23.100","exact":"3659.74...","rounded":"3660","mode":"HALF_UP"}]'::jsonb,
     'DERIVED', 'DRV_POOL_SEP1_000001', '2026-09-16 10:00:00+05:30',
     'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO pooling_service.allocations
    (id, tenant_id, pool_id, valuation_id, producer_ref, currency, amount_scale,
     component_value_minor_units, fund_share_minor_units, total_minor_units,
     weight, created_by)
VALUES
    ('ALC_RAVI_SEP1_00001', 'TEN_VALLEY_DAIRY_000000001', 'POL_SEP_FIRST_00001',
     'PVL_SEP1_0000000001', 'PRD_RAVI_HOUSEHOLD_0001', 'INR', 2,
     79360, 5180, 84540, 23100, 'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO pooling_service.producer_economic_events
    (id, tenant_id, pool_id, allocation_id, producer_ref, currency, amount_scale,
     amount_minor_units, kind, supersedes_event_id, origin_kind,
     origin_derivation_id, created_by)
VALUES
    -- correction_names_what_it_supersedes: an ORIGINAL supersedes nothing, and
    -- anything that is not an ORIGINAL must say what it replaces.
    ('PEV_RAVI_SEP1_0001', 'TEN_VALLEY_DAIRY_000000001', 'POL_SEP_FIRST_00001',
     'ALC_RAVI_SEP1_00001', 'PRD_RAVI_HOUSEHOLD_0001', 'INR', 2, 84540,
     'ORIGINAL', NULL, 'DERIVED', 'DRV_POOL_SEP1_000001', 'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO pooling_service.recovery_retroactivity_policies
    (id, tenant_id, name, mode, max_lookback_days, currency, amount_scale,
     minimum_adjustment_minor_units, effective_from, effective_to, created_by)
VALUES
    ('RRP_VALLEY_2026_001', 'TEN_VALLEY_DAIRY_000000001',
     'Do not reopen a settled fortnight', 'DO_NOT_REOPEN', 30, 'INR', 2, 500,
     '2026-01-01 00:00:00+05:30', NULL, 'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Balance: does what left equal what arrived
--
-- Three flows along one route. The third is unmeasured — nobody gauged the
-- tanker into the plant that morning — and measured_flow_is_weighted refuses a
-- measured flow with no uncertainty, because a reconciliation weights each flow
-- by how well it is known and a missing weight silently becomes a perfect one.
-- The node kinds here are the four that balance-service recognises; the bulk
-- cooler is a CHILLING_UNIT to this service and a BULK_COOLER to material.
-- ---------------------------------------------------------------------------

SET LOCAL search_path = balance_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO balance_service.balance_windows
    (id, tenant_id, route_ref, period_start, period_end, unit, status,
     created_by, updated_by)
VALUES
    ('BWN_BAR_20260915_01', 'TEN_VALLEY_DAIRY_000000001', 'RT-BAR-01',
     '2026-09-15 05:00:00+05:30', '2026-09-15 12:00:00+05:30', 'LITRES', 'ACCEPTED',
     'USR_ANITA_MANAGER_00000001', 'USR_ANITA_MANAGER_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO balance_service.flow_measurements
    (id, tenant_id, window_id, flow_id, from_node_id, from_node_kind, to_node_id,
     to_node_kind, measured, standard_uncertainty, unmeasured, observation_ref,
     created_by)
VALUES
    ('FLM_BOOTH_COOLER_01', 'TEN_VALLEY_DAIRY_000000001', 'BWN_BAR_20260915_01',
     'F-BOOTH-COOLER', 'CC-BAR-01', 'COLLECTION_CENTRE', 'BC-BAR-01', 'CHILLING_UNIT',
     4182.000, 12.500, false, 'OBS_TANKER_MASS_00001', 'USR_ANITA_MANAGER_00000001'),
    ('FLM_COOLER_TANKER1', 'TEN_VALLEY_DAIRY_000000001', 'BWN_BAR_20260915_01',
     'F-COOLER-TANKER', 'BC-BAR-01', 'CHILLING_UNIT', 'TK-MH12-AB-4471', 'TANKER',
     4170.000, 12.000, false, '', 'USR_ANITA_MANAGER_00000001'),
    ('FLM_TANKER_PLANT_1', 'TEN_VALLEY_DAIRY_000000001', 'BWN_BAR_20260915_01',
     'F-TANKER-PLANT', 'TK-MH12-AB-4471', 'TANKER', 'PL-BAR-01', 'PLANT',
     0.000, NULL, true, '', 'USR_ANITA_MANAGER_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO balance_service.reconciliation_runs
    (id, tenant_id, window_id, converged, residual_before, residual_after,
     model_version, gross_error_threshold, reason, accepted_at, accepted_by,
     created_by)
VALUES
    -- model_answer_names_its_model: a run that produced an answer has to say
    -- which model produced it, and acceptance_is_attributed refuses an
    -- acceptance with nobody's name against it.
    ('RCR_BAR_20260915_01', 'TEN_VALLEY_DAIRY_000000001', 'BWN_BAR_20260915_01',
     true, 12.000, 0.400, 'mlcore-recon-0.3.1', 3.0, '',
     '2026-09-15 12:30:00+05:30', 'USR_ANITA_MANAGER_00000001',
     'USR_ANITA_MANAGER_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO balance_service.reconciled_flows
    (run_id, tenant_id, flow_id, measured, reconciled, adjustment, test_statistic,
     gross_error, unmeasured)
VALUES
    ('RCR_BAR_20260915_01', 'TEN_VALLEY_DAIRY_000000001', 'F-BOOTH-COOLER',
     4182.000, 4176.000, -6.000, 0.48, false, false),
    ('RCR_BAR_20260915_01', 'TEN_VALLEY_DAIRY_000000001', 'F-COOLER-TANKER',
     4170.000, 4176.000, 6.000, 0.50, false, false),
    -- unmeasured_flow_is_never_a_gross_error: a flow nobody measured cannot be
    -- accused of being wrong. The reconciliation infers it from the others.
    ('RCR_BAR_20260915_01', 'TEN_VALLEY_DAIRY_000000001', 'F-TANKER-PLANT',
     0.000, 4176.000, 4176.000, 0.0, false, true)
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------------------------------
-- Settlement: what Ravi's household is actually paid
--
-- The one piece of arithmetic in this file that a member would check, so it is
-- written out. Four live collections, the corrected one among them:
--
--   14 Sep morning  6.250 L @ 34.50   215.63
--   14 Sep evening  5.800 L @ 34.50   200.10   (the correction, not the original)
--   15 Sep morning  6.250 L @ 34.50   215.63
--   15 Sep evening  4.800 L @ 33.80   162.24
--                            gross    793.60  = 79360
--   less one feed-credit instalment            = 10000
--                              net    693.60  = 69360
--
-- producer_payables_adds_up enforces net = gross - deducted, so a seed that got
-- this wrong would be refused rather than quietly shown to somebody.
-- ---------------------------------------------------------------------------

SET LOCAL search_path = settlement_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO settlement_service.payment_cycles
    (id, tenant_id, society_code, name, period_start, period_end, currency,
     amount_scale, deduction_policy, status, gathered_at, approved_at, approved_by,
     paid_at, created_by, updated_by)
VALUES
    -- payment_cycles_approved_after_gathering and _paid_after_approval put the
    -- states in order: a cycle cannot be approved before it was gathered, and
    -- cannot be paid before it was approved.
    ('CYC_SEP_FIRST_00001', 'TEN_VALLEY_DAIRY_000000001', 'SOC-BAR-01',
     'September, first fortnight', '2026-09-01', '2026-09-15', 'INR', 2,
     'CAP_AT_EARNINGS', 'PAID', '2026-09-16 12:00:00+05:30',
     '2026-09-17 09:00:00+05:30', 'USR_SUNIL_ACCOUNTS_000001',
     '2026-09-17 15:00:00+05:30',
     'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001'),
    ('CYC_SEP_SECOND_0001', 'TEN_VALLEY_DAIRY_000000001', 'SOC-BAR-01',
     'September, second fortnight', '2026-09-16', '2026-09-30', 'INR', 2,
     'CAP_AT_EARNINGS', 'OPEN', NULL, NULL, NULL, NULL,
     'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO settlement_service.cycle_lines
    (id, tenant_id, cycle_id, producer_ref, collection_id, collected_on, shift,
     quantity, quantity_unit, rate, currency, amount_scale, amount_minor_units)
VALUES
    ('CLN_RAVI_0914_AM_1', 'TEN_VALLEY_DAIRY_000000001', 'CYC_SEP_FIRST_00001',
     'PRD_RAVI_HOUSEHOLD_0001', 'PCL_RAVI_0914_AM_01', '2026-09-14', 'MORNING',
     '6.250', 'PER_LITRE', '34.50', 'INR', 2, 21563),
    ('CLN_RAVI_0914_PM_2', 'TEN_VALLEY_DAIRY_000000001', 'CYC_SEP_FIRST_00001',
     'PRD_RAVI_HOUSEHOLD_0001', 'PCL_RAVI_0914_PM_02', '2026-09-14', 'EVENING',
     '5.800', 'PER_LITRE', '34.50', 'INR', 2, 20010),
    ('CLN_RAVI_0915_AM_1', 'TEN_VALLEY_DAIRY_000000001', 'CYC_SEP_FIRST_00001',
     'PRD_RAVI_HOUSEHOLD_0001', 'PCL_RAVI_0915_AM_01', '2026-09-15', 'MORNING',
     '6.250', 'PER_LITRE', '34.50', 'INR', 2, 21563),
    ('CLN_RAVI_0915_PM_1', 'TEN_VALLEY_DAIRY_000000001', 'CYC_SEP_FIRST_00001',
     'PRD_RAVI_HOUSEHOLD_0001', 'PCL_RAVI_0915_PM_01', '2026-09-15', 'EVENING',
     '4.800', 'PER_LITRE', '33.80', 'INR', 2, 16224)
ON CONFLICT (id) DO NOTHING;

INSERT INTO settlement_service.recoveries
    (id, tenant_id, producer_ref, kind, reference, currency, amount_scale,
     principal_minor_units, recovered_minor_units, instalment_minor_units,
     priority, status, opened_on, created_by, updated_by)
VALUES
    -- recoveries_never_over_recovered: what has been taken back can never exceed
    -- what was lent. One instalment of 100.00 has been taken so far.
    ('RCV_RAVI_FEED_0001', 'TEN_VALLEY_DAIRY_000000001', 'PRD_RAVI_HOUSEHOLD_0001',
     'FEED_CREDIT', 'INV-FEED-2026-0002', 'INR', 2, 50000, 10000, 10000, 1,
     'OUTSTANDING', '2026-08-20',
     'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001'),
    ('RCV_MEENA_ADV_00001', 'TEN_VALLEY_DAIRY_000000001', 'PRD_MEENA_HOUSE_000001',
     'ADVANCE', 'ADV/2026/0081', 'INR', 2, 200000, 200000, 25000, 2,
     'SETTLED', '2026-03-01',
     'USR_SUNIL_ACCOUNTS_000001', 'USR_SUNIL_ACCOUNTS_000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO settlement_service.cycle_deductions
    (id, tenant_id, cycle_id, recovery_id, producer_ref, kind, reference,
     currency, amount_scale, amount_minor_units)
VALUES
    ('CDD_RAVI_FEED_00001', 'TEN_VALLEY_DAIRY_000000001', 'CYC_SEP_FIRST_00001',
     'RCV_RAVI_FEED_0001', 'PRD_RAVI_HOUSEHOLD_0001', 'FEED_CREDIT',
     'INV-FEED-2026-0002', 'INR', 2, 10000)
ON CONFLICT (id) DO NOTHING;

INSERT INTO settlement_service.producer_payables
    (id, tenant_id, cycle_id, producer_ref, currency, amount_scale, gross_minor_units,
     deducted_minor_units, net_minor_units, carried_forward_minor_units, status,
     kind, adjusts_payable_id, reason, approved_at, approved_by, paid_at, paid_by,
     payment_reference, held_reason)
VALUES
    ('PBL_RAVI_SEP1_0001', 'TEN_VALLEY_DAIRY_000000001', 'CYC_SEP_FIRST_00001',
     'PRD_RAVI_HOUSEHOLD_0001', 'INR', 2, 79360, 10000, 69360, 0, 'PAID',
     'SETTLEMENT', NULL, NULL, '2026-09-17 09:00:00+05:30', 'USR_SUNIL_ACCOUNTS_000001',
     '2026-09-17 15:00:00+05:30', 'USR_SUNIL_ACCOUNTS_000001',
     'NEFT/2026/0917/88213', NULL),
    -- Held, and producer_payables_hold_has_a_reason will not let it be held
    -- without saying why. A member asking at the office gets this sentence.
    ('PBL_MEENA_SEP1_001', 'TEN_VALLEY_DAIRY_000000001', 'CYC_SEP_FIRST_00001',
     'PRD_MEENA_HOUSE_000001', 'INR', 2, 52000, 0, 52000, 0, 'HELD',
     'SETTLEMENT', NULL, NULL, NULL, NULL, NULL, NULL, NULL,
     'Identity mapping AMCU-0418 is inferred at 0.87 and unconfirmed; the '
     'collection slot for 15 September morning is in conflict.')
ON CONFLICT (id) DO NOTHING;

-- The adjustment, which is a payable of its own rather than an edit of the one
-- that was paid. It deducts nothing, states what it adjusts, and says why —
-- three separate constraints, each of which has refused a draft of this file.
INSERT INTO settlement_service.producer_payables
    (id, tenant_id, cycle_id, producer_ref, currency, amount_scale, gross_minor_units,
     deducted_minor_units, net_minor_units, carried_forward_minor_units, status,
     kind, adjusts_payable_id, reason)
VALUES
    ('PBL_RAVI_ADJ_00001', 'TEN_VALLEY_DAIRY_000000001', 'CYC_SEP_FIRST_00001',
     'PRD_RAVI_HOUSEHOLD_0001', 'INR', 2, 1250, 0, 1250, 0, 'PAYABLE',
     'ADJUSTMENT', 'PBL_RAVI_SEP1_0001',
     'The 14 September evening collection was re-analysed after the cycle was '
     'paid. Difference of 12.50 owed to the member.')
ON CONFLICT (id) DO NOTHING;

INSERT INTO settlement_service.notification_outbox
    (id, tenant_id, event, recipient_type, recipient_id, title, body,
     reference_type, reference_id, created_by, attempts, last_error,
     last_attempt_at, delivered_at)
VALUES
    ('OTB_RAVI_PAID_0001', 'TEN_VALLEY_DAIRY_000000001', 'settlement.payable.paid',
     'user', 'USR_RAVI_COLLECTOR_000001', 'Payment sent',
     'Your payment of INR 693.60 was sent on 17 September, reference NEFT/2026/0917/88213.',
     'producer_payable', 'PBL_RAVI_SEP1_0001', 'USR_SEED_OPERATOR_00000001',
     1, NULL, '2026-09-17 15:01:00+05:30', '2026-09-17 15:01:02+05:30'),
    -- Tried three times and still not delivered. This is the row the outbox
    -- watcher exists to notice: notification_outbox_delivery_was_attempted keeps
    -- a delivered row honest about having been attempted at all.
    ('OTB_MEENA_HELD_001', 'TEN_VALLEY_DAIRY_000000001', 'settlement.payable.held',
     'user', 'USR_MEENA_VET_00000000001', 'Payment held',
     'Your payment for 1-15 September is held pending an identity check.',
     'producer_payable', 'PBL_MEENA_SEP1_001', 'USR_SEED_OPERATOR_00000001',
     3, 'sms gateway returned 503', '2026-09-17 15:40:00+05:30', NULL),
    ('OTB_RAVI_ADJ_00001', 'TEN_VALLEY_DAIRY_000000001', 'settlement.payable.approved',
     'user', 'USR_RAVI_COLLECTOR_000001', 'Adjustment raised',
     'An adjustment of INR 12.50 for 14 September has been raised on your account.',
     'producer_payable', 'PBL_RAVI_ADJ_00001', 'USR_SEED_OPERATOR_00000001',
     0, NULL, NULL, NULL)
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- Shadow settlement: what the old system said, beside what this one computes
--
-- The point of the whole platform in two tables. Nobody is asked to trust a new
-- settlement figure on day one; the external system's number is imported as an
-- assertion, this platform's number is computed beside it, and the difference is
-- classified rather than argued about.
-- ---------------------------------------------------------------------------

SET LOCAL search_path = shadow_settlement_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO shadow_settlement_service.external_settlement_assertions
    (id, tenant_id, source_system_id, external_settlement_id, producer_ref,
     period_start, period_end, currency, amount_scale, total_minor_units,
     components, asserted_at, origin_kind, import_batch_id, source_record_id,
     source_payload_hash, valid_from, created_by)
VALUES
    ('ESA_RAVI_SEP1_0001', 'TEN_VALLEY_DAIRY_000000001', 'SRC_LEGACY_AMCU_0001',
     'AMCU-SETT-2026-0915-0417', 'PRD_RAVI_HOUSEHOLD_0001', '2026-09-01', '2026-09-15',
     'INR', 2, 69900, '[{"kind":"MILK","minor_units":79900},{"kind":"DEDUCTION","minor_units":-10000}]'::jsonb,
     '2026-09-17 06:00:00+05:30', 'IMPORTED', 'IMB_20260917_000001',
     'AMCU/2026/0915/0417', 'sha256:99887766554433221100ffeeddccbbaa99887766554433221100ffeeddccbbaa',
     '2026-09-17 06:00:00+05:30', 'USR_SEED_OPERATOR_00000001'),
    ('ESA_MEENA_SEP1_001', 'TEN_VALLEY_DAIRY_000000001', 'SRC_LEGACY_AMCU_0001',
     'AMCU-SETT-2026-0915-0418', 'PRD_MEENA_HOUSE_000001', '2026-09-01', '2026-09-15',
     'INR', 2, 52000, '[{"kind":"MILK","minor_units":52000}]'::jsonb,
     '2026-09-17 06:00:00+05:30', 'IMPORTED', 'IMB_20260917_000001',
     'AMCU/2026/0915/0418', 'sha256:1122334455667788990011223344556677889900112233445566778899001122',
     '2026-09-17 06:00:00+05:30', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO shadow_settlement_service.shadow_settlement_computations
    (id, tenant_id, assertion_id, producer_ref, period_start, period_end, currency,
     amount_scale, total_minor_units, components, policy_version, rate_card_id,
     rounding_trail, input_digest, as_of, origin_kind, derivation_id, created_by)
VALUES
    ('SSC_RAVI_SEP1_0001', 'TEN_VALLEY_DAIRY_000000001', 'ESA_RAVI_SEP1_0001',
     'PRD_RAVI_HOUSEHOLD_0001', '2026-09-01', '2026-09-15', 'INR', 2, 69360,
     '[{"kind":"MILK","minor_units":79360},{"kind":"DEDUCTION","minor_units":-10000}]'::jsonb,
     'v3', 'RTC_CHART_SEP2026_01',
     '[{"step":"C1","exact":"215.625","rounded":"215.63","mode":"HALF_UP"}]'::jsonb,
     'sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789',
     '2026-09-17 07:00:00+05:30', 'DERIVED', 'DRV_SHADOW_SEP1_0001',
     'USR_SEED_OPERATOR_00000001'),
    ('SSC_MEENA_SEP1_001', 'TEN_VALLEY_DAIRY_000000001', 'ESA_MEENA_SEP1_001',
     'PRD_MEENA_HOUSE_000001', '2026-09-01', '2026-09-15', 'INR', 2, 52000,
     '[{"kind":"MILK","minor_units":52000}]'::jsonb,
     'v3', 'RTC_CHART_SEP2026_01', '[]'::jsonb,
     'sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef',
     '2026-09-17 07:00:00+05:30', 'DERIVED', 'DRV_SHADOW_SEP1_0002',
     'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO shadow_settlement_service.settlement_divergences
    (id, tenant_id, assertion_id, computation_id, producer_ref, currency,
     amount_scale, delta_minor_units, classification, rationale, evidence,
     ml_hypotheses, status, created_by, updated_by)
VALUES
    -- 699.00 asserted against 693.60 computed. The difference is the milk line,
    -- not the deduction, and it is the 14 September evening collection that was
    -- re-analysed — the old system still holds the pre-correction figure.
    ('SDV_RAVI_SEP1_0001', 'TEN_VALLEY_DAIRY_000000001', 'ESA_RAVI_SEP1_0001',
     'SSC_RAVI_SEP1_0001', 'PRD_RAVI_HOUSEHOLD_0001', 'INR', 2, 540,
     'INPUT_DIFFERENCE',
     'Milk line differs by 5.40 and the deduction agrees. The external system '
     'holds fat 3.80 for 14 September evening; this platform holds the corrected '
     '4.60 from the duplicate sample re-analysed on 16 September.',
     '[{"collection":"PCL_RAVI_0914_PM_02","supersedes":"PCL_RAVI_0914_PM_01"}]'::jsonb,
     '[]'::jsonb, 'UNDER_REVIEW',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001'),
    -- match_has_no_delta: a divergence classified MATCH must have a delta of
    -- exactly zero, so "they agree" cannot be said about figures that do not.
    ('SDV_MEENA_SEP1_001', 'TEN_VALLEY_DAIRY_000000001', 'ESA_MEENA_SEP1_001',
     'SSC_MEENA_SEP1_001', 'PRD_MEENA_HOUSE_000001', 'INR', 2, 0,
     'MATCH', 'Both figures are 520.00.', '[]'::jsonb, '[]'::jsonb, 'ACCEPTED',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- The audit trail
--
-- Written unsealed, then sealed, then anchored — which is the order the platform
-- uses and the reason it uses it. Chaining during the insert would mean taking a
-- per-tenant lock inside every business transaction; instead rows are written
-- unordered and a sealer links them afterwards, and gavya_audit_chain_status
-- reports how much is not yet covered rather than pretending everything is.
--
-- The trail is append-only by grant and by trigger, so these rows can be
-- inserted and never edited. That is also why they carry the before-image in
-- old_value: once written, the only way to say what a figure used to be is to
-- have said it at the time.
-- ---------------------------------------------------------------------------

SET LOCAL search_path = public;  -- set by deploy/seed: this service's own search_path
INSERT INTO public.audit_logs
    (id, tenant_id, actor_id, actor_type, action, resource_type, resource_id,
     old_value, new_value, ip_address, user_agent, service_name, trace_id,
     created_at, created_by)
VALUES
    ('AUD_SEED_0000000000001', 'TEN_VALLEY_DAIRY_000000001', 'USR_RAVI_COLLECTOR_000001',
     'user', 'milk.record.created', 'milk_record', 'MRC_LAKSHMI_AM_00000001',
     NULL, '{"quantity_liters":"6.250"}', '198.51.100.9', 'Gavya Booth/2.3 (Android)',
     'milk-service', '4bf92f3577b34da6a3ce929d0e0e4736', '2026-09-15 06:12:00+05:30',
     'USR_RAVI_COLLECTOR_000001'),
    -- A correction, with what it overwrote. This is the entry somebody reads
    -- when a member asks why their fortnight changed after it was paid.
    ('AUD_SEED_0000000000002', 'TEN_VALLEY_DAIRY_000000001', 'USR_SUNIL_ACCOUNTS_000001',
     'user', 'procurement.collection.superseded', 'priced_collection',
     'PCL_RAVI_0914_PM_01',
     '{"fat":"3.80","rate":"33.80","amount_minor_units":19604}',
     '{"superseded_by":"PCL_RAVI_0914_PM_02","fat":"4.60","rate":"34.50","amount_minor_units":20010}',
     '203.0.113.17', 'Gavya Console/1.0', 'procurement-service',
     '7c1e88a0b4d24e5fb6a1c2d3e4f50617', '2026-09-16 11:00:00+05:30',
     'USR_SUNIL_ACCOUNTS_000001'),
    ('AUD_SEED_0000000000003', 'TEN_VALLEY_DAIRY_000000001', 'USR_SUNIL_ACCOUNTS_000001',
     'user', 'settlement.payable.approved', 'producer_payable', 'PBL_RAVI_SEP1_0001',
     '{"status":"PAYABLE"}', '{"status":"APPROVED","net_minor_units":69360}',
     '203.0.113.17', 'Gavya Console/1.0', 'settlement-service',
     '9d4c7b2a1f6e3058c9b8a7d6e5f40312', '2026-09-17 09:00:00+05:30',
     'USR_SUNIL_ACCOUNTS_000001'),
    ('AUD_SEED_0000000000004', 'TEN_VALLEY_DAIRY_000000001', 'USR_SUNIL_ACCOUNTS_000001',
     'user', 'settlement.payable.paid', 'producer_payable', 'PBL_RAVI_SEP1_0001',
     '{"status":"APPROVED"}',
     '{"status":"PAID","payment_reference":"NEFT/2026/0917/88213"}',
     '203.0.113.17', 'Gavya Console/1.0', 'settlement-service',
     '9d4c7b2a1f6e3058c9b8a7d6e5f40312', '2026-09-17 15:00:00+05:30',
     'USR_SUNIL_ACCOUNTS_000001'),
    ('AUD_SEED_0000000000005', 'TEN_VALLEY_DAIRY_000000001', 'SVC_BOOTH_TABLET_0001',
     'service', 'ingestion.record.quarantined', 'quarantined_record',
     'QRC_STALE_GEN_000001', NULL,
     '{"reason":"STALE_GENERATION","generation":1,"current_generation":2}',
     '198.51.100.9', 'Gavya Booth/2.3 (Android)', 'ingestion-service',
     '2a6b5c4d3e2f1009887766554433221f', '2026-09-15 06:30:00+05:30',
     'SVC_BOOTH_TABLET_0001')
ON CONFLICT (id) DO NOTHING;

-- Seal the rows just written, then anchor the head of the chain. Both use the
-- platform's own functions rather than computing a hash here: a seed that
-- invented its own chain would produce a trail that verifies against nothing.
SELECT gavya_seal_audit_log('TEN_VALLEY_DAIRY_000000001');
SELECT gavya_checkpoint_audit_chain('TEN_VALLEY_DAIRY_000000001');

-- ---------------------------------------------------------------------------
-- Hill Creamery
--
-- A second tenant, with just enough in it that isolation is demonstrable: its
-- own farm, its own animal, its own morning. A query that forgets its tenant now
-- returns six animals where it should return five, which is a visibly wrong
-- answer rather than a plausible one.
--
-- The tenant setting changes here, and everything below belongs to Hill
-- Creamery. Under row-level security a row written with the setting above would
-- be refused rather than misfiled.
-- ---------------------------------------------------------------------------

SET LOCAL app.tenant_id = 'TEN_HILL_CREAMERY_00000001';

SET LOCAL search_path = tenant_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO tenant_service.tenants
    (id, name, slug, plan, status, contact_email, contact_phone, address,
     country, timezone, currency, currency_scale, max_users, max_cattle,
     created_by, updated_by)
VALUES
    ('TEN_HILL_CREAMERY_00000001', 'Hill Creamery', 'hill-creamery',
     'free', 'active', 'office@hill-creamery.example', '+91 20 5550 0202',
     '12 Ridge Lane, Panchgani, Maharashtra', 'India', 'Asia/Kolkata',
     'INR', 2, 5, 60, 'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

SET LOCAL search_path = billing_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO billing_service.tenant_currency (tenant_id, currency, currency_scale)
VALUES ('TEN_HILL_CREAMERY_00000001', 'INR', 2) ON CONFLICT (tenant_id) DO NOTHING;

SET LOCAL search_path = order_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO order_service.tenant_currency (tenant_id, currency, currency_scale)
VALUES ('TEN_HILL_CREAMERY_00000001', 'INR', 2) ON CONFLICT (tenant_id) DO NOTHING;

SET LOCAL search_path = product_catalog_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO product_catalog_service.tenant_currency (tenant_id, currency, currency_scale)
VALUES ('TEN_HILL_CREAMERY_00000001', 'INR', 2) ON CONFLICT (tenant_id) DO NOTHING;

SET LOCAL search_path = cattle_market_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO cattle_market_service.tenant_currency (tenant_id, currency, currency_scale)
VALUES ('TEN_HILL_CREAMERY_00000001', 'INR', 2) ON CONFLICT (tenant_id) DO NOTHING;

SET LOCAL search_path = health_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO health_service.tenant_currency (tenant_id, currency, currency_scale)
VALUES ('TEN_HILL_CREAMERY_00000001', 'INR', 2) ON CONFLICT (tenant_id) DO NOTHING;

SET LOCAL search_path = milk_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO milk_service.tenant_timezone (tenant_id, timezone)
VALUES ('TEN_HILL_CREAMERY_00000001', 'Asia/Kolkata') ON CONFLICT (tenant_id) DO NOTHING;

SET LOCAL search_path = identity_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO identity_service.roles
    (id, tenant_id, name, description, is_builtin, created_by, updated_by)
VALUES
    ('ROL_HILL_ADMIN_00001', 'TEN_HILL_CREAMERY_00000001', 'admin',
     'Administers the tenant.', true,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO identity_service.users
    (id, email, email_normalised, full_name, password_hash, status,
     created_by, updated_by)
VALUES
    ('USR_HILL_OWNER_00001', 'owner@hill-creamery.example', 'owner@hill-creamery.example',
     'Kiran Bhosale',
     '$argon2id$v=19$m=65536,t=3,p=4$r+ZbPZHwnO3iNxZR+M/hDg$336rA5aZBHLddl/k+64E/iPaiRLKYE2lE1w6zFg0Ix0',
     'active', 'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO identity_service.tenant_memberships
    (id, tenant_id, user_id, role_id, status, is_default, created_by, updated_by)
VALUES
    ('MEM_HILL_OWNER_00001', 'TEN_HILL_CREAMERY_00000001', 'USR_HILL_OWNER_00001',
     'ROL_HILL_ADMIN_00001', 'active', true,
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

SET LOCAL search_path = farm_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO farm_service.farms
    (id, tenant_id, name, code, address, city, state, country, capacity,
     manager_id, status, created_by, updated_by)
VALUES
    ('FRM_PANCHGANI_00001', 'TEN_HILL_CREAMERY_00000001', 'Panchgani Farm', 'PAN-01',
     'Survey 9, Panchgani', 'Panchgani', 'Maharashtra', 'India', 40,
     'USR_HILL_OWNER_00001', 'active',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

-- Note the tag number: VD-0001 belongs to Valley Dairy's Lakshmi. Two tenants
-- may use the same tag, and a query that drops its tenant clause finds two
-- animals where a farm has one.

SET LOCAL search_path = cattle_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO cattle_service.cattle
    (id, tenant_id, tag_number, name, breed_id, date_of_birth, gender, status,
     weight, color, owner_id, farm_id, created_by, updated_by)
VALUES
    ('CAT_HILL_NANDINI_001', 'TEN_HILL_CREAMERY_00000001', 'VD-0001', 'Nandini',
     NULL, '2021-01-12', 'F', 'active', 388.00, 'White',
     'USR_HILL_OWNER_00001', 'FRM_PANCHGANI_00001',
     'USR_SEED_OPERATOR_00000001', 'USR_SEED_OPERATOR_00000001')
ON CONFLICT (id) DO NOTHING;

SET LOCAL search_path = milk_service, public;  -- set by deploy/seed: this service's own search_path
INSERT INTO milk_service.milk_sessions
    (id, tenant_id, cattle_id, session_date, shift_type, status, created_by, updated_by)
VALUES
    ('MSS_HILL_AM_0000001', 'TEN_HILL_CREAMERY_00000001', 'CAT_HILL_NANDINI_001',
     '2026-09-15', 'morning', 'closed',
     'USR_HILL_OWNER_00001', 'USR_HILL_OWNER_00001')
ON CONFLICT (id) DO NOTHING;

INSERT INTO milk_service.milk_records
    (id, tenant_id, session_id, cattle_id, quantity_liters, recorded_at,
     recorded_by, created_by, updated_by)
VALUES
    ('MRC_HILL_AM_0000001', 'TEN_HILL_CREAMERY_00000001', 'MSS_HILL_AM_0000001',
     'CAT_HILL_NANDINI_001', 7.300, '2026-09-15 06:30:00+05:30',
     'USR_HILL_OWNER_00001', 'USR_HILL_OWNER_00001', 'USR_HILL_OWNER_00001')
ON CONFLICT (id) DO NOTHING;

SET LOCAL search_path = public;  -- set by deploy/seed: this service's own search_path
INSERT INTO public.audit_logs
    (id, tenant_id, actor_id, actor_type, action, resource_type, resource_id,
     old_value, new_value, service_name, created_at, created_by)
VALUES
    ('AUD_SEED_HILL_000001', 'TEN_HILL_CREAMERY_00000001', 'USR_HILL_OWNER_00001',
     'user', 'milk.record.created', 'milk_record', 'MRC_HILL_AM_0000001',
     NULL, '{"quantity_liters":"7.300"}', 'milk-service',
     '2026-09-15 06:30:00+05:30', 'USR_HILL_OWNER_00001')
ON CONFLICT (id) DO NOTHING;

-- Each tenant's chain is its own: a checkpoint for Valley says nothing about
-- Hill, which is what makes one tenant's trail verifiable without the other's.
SELECT gavya_seal_audit_log('TEN_HILL_CREAMERY_00000001');
SELECT gavya_checkpoint_audit_chain('TEN_HILL_CREAMERY_00000001');

COMMIT;
