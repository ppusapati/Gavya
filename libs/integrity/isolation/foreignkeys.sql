-- Foreign keys that cannot cross a tenant boundary.
--
-- Row-level security does not cover this, and the gap is not obvious. A foreign
-- key is checked by the system rather than by the querying role, so the check is
-- not subject to the policies. Against this schema, as it shipped:
--
--     alpha> SELECT count(*) FROM milk_sessions WHERE id = '<one of beta's>';
--     0
--     alpha> INSERT INTO milk_records (tenant_id, session_id, ...)
--            VALUES ('<alpha>', '<beta's session>', ...);
--     INSERT 0 1
--
-- Alpha cannot see the session and can still record milk against it. Two things
-- follow. The rows are wrong in a way that reads as corruption rather than as an
-- attack: alpha's own queries join to a session they cannot see and come back
-- empty. And the constraint answers a question it should not — whether an
-- identifier exists in some other tenant — one probe at a time, which is an
-- enumeration channel straight through the isolation boundary.
--
-- The fix is to put the tenant in the key. A reference from (tenant, row) can
-- then only resolve to a row of the same tenant, and the database enforces it
-- for the same reason it enforces the rest of referential integrity.
--
--
-- WHY THIS IS DERIVED RATHER THAN WRITTEN OUT
--
-- Forty-six constraints across thirty-two target tables, each needing a matching
-- unique key. Written by hand that is forty-six chances to transpose a column
-- name, and the ones that were got wrong would still work — a foreign key that
-- names the wrong pair of columns is a constraint, just not the one intended.
-- Derived from the catalogue, the conversion is the same operation every time
-- and the report says which constraints it changed.
--
--
-- WHAT IS DELIBERATELY LEFT ALONE
--
-- A foreign key whose target has no tenant of its own — a lookup table shared by
-- everybody — is not converted, because there is no tenant on the far side to
-- match against. Those appear in the report so the decision is visible rather
-- than implied.
--
-- And this only reaches references that are declared. Of 136 columns named for a
-- reference, 46 have a foreign key; the other 90 include milk_sessions.cattle_id
-- and cattle.farm_id, which point at a real table and are enforced by nothing at
-- all. Those are not fixed here: some of the 90 are not references (a trace
-- identifier, an identifier from somebody else's system), and adding a
-- constraint to the rest is a decision per column about what the data already
-- contains. gavya_unconstrained_reference_report lists them so the choice is in
-- front of somebody rather than implied by silence.

-- ---------------------------------------------------------------------------
-- The conversion
-- ---------------------------------------------------------------------------

-- gavya_fk_action turns a catalogue code back into the clause that produced it.
-- Preserving the action matters: silently turning a CASCADE into NO ACTION would
-- leave rows behind that the schema says should have gone.
CREATE OR REPLACE FUNCTION gavya_fk_action(code "char") RETURNS text AS $fn$
BEGIN
    CASE code
        WHEN 'a' THEN RETURN 'NO ACTION';
        WHEN 'r' THEN RETURN 'RESTRICT';
        WHEN 'c' THEN RETURN 'CASCADE';
        ELSE
            -- SET NULL and SET DEFAULT on a composite key would target the
            -- tenant column too, which is NOT NULL, so the action would fail at
            -- the moment it was needed. PostgreSQL 15 and later can name the
            -- columns to set; this schema has neither action today, so rather
            -- than emit a clause nothing here has ever exercised, it stops.
            RAISE EXCEPTION 'gavya: foreign key action % is not handled', code
                USING HINT = 'a composite key needs ON DELETE SET NULL (column); '
                             'add and test that clause before using this action';
    END CASE;
END
$fn$ LANGUAGE plpgsql IMMUTABLE;

DROP FUNCTION IF EXISTS gavya_make_foreign_keys_tenant_safe(text);
CREATE FUNCTION gavya_make_foreign_keys_tenant_safe(p_schema text DEFAULT NULL)
RETURNS TABLE(schema_name text, constraint_name text, outcome text) AS $fn$
DECLARE
    r         record;
    src_col   text;
    tgt_col   text;
    uniq_name text;
    upd       text;
    del       text;
BEGIN
    FOR r IN
        SELECT c.oid, c.conname, c.conrelid, c.confrelid, c.conkey, c.confkey,
               c.confupdtype, c.confdeltype,
               n.nspname  AS src_nsp, src.relname AS src_rel,
               tn.nspname AS tgt_nsp, tgt.relname AS tgt_rel
        FROM pg_constraint c
        JOIN pg_class src     ON src.oid = c.conrelid
        JOIN pg_namespace n   ON n.oid = src.relnamespace
        JOIN pg_class tgt     ON tgt.oid = c.confrelid
        JOIN pg_namespace tn  ON tn.oid = tgt.relnamespace
        WHERE c.contype = 'f'
          AND n.nspname NOT IN ('pg_catalog', 'information_schema')
          AND n.nspname NOT LIKE 'pg_%'
          AND (p_schema IS NULL OR n.nspname = p_schema)
        ORDER BY n.nspname, src.relname, c.conname
    LOOP
        schema_name := r.src_nsp;
        constraint_name := r.conname;

        -- Already carries the tenant.
        IF EXISTS (
            SELECT 1 FROM unnest(r.conkey) k
            JOIN pg_attribute a ON a.attrelid = r.conrelid AND a.attnum = k
            WHERE a.attname = 'tenant_id'
        ) THEN
            outcome := 'already tenant-safe';
            RETURN NEXT;
            CONTINUE;
        END IF;

        -- Nothing to match against on one side or the other.
        IF NOT EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = r.conrelid
                         AND a.attname = 'tenant_id' AND a.attnum > 0 AND NOT a.attisdropped)
           OR NOT EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = r.confrelid
                            AND a.attname = 'tenant_id' AND a.attnum > 0 AND NOT a.attisdropped) THEN
            outcome := format('left alone: %I.%I or %I.%I has no tenant of its own',
                              r.src_nsp, r.src_rel, r.tgt_nsp, r.tgt_rel);
            RETURN NEXT;
            CONTINUE;
        END IF;

        IF array_length(r.conkey, 1) <> 1 THEN
            outcome := 'left alone: more than one column and no tenant among them';
            RETURN NEXT;
            CONTINUE;
        END IF;

        SELECT a.attname INTO src_col FROM pg_attribute a
        WHERE a.attrelid = r.conrelid AND a.attnum = r.conkey[1];
        SELECT a.attname INTO tgt_col FROM pg_attribute a
        WHERE a.attrelid = r.confrelid AND a.attnum = r.confkey[1];

        -- A composite foreign key needs a unique key to point at. Added if the
        -- target does not already have one on exactly (tenant_id, col).
        uniq_name := left(r.tgt_rel || '_tenant_' || tgt_col || '_key', 63);
        IF NOT EXISTS (
            SELECT 1 FROM pg_constraint u
            WHERE u.conrelid = r.confrelid AND u.contype IN ('p', 'u')
              AND array_length(u.conkey, 1) = 2
              AND (SELECT count(*) FROM unnest(u.conkey) k
                   JOIN pg_attribute a ON a.attrelid = u.conrelid AND a.attnum = k
                   WHERE a.attname IN ('tenant_id', tgt_col)) = 2
        ) THEN
            EXECUTE format('ALTER TABLE %I.%I ADD CONSTRAINT %I UNIQUE (tenant_id, %I)',
                           r.tgt_nsp, r.tgt_rel, uniq_name, tgt_col);
        END IF;

        upd := gavya_fk_action(r.confupdtype);
        del := gavya_fk_action(r.confdeltype);

        EXECUTE format('ALTER TABLE %I.%I DROP CONSTRAINT %I',
                       r.src_nsp, r.src_rel, r.conname);
        EXECUTE format(
            'ALTER TABLE %I.%I ADD CONSTRAINT %I FOREIGN KEY (tenant_id, %I) '
            'REFERENCES %I.%I (tenant_id, %I) ON UPDATE %s ON DELETE %s',
            r.src_nsp, r.src_rel, r.conname, src_col,
            r.tgt_nsp, r.tgt_rel, tgt_col, upd, del);

        outcome := format('now (tenant_id, %I) -> %I.%I (tenant_id, %I)',
                          src_col, r.tgt_nsp, r.tgt_rel, tgt_col);
        RETURN NEXT;
    END LOOP;
