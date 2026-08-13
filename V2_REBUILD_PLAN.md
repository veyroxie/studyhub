# StudyHub v2 — Invoicing & Infrastructure Rebuild Plan

Status: DRAFT for review. No code changes implied by this document.
Built from: a full audit of the current invoicing subsystem (file:line refs below)
plus a concept study of the three cobibot repos (Go backend, SvelteKit frontend,
Python MCP backend). Cobibot is a reference for *concepts*, never a stack to port.

---

## 0. Non-negotiable constraints (these bound every stage)

1. **Parallel build, proven cutover.** v1 keeps billing the centre until v2 is
   validated on a copy of real data. No in-place mutation of the live system.
2. **Cron freeze Aug 1-7.** The monthly auto-biller runs on days 1-7. No schema
   change to `invoices`/pricing tables lands in that window. Stage 1 starts after.
3. **Money is never `float64`.** Postgres `numeric` (or integer minor units).
   Cobibot uses float64 for money — the one thing we deliberately do NOT copy.
4. **No silent zeros.** A missing price errors or flags a line; it never bills RM 0
   and never silently skips a student.

---

## 1. Guiding principles (from the studies, scaled to a 2-person product)

- **Everything is a line item.** Tuition, registration, deposit, materials,
  self-study, and every discount are lines. Collapse the six special-cased
  discount columns into generic lines.
