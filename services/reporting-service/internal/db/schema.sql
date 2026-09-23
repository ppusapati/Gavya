CREATE TABLE IF NOT EXISTS reports (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    name VARCHAR(200) NOT NULL,
    report_type VARCHAR(100) NOT NULL,
    parameters JSONB,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    file_path TEXT,
    file_format VARCHAR(20),
    requested_by VARCHAR(26) NOT NULL,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS report_schedules (
    id VARCHAR(26) PRIMARY KEY,
    tenant_id VARCHAR(26) NOT NULL,
    report_type VARCHAR(100) NOT NULL,
    schedule VARCHAR(100) NOT NULL,
    parameters JSONB,
    is_active BOOLEAN NOT NULL DEFAULT true,
    last_run_at TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by VARCHAR(26) NOT NULL,
    updated_by VARCHAR(26) NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_reports_tenant ON reports(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_schedules_tenant ON report_schedules(tenant_id, is_active);

-- ---------------------------------------------------------------------------
-- Running a report, and firing a schedule.
--
-- Everything above this line describes a request. Nothing in the platform acted
-- on one: a report was written with status 'pending' and stayed there, and
-- next_run_at was a column nothing computed. These columns are what a runner
-- needs to do the work and — more importantly — to say what happened when it
-- could not.
--
-- ALTER rather than columns added inside the CREATE TABLE above, because
-- CREATE TABLE IF NOT EXISTS does nothing when the table exists. That mistake
-- has been made in this repository before: production-service added columns
-- that way, they appeared on a fresh database and on no database that had ever
-- run the previous version, and the service failed at its first insert.
-- e2e/migration_test.go exists because of it.
-- ---------------------------------------------------------------------------

-- What the run produced.
--
-- The bytes live here rather than on a disk. There is no object storage in this
-- platform and no shared volume between replicas, so a file written by one pod
-- is a file the next request cannot read; and the procedure that was supposed
-- to hand it over returns a path rather than a URL, which no browser can fetch.
-- A few hundred kilobytes of CSV in a column the tenant's own policies already
-- protect is the arrangement that actually delivers the report.
ALTER TABLE reports ADD COLUMN IF NOT EXISTS content BYTEA;
ALTER TABLE reports ADD COLUMN IF NOT EXISTS content_type TEXT;
ALTER TABLE reports ADD COLUMN IF NOT EXISTS row_count BIGINT;

-- Whether the report is all of the answer.
--
-- A renderer stops at a ceiling rather than pulling an unbounded result into
-- memory. A truncated report that does not say so is worse than no report: it
-- is a total somebody will act on, short by an amount nothing on the page
-- discloses.
ALTER TABLE reports ADD COLUMN IF NOT EXISTS truncated BOOLEAN NOT NULL DEFAULT false;

-- Why it failed, and how many times it has been tried.
--
-- A report that failed with no reason recorded is one nobody can act on; the
-- person who asked for it sees 'failed' and has nothing to correct. attempts
-- bounds a crash loop: a report whose renderer panics the process would
-- otherwise be picked up again by the replacement, for ever.
ALTER TABLE reports ADD COLUMN IF NOT EXISTS failure_reason TEXT;
ALTER TABLE reports ADD COLUMN IF NOT EXISTS attempts INTEGER NOT NULL DEFAULT 0;

-- The zone a schedule's times are in.
--
-- Seven in the morning is seven where the society is. Evaluated in UTC, a
-- schedule set by a co-operative in Maharashtra fires at half past twelve in
-- the afternoon, and a report covering 'yesterday' covers a day that ended five
-- and a half hours before the one everybody means.
--
-- The column has a default because an ALTER on an existing table needs one, and
-- 'UTC' is the only defensible value for a row written before anybody was
-- asked: it is wrong for most societies and it is not a guess at which one they
-- are in. The service refuses to create a new schedule without a zone, so the
-- default applies to backfilled rows only — and a backfilled row is one
-- somebody should look at.
ALTER TABLE report_schedules ADD COLUMN IF NOT EXISTS timezone TEXT NOT NULL DEFAULT 'UTC';

-- What went wrong the last time this schedule fired.
--
-- A schedule that has stopped producing its report is noticed weeks later, when
-- somebody asks where the report went. This is where the answer is.
ALTER TABLE report_schedules ADD COLUMN IF NOT EXISTS last_error TEXT;

-- The runner claims work with FOR UPDATE SKIP LOCKED over these orders, and
-- both sweeps run across every tenant, so neither index can be tenant-first.
CREATE INDEX IF NOT EXISTS idx_reports_pending ON reports(status, created_at)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_schedules_due ON report_schedules(next_run_at)
    WHERE is_active AND deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- Reading across tenants.
--
-- Both sweeps have no single tenant, and the isolation policies refuse a read
-- with no tenant set — correctly. So each goes through a definer-rights
-- function, which is the same arrangement settlement-service's outbox uses and
-- for the same reason: it limits the cross-tenant read to one table and one
-- question rather than handing the sweep a way round the policies in general.
--
-- The search_path is pinned to this service's own schema. A definer-rights
-- function pins its path deliberately — that is what stops a caller redirecting
-- it at a table of their own — so the pin has to name where the tables actually
-- are. Naming `public` while the tables live elsewhere resolves nothing, and
-- goes on resolving nothing silently.
-- ---------------------------------------------------------------------------

-- Reports waiting to be run, oldest first.
--
-- Rows already claimed by another replica are skipped rather than waited for:
-- two runners should share the queue, not queue behind each other. A report
-- left in 'running' by a process that died is picked up by the reaper below
-- rather than here, because a row that is genuinely being worked on and a row
-- whose worker is gone look identical from this side.
CREATE OR REPLACE FUNCTION gavya_reporting_reports_to_run(p_limit integer, p_max_attempts integer)
RETURNS SETOF reports AS $fn$
BEGIN
    RETURN QUERY
        SELECT * FROM reports
         WHERE status = 'pending'
           AND deleted_at IS NULL
           AND attempts < p_max_attempts
         ORDER BY created_at
         LIMIT p_limit
           FOR UPDATE SKIP LOCKED;
END
$fn$ LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = reporting_service, public;

-- Reports whose runner never came back.
--
-- started_at older than the cutoff and still 'running' means the process that
-- claimed it is gone — there is no other way out of that state. Returned so the
-- runner can put them back, or fail them for good once they have had their
-- attempts.
CREATE OR REPLACE FUNCTION gavya_reporting_reports_abandoned(p_older_than interval, p_limit integer)
RETURNS SETOF reports AS $fn$
BEGIN
    RETURN QUERY
        SELECT * FROM reports
         WHERE status = 'running'
           AND deleted_at IS NULL
           AND started_at IS NOT NULL
           AND started_at + p_older_than <= NOW()
         ORDER BY started_at
         LIMIT p_limit
           FOR UPDATE SKIP LOCKED;
END
$fn$ LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = reporting_service, public;

-- Schedules whose next firing has passed.
--
-- A schedule with no next_run_at is one nothing has ever computed a firing for,
-- which is every row written before this existed. Those are included so the
-- runner adopts them on its first sweep rather than leaving them dormant for
-- ever — the state they were already in.
CREATE OR REPLACE FUNCTION gavya_reporting_schedules_due(p_limit integer)
RETURNS SETOF report_schedules AS $fn$
BEGIN
    RETURN QUERY
        SELECT * FROM report_schedules
         WHERE is_active
           AND deleted_at IS NULL
           AND (next_run_at IS NULL OR next_run_at <= NOW())
         ORDER BY next_run_at NULLS FIRST
         LIMIT p_limit
           FOR UPDATE SKIP LOCKED;
END
$fn$ LANGUAGE plpgsql VOLATILE SECURITY DEFINER SET search_path = reporting_service, public;

-- Granted to the application role where that role exists. It exists wherever
-- libs/integrity/isolation has been applied — every deployment — and not in a
-- test database built from this file alone, where the schema-applying user runs
-- the sweeps itself.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'gavya_app') THEN
        REVOKE ALL ON FUNCTION gavya_reporting_reports_to_run(integer, integer) FROM PUBLIC;
        GRANT EXECUTE ON FUNCTION gavya_reporting_reports_to_run(integer, integer) TO gavya_app;
        REVOKE ALL ON FUNCTION gavya_reporting_reports_abandoned(interval, integer) FROM PUBLIC;
        GRANT EXECUTE ON FUNCTION gavya_reporting_reports_abandoned(interval, integer) TO gavya_app;
        REVOKE ALL ON FUNCTION gavya_reporting_schedules_due(integer) FROM PUBLIC;
        GRANT EXECUTE ON FUNCTION gavya_reporting_schedules_due(integer) TO gavya_app;
    END IF;
END
$$;
