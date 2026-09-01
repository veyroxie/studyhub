#!/usr/bin/env bash
# Pre-deploy check for a migration that rewrites existing data.
#
# Copies the PRODUCTION database into a throwaway local Postgres, applies the
# pending migrations to that copy, and runs the backfill differ against it. The
# droplet is only ever read from (pg_dump); nothing here writes to production.
#
# Used before deploying 0047, which converts class_schedule_history into
# class_schedule_versions. A wrong backfill would rewrite the timeline that
# attendance and invoices are anchored to, so it is checked against real data
# first rather than only against seeded fixtures.
#
#   make migration-dryrun
set -euo pipefail

DROPLET="${DROPLET:-root@167.99.64.149}"
CONTAINER="studyhub-dryrun-pg"
PORT="${DRYRUN_PORT:-55433}"
DSN="postgres://stratum:stratum_dev@localhost:${PORT}/studyhub_dryrun?sslmode=disable"
DUMP="$(mktemp -t studyhub-dryrun-XXXXXX.sql)"

cleanup() {
  rm -f "$DUMP"
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "==> Dumping production (read-only) ..."
# pg_dump runs inside the droplet's postgres container; --clean is deliberately
# NOT passed, the target is a fresh empty database.
ssh -o ConnectTimeout=10 "$DROPLET" \
  'docker exec $(docker ps -qf name=postgres) pg_dump -U studyhub studyhub' > "$DUMP"
echo "    $(wc -l < "$DUMP") lines"

echo "==> Starting throwaway Postgres on :${PORT} ..."
docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
docker run --rm -d --name "$CONTAINER" \
  -e POSTGRES_USER=stratum -e POSTGRES_PASSWORD=stratum_dev -e POSTGRES_DB=studyhub_dryrun \
  -p "${PORT}:5432" postgres:16-alpine >/dev/null
until docker exec "$CONTAINER" pg_isready -U stratum >/dev/null 2>&1; do sleep 1; done

echo "==> Restoring the copy ..."
# The dump names the production role; map it onto the local one.
docker exec "$CONTAINER" psql -U stratum -d studyhub_dryrun -q -c \
  "DO \$\$ BEGIN CREATE ROLE studyhub; EXCEPTION WHEN duplicate_object THEN NULL; END \$\$;" >/dev/null
docker exec -i "$CONTAINER" psql -U stratum -d studyhub_dryrun -q < "$DUMP" >/dev/null 2>&1 || true

echo "==> Applying pending migrations to the copy, then diffing resolvers ..."
# The test harness boots InitDB, which runs the migrations, then the differ
# compares the pre-migration rule against the migrated tables.
cd backend
TEST_DATABASE_URL="$DSN" go test ./internal/handlers/ \
  -run 'TestScheduleBackfill_MatchesLegacyResolver' -count=1 -v 2>&1 |
  grep -E 'compared|PASS|FAIL|legacy said' || true

echo
echo "PASS above means the migrated data answers identically to the old rule."
echo "Any 'legacy said' line names a class and date that diverged — do not deploy."
