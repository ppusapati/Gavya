#!/bin/bash
# Build the database the services expect, and lock it down before they connect.
#
# Two things were missing from a compose deployment and each hid the other.
#
# Nothing created the tables. No service applies its schema at startup and
# nothing was mounted here, so `docker compose up` handed twenty-two services an
# empty database and every query failed on a relation that did not exist. The
# stack had never been run end to end.
#
# And every service connected as `postgres`. A superuser is not subject to
# row-level security, so the isolation policies would have been decorative even
# once the tables existed.
#
# This runs once, when the data directory is first created, before any service
# can reach the database. It is a shell script rather than a set of .sql files
# because the service schemas have to be applied in a loop — a fixed list is a
# thing somebody has to remember to add a new service to, and forgetting means a
# service starts against tables that are not there.
set -euo pipefail

: "${GAVYA_APP_PASSWORD:?the application role needs a password; set GAVYA_APP_PASSWORD}"

psql=(psql --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" --no-psqlrc --quiet
      --set ON_ERROR_STOP=1)

echo "gavya: applying the ULID polyfill"
"${psql[@]}" --file /repo/pkg/database/schema/gen_ulid_polyfill.sql

echo "gavya: applying service schemas"
count=0
for f in /repo/services/*/internal/db/schema.sql; do
    echo "gavya:   $(basename "$(dirname "$(dirname "$(dirname "$f")")")")"
    "${psql[@]}" --file "$f"
    count=$((count + 1))
done
if [ "$count" -eq 0 ]; then
    echo "gavya: no service schemas were found under /repo — the repository is not mounted" >&2
    exit 1
fi
echo "gavya: $count service schemas applied"

echo "gavya: applying tenant isolation"
"${psql[@]}" --file /repo/libs/integrity/isolation/isolation.sql
"${psql[@]}" --command "SELECT gavya_apply_tenant_isolation();"
"${psql[@]}" --command "SELECT gavya_grant_app_access();"

# Row-level security does not reach foreign keys: the check runs as the system,
# not as the querying role, so a single-column key lets one tenant reference
# another's row and reports whether it exists.
echo "gavya: making foreign keys tenant-safe"
"${psql[@]}" --file /repo/libs/integrity/isolation/foreignkeys.sql
"${psql[@]}" --command "SELECT gavya_make_foreign_keys_tenant_safe();"

# The references the schema names and does not enforce. After the converter,
# because it relies on the unique keys the converter adds.
echo "gavya: enforcing the declared references"
"${psql[@]}" --file /repo/libs/integrity/isolation/references.sql
"${psql[@]}" --command "SELECT constraint_name, outcome FROM gavya_enforce_references();" > /dev/null

# A reference-shaped column nobody has decided about.
#
# This is now a failure, and it was a warning until the backlog behind it was
# cleared. Both are defensible positions and only one of them is defensible at a
# time: while sixty-three columns had no decision, failing here would have meant
# a deployment that never finished and a check somebody switched off. With the
# backlog at zero, warning would mean the sixty-fourth arrives unnoticed and the
# work is undone one column at a time.
#
# Both views are counted. The first asks about columns whose target can be
# guessed from the name; the second about the ones where it cannot, which is the
# majority and was invisible until it was counted.
undecided=$("${psql[@]}" --tuples-only --no-align --command \
    "SELECT (SELECT count(*) FROM gavya_undecided_references)
          + (SELECT count(*) FROM gavya_unguessable_references)")
if [ "$undecided" != "0" ]; then
    echo "gavya: $undecided reference-shaped column(s) have no decision recorded." >&2
    echo "gavya: add each to gavya_reference_decisions in " \
         "libs/integrity/isolation/references.sql, saying whether it is a reference and why." >&2
    "${psql[@]}" --command "SELECT * FROM gavya_undecided_references" >&2
    "${psql[@]}" --command "SELECT * FROM gavya_unguessable_references" >&2
    exit 1
fi

# Some tables cannot be isolated by a tenant column because they do not have
# one, and are isolated another way. Applied after the sweep so it is the sweep's
# result that gets corrected, not the other way round.
for extra in /repo/services/*/internal/db/isolation.sql; do
    [ -e "$extra" ] || continue
    echo "gavya: applying $(basename "$(dirname "$(dirname "$(dirname "$extra")")")") isolation"
    "${psql[@]}" --file "$extra"
done

# Append-only enforcement and the hash chain. After the grants, because it
# revokes some of them back: the application may add to the audit trail and may
# not edit it.
echo "gavya: making the audit trail append-only and tamper-evident"
"${psql[@]}" --file /repo/services/audit-service/internal/db/tamper_evidence.sql

# The step above creates a table of its own, after the sweep has already run, so
# it would come up with no policy. Sweeping again covers it — and the sweep only
# touches row-level security, not grants, so the revokes just made survive it.
# Ordering the other way round instead would put the grants after the revokes and
# hand the application back the ability to edit the trail.
echo "gavya: re-sweeping for tables created since"
"${psql[@]}" --command "SELECT gavya_apply_tenant_isolation();" > /dev/null

