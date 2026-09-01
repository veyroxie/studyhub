# StudyHub task runner. Deterministic commands only.
# The deploy flow encodes the runbook: main -> prod (ff-only) -> droplet pull
# -> compose build/up -> verify. Production env lives only in the droplet's
# /root/studyhub/.env; a var must ALSO be forwarded in docker-compose.yml or
# it never reaches the container.

DROPLET ?= root@167.99.64.149
REMOTE_DIR ?= /root/studyhub
SSH := ssh -o ConnectTimeout=10 $(DROPLET)

.DEFAULT_GOAL := help
.PHONY: help check check-full test-up test-dev test-down test-reset release deploy deploy-nobuild verify ship logs psql ssh failed-emails migration-dryrun

help:  ## list targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  make %-14s %s\n", $$1, $$2}'

check:  ## build + vet + gofmt + js syntax + unit tests + compose config (no DB)
	./scripts/check.sh

check-full:  ## everything in check plus DB-backed Go tests on a throwaway Postgres
	./scripts/check.sh --full

test-up:  ## local test stack at :8081 (production-shaped image)
	./scripts/test-env.sh up

test-dev:  ## fast loop: Postgres in Docker, API from source (~3s per Go change)
	./scripts/test-env.sh dev

test-down:  ## stop the test stack (keeps its data)
	./scripts/test-env.sh down

test-reset:  ## wipe the test database and reseed demo data
	./scripts/test-env.sh reset

release:  ## fast-forward prod from main and push both (guards: on main, clean tree)
	@test "$$(git branch --show-current)" = main || { echo "not on main"; exit 1; }
	@git diff --quiet && git diff --cached --quiet || { echo "working tree not clean"; exit 1; }
	git push origin main
	git checkout prod
	git merge --ff-only main
	git push origin prod
	git checkout main
	@echo "prod is at $$(git rev-parse --short prod)"

deploy:  ## droplet: pull prod, rebuild the api image, restart, then verify
	$(SSH) "cd $(REMOTE_DIR) && git pull --ff-only origin prod && VERSION=$$(git rev-parse --short HEAD) docker compose build api && docker compose up -d api"
	@sleep 5
	@$(MAKE) --no-print-directory verify

deploy-nobuild:  ## droplet: pull + restart only (compose/env-only changes, no image rebuild)
	$(SSH) "cd $(REMOTE_DIR) && git pull --ff-only origin prod && docker compose up -d api"
	@sleep 5
	@$(MAKE) --no-print-directory verify

verify:  ## prove the deploy: fresh uptime, migrations applied, prod config, health ok
	@$(SSH) '\
	  cid=$$(docker ps -qf name=api); \
	  echo "--- boot log (migrations + config) ---"; \
	  docker logs --since 15m $$cid 2>&1 | grep -iE "migration|applied|effective config|mailer initialised" | head -12; \
	  echo "--- health ---"; \
	  h=$$(curl -s localhost:8080/api/health); echo $$h; \
	  up=$$(echo $$h | grep -oP "\"uptime_sec\":\K[0-9]+"); \
	  ok=$$(echo $$h | grep -oP "\"ok\":\Ktrue|false"); \
	  echo "--- verdict ---"; \
	  if [ "$$ok" = true ] && [ "$$up" -lt 900 ]; then echo "DEPLOY VERIFIED: healthy, booted $$up seconds ago"; \
	  else echo "CHECK NEEDED: ok=$$ok uptime=$$up sec (old container still running?)"; exit 1; fi'

ship: check-full release deploy  ## the whole flow: full checks -> release -> deploy -> verify

migration-dryrun:  ## copy prod into a throwaway DB, migrate it, diff the backfill (read-only on prod)
	./scripts/migration-dryrun.sh

logs:  ## follow the production api log
	$(SSH) 'docker logs -f --tail 100 $$(docker ps -qf name=api)'

psql:  ## psql shell on the PRODUCTION database (read with care)
	$(SSH) -t 'docker exec -it $$(docker ps -qf name=postgres) psql -U studyhub studyhub'

failed-emails:  ## list permanently failed queue rows (nothing retries these)
	$(SSH) 'docker exec $$(docker ps -qf name=postgres) psql -U studyhub studyhub -c "SELECT id, to_addr, subject, last_error, created_at FROM email_queue WHERE status='"'"'failed'"'"' ORDER BY created_at DESC LIMIT 20"'

ssh:  ## shell on the droplet
	$(SSH)
