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

# Errors are CAPTURED, not discarded. This line used to end in
# `>/dev/null 2>&1 || true`, which threw away both the output and the exit
# code -- so a restore that dropped most of the database looked identical to
# one that worked, and every check downstream ran against partial data while
# reporting PASS. Ownership/role noise is expected and tolerated; the row
# counts below are what actually decides whether the copy is trustworthy.
RESTORE_LOG="$(mktemp)"
docker exec -i "$CONTAINER" psql -U stratum -d studyhub_dryrun -q < "$DUMP" >"$RESTORE_LOG" 2>&1 || true
RESTORE_ERRS=$(grep -c '^ERROR:' "$RESTORE_LOG" || true)
if [ "${RESTORE_ERRS:-0}" -gt 0 ]; then
  echo "    ${RESTORE_ERRS} restore error(s); first 5:"
  grep '^ERROR:' "$RESTORE_LOG" | head -5 | sed 's/^/      /'
fi

echo "==> Verifying the copy actually IS production ..."
# Measure the outcome, not the mechanism -- the same rule as the job
# heartbeats. "psql exited" says nothing; "the copy holds the same number of
# rows" is the claim worth making.
COUNT_SQL="SELECT 'attendance',COUNT(*) FROM attendance UNION ALL SELECT 'classes',COUNT(*) FROM classes UNION ALL SELECT 'enrollments',COUNT(*) FROM enrollments UNION ALL SELECT 'invoices',COUNT(*) FROM invoices UNION ALL SELECT 'students',COUNT(*) FROM students ORDER BY 1;"
PROD_COUNTS=$(ssh -o ConnectTimeout=10 "$DROPLET" "docker exec \$(docker ps -qf name=postgres) psql -U studyhub -d studyhub -At -F'|' -c \"$COUNT_SQL\"")
COPY_COUNTS=$(docker exec "$CONTAINER" psql -U stratum -d studyhub_dryrun -At -F'|' -c "$COUNT_SQL")
COPY_TRUSTWORTHY=1
if [ "$PROD_COUNTS" != "$COPY_COUNTS" ]; then
  COPY_TRUSTWORTHY=0
  echo "    MISMATCH -- the copy is NOT production. Everything below is measured"
  echo "    against partial data and proves nothing. (< production, > copy)"
  diff <(echo "$PROD_COUNTS") <(echo "$COPY_COUNTS") | sed 's/^/      /' || true
else
  echo "    row counts match production:"
  echo "$COPY_COUNTS" | sed 's/^/      /'
fi

echo "==> Applying pending migrations to the copy, then diffing resolvers ..."
# The test harness boots InitDB, which runs the migrations, then the differ
# compares the pre-migration rule against the migrated tables.
cd backend
TEST_DATABASE_URL="$DSN" go test ./internal/handlers/ \
  -run 'TestScheduleBackfill_MatchesLegacyResolver' -count=1 -v 2>&1 |
  grep -E 'compared|PASS|FAIL|legacy said' || true

echo
echo "==> Pricing coverage after migration (0051-0053, real data) ..."
# What the schedule differ cannot tell us: how much of the tier backfill
# actually landed. A migration that applies without error can still leave most
# of the estate unpriceable, which is the state this rework exists to end.
docker exec "$CONTAINER" psql -U stratum -d studyhub_dryrun -q -c "
SELECT COALESCE(pc.name, '(no category)') AS category,
       CASE WHEN COALESCE(c.default_tier_name,'') = '' THEN '(needs a tier)'
            ELSE c.default_tier_name END AS tier,
       COUNT(*) AS classes
  FROM classes c
  LEFT JOIN pricing_categories pc ON pc.id = c.pricing_category_id
 WHERE c.deleted_at IS NULL
 GROUP BY 1,2 ORDER BY 1,3 DESC;"

echo "    Live enrolments still needing a tier (Nadine's to-do list):"
docker exec "$CONTAINER" psql -U stratum -d studyhub_dryrun -q -c "
SELECT c.name AS class, COUNT(*) AS students
  FROM enrollments e
  JOIN classes c ON c.id = e.class_id
  LEFT JOIN pricing_categories pc ON pc.id = c.pricing_category_id
 WHERE e.ended_on IS NULL AND c.deleted_at IS NULL
   AND COALESCE(e.tier_name,'') = ''
   AND COALESCE(pc.credit_covered, FALSE) = FALSE
 GROUP BY 1 ORDER BY 2 DESC, 1;"

echo
if [ "$COPY_TRUSTWORTHY" -eq 0 ]; then
  echo "DO NOT DEPLOY on the strength of this run: the copy did not match"
  echo "production, so every check above ran against partial data."
  exit 1
fi
echo "PASS above means the migrated data answers identically to the old rule."
echo "Any 'legacy said' line names a class and date that diverged — do not deploy."
