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

## 8.55 B5 progress (2026-08-20)

**Shipped.**

- **Migration 0036 — `products`.** Catalogue of sellable items (`tuition`,
  `registration`, `deposit`, `material`, `selfstudy`, `addon`). Tuition rows
  seeded with `INSERT ... SELECT` from `pricing_tiers` so per-tenant edited
  prices carry over; Registration seeded at RM250 active; the two level-based
  Deposit rows seeded **inactive** because the amounts are unconfirmed. Nothing
  reads `products` yet — `pricing_tiers` is still the cron's source of truth.
- **Migration 0037 — `classes.monthly_fee_override`.** Wins over the matrix when
  `> 0`; `0` means unset. Exists because the 2x2 matrix cannot price Phonics (no
  level band) or a 30-minute group, and both were billing RM 0.
- **Migration 0038 — `invoices.period`.** Explicit `YYYY-MM` billing month,
  backfilled with `substr(created_on,1,7)` to exactly match what the old dedup
  keyed on. Cron dedups on it; both write paths maintain it; the update endpoint
  recomputes it since it can change `type` and `created_on`.

**Deliberately dropped from step 2.** The `draft|issued|paid|void` status
rename is a vocabulary sweep across ~6 sites in `handlers_invoices.go` plus
`billing.js` and the cron, not a migration. The four current statuses are ugly
and they work. Do it as its own commit with a mapping table, or not at all.

- **Migration 0039 — one monthly invoice per student per month.** Partial unique
  index on `(tenant_id, student_id, period)`, and the cron insert is now
  `ON CONFLICT DO NOTHING` with the losing run skipping its email. Production
  duplicate check returned 0 rows on 2026-08-20 before this was written.
  Behaviour verified against a live database: duplicate rejected; `ON CONFLICT`
  a no-op; different month allowed; two Registration invoices in one month
  allowed; re-issue after voiding allowed. The API answers 409 naming the month
  rather than a 500.

**Superseded — the check below has been run and passed.** The partial unique index
`(tenant_id, student_id, period) WHERE type='Monthly' AND deleted_at IS NULL`
is what makes the monthly run race-free via `ON CONFLICT DO NOTHING` instead of
the preloaded dedup map, and it is what makes R5 (generate for an arbitrary
month) trivial. It is NOT written yet because a pre-existing duplicate fails the
migration, and a failed migration stops the API booting. Run first:

```sql
SELECT tenant_id, student_id, period, count(*) AS copies, string_agg(id, ', ')
FROM invoices
WHERE type='Monthly' AND deleted_at IS NULL AND period <> ''
GROUP BY tenant_id, student_id, period HAVING count(*) > 1;
```

Empty means the index is safe to add. Verified locally against a database
carrying a planted duplicate; the query catches it.

**Structural finding.** 30 production classes have no level band, so they price
at 0. Five are Self-Study and correctly unpriced. The rest split into private
one-to-one classes named `Teacher X (Student)` and level classes named
"Level 1 & 2" / "Level 3 & 4". The latter **straddle both pricing bands**
(`1-3` and `4-6`), so no band is correct for them. This is the same mismatch as
Phonics: the matrix assumes one band per class and the centre's classes do not
work that way. Their own numbers imply an hourly model (RM60/hr group, RM65/hr
for the upper band) — worth pricing per hour x duration x sessions in v2 rather
than extending the matrix. `monthly_fee_override` covers it in the meantime.

Severity note: `package_amount > 0` short-circuits class-fee pricing entirely,
so only students with no package amount are exposed. Those get **no invoice at
all** (a logged warning), not an RM 0 one.

---

## 8.7 Session-based billing (decided 2026-08-20) — GREENLIT 2026-08-26

