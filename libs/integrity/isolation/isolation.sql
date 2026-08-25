-- Tenant isolation, enforced by the database rather than by every query.
--
-- Application code scopes queries by tenant. It does so correctly almost all of
-- the time, and the exceptions are not distributed randomly: they cluster in the
-- statements nobody reads twice. Two were found in cattle-market alone — an
-- UPDATE whose WHERE clause had lost its tenant, and a list query that never had
-- one. Both returned plausible results. Neither test failed.
--
-- Row-level security moves the check to the one place every query has to pass
-- through. A statement that forgets its tenant then returns nothing or is
-- refused, instead of returning somebody else's milk.
--
--
-- WHO CONNECTS MATTERS MORE THAN THE POLICIES
--
-- A superuser bypasses row-level security entirely, and so does a role holding
-- BYPASSRLS. Every service in docker-compose.yaml connects as `postgres`. Under
-- that user these policies do exactly nothing, and nothing about the database
-- says so: the tables report RLS enabled, the policies are listed, and every
-- cross-tenant read succeeds.
--
-- So this file creates two roles and the difference between them is the whole
-- control:
--
--   gavya_owner  owns the tables and holds BYPASSRLS. Migrations, backfills and
--                support queries run as this role. Bypassing is something a
--                person does deliberately, under a name that appears in the
--                logs.
--
--   gavya_app    is what the services connect as. NOSUPERUSER, NOBYPASSRLS, and
--                not the owner of anything. It cannot escape the policies by any
--                route.
--
-- The tables are FORCE-enabled as well as enabled. FORCE subjects the owner to
-- the policies too, so pointing a service at the owning role by mistake fails
-- closed rather than silently disabling isolation. Deliberate bypass then
-- requires the BYPASSRLS grant, which is explicit and auditable, rather than
-- being an accident of which user ran the deployment.
--
--
-- WHY THE UNSET CASE RAISES RATHER THAN RETURNS NOTHING
--
-- A query that silently finds nothing is indistinguishable from a tenant that
-- genuinely has no data, so a connection-handling bug reaches production wearing
-- an empty result set. The policies therefore go through gavya_current_tenant(),
-- which raises instead of returning nothing.
--
-- A plain current_setting('app.tenant_id') looks like it does the same job, and
-- on a fresh connection it does: an unset parameter raises 42704. It stops doing
-- it the moment connections are pooled. Once any request has set the parameter,
-- the session has it defined, and RESET — or set_config to NULL — leaves it as
-- the empty string rather than undefined. current_setting then returns '',
-- nothing raises, and every subsequent request that forgot its tenant quietly
-- matches no rows.
--
-- That failure only appears under connection reuse: under load, in production,
-- and never in a test that opens one connection and closes it. So the empty
-- string is treated as what it is — a connection that was handed back without a
-- tenant — and refused in the same way as a missing one.
--
--
-- WHY SOFT DELETION IS NOT IN HERE
--
-- An earlier attempt at these policies included `deleted_at IS NULL` in the read
-- policy. That conflates two unrelated things. Visibility of deleted rows is a
-- product question with legitimate exceptions — audit, restore, an auditor
-- asking what was removed — while tenancy has none. Worse, a SELECT policy that
-- hides deleted rows makes un-deleting one impossible, because the UPDATE cannot
-- see the row it is meant to change.

-- ---------------------------------------------------------------------------
-- Roles
-- ---------------------------------------------------------------------------

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'gavya_owner') THEN
        CREATE ROLE gavya_owner NOLOGIN NOSUPERUSER BYPASSRLS NOCREATEROLE;
    END IF;
    -- LOGIN with no password. Under any real authentication method a
    -- passwordless role cannot connect, so the default state is closed; the
    -- deployment sets the password, which is where a credential belongs. The
    -- alternative — creating it NOLOGIN — leaves services unable to connect on
    -- first deploy, and that gets resolved at 2am by granting somebody
    -- superuser.
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'gavya_app') THEN
        CREATE ROLE gavya_app LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE;
    END IF;

    -- Re-asserted on every run rather than only at creation. A role that was
    -- created by hand, or granted BYPASSRLS during an incident and never
    -- reverted, is the failure this is here to catch — and it is invisible
    -- otherwise, because everything keeps working.
    ALTER ROLE gavya_app LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE;
