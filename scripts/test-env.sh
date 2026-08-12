#!/usr/bin/env bash
# Drive the local test stack — a full StudyHub (API + frontend + its own
# Postgres) at http://localhost:8081, isolated from everything else.
#
#   ./scripts/test-env.sh up       start it (builds on first run)
#   ./scripts/test-env.sh dev      FAST loop: Postgres in Docker, API on the
#                                  host — a backend change is ~3s, not ~60s
#   ./scripts/test-env.sh reset    wipe the database and reseed demo data
#   ./scripts/test-env.sh down     stop it (keeps the data)
#   ./scripts/test-env.sh clean    stop it and delete its data + uploads
#   ./scripts/test-env.sh logs     follow the API log
#   ./scripts/test-env.sh psql     open a shell on the test database
#   ./scripts/test-env.sh status   what is running, and on which port
#
# Which mode to use:
#   up   — closest to production (same image shape); use before deploying.
#   dev  — fastest iteration; the API runs from source, so a Go change needs
#          only Ctrl-C and re-run. Frontend edits are instant in both modes.
set -euo pipefail
cd "$(dirname "$0")/.."

C="docker compose -f docker-compose.test.yml"
URL="http://localhost:8081"

wait_healthy() {
  printf 'waiting for the API'
  for _ in $(seq 1 60); do
    if curl -fsS "$URL/api/health" >/dev/null 2>&1; then
      printf '\n'
      return 0
    fi
    printf '.'; sleep 1
  done
  printf '\n'
  echo "API did not come up. Recent log:" >&2
  $C logs --tail 40 api >&2
  return 1
}

banner() {
  cat <<EOF

  Test stack is up:  $URL
  Sign in:           admin@studyhub.com / admin123

  Separate database, demo data seeded, background jobs off, email and web
  push only logged. Frontend is live-mounted — edit js/html, refresh.

  ./scripts/test-env.sh reset   fresh database
  ./scripts/test-env.sh logs    follow the API log
EOF
}

case "${1:-up}" in
  up)
    $C up -d --build
    wait_healthy
    banner
    ;;
  dev)
    # Postgres in Docker, API from source on the host. Skips the image build
    # entirely: `go build` here is ~3s against Docker's ~60s, and the Go
    # server serves ../frontend directly so the bind mount isn't needed.
    # The containerised API owns :8081 in `up` mode — stop it or the host
    # process cannot bind the port.
    $C stop api >/dev/null 2>&1 || true
    $C up -d postgres
    printf 'waiting for postgres'
    until docker compose -f docker-compose.test.yml exec -T postgres pg_isready -U studyhub >/dev/null 2>&1; do
      printf '.'; sleep 1
    done
    printf '\n'
    echo "API on http://localhost:8081 (Ctrl-C to stop, re-run after a Go change)"
    cd backend
    APP_ENV=development \
    PORT=8081 \
    DATABASE_URL="postgres://studyhub:testonly@localhost:55433/studyhub?sslmode=disable" \
    JWT_SECRET=test-only-secret-not-for-production-use \
    SEED_ADMIN_PASSWORD=admin123 \
    SEED_DEMO_DATA=1 \
    ALLOWED_ORIGIN=http://localhost:8081 \
    APP_URL=http://localhost:8081 \
    EMAIL_FROM="StudyHub Test <test@localhost>" \
    TZ=Asia/Kuala_Lumpur \
      GOTOOLCHAIN=auto go run ./cmd/api
    ;;
  reset)
    # Drop the schema and let the API rebuild it on restart. This re-runs
    # every migration against an empty database (the honest test that they
    # work from scratch) without tearing down the volume or rebuilding the
    # image, which is the slow part.
    $C up -d postgres >/dev/null
    until $C exec -T postgres pg_isready -U studyhub >/dev/null 2>&1; do sleep 1; done
    $C exec -T postgres psql -U studyhub -d studyhub -q \
      -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;' >/dev/null
    if docker ps --format '{{.Names}}' | grep -q '^studyhub-test-api-1$'; then
      $C restart api >/dev/null
      wait_healthy
      echo "Database wiped and reseeded."
      banner
    else
      echo "Database wiped. Start the API with 'up' or 'dev' to reseed."
    fi
    ;;
  down)   $C down; echo "Stopped (data kept)." ;;
  clean)  $C down -v; echo "Stopped and data deleted." ;;
  logs)   $C logs -f api ;;
  psql)   $C exec postgres psql -U studyhub -d studyhub ;;
  status)
    $C ps
    curl -fsS "$URL/api/health" >/dev/null 2>&1 \
      && echo "health: ok  ($URL)" \
      || echo "health: not responding"
    ;;
  *) echo "usage: $0 {up|reset|down|clean|logs|psql|status}" >&2; exit 1 ;;
esac
