-- An audit trail that can be shortened without anybody noticing is not one.
--
-- Two separate problems, and the schema above solves neither.
--
--
-- A RECORD WRITTEN AFTERWARDS CAN BE LOST
--
-- The obvious arrangement is that a service makes its change and then calls the
-- audit service to record it. Every failure between those two — a restart, a
-- dropped connection, the audit service being down for a minute — leaves the
-- change made and unrecorded. Nothing reports it, because from the caller's
-- point of view the change succeeded.
--
-- For a platform whose pitch is that its numbers can be trusted, a trail with
-- holes is worse than no trail: it is a complete-looking record of an incomplete
-- set of events, and the missing ones are disproportionately the interesting
-- ones, since the same conditions that lose a record are the conditions under
-- which things go wrong.
--
-- So the record is written in the same transaction as the change. All services
-- share one database, so audit_logs is reachable from any of their connections,
-- and libs/integrity/audit writes into the caller's transaction. Either both
-- land or neither does, and that is not a discipline anybody has to remember —
-- it is what a transaction means.
--
--
-- A RECORD THAT CAN BE EDITED IS EVIDENCE OF NOTHING
--
-- The rest is about somebody with access to the database. Two layers.
--
-- Append-only: the application role may insert and read, and may not update or
-- delete. Enforced by grant and by trigger rather than by convention, because
-- convention is what "the application never updates audit rows" was before
-- somebody wrote a cleanup script.
--
-- And a hash chain, because append-only stops the application, not the person
-- holding the database. Each sealed row carries the hash of the row before it,
-- so removing a row or changing a field breaks every link after it and the break
-- names the row where it happened. That does not make tampering impossible. It
-- makes it detectable, which is the property an auditor is actually asking about.
--
--
-- WHAT THE CHAIN DOES NOT CATCH, AND WHAT DOES
--
-- A chain detects a row changed and a row removed from the middle. It does not
-- detect the end being cut off, because a shorter chain is still a valid chain:
-- delete the last two rows and everything that remains verifies perfectly. That
-- is not a flaw in this implementation, it is what a hash chain is, and any
-- claim of tamper-evidence that does not say so is overselling.
--
-- The answer is an anchor: the head of the chain recorded somewhere the person
-- editing the rows does not control. gavya_checkpoint_audit_chain writes one,
-- and verification compares the head against the newest one — so truncation
-- becomes a chain that is *behind* where it was, which is loud.
--
-- Kept in this same database, a checkpoint only defends against somebody who
-- forgets to edit it too. That is worth having and it is not the real defence.
-- The real defence is getting the checkpoint out of the database — the audit
-- service exports each one, and an exported head is a number an auditor can hold
-- that nobody with a database connection can reach.
--
--
-- WHY SEALING IS SEPARATE FROM WRITING
--
-- Chaining needs an order, and an order needs writers to agree on one. Doing it
-- during the insert means taking a per-tenant lock inside the business
-- transaction and holding it until that transaction commits — which serialises
-- every write for a tenant behind every other, including the slow ones. A
-- chilling centre would not notice; a plant taking thousands of collections a
-- minute would stop.
--
-- So rows are written unsealed and unordered, and a sealer walks the unsealed
-- ones afterwards and links them. The cost is that the newest rows are not yet
-- covered, and that is reported rather than glossed: gavya_audit_chain_status
-- says how many rows are unsealed and how old the oldest one is, so "the last
-- ten seconds are not yet tamper-evident" is a fact somebody can see rather than
-- an assumption they have to make.

-- ---------------------------------------------------------------------------
-- The chain columns
-- ---------------------------------------------------------------------------

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                   WHERE table_name = 'audit_logs' AND column_name = 'seq') THEN
        -- Position in this tenant's chain. Null until sealed.
        ALTER TABLE audit_logs ADD COLUMN seq BIGINT;
        -- The hash of the row before this one, or a fixed opening value for the
        -- first. Null until sealed.
        ALTER TABLE audit_logs ADD COLUMN previous_hash TEXT;
        -- This row's own hash, over its content and the previous hash.
        ALTER TABLE audit_logs ADD COLUMN row_hash TEXT;
        ALTER TABLE audit_logs ADD COLUMN sealed_at TIMESTAMPTZ;
    END IF;
