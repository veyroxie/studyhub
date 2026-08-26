# V2 Idea Menu — consolidated from the six-part audit (2026-08-06)

Sources: backend architecture, data layer, connectivity, frontend + testing,
security, infra/ops audits + external best-practice research. Each item has an
ID (pick by ID), severity, and effort (S = under an hour, M = a day-ish,
L = multi-day). Items marked [BUG] are broken today, not design choices.

---

## Tier 0 — broken now; fix regardless of v2 (all [BUG])

| ID | Item | Sev | Effort |
|----|------|-----|--------|
| A1 | MFA login lockout: frontend never handles the mfaRequired challenge; an admin who enables MFA can never log in. Build the 6-digit prompt (or hide the MFA toggle until built). | CRIT | M |
| A2 | Teacher "My Class Parents" announcements reach every parent: targetClassIds dropped client-side and absent server-side. Privacy. | HIGH | M |
| A3 | Parent "I've paid" fake success: network-failure branch marks the invoice submitted locally, toasts "will sync later", never syncs. | HIGH | S |
| A4 | Uploads destroyed every deploy: no volume for /app/uploads. One compose line. | HIGH | S |
| A5 | Port 8080 public over plaintext, bypassing ufw + Caddy TLS. Bind 127.0.0.1. | HIGH | S |
| A6 | Env safeguards (plan 6.5, still unlanded): compose APP_ENV default -> development; gate jobs + mailer on production; boot config-check refusing dangerous env/DB combos; .gitignore .env.* + uploads/; .dockerignore secrets. | HIGH | S-M |
| A7 | DB pool 150 vs Postgres max 100 (plus work_mem OOM math). SetMaxOpenConns(40) + max_connections=60. Likely contributor to the RM 0 incident. | HIGH | S |
| A8 | ALERT_EMAIL never forwarded into the container: the built-in health alerter emails nobody. Wire through compose + .env.example. | HIGH | S |
| A9 | tenant lookup defaults to tenant 1 on DB error (mis-attributes webhooks/audit); email queue + overdue reminders have no claim step (double-send on overlap). | HIGH | S-M |
| A10 | Pre-commit hygiene: split migration 0030 (drop the redundant school_fees ALTER, keep the index); stop piping migration SQL through the ?->$N rewriter (migrate.go one-liner); escalate checksum drift to fatal. | MED | S |
| A11 | Reflected XSS in student search box (unescaped value attribute); escape emptyState()/showConfirm() helper args. | MED | S |
| A12 | Admin Import / Reset-all-data buttons only touch localStorage (silent placebo). Wire to /api/admin/import + /api/admin/clear-seed or remove. | MED | S |
| A13 | Password reset leaves stolen access JWTs valid up to 30 days. Add users.session_epoch claim check. | HIGH | M |
| A14 | Announcements privacy sibling: attendance quick-announce hardcodes audience 'All Parents'. Fix with A2. | MED | S |
| A15 | Parent iCal feed shows cancelled sessions: `handlers_ical.go:150-169` expands a class's weekday into dates but never consults `cancelled_classes` or `holidays`, so a subscribed parent still sees a class the centre cancelled and turns up for it. Wrong today, independent of v2. Fix with the shared session expander (`V2_REBUILD_PLAN.md` 8.7.1) so billing and the feed agree on what a session is. | HIGH | M |
| A16 | **FIXED 2026-08-26** (duration-based grant + migration 0041 top-up + absent flow + shared creditsForClass helper). Was: Cancellation credit grant is 4x too small: `handlers_cancelled.go` hardcodes `minutes=1` per student, but the agreed unit (WhatsApp 02/04/2026, Nadine-confirmed) is 1 credit = 15 min, so a 1-hour class must grant 4 (duration/15). The frontend absent flow's 4 is correct. Live impact: Nadine's 19/08 "insufficient credit" when redeeming a replacement. Fix = grant (end_time-time)/15, and backfill or top up credits granted at 1. Blocks replying to her 17/08 "which button" question. | HIGH | S |
| B9 | Reschedule a class session (decided 2026-08-26): alongside cancel+credit (per-student make-ups at different times), admin can move ONE dated session wholesale to another date for all students. Shape: a `class_session_moves` exception row (tenant_id, class_id, from_date, to_date) in the 0040 keyed-by-(class_id,date) family — NOT a cancellation (no credits granted: the class still happens). Touches: calendar render (strike original, show moved), the 8.7 session expander (classify dates held/cancelled/holiday/moved-out/moved-in; cross-month moves change billed counts), iCal (same UID, new DTSTART), and parent announcement on move. Build with/after the session expander so billing and display agree. | MED | M |

