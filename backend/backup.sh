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
# Bucket addressing style. The default (virtual-host, bucket.host) is what
# BOTH DigitalOcean Spaces and Cloudflare R2 want with s3cmd -- R2 was
# verified against it on 2026-09-02. Only override this for a provider that
# requires path style; getting it wrong surfaces as a 403
# SignatureDoesNotMatch, which reads like a bad secret key and is not.
S3_HOST_BUCKET="${S3_HOST_BUCKET:-%(bucket)s.$S3_HOST}"

if [ -n "$S3_BUCKET" ] && [ -n "$S3_ACCESS_KEY" ]; then
  s3cmd put "${BACKUP_FILE}.gz" "s3://$S3_BUCKET/backups/" \
    --access_key="$S3_ACCESS_KEY" \
    --secret_key="$S3_SECRET_KEY" \
    --host="$S3_HOST" \
    --host-bucket="$S3_HOST_BUCKET" \
    --region="${S3_REGION:-auto}" \
    --no-mime-magic
  echo "[$(date)] Uploaded to s3://$S3_BUCKET/backups/"
  # Heartbeat AFTER the upload, never before: a local dump that never left the
  # droplet is the failure this is here to catch (0048).
  psql "$DATABASE_URL" -q -c \
    "INSERT INTO job_heartbeats(name,last_success_at,detail) VALUES('backup-upload',NOW(),'$(basename "${BACKUP_FILE}.gz")')
     ON CONFLICT (name) DO UPDATE SET last_success_at=NOW(), detail=EXCLUDED.detail" 2>/dev/null || true
else
  echo "[$(date)] S3 not configured — local backup only"
fi
