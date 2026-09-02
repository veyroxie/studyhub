#!/bin/bash
# StudyHub backup verification — restores the latest dump to a throwaway
# database and asserts row counts on critical tables. Run weekly on the
# HOST so a corrupted dump is detected before you need it:
#   0 4 * * 0 docker exec studyhub-api-1 /app/backup_verify.sh >> /var/log/studyhub-backup-verify.log 2>&1
#
# An untested backup is a hope, not a backup. The cost of running this
# weekly is ~30s of DB CPU; the cost of skipping it is "we had backups but
# they were empty" during a real recovery.

set -euo pipefail

DATABASE_URL="${DATABASE_URL:-postgres://studyhub:changeme@postgres:5432/studyhub?sslmode=disable}"
BACKUP_DIR="${BACKUP_DIR:-/app/backups}"
VERIFY_DB="${VERIFY_DB:-studyhub_verify_$$}"

# Latest local backup.
LATEST=$(ls -t "$BACKUP_DIR"/studyhub_*.sql.gz 2>/dev/null | head -1 || true)
if [ -z "$LATEST" ]; then
  echo "[$(date)] FAIL: no backup files found in $BACKUP_DIR"
  exit 1
fi
echo "[$(date)] Verifying: $LATEST"

# Strip database name so we can connect to the postgres admin DB to create
# the throwaway one. The URL is e.g. postgres://user:pass@host:5432/dbname.
ADMIN_URL=$(echo "$DATABASE_URL" | sed -E 's|/[^/?]+(\?.*)?$|/postgres\1|')

cleanup() {
  psql "$ADMIN_URL" -c "DROP DATABASE IF EXISTS \"$VERIFY_DB\"" >/dev/null 2>&1 || true
}
trap cleanup EXIT

psql "$ADMIN_URL" -c "CREATE DATABASE \"$VERIFY_DB\"" >/dev/null

# Restore
RESTORE_URL=$(echo "$DATABASE_URL" | sed -E "s|/[^/?]+(\?.*)?$|/$VERIFY_DB\1|")
gunzip -c "$LATEST" | psql "$RESTORE_URL" >/dev/null 2>&1

# Sanity checks
declare -A EXPECTED_NONZERO=( [users]=1 [tenants]=1 )
for table in users tenants students invoices; do
  COUNT=$(psql "$RESTORE_URL" -At -c "SELECT COUNT(*) FROM $table" 2>/dev/null || echo "-1")
  if [ "$COUNT" = "-1" ]; then
    echo "[$(date)] FAIL: table $table missing from restored backup"
    exit 1
  fi
  if [ -n "${EXPECTED_NONZERO[$table]:-}" ] && [ "$COUNT" -lt 1 ]; then
    echo "[$(date)] FAIL: table $table has 0 rows (expected >=1)"
    exit 1
  fi
  echo "[$(date)] $table: $COUNT rows"
done

# Smoke-test a join to confirm relational integrity survived restore.
ORPHAN=$(psql "$RESTORE_URL" -At -c "SELECT COUNT(*) FROM invoices i LEFT JOIN students s ON s.id=i.student_id WHERE s.id IS NULL AND i.deleted_at IS NULL")
echo "[$(date)] orphan invoices (post-restore): $ORPHAN"

echo "[$(date)] OK: backup $LATEST restores cleanly"

# Heartbeat only on a clean verify: the point is proving a dump RESTORES, so a
# run that failed must leave the previous timestamp untouched (0048).
psql "$DATABASE_URL" -q -c \
  "INSERT INTO job_heartbeats(name,last_success_at,detail) VALUES('backup-verify',NOW(),'ok')
   ON CONFLICT (name) DO UPDATE SET last_success_at=NOW(), detail=EXCLUDED.detail" 2>/dev/null || true