## Tier 1 — v2 billing/data core (structural; mostly already in plan, now sharpened)

| ID | Item | Sev | Effort |
|----|------|-----|--------|
| B1 | Repository layer: store.scopedWhere(c, table) emitting tenant + deleted_at together; per-domain store files; migrate the ~19 list helpers + 6 update handlers. Kills 90+ scoping misses, 54 soft-delete misses, 26 copy-pasted list blocks, 16 duplicated column lists. Highest-leverage single change. | HIGH | L |
| B2 | core.AppError + core.Fail(): dual-channel errors, closes 139 unlogged 500s + err.Error() leaks; typed error codes. | HIGH | M |
| B3 | One migration source of truth: retire createSchema ALTERs + error-swallowing runMigrations into numbered migrations (goose-style; keep the existing good runner). CI gate on destructive statements. | HIGH | L |
| B4 | Schema integrity: FK constraints (invoices.student_id, payroll.staff_id first; '' -> NULL cleanup then NOT VALID/VALIDATE); CHECK constraints on every status enum; real DATE/TIMESTAMPTZ types for business dates; anchor all cron date math to tenants.timezone. | HIGH | L |
| B5 | Stripe-style invoice state machine: draft -> open (finalize assigns number from a locked counter row, freezes lines) -> paid/void; credit notes; (student, period) UNIQUE + ON CONFLICT for race-free cron idempotency. Replaces plan 3/4 details. | HIGH | L |
| B6 | enrolled_classes JSON-in-TEXT -> proper enrollment join table (kills LIKE matching, counter drift, orphan class ids). **Promoted to a hard prerequisite for session billing (2026-08-20):** the join carries no start date, so there is no way to know a student joined a class mid-month. Prorating a joiner — the thing Nadine asked for — is impossible until the join table exists with `started_on` / `ended_on`. Not optional cleanup; it blocks 8.7. | HIGH | M |
| B7 | Money end-to-end: NUMERIC in DB stays, Go switches to integer cents or shopspring/decimal; one shared formatter (currency/date/amount-in-words) used by PDF, UI, email. | MED | M |
| B8 | Soft-delete cascade policy: deleting a student consistently hides/handles invoices, attendance, credits (today parent and admin lists disagree). | HIGH | M |
| B9 | Missing composite indexes (invoices tenant+created_on partial first; 5 more); drop duplicate/redundant indexes. | MED | S |
| B10 | Drop dead weight: subjects + centers tables, registrations.school_fees, teacherHoursWorked, dead helpers, messages.js, feedbackReplies (both ends), 16 superseded collection GETs. | LOW | M |

## Tier 2 — security hardening (beyond A-items)

| ID | Item | Sev | Effort |
|----|------|-----|--------|
| C1 | RLS: fix the false 0015 comment now; then activate (deny-by-default NULL branch, studyhub_app LOGIN role, pinned-conn SET LOCAL) as the DB-level backstop. | HIGH | L |
| C2 | WebSocket auth parity: revocation + status checks before upgrade. | MED | S |
| C3 | Trust X-Forwarded-Proto only from the proxy; force Secure cookies in production. | MED | S |
| C4 | CSRF token bound to session (HMAC over jti) or __Host- prefix; exact-match exempt list. | MED | S |
| C5 | RateLimitLogin on MFA setup/confirm/disable; last_totp_step replay guard. | MED | S |
| C6 | /metrics + /api/health detail behind admin; public health returns ok-only. | MED | S |
| C7 | CSP without unsafe-inline (depends on E2 inline-handler migration); object-src none, base-uri self; drop the CDN Tailwind (a build-time pipeline already exists). | MED | M |
| C8 | CI security gates: un-continue-on-error govulncheck; SHA-pin gosec/govulncheck actions; bump alpine 3.19 + 2024 Python pins; pip-audit. | MED | S |
| C9 | Upload quota/GC per user; superadmin check on the tenant_id==0 god-mode branch; webhook event dedup table. | MED | M |
| C10 | iCal/suspension: bump token version on suspension; status check in feed. | LOW | S |

