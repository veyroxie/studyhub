#!/bin/bash
# StudyHub PostgreSQL backup script
# Run via cron on the HOST:
#   0 2 * * * docker exec studyhub-api-1 /app/backup.sh >> /var/log/studyhub-backup.log 2>&1

set -euo pipefail

DATABASE_URL="${DATABASE_URL:-postgres://studyhub:changeme@postgres:5432/studyhub?sslmode=disable}"
BACKUP_DIR="${BACKUP_DIR:-/app/backups}"
KEEP_DAYS="${KEEP_DAYS:-365}"

mkdir -p "$BACKUP_DIR"

TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_FILE="$BACKUP_DIR/studyhub_$TIMESTAMP.sql"

# Dump database
pg_dump "$DATABASE_URL" > "$BACKUP_FILE"

# Compress
gzip "$BACKUP_FILE"

echo "[$(date)] Backup created: ${BACKUP_FILE}.gz"

# Remove old local backups
find "$BACKUP_DIR" -name "studyhub_*.sql.gz" -mtime +$KEEP_DAYS -delete
echo "[$(date)] Pruned backups older than $KEEP_DAYS days"

# Upload to DigitalOcean Spaces (if configured)
S3_BUCKET="${S3_BUCKET:-}"
S3_ACCESS_KEY="${S3_ACCESS_KEY:-}"
S3_SECRET_KEY="${S3_SECRET_KEY:-}"
S3_HOST="${S3_HOST:-sgp1.digitaloceanspaces.com}"

if [ -n "$S3_BUCKET" ] && [ -n "$S3_ACCESS_KEY" ]; then
  s3cmd put "${BACKUP_FILE}.gz" "s3://$S3_BUCKET/backups/" \
    --access_key="$S3_ACCESS_KEY" \
    --secret_key="$S3_SECRET_KEY" \
    --host="$S3_HOST" \
    --host-bucket="%(bucket)s.$S3_HOST" \
    --no-mime-magic
  echo "[$(date)] Uploaded to s3://$S3_BUCKET/backups/"
else
  echo "[$(date)] S3 not configured — local backup only"
fi
