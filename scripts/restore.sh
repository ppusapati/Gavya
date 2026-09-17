#!/usr/bin/env bash
# Restore a backup, and say what came back.
#
# The half nobody writes. A backup that has never been restored is a belief, and
# the belief fails at the one moment it is being relied on — so this exists to be
# run on an ordinary afternoon against a scratch database, not only at three in
# the morning against the real one.
#
# What it does that a plain pg_restore does not:
#
#   - Restores the roles first, and does not fail if they are already there. Every
#     row-level security policy in this platform names gavya_app; restoring the
#     database onto a server without that role leaves every policy pointing at
#     nothing.
#
#   - Refuses to write over a database that already exists, unless told to. The
#     default is the safe one because the dangerous one is a keystroke away and
#     is unrecoverable.
#
#   - Checks what came back, rather than trusting the exit code. Row-level
#     security is the property this platform's tenant isolation rests on, and it
#     is a property of the restored schema rather than of the data: a restore
#     that quietly lost FORCE on one table produces a database that works and
#     serves every tenant's rows to whoever asks.
#
#   scripts/restore.sh --from ./backups/20260914T120000Z --into dairy_restored \
#     --server "postgres://postgres@localhost:5432/postgres?sslmode=disable"
#
# The application's password is not in the backup and is not set here. Set it
# from the deployment's secret afterwards, the way deploy/postgres-init does.
set -euo pipefail

from=""
into=""
server="${DATABASE_URL:-}"
force=0

while [ $# -gt 0 ]; do
  case "$1" in
    --from)   from="$2"; shift 2 ;;
    --into)   into="$2"; shift 2 ;;
    --server) server="$2"; shift 2 ;;
    --force)  force=1; shift ;;
    -h|--help)
      sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) echo "restore: unknown argument $1" >&2; exit 2 ;;
  esac
done

[ -n "$from" ]   || { echo "restore: --from is required" >&2; exit 2; }
[ -n "$into" ]   || { echo "restore: --into is required (the database to create)" >&2; exit 2; }
[ -n "$server" ] || { echo "restore: --server is required: a maintenance connection" >&2; exit 2; }
[ -f "$from/database.dump" ] || { echo "restore: no database.dump in $from" >&2; exit 2; }
for tool in pg_restore psql; do
  command -v "$tool" >/dev/null 2>&1 || { echo "restore: $tool is not on PATH" >&2; exit 2; }
done

# The target DSN is the maintenance one with the database name swapped.
target="$(printf '%s' "$server" | sed -E "s#(://[^/]+)/[^?]*#\1/$into#")"

exists="$(psql --dbname "$server" --tuples-only --no-align --command \
  "SELECT count(*) FROM pg_database WHERE datname = '$into'")"
if [ "$exists" != "0" ]; then
  if [ "$force" != "1" ]; then
    echo "restore: $into already exists. Pass --force to drop and replace it," >&2
    echo "restore: which is unrecoverable, or choose another name with --into." >&2
    exit 1
  fi
  echo "restore: dropping $into"
  psql --dbname "$server" --quiet --command "DROP DATABASE \"$into\" WITH (FORCE)"
fi

echo "restore: from $from into $into"

# Roles before the database. An error here is not fatal: on a server that
# already runs this platform the roles exist, and CREATE ROLE says so.
echo "restore:   roles"
psql --dbname "$server" --quiet --file "$from/roles.sql" > /dev/null 2>&1 || \
  echo "restore:   (some roles were already there, which is expected)"

psql --dbname "$server" --quiet --command "CREATE DATABASE \"$into\""

echo "restore:   database"
pg_restore --dbname "$target" --no-owner --exit-on-error "$from/database.dump"

# What came back. Not the exit code — the properties.
tables="$(psql --dbname "$target" --tuples-only --no-align --command \
  "SELECT count(*) FROM information_schema.tables
     WHERE table_schema NOT IN ('pg_catalog','information_schema')
       AND table_schema NOT LIKE 'pg\\_%'")"
echo "restore: $tables tables"

if [ -f "$from/manifest.txt" ]; then
  # shellcheck disable=SC1090
  want="$(grep '^tables=' "$from/manifest.txt" | cut -d= -f2)"
  if [ -n "$want" ] && [ "$want" != "$tables" ]; then
    echo "restore: the backup recorded $want tables and $tables came back." >&2
    echo "restore: something did not restore. Do not put this database into service." >&2
    exit 1
  fi
fi

# The isolation is the thing most worth checking, because losing it is silent.
# A table that carries a tenant and has no forced row-level security answers
# every tenant's question with every tenant's rows, and nothing about the
# database looks wrong.
unprotected="$(psql --dbname "$target" --tuples-only --no-align --command \
  "SELECT count(*) FROM gavya_isolation_report
    WHERE (has_tenant_column OR table_name = 'tenants')
      AND NOT (rls_enabled AND rls_forced)" 2>/dev/null || echo "?")"
if [ "$unprotected" = "?" ]; then
  echo "restore: could not read gavya_isolation_report; the isolation functions did not restore." >&2
  exit 1
fi
if [ "$unprotected" != "0" ]; then
  echo "restore: $unprotected tenant-owned tables came back without forced row-level security." >&2
  echo "restore: this database would serve every tenant's rows to whoever asks. Not usable." >&2
  exit 1
fi

protected="$(psql --dbname "$target" --tuples-only --no-align --command \
  "SELECT count(*) FROM gavya_isolation_report WHERE rls_enabled AND rls_forced")"
echo "restore: $protected tables isolated"
echo "restore: set the application's password before any service connects; it is"
echo "restore: deliberately not in the backup."
