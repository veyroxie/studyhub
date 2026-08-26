# StudyHub

Monorepo: Go backend + vanilla JS frontend + Python analytics service.

## Stack

Versions below are read from the lockfiles, not from memory. Re-verify before
relying on one; drift here has already caused wrong advice.

**Backend** (`backend/go.mod`)
- Go **1.25.12** + chi v5 router (REST API)
- PostgreSQL 16 via pgx v5 — raw SQL, no ORM
- JWT auth in HttpOnly cookies (`golang-jwt/jwt/v5`); `golang.org/x/crypto` for hashing
- Resend for transactional email (free tier), queued via `internal/store/email_queue.go`
- `gorilla/websocket` — live updates (`handlers/ws.go`)
- `SherClockHolmes/webpush-go` — VAPID web push (`internal/notify`, `handlers_push.go`)
- `jung-kurt/gofpdf` + `boombuler/barcode` — invoice and report PDFs (`internal/pdf`)
- Background jobs: `internal/jobs/cron.go`, started from `cmd/api/main.go` only when
  `APP_ENV=production` or `ENABLE_JOBS=1`

**Frontend**
- Vanilla ES6+, IIFE / `window.App` namespace
- HTML, no framework, no TypeScript, **no npm** — but there IS a build step, and it
  lives in `backend/Dockerfile`, not in a `package.json`
- Tailwind CSS **v4.3.0**, compiled by the standalone CLI in the Dockerfile's
  `tailwind-builder` stage from `frontend/tailwind.in.css` and overlaid into the
  runtime image. NOT the CDN. The committed `frontend/tailwind.css` is a build
  artifact; see "Tailwind" under Invariants.
- Every `frontend/js/**/*.js` is minified in place by esbuild (`--target=es2019`)
  in the same Docker stage, preserving the defer order and the lazy analytics load
- Chart.js 4.4.0 — lazy-injected from jsdelivr at runtime by `js/modules/analytics.js`,
  analytics page only. No other CDN script tags.
- Google Fonts via CDN (Cormorant Garamond, Fraunces, Instrument Sans, JetBrains Mono)
- PWA: `manifest.json` + `sw.js`

**Analytics**
- Python FastAPI 0.111 + pydantic **v2** microservice on port `8001` — internal only
  (not exposed to Caddy), single `services/analytics/main.py`