## Tier 3 — testing (currently zero frontend tests; backend jobs/money code untested)

| ID | Item | Sev | Effort |
|----|------|-----|--------|
| D1 | Unit tier: ~40 Vitest tests over App.Utils pure helpers (esc, currency, TZ-dependent date helpers — TZ pinned Asia/Kuala_Lumpur) + store.validate, loading IIFE modules unchanged via a VM shim. One afternoon, no app changes. | HIGH | S-M |
| D2 | Playwright e2e: 8 specs covering every money path (login, create invoice, mark paid + reload, parent submit incl. the network-abort fake-success assertion, bulk delete + double-click, student CRUD, registration approval incl. XSS payload assertion, role-switch isolation) + one axe a11y smoke. Programmatic login via storageState. Runs in CI against compose. | HIGH | M-L |
| D3 | Backend: table tests for the cron money math (early-bird, sibling, referral, overflow) — currently zero tests on the code that computes money. golangci-lint + coverage in CI. | HIGH | M |

## Tier 4 — ops/infra (right-sized for one droplet)

| ID | Item | Sev | Effort |
|----|------|-----|--------|
| E1 | Deploy pipeline reality: merge the pending release-please PR (pipeline has never run); checkout the triggering tag; test gate before deploy; export VERSION so /api/health stops saying "dev"; pre-deploy pg_dump as the rollback escape hatch; verify rollback health. | HIGH | M |
| E2 | Backups: version-controlled cron.d installer; staleness check keyed to .sql.gz + size; local retention 14d + Spaces lifecycle 365d; disk-space guard in backup_verify. | HIGH | S-M |
| E3 | Restore drill (once, ~45 min, throwaway droplet, restore from the S3 copy, measure RTO) -> docs/runbooks/restore-backup.md. Then the other four runbooks: droplet-down, deploy-failed, domain-tls, email-provider. | HIGH | M |
| E4 | Alerting stack, free tier: Sentry (or the built-in alerter once A8 wires it + a 5xx-count check), Better Stack uptime on /api/health, Healthchecks.io dead-man ping on the backup cron. | HIGH | S-M |
| E5 | Delete the analytics service (orphaned; gates API startup; 256M). Or wire it for real — decision needed. | MED | S |
| E6 | Liveness/readiness split: static /api/livez for Docker; deep /api/health for deploy gate + uptime monitor. | MED | S |
| E7 | Docs truth pass: README layout/commands (go run ./cmd/api), one env template, CONTRIBUTING "we don't do X" list (3 of 6 entries false), deploy docs matching the real pipeline. | MED | M |
| E8 | S3 uploads decision: fix S3_HOST->S3_ENDPOINT drift + UPLOAD_STORE wiring (uploads can never use S3 today) or drop the S3 branch. Payment provider env plumbing (Billplz/Stripe vars unsettable -> Pay Online 502s) + post-payment redirect handler. | HIGH | M |

## Tier 5 — frontend modernization (incremental, no framework)

