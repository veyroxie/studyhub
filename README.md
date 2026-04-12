# StudyHub

Tuition center management system for **The Study Hub** ([studyhub.fit](https://studyhub.fit)).
Built solo, deployed on a single DigitalOcean droplet, runs the full operations
of a tutoring center: students, classes, billing, attendance, feedback,
referrals, parent dashboards.

## Stack

- **Backend:** Go + chi router, PostgreSQL via pgx, JWT in HttpOnly cookies
- **Frontend:** Vanilla JS (`window.App` namespace, no build step), Tailwind via CDN
- **Email:** Resend (free tier) — falls back to stdout in dev mode
- **Deploy:** Docker Compose on DigitalOcean droplet, Caddy reverse proxy + HTTPS
- **Domain:** studyhub.fit (Namecheap)

No bundler, no transpiler, no ORM. Designed to be readable end-to-end by one
person at 2am.

## Repository layout

```
backend/
  main.go                  HTTP server, routes, graceful shutdown
  middleware.go            CORS, request ID, security headers, recovery, rate limit
  database.go              Schema bootstrap + legacy idempotent migrations
  migrate.go               Atlas-style numbered SQL migration applier
  migrations/              Numbered .sql files (see migrations/README.md)
  db.go                    DB wrapper, ?-to-$N placeholder translation
  models.go                Struct definitions for every entity
  auth.go                  Login, logout, JWT issuance, /me endpoint
  handlers.go              Shared helpers + snapshot endpoint only
  handlers_<domain>.go     One file per domain (students, classes, invoices, ...)
  handlers_referrals.go    Referral discount system (codes, milestones, credits)
  handlers_meta.go         Health check, version info
  handlers_password.go     Forgot password + reset flow
  mailer.go                Resend HTTP client + dev-mode stdout fallback
  email_tokens.go          Verification/reset/set-password token helpers
  seed.go                  Optional dev seed data
  rate-limit.go            Per-route rate limit middleware

frontend/
  index.html               Login page + main app shell
  register.html            Public registration form (parent + teacher tabs)
  reset.html               Password reset landing page (consumes ?token=)
  styles.css               Theme CSS variables, bento tiles, transitions
  js/
    api.js                 Single API wrapper (auth, fetch, error handling)
    store.js               Observable state with localStorage persistence
    router.js              navigate(pageId), refresh()
    main.js                DOMContentLoaded init, module registration
    theme.js               Light/dark themes, sidebar toggle
    utils.js               showModal, showToast, formatDate, esc(), badge()
    notifs.js              Notification bell + panel
    tutorial.js            First-login walkthrough
    modules/               One file per page (dashboard, billing, students, ...)

services/
  analytics/               Optional Python FastAPI microservice (port 8001)

scripts/
  backup.sh                Nightly Postgres dump (cron-driven on the droplet)
```

## Local development

```bash
# Backend
cd backend
cp .env.example .env       # then edit values
go run .                   # http://localhost:8080

# Frontend
# Open http://localhost:8080/ in your browser — Go serves the static files
```

The Go server serves the `frontend/` directory directly, so there's nothing to
build. Edit a `.js` or `.html` file, refresh the browser, done.

### Environment variables

See `backend/.env.example` for the full list. Required for production:

- `JWT_SECRET` — at least 16 chars, must persist across restarts
- `DATABASE_URL` — Postgres connection string
- `ALLOWED_ORIGIN` — your frontend origin for CORS
- `RESEND_API_KEY` — leave blank in dev (mailer falls back to stdout)
- `EMAIL_FROM` — defaults to `The Study Hub <hello@studyhub.fit>`
- `APP_URL` — base URL for links inside emails (defaults to `https://studyhub.fit`)
- `APP_ENV` — `development` / `staging` / `production` (defaults to `development`)

The app loads `.env` first then `.env.${APP_ENV}` on top, so you can keep
shared defaults in `.env` and overrides in `.env.production` etc.

## Database migrations

There are two migration paths, both run on every startup:

1. **Legacy** (`runMigrations` in `database.go`) — idempotent
   `ALTER TABLE ... IF NOT EXISTS` blocks. Existing prod databases were
   provisioned this way. Do **not** add new ones here.
2. **Atlas-style** (`runFileMigrations` in `migrate.go` + `backend/migrations/`)
   — numbered `.sql` files applied in order, tracked by checksum in the
   `schema_migrations` table. **All new schema changes go here.** See
   [`backend/migrations/README.md`](backend/migrations/README.md).

## Deploy

The user pushes to the `prod` branch from their own terminal. The droplet has
a checkout of the same branch at **`/root/studyhub`** (i.e. `~/studyhub`
when SSH'd in as root). Deploy is:

```bash
ssh root@167.99.64.149
cd ~/studyhub && git pull && docker compose build api && docker compose up -d api
```

Smoke-test after deploy:

```bash
curl -s http://localhost:8080/api/health
docker logs --tail 50 studyhub-api-1
```

Health should return `{"ok":true,"db":"ok",...}`. Logs should show the
structured JSON output and the `background jobs starting` line.

Caddy handles HTTPS via Let's Encrypt automatically.

Health check: `GET /api/health` returns `{ok, db, version, env}`. Point your
uptime monitor (UptimeRobot, BetterStack, etc.) at this.

## Architecture notes

- **Multi-tenant ready, single-tenant in practice.** Every table has a
  `tenant_id` column scoped through `tenantID(claims)`. Currently always 1.
  Lets us add a second center later without a schema migration.
- **Soft deletes everywhere.** `deleted_at TIMESTAMPTZ` on every entity.
  `WHERE deleted_at IS NULL` is the default filter; nothing is ever hard-deleted.
- **Audit logs** capture admin mutations (`audit_logs` table) — actor email,
  action, entity type, entity ID, freeform detail.
- **Snapshot endpoint** (`GET /api/snapshot`) returns the full state in one
  call. The frontend hydrates `App.Store` from this on every page load.
  Modules then read from the store rather than re-fetching.

## Conventions

See [`CONTRIBUTING.md`](CONTRIBUTING.md).

## Changelog

See [`CHANGELOG.md`](CHANGELOG.md). Update it on any user-visible change.