END
$$;

-- One position per tenant, so two sealers cannot both claim it.
CREATE UNIQUE INDEX IF NOT EXISTS audit_logs_tenant_seq ON audit_logs (tenant_id, seq)
    WHERE seq IS NOT NULL;

-- Finding what still needs sealing has to stay cheap as the table grows, so the
-- index covers only the unsealed rows.
CREATE INDEX IF NOT EXISTS audit_logs_unsealed ON audit_logs (tenant_id, created_at, id)
    WHERE seq IS NULL;

-- ---------------------------------------------------------------------------
-- Append-only
-- ---------------------------------------------------------------------------

-- gavya_audit_is_append_only refuses any change to a row that is not the sealer
-- filling in the chain columns of a row that had none.
--
-- A trigger rather than only a grant, because a grant protects against the
-- application role and this also protects against the owner — which is who runs
-- the migration that was going to "just tidy up the old rows".
CREATE OR REPLACE FUNCTION gavya_audit_is_append_only() RETURNS trigger AS $fn$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'audit_logs is append-only: row % cannot be deleted', OLD.id
            USING ERRCODE = '42501',
                  HINT = 'retention is done by exporting and then dropping a partition, '
                         'not by deleting rows out of the middle of the chain';
    END IF;

    -- The only permitted change: an unsealed row becoming a sealed one.
    IF OLD.seq IS NOT NULL THEN
        RAISE EXCEPTION 'audit_logs is append-only: row % is already sealed', OLD.id
            USING ERRCODE = '42501';
    END IF;
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.tenant_id     IS DISTINCT FROM OLD.tenant_id
       OR NEW.actor_id      IS DISTINCT FROM OLD.actor_id
       OR NEW.actor_type    IS DISTINCT FROM OLD.actor_type
       OR NEW.action        IS DISTINCT FROM OLD.action
       OR NEW.resource_type IS DISTINCT FROM OLD.resource_type
       OR NEW.resource_id   IS DISTINCT FROM OLD.resource_id
       OR NEW.old_value     IS DISTINCT FROM OLD.old_value
       OR NEW.new_value     IS DISTINCT FROM OLD.new_value
       OR NEW.created_at    IS DISTINCT FROM OLD.created_at
       OR NEW.created_by    IS DISTINCT FROM OLD.created_by THEN
        RAISE EXCEPTION 'audit_logs is append-only: row % cannot have its content changed', OLD.id
            USING ERRCODE = '42501';
    END IF;
    RETURN NEW;
END
$fn$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS audit_logs_append_only ON audit_logs;
CREATE TRIGGER audit_logs_append_only
    BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION gavya_audit_is_append_only();

-- ---------------------------------------------------------------------------
-- The hash
-- ---------------------------------------------------------------------------