END
$$;

-- ---------------------------------------------------------------------------
-- The tenant of the current connection
-- ---------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION gavya_current_tenant() RETURNS text AS $fn$
DECLARE
    v text;
BEGIN
    v := current_setting('app.tenant_id', true);
    -- NULL is a connection that never had a tenant. Empty is a pooled
    -- connection that had one, was reset, and has been handed to a request that
    -- did not set one. Both are the same mistake and neither may return rows.
    IF v IS NULL OR v = '' THEN
        RAISE EXCEPTION 'no tenant is set on this connection'
            USING ERRCODE = '42501',
                  HINT = 'set app.tenant_id before querying; note that a reset '
                         'parameter reads as the empty string, not as missing';
    END IF;
    RETURN v;
END
$fn$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION gavya_current_tenant() IS
    'The tenant this connection is acting for. Raises rather than returning nothing when unset.';

-- ---------------------------------------------------------------------------
-- Applying the policy
-- ---------------------------------------------------------------------------

-- gavya_apply_tenant_isolation enables row-level security on every table that
-- has a tenant_id column, and reports what it did.
--
-- Driven by the column rather than by a list, because a list is a thing somebody
-- has to remember to add a table to. A new table with a tenant_id is isolated
-- the next time this runs; a new table without one shows up in the report below
-- as unprotected, where somebody has to look at it.
--
-- It sweeps every non-system schema for the same reason, and that is not
-- hypothetical: this database has a `masters` schema holding six tenant-owned
-- tables, and an audit scoped to `public` — which is what the earlier count of
-- 69 tenant-owned tables was — missed all six. Naming the schema would have left
-- them unprotected while the report said everything was covered.
-- Dropped rather than replaced: a CREATE OR REPLACE cannot change a function's
-- OUT parameters, so re-running this file after the signature changed failed
-- with the old definition left in place — which looks like a no-op and is not.
DROP FUNCTION IF EXISTS gavya_apply_tenant_isolation(text);
CREATE FUNCTION gavya_apply_tenant_isolation(p_schema text DEFAULT NULL)
RETURNS TABLE(schema_name text, table_name text, outcome text) AS $fn$
DECLARE
    r record;
BEGIN
    FOR r IN
        SELECT n.nspname AS nsp, c.relname, c.oid
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE c.relkind = 'r'
          AND n.nspname NOT IN ('pg_catalog', 'information_schema')
          AND n.nspname NOT LIKE 'pg_%'
          AND (p_schema IS NULL OR n.nspname = p_schema)
        ORDER BY n.nspname, c.relname
    LOOP
        schema_name := r.nsp;
        -- The tenants table is the registry of tenants themselves, so it has no
        -- tenant_id: its own primary key is the tenant. It still has to be
        -- isolated, or one tenant can enumerate every other tenant's name,
        -- contact address and plan.
        IF r.relname = 'tenants' THEN
            EXECUTE format('ALTER TABLE %I.%I ENABLE ROW LEVEL SECURITY', r.nsp, r.relname);
            EXECUTE format('ALTER TABLE %I.%I FORCE ROW LEVEL SECURITY', r.nsp, r.relname);
            EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON %I.%I', r.nsp, r.relname);
            EXECUTE format($p$
                CREATE POLICY tenant_isolation ON %I.%I
                FOR ALL
                USING (id = gavya_current_tenant())
                WITH CHECK (id = gavya_current_tenant())
            $p$, r.nsp, r.relname);
            table_name := r.relname;
            outcome := 'isolated on id';
            RETURN NEXT;
            CONTINUE;
        END IF;

        IF NOT EXISTS (
            SELECT 1 FROM pg_attribute a
            WHERE a.attrelid = r.oid AND a.attname = 'tenant_id'
              AND a.attnum > 0 AND NOT a.attisdropped
        ) THEN
            table_name := r.relname;
            outcome := 'no tenant_id column — NOT isolated, decide deliberately';
            RETURN NEXT;
            CONTINUE;
        END IF;

        EXECUTE format('ALTER TABLE %I.%I ENABLE ROW LEVEL SECURITY', r.nsp, r.relname);
        EXECUTE format('ALTER TABLE %I.%I FORCE ROW LEVEL SECURITY', r.nsp, r.relname);
        EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON %I.%I', r.nsp, r.relname);
        EXECUTE format($p$
            CREATE POLICY tenant_isolation ON %I.%I
            FOR ALL
            USING (tenant_id = gavya_current_tenant())
            WITH CHECK (tenant_id = gavya_current_tenant())
        $p$, r.nsp, r.relname);

        table_name := r.relname;
        outcome := 'isolated on tenant_id';
        RETURN NEXT;
    END LOOP;
