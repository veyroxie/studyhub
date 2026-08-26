# Contributing to StudyHub

This is a one-developer project, so "contributing" mostly means "future-you in
6 months". These are the rules that keep the codebase navigable.

Deep per-subsystem rules (billing precedence, tenant scoping, the session model,
the job roster, the frontend contract) live in `AI_DOCS/` — read the relevant one
before editing that subsystem. This file is the workflow; that is the detail.

## Before committing

- `cd backend && go build ./... && go vet ./...` must exit clean.
- `cd backend && go test ./...` — needs a Postgres; CI spins one up and sets
  `TEST_DATABASE_URL` + `APP_ENV=test`.
- Frontend unit tests: `TZ=Asia/Kuala_Lumpur node --test frontend/tests/unit/`.
  The pinned TZ is mandatory (CI runs UTC; the date helpers are UTC+8-sensitive).
  **CI does not run these** — it only `node --check`s each file, so run them yourself.
- Beyond that: open the page in a browser, click around, watch the console.
- Update `CHANGELOG.md` if the change is user-visible (new feature, bug fix,
  schema change, security tweak). Ops-only or pure refactors don't need an
  entry.
- Push to `prod` from your terminal (not from WSL — line endings get weird).

## Where to add things

### Backend

- **New REST endpoint:** add the handler to the existing `handlers_<domain>.go`
  if it fits. Create a new `handlers_<newdomain>.go` if it's a new concern.
  Wire the route in `internal/server` under the right group (public /
  authenticated / admin-only).
- **New entity:** add the struct to `internal/models` and the schema change to
  `backend/internal/store/migrations/NNNN_*.sql`. **Every new schema change goes in
  a numbered migration** (`migrate.go:22-23`) — a column added only to `createSchema`
  never reaches an existing database. See `AI_DOCS/database.md`.
- **New email template:** add to `internal/mailer/mailer.go` as a `Render*Email(...)` function.
  Inline HTML with the existing layout constants — no templating engine.
- **New background job:** add it to the ticker roster in `internal/jobs/jobs.go:41-63`.
  Jobs run only when `APP_ENV=production` or `ENABLE_JOBS=1`, each runs once at
  startup, and each is panic-wrapped. Read `AI_DOCS/jobs-and-outbound.md` first —
  the outbound kill switch exists because of a real incident.
- **Fire-and-forget work in a handler:** use `goSafe(name, fn)`, never a bare `go` —
  chi's Recoverer only wraps the request goroutine.

### Frontend

- **New page:** create `frontend/js/modules/<name>.js`, register it in
  `main.js`, add a nav button + page div in `index.html`.
- **New API call:** use `App.Api.get/post/put/del`. Errors are toasted
  automatically — pass `{silent: true}` if you want to handle inline.
- **New form:** the project doesn't have a form helper yet. Use the
  `<form id="x"><input name="y"></form>` + `new FormData(form)` pattern,
  validate manually, call `App.Api.post`.
- **Modals:** use `App.Utils.showModal(html)` / `hideModal(refreshAfter)`.
- **Toasts:** `App.Utils.showToast(msg, 'success'|'error'|'warning'|'info')`.
- **Theme tokens:** stick to the CSS variables in `styles.css` (`--gold`,
  `--off-white`, etc.). No hard-coded hex colors except in tinted stat cards.

## Conventions

### Go

- One file per domain in `handlers_<domain>.go`. Past ~400 lines, treat it as a
  *signal* that the domain may be too broad — not a gate. Several files are well
  over (`handlers_students.go`, `jobs/cron.go`) and that is accepted; do not start
  an unrequested split mid-task.
- DB access goes directly through `db.QueryRow` / `db.Exec`. No ORM, no
  repository pattern. Use `?` placeholders — `db.go` translates them to `$N`.
- All queries scope with `store.ScopeTenant(claims, alias)` and `deleted_at IS NULL`
  where the column exists. The old `(tenant_id=? OR ?=0)` form is retired — it
  defeated the composite indexes. **There is no database-level backstop**: RLS is
  dormant, so a missing tenant filter leaks across tenants silently.
  See `AI_DOCS/auth-and-tenancy.md`.
- Errors: return early with `respondError(w, "human message", 400)`. The
  helper automatically includes the request ID in the JSON response.
- Audit-loggable mutations: `core.LogAudit(...)` with an explicit tenantID. The
  older `handlers.logAudit` omits `tenant_id`, which defaults the row to tenant 1
  and leaks across tenants.

