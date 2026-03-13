#!/bin/bash
# StudyHub PostgreSQL backup script
# Run via cron: 0 2 * * * /app/backup.sh >> /var/log/studyhub-backup.log 2>&1

set -euo pipefail

DATABASE_URL="${DATABASE_URL:-postgres://studyhub:changeme@postgres:5432/studyhub?sslmode=disable}"
BACKUP_DIR="${BACKUP_DIR:-/app/backups}"
KEEP_DAYS="${KEEP_DAYS:-365}"

mkdir -p "$BACKUP_DIR"

TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_FILE="$BACKUP_DIR/studyhub_$TIMESTAMP.sql"

# Use pg_dump for consistent backup
pg_dump "$DATABASE_URL" > "$BACKUP_FILE"

# Compress
gzip "$BACKUP_FILE"

echo "[$(date)] Backup created: ${BACKUP_FILE}.gz"

# Remove backups older than KEEP_DAYS
find "$BACKUP_DIR" -name "studyhub_*.sql.gz" -mtime +$KEEP_DAYS -delete
echo "[$(date)] Pruned backups older than $KEEP_DAYS days"

# Optional: copy to DigitalOcean Spaces (requires s3cmd configured)
# Uncomment and set your bucket:
# S3_BUCKET="${S3_BUCKET:-}"
# if [ -n "$S3_BUCKET" ]; then
#   s3cmd put "${BACKUP_FILE}.gz" "s3://$S3_BUCKET/backups/"
#   echo "[$(date)] Uploaded to s3://$S3_BUCKET/backups/"
# fi