# Refuse to finish with an audit table the application can rewrite. A trail that
# can be edited is not evidence, and coming up without saying so is how nobody
# finds out until an auditor asks.
audit_writable=$("${psql[@]}" --tuples-only --no-align --command "
    SELECT count(*) FROM information_schema.role_table_grants
    WHERE grantee = 'gavya_app' AND table_name = 'audit_logs'
      AND privilege_type IN ('UPDATE', 'DELETE')")
if [ "$audit_writable" != "0" ]; then
    echo "gavya: the application role can still modify audit_logs" >&2
    exit 1
fi

# ---- APPLY ENDS HERE ----
# Everything below verifies what was applied and changes nothing that the
# migration runner has to repeat in the same order. The marker is not decoration:
# tools/dbadmin/internal/schema compares the sequence above against its own copy
# of it, and reads to this line. gavya_enforce_references() is called again below
# to report what it refused, and counting that as a step would make the two
# copies disagree about an order they actually agree on.

# The password is set here rather than in the SQL file, so a credential never
# enters the repository.
"${psql[@]}" --command \
    "ALTER ROLE gavya_app LOGIN PASSWORD '$(printf '%s' "$GAVYA_APP_PASSWORD" | sed "s/'/''/g")'"

# Refuse to finish with a table that carries a tenant and is not protected.
# Coming up half-isolated is worse than not coming up: the services would work,
# and one table would quietly serve every tenant.
unprotected=$("${psql[@]}" --tuples-only --no-align --command "
    SELECT count(*) FROM gavya_isolation_report
    WHERE (has_tenant_column OR table_name = 'tenants')
      AND NOT (rls_enabled AND rls_forced)")
if [ "$unprotected" != "0" ]; then
    echo "gavya: $unprotected tenant-owned tables are not isolated:" >&2
    "${psql[@]}" --command "
        SELECT schema_name, table_name FROM gavya_isolation_report
        WHERE (has_tenant_column OR table_name = 'tenants')
          AND NOT (rls_enabled AND rls_forced)" >&2
    exit 1
fi

# The same refusal for foreign keys. One left crossable is one probe away from
# telling a tenant what exists in another.
crossable=$("${psql[@]}" --tuples-only --no-align --command "
    SELECT count(*) FROM gavya_foreign_key_report
    WHERE both_sides_have_a_tenant AND NOT carries_the_tenant")
if [ "$crossable" != "0" ]; then
    echo "gavya: $crossable foreign keys can still cross a tenant boundary:" >&2
    "${psql[@]}" --command "
        SELECT schema_name, table_name, constraint_name, references_table
        FROM gavya_foreign_key_report
        WHERE both_sides_have_a_tenant AND NOT carries_the_tenant" >&2
    exit 1
fi

# A table with no tenant column is not a failure — schema_migrations has none
# and needs none — but it is not nothing either, and the only way it gets looked
# at is if it is said out loud.
"${psql[@]}" --tuples-only --no-align --command "
    SELECT 'gavya: note - ' || schema_name || '.' || table_name ||
           ' has no tenant column and no policy; confirm that is intended'
    FROM gavya_isolation_report
    WHERE NOT has_tenant_column AND NOT rls_enabled"

protected=$("${psql[@]}" --tuples-only --no-align --command "
    SELECT count(*) FROM gavya_isolation_report WHERE rls_enabled AND rls_forced")
keys=$("${psql[@]}" --tuples-only --no-align --command "
    SELECT count(*) FROM gavya_foreign_key_report WHERE carries_the_tenant")
# Every reference-shaped column nothing enforces, not only the ones whose target
# can be guessed from the name. Restricting this to a guessable target reported
# 26 where the real figure was 89, which is a number that reassures rather than
# informs.
loose=$("${psql[@]}" --tuples-only --no-align --command "
    SELECT count(*) FROM gavya_unconstrained_reference_report")
refused=$("${psql[@]}" --tuples-only --no-align --command "
    SELECT count(*) FROM gavya_enforce_references() WHERE outcome LIKE 'REFUSED%'")
if [ "$refused" != "0" ]; then
    echo "gavya: $refused declared reference(s) could not be enforced because the data already " \
         "violates them:" >&2
    "${psql[@]}" --command "SELECT * FROM gavya_enforce_references() WHERE outcome LIKE 'REFUSED%'" >&2
fi

echo "gavya: $protected tables isolated, $keys foreign keys carry the tenant; services connect as gavya_app"
# Not a failure. It is a standing count of references nothing enforces, printed
# so it is not discovered later as a surprise.
echo "gavya: note — $loose reference-shaped columns are enforced by nothing; see gavya_unconstrained_reference_report"
# The count above is columns nothing enforces, which is not the same as columns
# nobody decided: the check earlier in this script has already refused to finish
# if any lacked a decision. Every one of these is unenforced on purpose, and
# gavya_reference_decisions says why for each.