END
$fn$ LANGUAGE plpgsql;

COMMENT ON FUNCTION gavya_apply_tenant_isolation(text) IS
    'Enable and force row-level security, with a tenant policy, on every table that carries a tenant.';

-- ---------------------------------------------------------------------------
-- Privileges
-- ---------------------------------------------------------------------------

-- gavya_grant_app_access gives the application role the rights it needs and
-- nothing more. Deliberately not the owner: ownership plus a future ALTER TABLE
-- ... NO FORCE would silently return the system to no isolation at all.
DROP FUNCTION IF EXISTS gavya_grant_app_access(text);
CREATE FUNCTION gavya_grant_app_access(p_schema text DEFAULT NULL)
RETURNS void AS $fn$
DECLARE
    s text;
BEGIN
    -- Every non-system schema, for the same reason the policies sweep them all:
    -- a grant that names `public` leaves the application unable to read `masters`
    -- and nobody finds out until a query fails in production.
    FOR s IN
        SELECT nspname FROM pg_namespace
        WHERE nspname NOT IN ('pg_catalog', 'information_schema')
          AND nspname NOT LIKE 'pg_%'
          AND (p_schema IS NULL OR nspname = p_schema)
    LOOP
        EXECUTE format('GRANT USAGE ON SCHEMA %I TO gavya_app', s);
        EXECUTE format('GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA %I TO gavya_app', s);
        EXECUTE format('GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA %I TO gavya_app', s);
        -- So a table created later is reachable without anybody remembering to
        -- re-run the grant.
        EXECUTE format('ALTER DEFAULT PRIVILEGES IN SCHEMA %I GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO gavya_app', s);
    END LOOP;
END
$fn$ LANGUAGE plpgsql;

-- ---------------------------------------------------------------------------
-- Checking
-- ---------------------------------------------------------------------------

-- gavya_isolation_report lists every table and whether it is actually protected,
-- so the question "is tenant isolation on?" has an answer that comes from the
-- database rather than from a deployment note.
DROP VIEW IF EXISTS gavya_isolation_report;
CREATE VIEW gavya_isolation_report AS
SELECT
    n.nspname AS schema_name,
    c.relname AS table_name,
    EXISTS (
        SELECT 1 FROM pg_attribute a
        WHERE a.attrelid = c.oid AND a.attname = 'tenant_id'
          AND a.attnum > 0 AND NOT a.attisdropped
    ) AS has_tenant_column,
    c.relrowsecurity AS rls_enabled,
    c.relforcerowsecurity AS rls_forced,
    (SELECT count(*) FROM pg_policy p WHERE p.polrelid = c.oid) AS policies
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind = 'r'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY n.nspname, c.relname;
