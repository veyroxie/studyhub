# Contributing to StudyHub

This is a one-developer project, so "contributing" mostly means "future-you in
6 months". These are the rules that keep the codebase navigable.

## Before committing

- `cd backend && go build ./... && go vet ./...` must exit clean.
- For frontend changes: open the page in a browser, click around, watch the
  console for errors. There's no test suite yet — you are the test suite.
- Update `CHANGELOG.md` if the change is user-visible (new feature, bug fix,
  schema change, security tweak). Ops-only or pure refactors don't need an
  entry.
- Push to `prod` from your terminal (not from WSL — line endings get weird).

## Where to add things

### Backend

- **New REST endpoint:** add the handler to the existing `handlers_<domain>.go`
  if it fits. Create a new `handlers_<newdomain>.go` if it's a new concern.
  Wire the route in `main.go` under the right group (public / authenticated /
  admin-only).
- **New entity:** add the struct to `models.go`, the table to `database.go`'s
  `createSchema`, columns to `runMigrations` if extending an existing table.
  For brand-new tables, prefer `backend/migrations/NNNN_*.sql` over editing
  `database.go`.
- **New email template:** add to `mailer.go` as a `render*Email(...)` function.
  Inline HTML with the existing layout constants — no templating engine.
- **New background job:** none of these exist yet. When you need one, the
  pattern will be `time.NewTicker` in a goroutine started from `main.go`.

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

- One file per domain in `handlers_<domain>.go`. If a file grows past
  ~400 lines, that's a hint that the domain is too broad — split it.
- DB access goes directly through `db.QueryRow` / `db.Exec`. No ORM, no
  repository pattern. Use `?` placeholders — `db.go` translates them to `$N`.
- All queries scope by `(tenant_id=? OR ?=0)` and `deleted_at IS NULL` where
  applicable. Failure to do this leaks data across tenants.
- Errors: return early with `respondError(w, "human message", 400)`. The
  helper automatically includes the request ID in the JSON response.
- Audit-loggable mutations: `db.Exec` an `INSERT INTO audit_logs(...)` after
  the change. See existing handlers for the pattern.

### JS

- IIFE pattern: every module wraps in `(function() { window.App = window.App || {}; ... })();`
- No emojis in UI text unless the user explicitly asks for them.
- Use `App.Utils.esc(text)` whenever you interpolate user-provided strings
  into HTML — XSS is real even in admin panels.
- Date strings are ISO `YYYY-MM-DD` everywhere. Use `App.Utils.formatDate()`
  to render.
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

- **TypeScript, Vite, build step.** Vanilla JS is fast to write, fast to
  debug, ships in seconds. Type safety would be nice but the cost is too high
  for a solo project at this size.
- **GraphQL.** REST is simpler, well-understood, and the API has no external
  consumers.
- **An ORM.** Raw SQL is fine for ~25 tables. We're not in danger of N+1
  problems.
- **Microservices.** One Go binary, one database. The Python analytics
  service is the only exception and it's optional.
- **Refresh tokens.** Cookie JWT lifetime is generous, parents don't mind
  re-logging in monthly.
- **Swagger/OpenAPI.** No external API consumers; the frontend is the only
  client and we can read the source.

If you want to add one of these, write a doc explaining why the cost-benefit
has shifted.

## Adding a database migration

```bash
# Pick the next number after the highest existing file
ls backend/migrations/*.sql

# Create the new file
$EDITOR backend/migrations/0007_add_late_fee_column.sql
```

```sql
-- 0007_add_late_fee_column.sql
ALTER TABLE invoices ADD COLUMN IF NOT EXISTS late_fee DOUBLE PRECISION DEFAULT 0;
```

Commit, deploy. The server applies it on next boot, records the version in
`schema_migrations`, and skips it forever after.

**Never edit a migration file after it's been applied to any environment.**
The applier will warn you on every boot if you do, and different environments
will end up at different states. Add a new migration that fixes the old one
instead.

## Reporting bugs to future-you

When something breaks in production:

1. Note the timestamp (and ideally the request ID — it's in every error
   response: `{"error":"...", "request_id":"abc123"}`).
2. `ssh root@167.99.64.149 && docker logs studyhub-api --since 10m | grep abc123`
3. Look at the audit log table for the affected entity:
   `SELECT * FROM audit_logs WHERE entity_id='STU_xxx' ORDER BY created_at DESC;`

The request ID is the keystone — it links the user's complaint to the server
log line to the audit row.
