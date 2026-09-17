#!/usr/bin/env bash
# Take a backup of the platform's database, and read it back before saying it
# worked.
#
# There was no backup and no restore. This is the system of record for what a
# co-operative's producers are paid: the collections, the rate cards they were
# priced against, the payables, the payments, and the audit trail that makes any
# of it defensible afterwards. Losing it had no answer.
#
# Three things here are not the obvious pg_dump line, and each is the difference
# between a backup and a directory of files nobody has opened.
#
#   - The roles are dumped too, separately. A database dump does not contain
#     them: gavya_app is a cluster object, and every row-level security policy in
#     this platform names it. Restore the database alone onto a fresh server and
#     every policy refers to a role that is not there.
#
#   - The dump is read back. pg_restore --list on the finished file is cheap and
#     catches the failure that matters most: a dump truncated by a full disk,
#     which pg_dump does report but which a pipeline that ignores exit codes
#     turns into a plausible-looking file. A backup nobody has read is not a
#     backup.
#
#   - It refuses to write an empty one. A dump of a database that turned out to
#     be empty, or of the wrong database, is the kind of thing discovered during
#     a restore, which is the worst moment to discover it.
#
# The custom format is deliberate: it compresses, it can be restored selectively,
# and pg_restore can read its table of contents without unpacking it.
#
#   scripts/backup.sh --dsn "postgres://postgres@host:5432/dairy?sslmode=verify-full" --into /backups
#
# The DSN wants a superuser, or a role that can read every table: a backup taken
# as gavya_app is subject to row-level security and silently contains one
# tenant's rows.
set -euo pipefail

dsn="${DATABASE_URL:-}"
into="${GAVYA_BACKUP_DIR:-./backups}"
label=""

while [ $# -gt 0 ]; do
  case "$1" in
    --dsn)   dsn="$2"; shift 2 ;;
    --into)  into="$2"; shift 2 ;;
    --label) label="$2"; shift 2 ;;
    -h|--help)
      sed -n '2,36p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) echo "backup: unknown argument $1" >&2; exit 2 ;;
  esac
done

if [ -z "$dsn" ]; then
  echo "backup: no database given: pass --dsn or set DATABASE_URL" >&2
  exit 2
fi
for tool in pg_dump pg_dumpall pg_restore psql; do
  command -v "$tool" >/dev/null 2>&1 || { echo "backup: $tool is not on PATH" >&2; exit 2; }
done

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
[ -n "$label" ] && stamp="${stamp}-${label}"
dir="$into/$stamp"
mkdir -p "$dir"

echo "backup: $dir"

# The roles first. They are the smaller half and the one that is forgotten.
#
# --no-role-passwords omits the hashes: a backup of this platform should not
# carry the credential that opens it, and the application's password is set from
# the deployment's secret on restore. The role, its membership and its
# attributes are what the policies need.
echo "backup:   roles"
pg_dumpall --dbname "$dsn" --roles-only --no-role-passwords > "$dir/roles.sql"

echo "backup:   database"
pg_dump --dbname "$dsn" --format=custom --compress=9 --file "$dir/database.dump"

# Read it back. This is the step that makes the word "backup" honest.
echo "backup:   reading it back"
entries="$(pg_restore --list "$dir/database.dump" | grep -vc '^;' || true)"
if [ "${entries:-0}" -lt 50 ]; then
  echo "backup: the dump lists only ${entries:-0} objects, which is not this platform." >&2
  echo "backup: it is truncated, or it is a dump of the wrong database. Not keeping it." >&2
  rm -rf "$dir"
  exit 1
fi

tables="$(psql --dbname "$dsn" --tuples-only --no-align --command \
  "SELECT count(*) FROM information_schema.tables
     WHERE table_schema NOT IN ('pg_catalog','information_schema')
       AND table_schema NOT LIKE 'pg\\_%'")"
rows="$(psql --dbname "$dsn" --tuples-only --no-align --command \
  "SELECT COALESCE(sum(n_live_tup),0) FROM pg_stat_user_tables")"

# What was backed up, beside the backup. A restore that reproduces a different
# number of tables than were dumped is the thing this file is here to notice,
# and comparing it needs the figure from the day it was taken.
cat > "$dir/manifest.txt" <<EOF
taken_at=$stamp
tables=$tables
approx_rows=$rows
toc_entries=$entries
postgres_version=$(psql --dbname "$dsn" --tuples-only --no-align --command "SHOW server_version")
dumped_by=${GAVYA_BACKUP_BY:-$(id -un)@$(hostname)}
EOF

echo "backup: $tables tables, about $rows rows, $entries objects"
echo "backup: $(du -sh "$dir" | cut -f1) in $dir"
echo
echo "backup: this has not been restored. scripts/restore.sh does that, and until"
echo "backup: somebody has run it against this file the backup is untested."