END
$fn$ LANGUAGE plpgsql;

COMMENT ON FUNCTION gavya_make_foreign_keys_tenant_safe(text) IS
    'Rewrite single-column foreign keys between tenant-owned tables to carry the tenant.';

-- ---------------------------------------------------------------------------
-- Checking
-- ---------------------------------------------------------------------------

DROP VIEW IF EXISTS gavya_foreign_key_report;
CREATE VIEW gavya_foreign_key_report AS
SELECT
    n.nspname  AS schema_name,
    src.relname AS table_name,
    c.conname  AS constraint_name,
    tgt.relname AS references_table,
    EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.conrelid
              AND a.attname = 'tenant_id' AND a.attnum > 0 AND NOT a.attisdropped)
      AND EXISTS (SELECT 1 FROM pg_attribute a WHERE a.attrelid = c.confrelid
                    AND a.attname = 'tenant_id' AND a.attnum > 0 AND NOT a.attisdropped)
      AS both_sides_have_a_tenant,
    EXISTS (SELECT 1 FROM unnest(c.conkey) k
            JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k
            WHERE a.attname = 'tenant_id') AS carries_the_tenant
FROM pg_constraint c
JOIN pg_class src    ON src.oid = c.conrelid
JOIN pg_namespace n  ON n.oid = src.relnamespace
JOIN pg_class tgt    ON tgt.oid = c.confrelid
WHERE c.contype = 'f'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
ORDER BY n.nspname, src.relname, c.conname;

