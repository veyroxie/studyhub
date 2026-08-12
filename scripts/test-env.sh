#!/usr/bin/env bash
# Drive the local test stack — a full StudyHub (API + frontend + its own
# Postgres) at http://localhost:8081, isolated from everything else.
#
#   ./scripts/test-env.sh up       start it (builds on first run)
#   ./scripts/test-env.sh reset    wipe the database and reseed demo data
#   ./scripts/test-env.sh down     stop it (keeps the data)
#   ./scripts/test-env.sh clean    stop it and delete its data + uploads
#   ./scripts/test-env.sh logs     follow the API log
#   ./scripts/test-env.sh psql     open a shell on the test database
#   ./scripts/test-env.sh status   what is running, and on which port
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
  reset)
    # Drop the volume so the next boot re-runs migrations from scratch and
    # reseeds — this is also the honest test that migrations work on empty.
    $C down -v
    $C up -d --build
    wait_healthy
    echo "Database wiped and reseeded."
    banner
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