- **One generic charge/discount shape** (cobibot's, hardened):
  `{ name, kind: item|charge|discount, qty, unitAmount, percent?, percentCap?, base }`.
  A surcharge and a discount share the shape; `base` + order are pinned server-side.
- **Snapshot inputs into the invoice** (jsonb), don't reference a live template.
  Invoices must freeze what they were built from. (cobibot does this correctly.)
- **Thin transport -> service -> repo.** Collapse cobibot's 5 layers
  (resolver/core/service/repository + interface/impl split) to 3. Keep interfaces
  only where we actually mock.
- **Centralize tenant scoping + soft-delete** so a query can't forget them — a repo
  helper that *requires* `tenantID`, not hand-written `AND tenant_id=?` per query.
- **Dual-channel errors**: client-safe `{code,message}` vs internal `{err,stack,requestID}`,
  one `Handle`/presenter path. (cobibot's `AppError` — portable almost verbatim.)
- **Structured recurrence + CAS-claim cron** for monthly billing (multi-instance safe).
- **Block-don't-truncate saves**; validate at the boundary; typed per-boundary shapes.
- **Config choke point + typed env manifest** validated at boot, with secret redaction.

---

## 2. Target data model

### `invoices`
`id, tenant_id, student_id, status (enum: draft|issued|paid|void), issued_no,
receipt_no, currency, period_start, period_end, created_on, due_date, paid_on,
deleted_at, line_items jsonb, subtotal, total, template_snapshot jsonb`.

- **Kill** `discount_pct, sibling_discount, sibling_ids, referral_credit,
  early_bird_cutoff, early_bird_discount` — migrate their values into `line_items`.
- `status` becomes a real lifecycle: draft -> issued -> paid (locked) / void.

### line item (inside `line_items` jsonb)
`{ kind: item|charge|discount, name, descriptor, qty, unitAmount, percent?,
percentCap?, base: subtotal|running, amount (server-computed), source (preset id
or "manual"), periodStart?, periodEnd? }`.

### `products` (catalog) — replaces `pricing_tiers` + the hardcoded JS catalog
`id, tenant_id, category (tuition|registration|deposit|material|selfstudy|addon),
name, default_unit_amount, class_type?, level_band?, active, deleted_at`.
- Tuition products carry `(class_type, level_band)` so the cron can price a class.
- Registration/deposit/materials are free products — solves "where do I add
  registration + deposit" and the dropped `registrations.school_fees` flow.

### `invoice_templates` — Postgres, NOT Redis
`id, tenant_id, name, metadata jsonb` where metadata is the reusable subset:
charges, discounts, notes, branding. (Follow cobibot's better `Reporting` entity
model — a small table + jsonb — not its Redis invoice-template model.)

---

## 3. Money & totals — pin the current inconsistency

Today the frontend applies discounts off the **net** subtotal (`billing.js:1293-1302`)
while the cron applies a flat discount off the **full** amount (`cron.go:517-531`).
v2 defines ONE server-side `NormalizeInvoice`:

```
subtotal   = sum(item lines: qty * unitAmount)
discounts  = each: usePercent ? min(subtotal*percent, percentCap) : amount   (>=0, stored negative)
afterDisc  = max(subtotal - sum(discounts), 0)
charges    = each: usePercent ? afterDisc*percent : amount
total      = afterDisc + sum(charges)      (+ tax, + min-charge topup — future hooks)
```

Client never sends totals; server recomputes and stores authoritative values
(extends today's `NormalizeLineItems`, `models.go:85`). Recommendation: discounts
off subtotal, documented once, both cron and UI use the same function.

---

## 4. Invoice + template lifecycle (the pattern you wanted)

- **Two serializers over one draft state** (cobibot invoice pattern):
  `toInvoicePayload()` (full: student, period, type, lines) and
  `toTemplatePayload()` (subset: charges, discounts, notes, branding — omits
  per-invoice fields). The subset boundary IS the template design.
- **Save** = persist the subset. **Apply** = copy subset into the current draft,
  then re-validate. Add the merge/confirm/dirty-guard cobibot's `useTemplate` lacks
  (it clobbers unconditionally).
- **Edit template** = delete + re-save (no in-place update — even cobibot skips it).
- **Edit-until-paid, then locked.** Unpaid invoices edit their line items
  non-destructively (no more wiping on amount change, `handlers_invoices.go:214`).
  Once paid -> locked; changes are a **credit-note invoice** (negative lines).
  The credit note must NOT retro-corrupt the referral milestone (counts 3 paid
  Monthly invoices, `handlers_referrals.go:105`) or `receipt_no_seq`.

### 4.1 Bulk operations (multi-select delete) — requested by the centre

Phone-style multi-select on the invoice list: a checkbox per row + "select all on
page", a "Delete selected (N)" button, one confirm dialog, one request.

- Backend: `POST /api/invoices/bulk-delete` taking `{ids: [...]}`. Soft-delete
  (`deleted_at=NOW()`) scoped by tenant + `deleted_at IS NULL`, admin-only,
  audit-logged per id — mirrors the single delete (`handlers_invoices.go:233`).
  Idempotent; returns the count actually deleted.
- Policy: bulk-delete **unpaid/draft** invoices freely. Paid invoices are
  financial records — under the edit-until-paid-then-locked rule (section 4) they
  are **voided via credit note, not deleted**. (Today single-delete has no guard
  and will delete paid invoices; v2 tightens this.)
- Soft delete is recoverable, so an accidental bulk delete is not catastrophic; a
  "Trash / restore" view is a cheap follow-on.
- **Shippable NOW, independent of the rebuild**: it only soft-deletes existing
  rows and touches none of the cron-frozen billing schema. Good first standalone win.

### 4.2 All client-facing fields editable + standardised formatting (template-driven)

Principle (from the "make everything editable" ask): every field that appears on a
client-facing document — invoice number, dates, student/parent details, line-item
names/descriptors, notes, terms, bank details, branding — is **editable data**, never
hardcoded. The invoice number is just the first instance: today it is the internal
`id` (`pdf_invoices.go:341`); v2 gives it (and every shown field) an editable,
defaulted value while the internal `id` stays fixed as the key.

Model it on cobibot's report-PDF template system (reporting study):
- The invoice is a **structured document** (fields + line items + presentation), not a
  fixed layout. Client-facing fields are entries in that document, so adding/editing
  one is a data change, not a schema + six-SELECT change (the coupling smell in the
  appendix).
- **Standardised formatting** via ONE shared formatter — number / currency / percent /
  date and amount-in-words — lifted from cobibot's dependency-free `table-expr.ts`
  formatter + `amountInWords`. So "RM 1,234.00", dates, and the ringgit-in-words line
  read identically across PDF, list, and email instead of ad-hoc per call site.
- **Templates** save / apply / edit the document's editable + presentation fields (the
  save/apply/delete lifecycle from the invoice-template study), so a standard invoice
  shape is defined once and reused, with any field overridable per invoice.

Interim (if a single editable field is wanted before the document model lands): an
editable `invoice_no` is an additive nullable column defaulting to `id`, surfaced via
`COALESCE(invoice_no, id)` in the read paths — the same pattern as the editable
Student Number. Kept as a fallback, not the target.

---

## 5. Automations kept, plumbing generalized

Monthly cron, sibling, early-bird, referral, self-study FOC stay — but each becomes
a **preset that emits generic line items**, not a schema column + code branch.

- **Cron**: replace the day-1-7 scan (`cron.go:92-123`) with a structured
  recurrence (`frequency/dayOfMonth/time`) + a `next_run_at` column claimed by a
  conditional UPDATE (compare-and-swap) — multi-instance-safe, lock-free. This is
  the single best pattern found in cobibot (`reporting-v2/min1-cron.go:82-98`).
- **Missing price** -> an error/flagged line, never a silent skip
  (`cron.go:476-489`).
- **Invoice numbering**: per-tenant Postgres sequence or `SELECT ... FOR UPDATE`
  counter — cobibot's read-latest-and-increment races (their own TODO).

---

## 6. Cross-cutting infra (cheap, high-value — from the MCP study)

- **Config choke point + env manifest** validated at boot, `dump_safe()` redaction.
  (`.env.example` already added as the first step.)
- **Migrations: one source of truth.** Today the `invoices` table is defined in
  three disagreeing places (CREATE TABLE + ALTER array + numbered migrations).
  Consolidate; add a **CI gate that fails on destructive `DROP`**.
- **Typed per-boundary shapes** (wire DTO != domain != write-input != query-filter);
  enums for closed sets; validation at the edge.
- **`/ready`** distinguishing required vs fail-open deps; DB-pool + error-rate runbooks.
- **Test-name grammar** (success/reject/can/ignore) + real-DB integration tests for
  billing math.
- **HTML-template -> PDF via headless converter** (Gotenberg/Chromium) — replaces the
  hand-built Go PDF and its discount-inference (`reconstructGross`,
  `earlyBirdDiscount`, `pdf_invoices.go:227-244`), and makes branding/templates easy.
  DECISION NEEDED (section 9).

---

## 6.5 Safeguards — non-production must never act on real people

Motivating incident (2026-07-31): a `docker compose up` on the dev box came up as
`env=production` because prod values were exported in the shell and override `.env`.
It connected to a database holding real parent records and fired the overdue-reminder
job at 9 real parents on startup — stopped ONLY because the Resend domain was
unverified (a 403). Nothing in the system prevented it. Do NOT "fix" that 403 by
verifying the domain — that would let a dev box actually send. Build these instead:

1. **Outbound off unless explicitly production.** Email, web-push, and payment
   webhooks gate behind `APP_ENV=production` AND an explicit `OUTBOUND_ENABLED=1`.
   Merely having an API key present must NOT be enough to send.
2. **Config defaults to development, never production.** Flip
   `${APP_ENV:-production}` in compose to `:-development`; production is opt-in, so a
   forgotten variable fails safe.
3. **Startup guard against dangerous combos.** A boot config-check (the MCP-study
   choke point) logs the effective config (secrets redacted) and REFUSES to start
   (or forces safe-mode) on e.g. `APP_ENV=production` + localhost DB, or a non-prod
   env pointed at the production DB host.
4. **Background jobs gated.** Monthly cron + overdue-reminder + early-bird jobs do
   NOT run unless `APP_ENV=production` (or explicit `ENABLE_JOBS=1`). A dev box must
   never generate invoices or send reminders just by booting.
5. **Non-prod email allowlist.** When outbound is enabled outside production, every
   recipient is checked against a test-address allowlist; anything else is dropped
   and logged. Real parents are unreachable from a non-prod box even by mistake.
6. **`.dockerignore` excludes secrets.** Add `.env`, `*.env`, `*.key`, `*.pem` so a
   stray env file can never be baked into an image (today it doesn't mention `.env`).
7. **Dev DB isolation enforced, not assumed.** A startup assertion refuses to run
   migrations/jobs against a DB whose host is the production droplet unless
   `APP_ENV=production`.
8. **Cron dry-run.** `CRON_DRY_RUN=1` computes and logs what would be billed without
   writing or sending — for validating the v2 biller against a prod-data snapshot.

Priority: 1, 2, 4, 6 are cheap and must land BEFORE any v2 billing code runs against
real data (including a local test with a prod snapshot).

---

## 7. Explicitly DO NOT copy from cobibot

`float64` money; Redis-as-source-of-truth templates; 5-layer interface/impl
duplication; the Ent privacy-rule DSL (507 lines/entity, built for ~8 viewer types —
studyhub has 2 roles, one centre per deploy); NATS event bus; Cognito/JWKS + key
rotation (studyhub self-signs a cookie JWT). GraphQL/gqlgen and the Svelte/urql
frontend machinery do not port to a REST + vanilla-JS app — see section 9.

---

## 8. Staged migration (parallel, cron-safe)

- **Stage 0 (today):** fix `pricing_tiers` data; split/drop the redundant half of the
  uncommitted `0030` migration.
- **Stage 1 (after Aug 7):** consolidate the schema to one migration source; add
  `products`, `invoice_templates`, make `invoices.line_items` authoritative;
  **backfill** `line_items` from the six columns using the PDF's reconstruct logic
  ONCE (then delete that logic); keep old columns read-only.
- **Stage 2:** service/repo layer + `NormalizeInvoice` + generic charges/discounts;
  presets emit line items; non-destructive edit; edit-until-paid lock.
- **Stage 3:** templates (save/apply/delete); product-catalog admin UI;
  missing-price errors.
- **Stage 4:** cron CAS-claim rewrite; drop the old discount columns; remove PDF
  inference.
- **Stage 5:** infra hardening (env manifest, CI destructive-change gate, runbooks,
  billing integration tests).

Each stage: build on a branch, test on the local copy, validate against a **snapshot
of prod data**, then cutover.

---

## 8.5 Committed scope (decided 2026-08-06)

All four tiers of V2_IDEAS.md are committed scope: Tier 1 (billing/data core),
Tiers 2+3 (security + testing), Tier 4 (ops/infra), Tier 5 (frontend
modernization), plus the gap-audit addendum items promoted there. Tier 6
product decisions remain open per item.

Tier 0 status (implemented, verified — build + vet + full handler test suite
against fresh Postgres): A1 MFA login prompt; A2/A14 announcement targeting
(migration 0031, server-side parent filter); A3 honest payment-failure UX; A4
uploads volume; A5 loopback port bind; A6 env safeguards (dev-default compose,
outbound kill-switch on mailer AND web push, ENABLE_JOBS gate, boot env/DB
guard, gitignore/dockerignore); A7 pool 40 / max_connections 60; A8
ALERT_EMAIL + seed vars forwarded; A9 tenant-lookup logging + email-queue
claim/lease + reminder claim-before-send; A10 0030 split + migration rewriter
bypass + fatal checksum drift; A11 search XSS + helper escaping; A12 import/
clear-seed wired honestly (NOTE: gap audit recommends deleting the one-shot
importer instead — open decision); A13 sessions_invalid_before (migration
0032). Landed from the gap audit: NEW-1 skip-logging, NEW-6 push gate, NEW-26
seed rand check.

Deploy prerequisites for these changes (droplet .env, BEFORE next deploy):
set APP_ENV=production and OUTBOUND_ENABLED=1 explicitly, and mkdir uploads/
next to the compose file. Local dev: docker compose down -v once (0030 was
rewritten; checksum drift is now fatal).

Shape changes from the final gap audit: single core-owned outbound gate is the
model (email + push landed; payment webhooks already fail closed); PDF UTF-8
font is a Tier-0-class prerequisite for issuable documents; PDPA PII inventory
precedes B1; credit-ledger + payroll join D3's money-test scope; NEW-1's
required-level-band lands with B5.

---

## 8.6 Centre requests — 2026-08-07..12 (Nadine, Ying Quah)

Committed to the centre for "end of this week". Ordered by dependency, not by
the order they were asked.

R1. **Subjects: Math and Mandarin, for both Group and Private.** Pricing today
    is strictly 2D — pricing_tiers(class_type, level_band), 4 rows, and 0016
    states "the centre doesn't have subjects, it has levels". That assumption
    is now dead. If a subject changes the price this becomes
    (subject × type × level) = 8+ rows and lands as the products catalog (B5);
    if it does not, subject is an attribute of the class and only R2 is needed.
    BLOCKED ON: does Math cost the same as Mandarin?

R2. **Invoice shows the class name** ("Math group lessons"). The monthly line
    already renders from monthlyClassLineName(); this is a naming/description
    change, cheap, and independent of R1's pricing question.

R3. **Early bird as flat RM10, not a percentage.** The cron ALREADY uses flat
    RM10 (cron.go EarlyBirdRM); only the manual invoice builder still applies a
    percentage (billing.js discountPct). So this is removing an inconsistency
    the audit already flagged, not new behaviour. Smallest item on the list.

R4. **Prorating on the invoice** — bill a partial month for a student who
    joins or leaves mid-month. Needs a stated rule (see open question) and a
    line-item shape that shows the basis, or parents will query it.

R5. **Generate invoices for an arbitrary month**, not just the current one.
    The cron is date-driven (days 1-7) with a dedup key of tenant|student for
    the current period; generating March from August needs the period to be an
    explicit input. Pairs with B5's (student, period) UNIQUE idempotency.

R6. **Freeze / resume a student's billing for a month.** Half-built already:
    students.subscription_status + paused_at + resumed_at exist and the cron
    skips non-active students. Needs the button, and a decision on what
    happens to an invoice that was already issued for the frozen month.

R7. **Quit reasons + inactive date** (Ying Quah, seconded). When a student goes
    inactive, record why (migrated overseas, different educational goals, went
    quiet, cost, ...) and when, for retention analysis. Wants a fixed list, not
    free text, or it cannot be analysed. Independent of everything above —
    students table + the deactivate flow + a breakdown in analytics.

Sequencing note: R3 and R2 are hours. R7 is self-contained. R1/R4/R5/R6 all
touch invoice generation, which is exactly what B5 rewrites — doing them on
the current schema means doing them twice.

---

## 9. Open decisions for you

1. **Stack: stay REST + vanilla JS, or adopt GraphQL / a frontend build step?**
   Recommendation: **stay** — the value from cobibot is concepts, not the stack; a
   stack swap multiplies rebuild risk on a live billing system. Revisit the frontend
   build step separately once the backend is solid.
2. **PDF: keep the Go PDF library, or move to HTML-template -> headless converter?**
   Recommendation: move — it kills the discount-inference bug class and makes
   templates/branding tractable.
3. **Tax/SST:** model a tax hook now (Malaysia SST) or defer? Currently hardcoded 0.
4. **Single-tenant reality:** is studyhub effectively one centre per deployment? If
   yes, we drop most tenancy machinery (keep `tenant_id` but skip the heavy scoping).

---

## Appendix: current-state defects (audit, with refs)

- Three conflicting definitions of the `invoices` table with contradictory money
  types (`database.go` CREATE TABLE + ALTER array + numbered migrations).
- Six discount columns duplicating `line_items`; the self-study FOC discount is
  already a pure generic line — the two models coexist.
- Destructive edit wipes `line_items` and leaves the six columns stale, so the PDF
  prints a fictitious "early bird" gap (`handlers_invoices.go:214`,
  `pdf_invoices.go:227-244`).
- Silent zero everywhere: `feeFor->0` (`billing.js:31`), cron skips
  (`cron.go:476-489`), `COALESCE(...,0)` on every money read.
- `registrations.school_fees` collected but never reaches billing.
- Pricing is a fixed 2x2 matrix seeded in a migration; catalog hardcoded in JS;
  rates (RM 10) duplicated across cron and frontend.
</content>
