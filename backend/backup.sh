#!/bin/bash
# StudyHub SQLite backup script
# Run via cron: 0 2 * * * /app/backup.sh >> /var/log/studyhub-backup.log 2>&1

set -euo pipefail

DB_PATH="${DB_PATH:-/app/studyhub.db}"
BACKUP_DIR="${BACKUP_DIR:-/app/backups}"
KEEP_DAYS="${KEEP_DAYS:-30}"

mkdir -p "$BACKUP_DIR"

TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_FILE="$BACKUP_DIR/studyhub_$TIMESTAMP.db"

# Use SQLite's online backup (safe while DB is in use)
sqlite3 "$DB_PATH" ".backup '$BACKUP_FILE'"

# Compress
gzip "$BACKUP_FILE"

echo "[$(date)] Backup created: ${BACKUP_FILE}.gz"

# Remove backups older than KEEP_DAYS
find "$BACKUP_DIR" -name "studyhub_*.db.gz" -mtime +$KEEP_DAYS -delete
echo "[$(date)] Pruned backups older than $KEEP_DAYS days"

# Optional: copy to DigitalOcean Spaces (requires s3cmd configured)
# Uncomment and set your bucket:
# S3_BUCKET="${S3_BUCKET:-}"
# if [ -n "$S3_BUCKET" ]; then
#   s3cmd put "${BACKUP_FILE}.gz" "s3://$S3_BUCKET/backups/"
#   echo "[$(date)] Uploaded to s3://$S3_BUCKET/backups/"
# fi