### JS

- IIFE pattern: every module wraps in `(function() { window.App = window.App || {}; ... })();`
- No emojis in UI text unless the user explicitly asks for them.
- Use `App.Utils.esc(text)` whenever you interpolate user-provided strings
  into HTML — XSS is real even in admin panels.
- Date strings are ISO `YYYY-MM-DD` everywhere. Use `App.Utils.formatDate()` to
  render, and `App.Utils.localDate()` / `today()` to produce one. **Never**
  `toISOString().slice(0,10)` — it returns the UTC day, which in UTC+8 is still
  yesterday until 08:00 local. That bug mis-dated check-ins and self-study rows.
- Currency is always RM (Malaysian Ringgit). `App.Utils.formatCurrency(n)`
  for display.

### Git

- **Conventional commit format:** `<type>(<scope>): <summary>`
  - Types: `feat`, `fix`, `refactor`, `chore`, `docs`, `test`, `perf`
  - Scope: the domain area (`billing`, `auth`, `students`, `calendar`, etc.)
  - Examples:
    - `feat(billing): add early bird discount auto-detection`
    - `fix(invoices): parent query missing referral_credit column`
    - `chore(docker): add .dockerignore and optimize build flags`
    - `docs: update CHANGELOG with enrollment flow changes`
  - Keep the summary line under 72 characters, imperative mood.
- Push branch is `prod`. Main is the long-running default but `prod` is what
  the droplet pulls.
- One commit per logical change. Don't squash unrelated work into one commit.

## Things we deliberately don't do

These come up periodically and the answer is always "no, not worth it":

- **TypeScript, Vite, npm.** Vanilla JS is fast to write, fast to debug, ships in
  seconds. Type safety would be nice but the cost is too high for a solo project at
  this size. (Note: there IS a build step — the Dockerfile compiles Tailwind and
  minifies JS with standalone binaries. It just doesn't involve npm or a bundler.)
- **GraphQL.** REST is simpler, well-understood, and the API has no external
  consumers.
- **An ORM.** Raw SQL is fine for ~25 tables. We're not in danger of N+1
  problems.
- **Microservices.** One Go binary, one database. The Python analytics
  service is the only exception and it's optional.
- **A queue service / external scheduler.** In-process tickers are enough at this
  size; everything lives in `internal/jobs`.
- **A heavier API doc toolchain.** There is a hand-maintained
  `backend/internal/handlers/openapi.yaml` served at `/api/openapi.yaml`; that is as
  far as it goes.

If you want to add one of these, write a doc explaining why the cost-benefit
has shifted.

## Adding a database migration

```bash
# Pick the next number after the highest existing file
ls backend/internal/store/migrations/*.sql

# Create the new file
$EDITOR backend/internal/store/migrations/0007_add_late_fee_column.sql
```

```sql
-- 0041_add_late_fee_column.sql
--
-- Why: state the problem this solves, not just what it does.
-- Money is NUMERIC(12,2), never DOUBLE PRECISION — floats accumulate rounding
-- error in billing math (see 0025_money_numeric.sql).

ALTER TABLE invoices ADD COLUMN IF NOT EXISTS late_fee NUMERIC(12,2) NOT NULL DEFAULT 0;
```

Every statement must be idempotent (`IF NOT EXISTS`, `ON CONFLICT DO NOTHING`), and
a risky constraint belongs in its own follow-up migration — a failed migration stops
the API booting. Full house style in `AI_DOCS/database.md`.

Commit, deploy. The server applies it on next boot, records the version in
`schema_migrations`, and skips it forever after.

**Never edit a migration file after it's been applied to any environment.** The
applier stores a checksum and a mismatch is a **fatal boot error**, not a warning —
the API will refuse to start. Add a new migration that fixes the old one instead.

## Reporting bugs to future-you

When something breaks in production:

1. Note the timestamp (and ideally the request ID — it's in every error
   response: `{"error":"...", "request_id":"abc123"}`).
2. `ssh root@167.99.64.149 && docker logs studyhub-api --since 10m | grep abc123`
3. Look at the audit log table for the affected entity:
   `SELECT * FROM audit_logs WHERE entity_id='STU_xxx' ORDER BY created_at DESC;`

The request ID is the keystone — it links the user's complaint to the server
log line to the audit row.