| ID | Item | Sev | Effort |
|----|------|-----|--------|
| F1 | Snapshot replaces, never merges (Object.assign bug); ARRAY_DEFAULTS on cold start; stop client-side mangling of server truth (validate()). | HIGH | S-M |
| F2 | Kill inline onclick handlers -> data-id + delegated listeners (prereq for C7 CSP; fixes the 60-site injection-by-convention class). | MED | L |
| F3 | withLoading + double-submit guards on all money buttons; loading + error render states per module; route-change state reset (leaking selections/drafts). | HIGH | M |
| F4 | JSDoc + tsc --checkJs in CI (typed JS, zero build step); then incremental ES-modules migration (import maps, 37signals-style). | MED | M-L |
| F5 | Shared helpers: one pagination control (3 copies), one _field (5 copies), one _statCard (3 divergent); split the 1900/1700-line modules along existing seams. | MED | M |
| F6 | XSS-proof templating for new/refactored views: lit-html standalone (~3KB, no build), DOMPurify only where rich text must render. | MED | M |
| F7 | A11y: unset aria-hidden on the open modal (one line — modals are invisible to screen readers), label/for on login, role=alert on login error, keyboard path to row menus. | MED | S-M |
| F8 | WS reconnect backoff + cap; pagehide->visibilitychange persistence. | LOW | S |

## Tier 6 — product decisions (build the UI or delete the endpoint)

| ID | Item | Decision needed |
|----|------|-----|
| G1 | 14 implemented endpoints with no UI: DSAR export (ToS text promises it), iCal calendar URL, audit-log viewer, user management + account unlock, feedback edit/delete + replies, workshop edit, hard deletes. Build minimal UI, or remove routes. | per-endpoint |
| G2 | openapi.yaml: 70% of routes undocumented. Regenerate and keep as contract, or delete (it claims accuracy it doesn't have). | keep vs delete |
| G3 | Multi-tenant honesty: registrations hardcode tenant 1; public signup ignores host tenant. Either commit to single-tenant (drop ceremony, keep tenant_id dormant) or fix the tenant-resolution paths. Affects C1 scope. | strategic |

---

## Gap-audit addendum (2026-08-06, second pass — NEW-1..NEW-30)

A completeness audit over the areas the six audits covered thinly (PDF, email
templates, payroll, registrations, credits, PWA, seed/import, DSAR) found 30
further defects. Highest severity, promoted into scope:

- NEW-1 Tier 0: unpriced class (no level band) = student silently never billed
  — the likelier RM 0 mechanism. Skip-logging LANDED; required-band + admin
  banner queued.
- NEW-2 Tier 0: PDFs render non-Latin (CJK/Tamil) names as "..." — cp1252 core
  font. Needs a UTF-8 TTF embedded (font choice = user decision).
- NEW-3/NEW-22: PDPA delete + DSAR export both miss most PII-holding tables
  (invoice descriptions carry student names; push subs survive deletion).
  Requires a PII data inventory BEFORE B1; G1's "build DSAR UI" is downstream.
- NEW-4: ~29 real children's names hardcoded in handlers_import.go (and git
  history). A12's "wire the Import button" was the wrong direction — the
  one-shot Skooly importer should be deleted after migration, not exposed.
- NEW-5: check-in safety alerts suppressed family-wide whenever any invoice is
  Unpaid (i.e. the first week of every month, by design). Gate on Overdue only.
- NEW-6 LANDED: outbound kill-switch now also gates web push (was email-only).
- NEW-7: self-study duration is client-supplied and drives billing — compute
  server-side.
- NEW-12: over-discounted invoices email "RM 0.00" with unreconciled line
  items; NEW-15/16/17/18: payroll rounding/midnight/frozen-row/lock gaps —
  extend D3's money tests to the credit ledger + payroll (NEW-9/10/11).
- NEW-21: all tenants' emails brand as tenant 1; PrimaryColor unescaped.
- Full list with refs: task output archived; items NEW-8..NEW-30 tracked here
  as the audit's numbered findings.

Corrections to this file from the second pass: A9's email-queue/reminder claim
work is DONE (landed in the working tree); A9 now = tenant-lookup logging
(landed) + the remaining cron.go:568 audit-stamp site. A6/A12/A13: landed —
see V2_REBUILD_PLAN.md "Tier 0 status".

---

Recommended sequencing: Tier 0 this week (everything is S/M and independent);
E1-E4 + D1 next (the "sleep at night" layer); then v2 proper starts at B1/B3/B5
per the staged plan in V2_REBUILD_PLAN.md section 8. F-tier rides along with the
invoice UI rebuild rather than as a separate project.