**Infrastructure**
- Docker Compose: 3 services (`api`, `analytics`, `postgres:16-alpine`)
- API binds `127.0.0.1:8080` only; Caddy reverse-proxies to it (auto HTTPS via Let's Encrypt)
- DigitalOcean droplet: `167.99.64.149`; domain `studyhub.fit` (Namecheap)
- Container TZ pinned to `Asia/Kuala_Lumpur` (UTC+8) — load-bearing, see Invariants
- Nightly DB backup to DigitalOcean Spaces (S3-compatible)
- GitHub Actions CI: `go vet` + `go mod tidy` check + build + `go test` against a real
  Postgres service container, plus `node --check` on every frontend JS file.
  CI does NOT run the frontend unit tests in `frontend/tests/unit/` — run those locally
  with `TZ=Asia/Kuala_Lumpur node --test frontend/tests/unit/`.
- release-please: automated versioning on `prod`

## Invariants

Rules that are not derivable from any single file you happen to open. Breaking one
is a data-integrity or security bug, not a style problem.

- **Tenant isolation is enforced at the QUERY layer, not by the database.** Every
  tenant-scoped query must append `store.ScopeTenant(claims, alias)`. Postgres RLS
  policies exist (migrations `0004`/`0015`) but are **dormant** — the app connects as
  a superuser (which bypasses RLS) and `store.RLSScope` is a deliberate passthrough.
  Never assume the DB will catch a missing tenant filter; it will not.
  See `notes/rls-activation.md` and `backend/internal/store/rls.go`.
- **Soft deletes everywhere.** Reads filter `deleted_at IS NULL`. A query without it
  resurrects deleted rows.
- **`?` placeholders, never `$N`.** The wrapper in `internal/store/db.go` translates
  them. Never concatenate values into SQL.
- **Timezone is UTC+8 and it is load-bearing.** Attendance, invoices, and the
  early-bird cutoff are date-sensitive. In JS, `toISOString()` yields the UTC date,
  which is still "yesterday" locally until 08:00 — use the `localDate`/`today`
  helpers in `js/utils.js`. In Go, rely on the container `TZ`, not `time.UTC`.
- **Migrations are append-only.** Add `backend/internal/store/migrations/NNNN_*.sql`
  with the next number (latest is `0040`). Never edit one that has shipped: the
  applier stores a checksum and a mismatch is a FATAL boot error, not a warning.
- **Tailwind classes are compiled from a source allowlist.** `tailwind.in.css`
  lists `@source` globs covering the 5 HTML files and `js/**/*.js`. A class used in
  a file no glob covers is never compiled and silently produces no styling — adding
  a new HTML page means adding an `@source` line. Serving `frontend/` outside Docker
  uses the committed (possibly stale) `tailwind.css`, so new classes may look broken
  locally while being correct in production.
- **Escape every user string in the frontend:** `App.Utils.esc(text)` when
  interpolating into HTML. Note `esc()` does not protect HTML attributes or JS string
  literals — `js/utils.js` documents the separate cases.
- **Fail safe on config.** `APP_ENV` defaults to `development`; production behaviour
  is opt-in. A variable that is not forwarded in `docker-compose.yml` never reaches
  the container no matter what `.env` says.

## Layout

- `backend/` — Go API server (`go.mod`, handlers, DB layer)
- `frontend/` — vanilla JS + HTML
- `services/analytics/` — Python FastAPI service (separate Dockerfile, `requirements.txt`)
- `docker-compose.yml` — orchestrates all three
- `AI_DOCS/` — per-subsystem spec sheets; read the relevant one BEFORE editing that
  subsystem. Each claim carries a `file:line` citation so it can be re-verified.
  Index: `AI_DOCS/README.md`.

| Working on | Read first |
| --- | --- |
| auth, roles, tenant scoping, CSRF, MFA | `AI_DOCS/auth-and-tenancy.md` |
| invoices, pricing, discounts, payments, referrals | `AI_DOCS/billing.md` |
| classes, sessions, cancellations, iCal, attendance | `AI_DOCS/calendar-and-sessions.md` |
| cron, email queue, push, websockets | `AI_DOCS/jobs-and-outbound.md` |
| SQL, migrations, schema, caching | `AI_DOCS/database.md` |
| any `frontend/js/` module | `AI_DOCS/frontend-contract.md` |

## Default branch

Releases ship from `prod`. PRs target `prod`.

## Coding guidelines

Global rules in `~/.claude/CLAUDE.md` apply as defaults. The overrides below
take precedence where studyhub's architecture differs from the generic spec.

### Backend (Go) — overrides to `~/.claude/guidelines/golang.md`

Load `@~/.claude/guidelines/golang.md` first, then apply these project-specific
overrides:

- **Error handling:** studyhub does NOT have an `apperror` package. Ignore all
  `apperror.Result[T]` / `apperror.New` / `apperror.Wrap` rules. Instead:
  - In handlers: `respondError(w, "human message", http.StatusXxx)` + early return.
  - In non-handler code: return `error` (standard Go pattern).
  - Log with `logFromReq(r).Error("msg", "err", err)` for request-scoped context.
- **File naming:** `snake_case.go` (standard Go convention). NOT `PascalCase.go`.
  Handler files: `handlers_<domain>.go` (one per domain).
- **Abbreviation casing:** follow Go stdlib convention — `ID`, `URL`, `JSON`,
  `HTTP`, `API`, `SQL`. NOT `Id`, `Url`, `Json`.
- **Enums:** string constants are fine. No `byte + iota` requirement.
  Roles are `"superadmin"`, `"admin"`, `"parent"`, `"teacher"` — matching JWT claims
  and JSON. Check admin access with `core.IsAdminRole(c)`, never `c.Role == "admin"`:
  the bare comparison locks superadmins out and is a recurring source of drift.
- **DB placeholders:** use `?` — the wrapper in `db.go` translates to `$N`.
- **Function size:** target ≤30 lines for handlers (they decode + validate +
  query + respond in one flow). Pure helpers should still be ≤15 lines.
- **`any` / `interface{}`:** allowed in `respond()` / `respondError()` JSON
  envelope and `map[string]any` for ad-hoc JSON responses. Not in business
  logic.
- **No ORM, no repository pattern.** Raw SQL via `db.QueryRow` / `db.Exec`.
  All queries scope by `tenant_id` and `deleted_at IS NULL`.

### Database — overrides to global DB rules

- **Tables/columns:** `snake_case` (PostgreSQL convention). NOT `PascalCase`.
  This is production data — cannot change.
- **Primary keys:** `id` (serial or text like `STU_xxx`). NOT `{TableName}Id`.
- **Foreign keys:** `<entity>_id` (e.g., `student_id`, `family_id`).
- **JSON keys in API responses:** `camelCase` (standard REST convention,
  matching Go struct `json:"camelCase"` tags).

### Frontend (vanilla JS)

Load `@~/.claude/guidelines/typescript.md` for general JS discipline. Overrides:

- **No TypeScript, no build step.** Vanilla ES6+ only.
- **IIFE pattern:** every module wraps in `(function() { window.App = ...; })();`
- **Namespace:** all state/functions under `window.App`.
- **XSS prevention:** always `App.Utils.esc(text)` when interpolating user
  strings into HTML.
- **API calls:** `App.Api.get/post/put/del(path, body, opts)`. Errors auto-toast
  unless `{silent: true}`.
- **No `Promise.all` requirement** — modules load synchronously via script tags,
  API calls are typically sequential user actions.
- **File naming:** `camelCase.js` for modules (e.g., `dashboard.js`, `store.js`).

### Python (analytics service)

Load `@~/.claude/guidelines/python.md`. Overrides:

- **Standalone service.** The guideline's `Result[T]` dataclass pattern is optional here — raw exceptions at module boundaries are fine; log + re-raise is enough.
- **Database identifiers stay `snake_case`** in this service too (matches the Go backend and shared Postgres schema). Ignore the guideline's PascalCase-for-DB-columns rule.

## Changelog

- `CHANGELOG.md` — human-curated, Keep-a-Changelog format. Add to `## [Unreleased]`.
- `RELEASES.md` — auto-generated by release-please from Conventional Commits on `prod`.

## Commit format

Conventional Commits: `<type>(<scope>): <summary>`

Types: `feat`, `fix`, `refactor`, `chore`, `docs`, `test`, `perf`

Examples:
- `feat(billing): add early bird discount auto-detection`
- `fix(invoices): parent query missing referral_credit column`
- `chore(docker): add .dockerignore and optimize build flags`
