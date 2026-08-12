#!/usr/bin/env bash
# Pre-deploy check — run this before merging to prod.
#
#   ./scripts/check.sh          # everything except the DB-backed Go tests
#   ./scripts/check.sh --full   # also spins up a throwaway Postgres for them
#
# Exits non-zero on the first failure, so it is safe to chain:
#   ./scripts/check.sh --full && git checkout prod && git merge --ff-only main
set -euo pipefail
cd "$(dirname "$0")/.."

FULL=0
[[ "${1:-}" == "--full" ]] && FULL=1

pass() { printf '  \033[32mok\033[0m   %s\n' "$1"; }
step() { printf '\n\033[1m%s\033[0m\n' "$1"; }

step "Backend"
(cd backend && GOTOOLCHAIN=auto go build ./...);           pass "go build"
(cd backend && GOTOOLCHAIN=auto go vet ./...);             pass "go vet"
fmt=$(cd backend && gofmt -l . | grep -v '^vendor/' || true)
[[ -z "$fmt" ]] || { echo "  gofmt needed on:"; echo "$fmt"; exit 1; }
pass "gofmt"

step "Frontend"
# Syntax-check every module the browser loads.
find frontend -name '*.js' -not -path '*/tests/*' -print0 | xargs -0 -n1 node --check
pass "js syntax"
# Pure-helper unit tests. TZ pinned: the local-date helpers exist because
# toISOString() returns the UTC day, and CI runs in UTC.
TZ=Asia/Kuala_Lumpur node --test frontend/tests/unit/*.test.mjs >/dev/null
pass "unit tests"

step "Compose"
docker compose config >/dev/null
pass "docker compose config"
# Any env var the backend reads must also be forwarded by compose, or setting
# it in .env silently does nothing (this has bitten twice).
missing=$(comm -23 \
  <(grep -rhoP 'os\.Getenv\("\K[A-Z_]+' backend --include='*.go' | sort -u) \
  <(grep -oP '^\s+\K[A-Z_]+(?=: \$\{)' docker-compose.yml | sort -u) \
  | grep -vE '^(PORT|LOG_LEVEL|TEST_DATABASE_URL|UPLOAD_STORE|PAYMENT_PROVIDER|BILLPLZ_|STRIPE_|S$)' || true)
if [[ -n "$missing" ]]; then
  echo "  read by Go but not forwarded in docker-compose.yml:"; echo "$missing" | sed 's/^/    /'
  exit 1
fi
pass "env vars forwarded"

if [[ $FULL -eq 1 ]]; then
  step "Backend tests (throwaway Postgres)"
  name="shcheck$$"
  docker run -d --rm --name "$name" -p 127.0.0.1:55440:5432 \
    -e POSTGRES_USER=studyhub -e POSTGRES_PASSWORD=test -e POSTGRES_DB=studyhub_test \
    postgres:16-alpine >/dev/null
  trap 'docker stop "$name" >/dev/null 2>&1 || true' EXIT
  for _ in $(seq 1 30); do docker exec "$name" pg_isready -U studyhub >/dev/null 2>&1 && break; sleep 1; done
  (cd backend && TEST_DATABASE_URL="postgres://studyhub:test@127.0.0.1:55440/studyhub_test?sslmode=disable" \
    GOTOOLCHAIN=auto go test ./... )
  pass "go test (fresh schema + migrations)"
else
  printf '\n  (skipped DB-backed Go tests — re-run with --full)\n'
fi

printf '\n\033[32mAll checks passed.\033[0m\n'