**Nadine acked the parent-facing consequence on 26/08** (amounts vary with the
session count; invoices still generate on the 1st; holidays must be entered
before the month starts). **Per-student rates confirmed:** in a mixed L3&4
class, each student pays their own band (L3 RM60/hr, L4 RM65/hr), so the rate
resolves from (student's level band x class type) with a per-class
`session_rate` override for specials. Every decision gate is now closed:
F1 credits-only, F8 per-session storage, rate resolution, and the go itself.
Prerequisite chain (8.7.2) can start.

**Decision: retire monthly fees. Price is rate x sessions actually scheduled.**

The centre prices per session and always has. `pricing_tiers` stores a monthly
figure, and every awkward case is the same mismatch showing through:

| Symptom | Same root cause |
|---|---|
| Phonics RM60/hr, no level band | priced per session, matrix wants a month |
| 30-minute Math RM30 | priced per session |
| "Level 3 & 4" straddles both bands | class isn't one band, it's a set of sessions |
| Five-week months bill the same as four | a month is not a fixed number of sessions |
| Prorating a mid-month joiner | needs a session count, not a fraction |
| One-off / extra classes | no way to bill a single session |
| `monthly_fee_override` (0037) | a patch over the same gap |

Confirmed by Nadine 2026-08-17..20: the 30-minute class and Phonics are both per
session. Their own numbers already imply the rate — RM240/month over 4 weekly
one-hour sessions is RM60/hr, matching Phonics exactly; the RM260 band is
RM65/hr.

**Shape (to be designed, not yet built).**

- A class carries a session rate and a duration, not a monthly fee.
- The monthly run counts the sessions that actually fall in the period for each
  enrolled student — from the class schedule, minus `cancelled_classes`, minus
  `holidays` — and bills `rate x count`, one line per class showing the count.
- Prorating stops being a feature: a student who joins mid-month simply has
  fewer sessions in their first period.
- One-off classes become a session with no recurring parent, or an ad-hoc line
  at the same rate.
- `monthly_fee_override` and `pricing_tiers` both retire once this lands.

**Consequences that need deciding before any code.**

1. **Invoice totals will vary month to month.** Parents used to a flat RM240 will
   see RM300 in a five-week month. The centre has to tell families first. This is
   a business decision, not a technical one.
2. **Migration of historical invoices.** Existing invoices stay as they are; only
   generation changes. Reporting that compares months must expect variation.
3. **Holidays and cancellations become load-bearing for money.** Today a missing
   holiday row is cosmetic. After this it changes what a parent is charged, so
   the holiday calendar needs to be right before go-live.
4. **The early-bird, sibling and referral discounts** are flat RM amounts and are
   unaffected, but the base they come off now moves.

### 8.7.1 Audit — the full change surface (2026-08-20)

**Everything that reads a monthly price.** This is the complete list; there is no
shared helper, so each is its own edit.

| Site | What it does | After |
|---|---|---|
| `jobs/cron.go:345` | `COALESCE(NULLIF(monthly_fee_override,0), pt.monthly_fee, 0)` per class | replaced by rate x session count |
| `jobs/cron.go:~505` | one invoice line per enrolled class at `m.fee` | line gains a session count and rate |
| `handlers_classes.go:64` | `listPricingTiers` feeds the snapshot | retires with `pricing_tiers` |
| `handlers_pricing.go:37` | admin edits a tier's `monthly_fee` | becomes editing a session rate |
| `handlers_classes.go:41/145/213` | class CRUD carries `monthly_fee_override` | becomes `session_rate` + duration |
| `billing.js:31` | `feeFor()` looks up the tier for the invoice builder | reads the rate instead |
| `calendar.js:957/1101/1109` | the pricing-matrix editor UI | becomes a rate editor |
| `calendar.js:648/708/1219/1257` | the custom monthly fee field (0037) | retires |
| `models.go:191/200` | `MonthlyFeeOverride`, `MonthlyFee` | retire |

**The pattern already exists in the codebase.** Self-study overflow
(`cron.go:266`) already bills `hours x SelfStudyOverflowRatePerHour` and builds a
line item carrying `Qty`, `UnitPrice` and a descriptor. Session billing is that
same shape applied to tuition, so `InvoiceLineItem` needs no change: `Qty`
becomes the session count and `UnitPrice` the rate. The PDF and the frontend
already render `Qty x UnitPrice`, so neither needs reworking.

**What is genuinely missing: a session expander.** Nothing server-side turns "a
class on Mondays" into "these dates in this month".

`handlers_ical.go:150-169` is the closest thing — it walks a date range and keeps
days matching `parseDayName(cls.Day)`. It is the right skeleton and
`parseDayName` is reusable, but it is **holiday-blind and cancellation-blind**.

> **Separate live bug, found during this audit.** Because the iCal feed does not
> consult `cancelled_classes` or `holidays`, a parent subscribed to the calendar
> still sees sessions that were cancelled. That is wrong today, independently of
> billing. Worth fixing on its own; the fix is the same filter session billing
> needs, so build the expander once and use it in both.

Proposed shape: `jobs.SessionsInPeriod(classID, from, to) []time.Time`, filtering
`cancelled_classes` and `holidays`, used by the cron, the iCal feed, and the
"how many sessions" hint in the invoice UI. One function, three callers, which
is what makes it worth extracting rather than inlining.

**Risk register.**

1. **Holidays become money.** `holidays` is currently cosmetic; after this a
   missing row overcharges a parent. The calendar must be correct and complete
   for the billing period *before* go-live, and adding a holiday retroactively
   changes what someone owed.
2. **Cancellations become money, and already grant credits.** A cancelled session
   both removes a charge and issues a replacement credit. Decide whether that is
   double compensation, because today the credit is the only compensation.
3. **Enrolment has no start date.** `students.enrolled_classes` is a bare id
   list, so there is no way to know a student joined a class mid-month. Prorating
   a joiner needs an enrolment date, which means the join table from B6 is a
   prerequisite, not an optional cleanup.
4. **Five-week months raise bills ~25%.** Parent-facing, needs telling first.
5. **`package_amount` short-circuits everything** (`cron.go:~505`). Decide whether
   a manual package amount still overrides a computed session total, or retires.

**Prerequisites, in order.** B6 enrolment join table (risk 3) -> session expander
+ iCal fix -> rate fields on classes -> cron switchover -> retire
`pricing_tiers` / `monthly_fee_override`.

**Sequencing.** This supersedes B5 step 3 (`NormalizeInvoice`) — normalising the
totals is worth doing as part of this rather than twice. It does not block B5
step 4 (products CRUD, one-off lines), which is useful either way and should
land first.

### 8.7.2 Second audit — plan vs the AI_DOCS invariants (2026-08-21)

**Status 2026-08-21 (evening): F1 and F8 are decided (see inline), the credit
conflict is resolved (unit = 15 min, backend grant is the bug — A16), leaving F2
(freeze session dates on the line), F3 (cancellation hardening), F4 (holiday
predicate), F5 (zero-session rules), F6 (expander shape), F7 (change surface),
and the package_amount decision open.**

Checked 8.7/8.7.1 against `AI_DOCS/` (billing, calendar-and-sessions, database,
jobs-and-outbound). Findings ranked. F1 and F2 are design holes that change the
shape of the feature; the rest are gaps in the change surface or prerequisites.

**F1 — RESOLVED 2026-08-21: credits-only, always.** Ely decided: cancellations
never reduce an invoice, whenever they are logged; the replacement credit is the
sole compensation; only holidays reduce the billed session count. The billing
expander therefore subtracts holidays only, and `cancelled_classes` stays out of
the money path entirely. Fits the advance-billing model (invoice on the 1st) and
the early-bird incentive. Original analysis kept below for the record.

**F1 (original) — The plan bills "sessions actually scheduled", but the cron bills in
advance. (CRIT — decide before the expander is specced.)** Invoices are issued
on the 1st for the *current* month (`cron.go:444-460`: due the 7th, period =
1st..last day of the same month). On the 1st, the month's cancellations mostly
have not happened yet, so "minus `cancelled_classes`" subtracts almost nothing —
the count is a forecast, not an actual. Consequences:

- A session cancelled *after* issue does not reduce the invoice; the replacement
  credit stays the only compensation. One cancelled *before* issue reduces the
  invoice AND still grants a credit (`handlers_cancelled.go:64-69` grants
  unconditionally). Same event, different compensation depending on when the row
  was written — that inconsistency is a dispute generator, and it is the real
  form of risk 2's "double compensation" question.
- Options: (a) bill in arrears on actuals — the payroll half of the cron already
  bills the *previous* month (`cron.go:807`), so the pattern exists, but due
  dates and early-bird ("paid by the 7th of the billed month") shift meaning;
  (b) bill the forecast and reconcile next month with a credit line;
  (c) keep credits as the only cancellation compensation and make the expander
  ignore cancellations for money (holidays only). Pick one first; it decides
  what the expander must return.
- If (a): `monthlyPeriod` derives `period` from `createdOn[:7]`
  (`handlers_invoices.go:196-201`) — wrong under arrears (an October-issued
  September invoice must carry `2026-09`). The 0039 dedup itself still works.

**F2 — The schedule is a mutable template; billing from it has no history.
(HIGH.)** Sessions are computed from the *current* class row. Migration 0040's
own warning — editing a class "rewrites every Monday, past and future" — now
rewrites money: change a class from Monday to Tuesday and every past month's
count becomes unreproducible. The invoice must be the frozen record: the line
item should carry the actual session *dates* (in the descriptor), not just
`Qty x rate`, so a dispute six weeks later is answerable without replaying a
schedule that no longer exists. Same reason B5 freezes lines on issue.

**F3 — CLOSED 2026-08-28 (migration 0044).** Unique partial index +
`deleted_at` + dedupe/claw-back of historical duplicates; create is idempotent
(409, no re-grant); `DELETE /api/cancelled-classes/{id}` undoes with credit
claw-back and a reversal announcement; undo buttons in the calendar. Original
finding kept for the reasoning:

**F3 (original) — Cancellations are not undo-able and not idempotent; both become money
bugs. (HIGH — promote to prerequisite.)** Per `AI_DOCS/calendar-and-sessions.md`:
`cancelled_classes` has no unique `(class_id, date)`, no `deleted_at`, and no
DELETE/PUT route — every POST re-grants credits, and a mistaken cancellation can
never be reversed through the API. Cosmetic today; after 8.7 an accidental
cancellation permanently cuts a parent's bill with no undo. Needs: unique index
(own migration, after a production duplicate check — same playbook as 0039), an
undo route, and idempotent credit grants. Add to the prerequisite chain before
"cron switchover".

**F4 — CLOSED 2026-08-28.** Canonical predicate pair `core.HolidayCovers` /
`App.Utils.holidayCovers`, unit-locked on both sides; the backend's
open-ended-empty-end_date bug (reminders suppressed forever after any past
single-day holiday) is fixed. The expander must use the Go helper. Original
finding:

**F4 (original) — Holiday range semantics differ frontend vs backend, and become money.
(MED-HIGH.)** Frontend: multi-day only when `endDate >= date`
(`calendar.js:33-43`); backend: `date <= ? AND (end_date='' OR end_date >= ?)`
(`jobs.go:337-341`). A holiday with `end_date < date` silently degrades — today
cosmetic, after 8.7 it changes a charge. The expander must own ONE canonical
range predicate and both sides must use it. Holidays are per-tenant: each tenant
must maintain their own calendar for billing to be right (risk 1 assumed one
calendar).

**F5 — Zero-session months hit `ValidAmount`. (MED.)** `core.ValidAmount` is
strictly `> 0`. A count-0 class line and an all-lines-zero student need explicit
rules (skip line / skip student, mirroring today's `fee <= 0` skip at
`cron.go:487-493`) — and a skipped student must not burn that month's referral
credit.

**F6 — SHIPPED 2026-08-28 as `store.SessionsInPeriod`.** Classifies
`held | cancelled | holiday | moved_out | moved_in` (moves were not in the
original list; the 27/08 reschedule feature added them), returns TEXT local
dates, derives tenant from the class row, and carries `Billable()` (cancelled
bills, holiday does not, moves bill on origin). The iCal feed is the first
consumer; the cron switchover (F5) is the second. Original reasoning:

**F6 (original) — Expander shape: classify, don't filter. (MED.)**
`SessionsInPeriod(classID, from, to) []time.Time` has three problems:

- The iCal fix (A15) wants to emit `STATUS:CANCELLED` on the *same UID*, not
  omit the event — a filter that drops cancelled dates cannot produce that.
  Return classified dates (`held | cancelled | holiday`); billing counts `held`,
  iCal renders all three.
- `[]time.Time` re-imports the timezone bug class the repo spent effort
  banishing: every consumer compares TEXT `YYYY-MM-DD` in MYT
  (`AI_DOCS/database.md`). Return local date strings, or format exactly once
  inside the expander.
- Tenant scoping: holidays queries need the tenant id — derive it from the class
  row inside the function, not from claims (the iCal caller has synthetic
  claims).

**F7 — Change-surface omissions in the 8.7.1 table. (MED.)** Verified by grep:

| Missed site | Why it matters |
| --- | --- |
| `jobs/seed.go` | seeds `pricing_tiers` (and recomputes enrolment via LIKE at :228) — breaks on retire |
| `store/database.go:241` | `createSchema` still defines `pricing_tiers`; retiring needs a deprecation migration + createSchema edit for fresh DBs |
| `handlers_registrations.go:307/449` | student inserts write `enrolled_classes` — B6 surface |
| `AI_DOCS/billing.md`, `calendar-and-sessions.md`, `database.md` | document the matrix as current; must be updated in the same change (doc-drift rule) |
| `subjects.monthly_fee` | dead legacy (0002/0013), zero readers — deletable, not a blocker |

**B6 is mis-sized.** `enrolled_classes` has ~30 backend references across 8
files plus 12 frontend modules reading `enrolledClasses`. That is an L, not an
M. Consider staging: create the join table, dual-write, migrate readers
incrementally, keep the JSON column as a maintained mirror until the last LIKE
dies (`handlers_classes.go:108-117`, `handlers_cancelled.go:118`,
`parent_scope.go:20`, `seed.go:228`).

**B6 stage 1 SHIPPED 2026-08-28.** Migration `0043_enrollments_table.sql`
creates `enrollments` (`started_on` / `ended_on`, partial unique index on live
rows) and backfills from the JSON with deterministic `ENR_<student>_<class>`
ids. `store.SyncEnrollments` / `EndAllEnrollments` dual-write at all five
mutation sites (student create / update / delete, import pass-4, registration
approve post-commit); removal ENDS a row, never deletes it, so the enrolment
window survives for proration. The JSON column is still the only read path —
stage 2 migrates readers, stage 3 kills the LIKE matching. Locked by
`TestEnrollments_DualWriteLifecycle`.

**F8 — BUILT 2026-08-28 (migration 0045).** `pricing_tiers.hourly_rate`
(backfilled monthly/4 = the quoted 60/65/120/130), `classes.session_rate`
override, `students.level_band` (band only, Ely-confirmed; '' = class's
band), resolver `store.SessionRateFor` with no-silent-zeros errors, and the
admin UI for all three. The cron still bills monthly until F5. Decision
trail below:

**F8 (decision) — CLOSED 2026-08-26: per-student rates confirmed by Ely.** Build the rate
lookup as (student band x class type) matrix with per-class `session_rate`
override. Earlier amendment kept for the reasoning:

**F8 (amended 2026-08-21 after Nadine's answers):** private is confirmed double group (RM120/130 per hour), and mixed
L3&4 classes appear to charge by the STUDENT's level (L3 pays RM60/hr, L4 pays
RM65/hr in one class — her "Yes" was clearer about class-sharing than rates, so
re-confirm before building). Consequence if it holds: `pricing_tiers` does NOT
retire — it converts to an hourly matrix (class_type x level_band) as the
default rate source, and the per-class `session_rate` becomes the override for
specials (Phonics RM60 no-level, 30-min Math RM30, negotiated). Rate resolution:
per-class override, else (student band x class type) matrix. The deposit is also
settled: applied to the final month (held liability), so the 0036 deposit
products activate rather than delete. Per-session billing itself is still
awaiting her go ("let me get back to you") — do not switch the cron until then.

**F8 (superseded detail) — store per session.** Ely confirmed per-session rates
("no way it's that low for a whole month"), and the chat corroborates: RM240/mo
= 4 x RM60/hr sessions, Phonics RM60/hr, the 30-min class RM30 = 0.5h x RM60,
private = exactly double group (11/06 chat). Store `session_rate NUMERIC(12,2)`
per session; the UI enters/displays per-hour + duration. Original reasoning:

**F8 (original) — Rate unit decision. (LOW, but decide early.)** Nadine quotes per hour
(RM60/hr x duration); the simplest storage is per *session*. If rate-per-hour x
duration derived from `time`/`end_time`, then editing a class's times silently
changes its price (F2 again). Recommend: store `session_rate NUMERIC(12,2)` per
session (0 = unset, matching 0037's convention), show the per-hour equivalence
in the UI. Never `DOUBLE PRECISION` (0025); new columns go in a numbered
migration only, never `createSchema` alone (`AI_DOCS/database.md`).

**Revised prerequisite chain.** F1 decision -> B6 join table (`started_on`/
`ended_on`, expander intersects the enrolment window with session dates) ->
cancellation hardening (F3) + canonical holiday predicate (F4) -> classifying
expander + iCal A15 fix -> rate fields (F8) -> cron switchover (F5 rules) ->
retire `pricing_tiers` / `monthly_fee_override` + seed/doc sweep (F7).

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
