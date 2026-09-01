#!/usr/bin/env bash
# Prove the OFF-SITE backup is usable, not merely present.
#
# "The upload command exited 0" is not the same as "we can recover". This
# downloads the newest dump from object storage, restores it into a throwaway
# Postgres, and counts rows in the tables that matter. If this passes, losing
# the droplet is survivable.
#
# Run it after configuring S3, and occasionally thereafter.
#
#   make backup-verify-remote
set -euo pipefail

DROPLET="${DROPLET:-root@167.99.64.149}"
CONTAINER="studyhub-restore-check"
PORT="${RESTORE_PORT:-55434}"
WORK="$(mktemp -d -t studyhub-restore-XXXXXX)"

cleanup() {
  rm -rf "$WORK"
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "==> Listing what is actually in object storage ..."
# s3cmd runs inside the API container, which already holds the credentials.
LISTING=$(ssh -o ConnectTimeout=10 "$DROPLET" 'docker exec studyhub-api-1 sh -lc '"'"'
  [ -n "$S3_BUCKET" ] || { echo "NOT_CONFIGURED"; exit 0; }
  s3cmd ls "s3://$S3_BUCKET/backups/" \
    --access_key="$S3_ACCESS_KEY" --secret_key="$S3_SECRET_KEY" \
    --host="$S3_HOST" --host-bucket="${S3_HOST_BUCKET:-%(bucket)s.$S3_HOST}" \
    --region="${S3_REGION:-auto}" 2>&1 | tail -5
'"'"'')

if echo "$LISTING" | grep -q NOT_CONFIGURED; then
  echo "S3 is not configured on the droplet — nothing is being uploaded."
  echo "Set S3_BUCKET / S3_ACCESS_KEY / S3_SECRET_KEY in /root/studyhub/.env"
  exit 1
fi
echo "$LISTING"

NEWEST=$(echo "$LISTING" | awk '/\.sql\.gz$/ {print $NF}' | sort | tail -1)
if [ -z "$NEWEST" ]; then
  echo "FAIL: the bucket holds no .sql.gz backups."
  exit 1
fi
echo "    newest remote: $NEWEST"

echo "==> Downloading it back OUT of storage ..."
ssh -o ConnectTimeout=10 "$DROPLET" 'docker exec studyhub-api-1 sh -lc '"'"'
  s3cmd get --force "'"$NEWEST"'" /tmp/restore-check.sql.gz \
    --access_key="$S3_ACCESS_KEY" --secret_key="$S3_SECRET_KEY" \
    --host="$S3_HOST" --host-bucket="${S3_HOST_BUCKET:-%(bucket)s.$S3_HOST}" \
    --region="${S3_REGION:-auto}" >/dev/null 2>&1 && cat /tmp/restore-check.sql.gz
'"'"'' > "$WORK/dump.sql.gz"

if [ ! -s "$WORK/dump.sql.gz" ]; then
  echo "FAIL: downloaded nothing. The upload may have succeeded while read access does not work."
  exit 1
fi
gunzip -c "$WORK/dump.sql.gz" > "$WORK/dump.sql" 2>/dev/null || {
  echo "FAIL: the downloaded file is not valid gzip — it is corrupt in storage."
  exit 1
}
echo "    downloaded $(du -h "$WORK/dump.sql" | cut -f1) uncompressed"

echo "==> Restoring it into a throwaway Postgres ..."
docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
docker run --rm -d --name "$CONTAINER" \
  -e POSTGRES_USER=stratum -e POSTGRES_PASSWORD=stratum_dev -e POSTGRES_DB=restore_check \
  -p "${PORT}:5432" postgres:16-alpine >/dev/null
until docker exec "$CONTAINER" pg_isready -U stratum >/dev/null 2>&1; do sleep 1; done
docker exec "$CONTAINER" psql -U stratum -d restore_check -q -c \
  "DO \$\$ BEGIN CREATE ROLE studyhub; EXCEPTION WHEN duplicate_object THEN NULL; END \$\$;" >/dev/null
docker exec -i "$CONTAINER" psql -U stratum -d restore_check -q < "$WORK/dump.sql" >/dev/null 2>&1 || true

echo "==> Counting what came back ..."
fail=0
for t in students invoices attendance classes users; do
  n=$(docker exec "$CONTAINER" psql -U stratum -d restore_check -t -A -c "SELECT COUNT(*) FROM $t" 2>/dev/null || echo "ERR")
  printf '    %-12s %s\n' "$t" "$n"
  case "$n" in ''|ERR|0) fail=1 ;; esac
done

echo
if [ "$fail" -eq 0 ]; then
  echo "PASS — the off-site copy downloads and restores with data in every core table."
  echo "Losing the droplet is survivable."
else
  echo "FAIL — a core table is empty or unreadable in the restored copy."
  exit 1
fi