-- gavya_unconstrained_reference_report lists columns that look like references
-- and are enforced by nothing.
--
-- Named by convention only, these carry no integrity guarantee and no tenant
-- guarantee: nothing stops one holding an identifier from another tenant, or one
-- that was deleted, or one that never existed. Making it a foreign key is a
-- decision per column — some of these genuinely are not references — so this
-- reports rather than acts.
-- Two views in references.sql are built on this one, so they have to go first.
--
-- Without these two lines this file applies exactly once. On the second run the
-- DROP below is refused — PostgreSQL will not drop a view something depends on
-- — and because the DROP fails the CREATE after it fails too, and the whole
-- file dies here. Every deployment after the first would stop at this line, and
-- the checks in the rest of the file would silently stop being applied.
--
-- The same defect was found in references.sql, one file over, where a
-- DROP FUNCTION was refused by a view built on it. It is worth naming the
-- shape: a schema file that only works on an empty database is a schema file
-- that works once, and the second time is in production.
--
-- Dropped by name rather than with CASCADE. CASCADE would take out whatever
-- happened to depend on this view and not put it back, so a deployment that ran
-- this file and then did not run references.sql would come up with the
-- reference-decision checks quietly missing — a control reporting success while
-- doing nothing, which is the failure mode this whole directory exists to
-- prevent. Named drops fail loudly if the pair ever gets out of step.
-- references.sql recreates both, and the deploy runs it after this file.
DROP VIEW IF EXISTS gavya_undecided_references;
DROP VIEW IF EXISTS gavya_unguessable_references;

DROP VIEW IF EXISTS gavya_unconstrained_reference_report;
CREATE VIEW gavya_unconstrained_reference_report AS
SELECT
    n.nspname AS schema_name,
    c.relname AS table_name,
    a.attname AS column_name,
    -- The table this would point at if the naming holds, when one exists by
    -- that name. A guess, offered to shorten the reading, not to act on.
    (SELECT t.relname FROM pg_class t
     JOIN pg_namespace tn ON tn.oid = t.relnamespace
     WHERE t.relkind = 'r' AND tn.nspname = n.nspname
       AND t.relname IN (
           left(a.attname, length(a.attname) - 3) || 's',
           left(a.attname, length(a.attname) - 3))
     LIMIT 1) AS probably_references
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
WHERE c.relkind = 'r'
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND a.attname LIKE '%\_id'
  AND a.attname <> 'tenant_id'
  AND NOT EXISTS (
      SELECT 1 FROM pg_constraint k
      WHERE k.contype = 'f' AND k.conrelid = c.oid AND a.attnum = ANY (k.conkey))
ORDER BY n.nspname, c.relname, a.attname;