-- gavya_audit_row_hash is what a row hashes to, given what came before it.
--
-- Every field that carries meaning is in it. A field left out is a field that
-- can be changed without breaking the chain, which is the same as not recording
-- it. The separator is a character that cannot appear in any of the values, so
-- two different rows cannot produce the same input by shifting a boundary — an
-- actor of "ab" with an action of "c" must not hash the same as an actor of "a"
-- with an action of "bc".
CREATE OR REPLACE FUNCTION gavya_audit_row_hash(
    p_previous_hash text,
    p_id            text,
    p_tenant_id     text,
    p_actor_id      text,
    p_actor_type    text,
    p_action        text,
    p_resource_type text,
    p_resource_id   text,
    p_old_value     text,
    p_new_value     text,
    p_created_at    timestamptz,
    p_created_by    text,
    p_seq           bigint
) RETURNS text AS $fn$
BEGIN
    RETURN encode(sha256(convert_to(
        concat_ws(E'\\x1f',
            coalesce(p_previous_hash, ''),
            p_id, p_tenant_id, p_actor_id, p_actor_type, p_action,
            p_resource_type, p_resource_id,
            coalesce(p_old_value, ''), coalesce(p_new_value, ''),
            -- To the microsecond and in UTC, so the same instant always renders
            -- the same string whatever the session's time zone is set to.
            to_char(p_created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US'),
            p_created_by,
            p_seq::text
        ), 'UTF8')), 'hex');
END
$fn$ LANGUAGE plpgsql IMMUTABLE;

-- The opening link. A fixed, named value rather than an empty string, so a chain
-- that starts is distinguishable from one whose first row lost its previous_hash.
CREATE OR REPLACE FUNCTION gavya_audit_genesis_hash() RETURNS text AS $fn$
    SELECT encode(sha256(convert_to('gavya.audit.chain.genesis.v1', 'UTF8')), 'hex');
$fn$ LANGUAGE sql IMMUTABLE;

-- ---------------------------------------------------------------------------
-- Sealing
-- ---------------------------------------------------------------------------

-- gavya_seal_audit_log links the unsealed rows of one tenant into the chain.
--
-- Ordered by creation time and then by id, so the order is the same whoever runs
-- it and whenever: two sealers on the same rows produce the same chain.
--
-- The advisory lock is on the sealer, not on the writers, which is the whole
-- point of doing this separately. Writers never take it.
CREATE OR REPLACE FUNCTION gavya_seal_audit_log(p_tenant_id text, p_limit int DEFAULT 10000)
RETURNS TABLE(sealed bigint, last_seq bigint, last_hash text) AS $fn$
DECLARE
    r          record;
    v_seq      bigint;
    v_previous text;
    v_hash     text;
    v_count    bigint := 0;
BEGIN
    -- Transaction-scoped, released on commit. Two sealers on one tenant would
    -- otherwise both read the same tail and write the same sequence numbers;
    -- the unique index would stop the second, but as a lost round of work
    -- rather than as a wait.
    PERFORM pg_advisory_xact_lock(hashtext('gavya.audit.seal:' || p_tenant_id));

    SELECT a.seq, a.row_hash INTO v_seq, v_previous
    FROM audit_logs a
    WHERE a.tenant_id = p_tenant_id AND a.seq IS NOT NULL
    ORDER BY a.seq DESC
    LIMIT 1;

    IF v_seq IS NULL THEN
        v_seq := 0;
        v_previous := gavya_audit_genesis_hash();
    END IF;

    FOR r IN
        SELECT * FROM audit_logs a
        WHERE a.tenant_id = p_tenant_id AND a.seq IS NULL
        ORDER BY a.created_at, a.id
        LIMIT p_limit
    LOOP
        v_seq := v_seq + 1;
        v_hash := gavya_audit_row_hash(
            v_previous, r.id, r.tenant_id, r.actor_id, r.actor_type, r.action,
            r.resource_type, r.resource_id, r.old_value, r.new_value,
            r.created_at, r.created_by, v_seq);

        UPDATE audit_logs
        SET seq = v_seq, previous_hash = v_previous, row_hash = v_hash, sealed_at = now()
        WHERE id = r.id;

        v_previous := v_hash;
        v_count := v_count + 1;
    END LOOP;

    sealed := v_count;
    last_seq := v_seq;
    last_hash := v_previous;
    RETURN NEXT;
END
$fn$ LANGUAGE plpgsql;

COMMENT ON FUNCTION gavya_seal_audit_log(text, int) IS
    'Links a tenant''s unsealed audit rows into its hash chain.';

-- ---------------------------------------------------------------------------
-- Verifying
-- ---------------------------------------------------------------------------

-- gavya_verify_audit_chain recomputes a tenant's chain and reports the first
-- place it stops matching.
--
-- Recomputed rather than compared against a stored total: a stored total is
-- another row somebody with database access can edit. The chain is checked
-- against the content it claims to cover, which is the only check that means
-- anything.
CREATE OR REPLACE FUNCTION gavya_verify_audit_chain(p_tenant_id text)
RETURNS TABLE(
    intact       boolean,
    rows_checked bigint,
    broken_at    bigint,
    broken_id    text,
    detail       text
) AS $fn$
DECLARE
    r          record;
    v_expected text := gavya_audit_genesis_hash();
    v_seq      bigint := 0;
    v_count    bigint := 0;
    v_hash     text;
BEGIN
    FOR r IN
        SELECT * FROM audit_logs a
        WHERE a.tenant_id = p_tenant_id AND a.seq IS NOT NULL
        ORDER BY a.seq
    LOOP
        v_seq := v_seq + 1;

        -- A gap. Not the only thing that catches a removed row — the
        -- previous_hash check below would too, since the row after the gap
        -- points at a hash that is no longer the running one — but it names
        -- what happened instead of saying the chain does not line up, and the
        -- difference matters to whoever has to explain it.
        IF r.seq <> v_seq THEN
            intact := false; rows_checked := v_count; broken_at := r.seq; broken_id := r.id;
            detail := format('the chain jumps from %s to %s, so %s row(s) are missing',
                             v_seq - 1, r.seq, r.seq - v_seq);
            RETURN NEXT; RETURN;
        END IF;

        IF r.previous_hash IS DISTINCT FROM v_expected THEN
            intact := false; rows_checked := v_count; broken_at := r.seq; broken_id := r.id;
            detail := 'this row does not follow the one before it; a row was removed or reordered';
            RETURN NEXT; RETURN;
        END IF;

        v_hash := gavya_audit_row_hash(
            r.previous_hash, r.id, r.tenant_id, r.actor_id, r.actor_type, r.action,
            r.resource_type, r.resource_id, r.old_value, r.new_value,
            r.created_at, r.created_by, r.seq);

        IF r.row_hash IS DISTINCT FROM v_hash THEN
            intact := false; rows_checked := v_count; broken_at := r.seq; broken_id := r.id;
            detail := 'this row''s content does not match its own hash; a field was changed';
            RETURN NEXT; RETURN;
        END IF;

        v_expected := v_hash;
        v_count := v_count + 1;
    END LOOP;

    -- Everything that is here hangs together. That leaves the one thing
    -- rehashing cannot see: rows that are no longer here at all. A chain shorter
    -- than the last checkpoint has lost its tail.
    DECLARE
        v_cp_seq  bigint;
        v_cp_hash text;
    BEGIN
        SELECT c.seq, c.row_hash INTO v_cp_seq, v_cp_hash
        FROM audit_chain_checkpoints c
        WHERE c.tenant_id = p_tenant_id
        ORDER BY c.seq DESC LIMIT 1;

        IF v_cp_seq IS NOT NULL AND v_seq < v_cp_seq THEN
            intact := false; rows_checked := v_count; broken_at := v_seq + 1; broken_id := NULL;
            detail := format('the chain ends at %s but was anchored at %s, so %s row(s) have been '
                             'removed from the end — a shorter chain still verifies, which is why '
                             'the anchor exists', v_seq, v_cp_seq, v_cp_seq - v_seq);
            RETURN NEXT; RETURN;
        END IF;

        IF v_cp_seq IS NOT NULL AND v_cp_seq = v_seq AND v_expected IS DISTINCT FROM v_cp_hash THEN
            intact := false; rows_checked := v_count; broken_at := v_seq; broken_id := NULL;
            detail := 'the chain has the expected length but its head does not match the anchor';
            RETURN NEXT; RETURN;
        END IF;
    END;

    intact := true; rows_checked := v_count; broken_at := NULL; broken_id := NULL;
    detail := NULL;
    RETURN NEXT;
END
$fn$ LANGUAGE plpgsql STABLE;

COMMENT ON FUNCTION gavya_verify_audit_chain(text) IS
    'Recomputes a tenant''s audit chain and names the first row where it breaks.';

-- gavya_audit_chain_status says how much of a tenant's trail is covered.
--
-- The unsealed count and the age of the oldest unsealed row are the honest part:
-- sealing happens after the fact, so the newest rows are not yet tamper-evident,
-- and how far behind that is should be something somebody can see rather than
-- something they assume.
DROP VIEW IF EXISTS gavya_audit_chain_status;
CREATE VIEW gavya_audit_chain_status AS
SELECT
    tenant_id,
    count(*)                                            AS total_rows,
    count(*) FILTER (WHERE seq IS NOT NULL)             AS sealed_rows,
    count(*) FILTER (WHERE seq IS NULL)                 AS unsealed_rows,
    max(seq)                                            AS last_seq,
    min(created_at) FILTER (WHERE seq IS NULL)          AS oldest_unsealed_at,
    now() - min(created_at) FILTER (WHERE seq IS NULL)  AS unsealed_for
FROM audit_logs
GROUP BY tenant_id;

-- ---------------------------------------------------------------------------
-- Anchoring the head
-- ---------------------------------------------------------------------------

-- A record of where a tenant's chain had got to at a moment in time.
--
-- Its purpose is to catch the one thing the chain cannot: the end being cut off.
-- A chain that is shorter than the last checkpoint says it was has lost rows,
-- and no amount of rehashing what remains will show it.
CREATE TABLE IF NOT EXISTS audit_chain_checkpoints (
    id         BIGSERIAL PRIMARY KEY,
    tenant_id  VARCHAR(26) NOT NULL,
    seq        BIGINT NOT NULL,
    row_hash   TEXT NOT NULL,
    taken_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Set once the checkpoint has been copied somewhere outside this database.
    -- Until then it defends only against somebody who forgets to edit two tables
    -- instead of one, and that difference is worth being able to see.
    exported_at TIMESTAMPTZ,
    UNIQUE (tenant_id, seq)
);

CREATE INDEX IF NOT EXISTS audit_chain_checkpoints_tenant
    ON audit_chain_checkpoints (tenant_id, seq DESC);

CREATE OR REPLACE FUNCTION gavya_checkpoint_audit_chain(p_tenant_id text)
RETURNS TABLE(seq bigint, row_hash text, taken boolean) AS $fn$
DECLARE
    v_seq  bigint;
    v_hash text;
BEGIN
    SELECT a.seq, a.row_hash INTO v_seq, v_hash
    FROM audit_logs a
    WHERE a.tenant_id = p_tenant_id AND a.seq IS NOT NULL
    ORDER BY a.seq DESC LIMIT 1;

    IF v_seq IS NULL THEN
        seq := NULL; row_hash := NULL; taken := false;
        RETURN NEXT; RETURN;
    END IF;

    -- Nothing new to anchor. Returning taken = false rather than writing a
    -- duplicate row, so the checkpoint table measures progress rather than how
    -- often somebody ran the job.
    IF EXISTS (SELECT 1 FROM audit_chain_checkpoints c
               WHERE c.tenant_id = p_tenant_id AND c.seq = v_seq) THEN
        seq := v_seq; row_hash := v_hash; taken := false;
        RETURN NEXT; RETURN;
    END IF;

    INSERT INTO audit_chain_checkpoints (tenant_id, seq, row_hash)
    VALUES (p_tenant_id, v_seq, v_hash);

    seq := v_seq; row_hash := v_hash; taken := true;
    RETURN NEXT;
END
$fn$ LANGUAGE plpgsql;

COMMENT ON FUNCTION gavya_checkpoint_audit_chain(text) IS
    'Records where a tenant''s chain has got to, so the end being cut off is detectable.';

-- ---------------------------------------------------------------------------
-- Privileges
-- ---------------------------------------------------------------------------

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'gavya_app') THEN
        -- Insert and read. Not update, not delete: the application has no reason
        -- to change a record of what it did, and the gap between "no reason" and
        -- "no permission" is where a cleanup script lives.
        REVOKE UPDATE, DELETE ON audit_logs FROM gavya_app;
        GRANT INSERT, SELECT ON audit_logs TO gavya_app;

        -- Sealing updates rows, so it runs with the definer's rights and the
        -- application role is allowed to ask for it — but only through this
        -- function, which can only ever fill in chain columns.
        GRANT EXECUTE ON FUNCTION gavya_seal_audit_log(text, int) TO gavya_app;
        GRANT EXECUTE ON FUNCTION gavya_verify_audit_chain(text) TO gavya_app;
        GRANT EXECUTE ON FUNCTION gavya_checkpoint_audit_chain(text) TO gavya_app;
        GRANT SELECT ON gavya_audit_chain_status TO gavya_app;
        GRANT SELECT, INSERT ON audit_chain_checkpoints TO gavya_app;
        GRANT UPDATE (exported_at) ON audit_chain_checkpoints TO gavya_app;
        GRANT USAGE, SELECT ON SEQUENCE audit_chain_checkpoints_id_seq TO gavya_app;
    END IF;
END
$$;

-- The sealer has to update rows the application role may not. SECURITY DEFINER
-- rather than a broader grant: the only update it can perform is the one written
-- above, and the append-only trigger still refuses anything else even from here.
ALTER FUNCTION gavya_seal_audit_log(text, int) SECURITY DEFINER;
