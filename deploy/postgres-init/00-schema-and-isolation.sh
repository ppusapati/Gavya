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

protected=$("${psql[@]}" --tuples-only --no-align --command "
    SELECT count(*) FROM gavya_isolation_report WHERE rls_enabled AND rls_forced")
echo "gavya: $protected tables isolated; services connect as gavya_app"
