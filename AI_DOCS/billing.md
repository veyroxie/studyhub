# Billing: invoices, pricing, discounts, payments, referrals

Read this before touching `handlers_invoices.go`, `handlers_pricing.go`, `payments.go`,
`handlers_referrals.go`, `internal/jobs/cron.go`, `internal/pdf/`, or
`frontend/js/modules/billing.js`. Money and data integrity -- small steps, verify before
you commit.

## Money representation

Every money column is `NUMERIC(12,2)` (migrations `0002`, `0025`). Go and JSON carry them
as `float64`. **Never add a money column as `DOUBLE PRECISION`** -- `0025` exists precisely
because "floats accumulate rounding error in billing math" (`0025_money_numeric.sql:9`).

Rounding is round-half-away to 2dp via `round2` = `math.Round(v*100)/100`
(`models/models.go:76-79`), applied per line item and to the total. Do not introduce
`toFixed` or banker's rounding.

`core.ValidAmount(a)` is strictly `a > 0` (`core/respond.go:82`), enforced at invoice create
and update. A fully-discounted invoice cannot be stored as RM 0.

**Gateways take sen, the DB takes ringgit.** Both Billplz and Stripe receive `amount * 100`
formatted as an integer string; Stripe currency is hardcoded `myr`
(`payments.go:144, 290-291`). Confusing the two is a 100x billing error.

## Price resolution -- the precedence order

This is the single most valuable thing in this file. Reading only `handlers_pricing.go`
would lead you to believe the 2x2 matrix is the sole price source. It is the last resort.

```
student.package_amount  > 0  ->  use it, ignore per-class pricing entirely
                              |
                              +-- otherwise, per enrolled class:
                                    classes.monthly_fee_override  (0 = UNSET, not free)
                                    -> pricing_tiers[class_type][level_band].monthly_fee
                                    -> 0  -> line SKIPPED with a warning, never billed at 0
```

Implemented as `COALESCE(NULLIF(c.monthly_fee_override,0), pt.monthly_fee, 0)`
(`cron.go:345`) with the package short-circuit at `cron.go:476-478` and the skip at
`cron.go:487-493`.

`monthly_fee_override = 0` means **not set**, not free: "a genuinely free class is not a
thing the centre sells, and treating 0 as unset avoids threading a nullable through every
scan" (`0037_class_fee_override.sql:11-14`). Treating 0 as a real price recreates the RM 0
invoice bug that migration was written to fix.

**Session pricing (F8, migration `0045`, not yet consumed by the cron).** The parallel
per-SESSION price source for the 8.7 switchover is `store.SessionRateFor(db, classID,
studentBand)` (`store/rates.go`):

```
classes.session_rate            > 0  ->  use it (one session, outright)
  -> pricing_tiers[class_type][band].hourly_rate x class duration (time..end_time)
     where band = students.level_band, falling back to classes.level_band
  -> any hole (no band, no tier, rate 0, bad times)  ->  ERROR, never RM 0
```

The student's own `level_band` exists for mixed classes straddling the 1-3 / 4-6 boundary
(the L4 student in an L3&4 class pays RM65/hr). `hourly_rate` was backfilled as
`monthly_fee / 4` (four weekly 1-hour sessions), reproducing the centre's quoted
60/65/120/130. Unlike the monthly path's skip-with-warning, the resolver RETURNS an error --
the future cron decides whether to skip the line or flag the invoice, but it can never
silently price at 0. Locked by `TestSessionRateFor`.

**Dry run:** `GET /api/admin/billing/session-preview?month=YYYY-MM`
(`jobs/session_preview.go`) computes session totals (expander x resolver) beside the live
monthly fees per student, tenant-scoped, read-only. F5 rules live here: zero-billable line
skipped, nothing-to-bill student skipped (no invoice -> no referral credit consumed),
pricing hole flags the line. Locked by `TestSessionBillingPreview`. The switchover must
reuse this compute path, not reimplement it.

## Discount stacking order

Fixed, and the order is load-bearing for the clawback (`cron.go:537-545`):

```
full       = base - referralCredit - siblingDiscount     (floored at 0)
discounted = full - EarlyBirdRM                          (floored at 0)
earlyBirdApplied = full - discounted                     (the exact RM removed)
```

Early bird is applied **last** and stored as the exact difference, which is what lets
`applyEarlyBirdExpiry` restore full price precisely. Applying it first, or as a percentage,
breaks the arithmetic.

All three discounts are flat RM10 constants in `cron.go:19-38`: `EarlyBirdRM`,
`ReferralMonthlyRM`, `SiblingMonthlyRM`, and `SelfStudyOverflowRatePerHour`.

**Referral credit is per enrolled child, not per family** -- deliberately. A family with N
billable children burns N credits a month, and in the final month some siblings miss out.
`cron.go:23-29` marks this "intended per-child behaviour, not one-credit-per-family ...
accepted". Do not "fix" it.

### Early bird is a mutation, not a display rule

Invoices are issued at the discounted amount with `early_bird_cutoff` = the due date (the
7th). The hourly `applyEarlyBirdExpiry` job then adds the discount back to `amount`, flips
status to `Overdue`, removes the "Early bird discount" line item, and clears the fields --
making it self-terminating per invoice (`jobs.go:161-168, 212-218`). It targets only
`status IN ('Unpaid','Overdue')`; Paid and Pending Verification are exempt.

"Mutating amount keeps it the single source of truth for every payment path." Modelling
early bird as a render-time discount desynchronises every payment path.

A manual admin run after the 7th (`isLateRun`) bills full price, due in 7 days, with early
bird suppressed everywhere -- so a catch-up run never issues born-overdue invoices carrying
an already-expired discount (`cron.go:447-455`).

## The total is server-owned

When `lineItems` are posted, the server recomputes every item's `Amount` (`Qty * UnitPrice`,
discounts clamped `<= 0`, each rounded to 2dp) and **ignores the client's `amount`**
(`models.go:81-95`, `handlers_invoices.go:117-121`). `NormalizeLineItems` is the authority.

On update, `period` is recomputed rather than carried (the endpoint can change both type and
`created_on`, and a stale period would hide the invoice from the monthly dedup), and
`line_items` is cleared **only when the amount actually changed**, via a SQL `CASE`
comparing old and new at 2dp (`handlers_invoices.go:229-237`).

## Idempotency

- **Monthly invoices**: partial unique index on `(tenant_id, student_id, period)` `WHERE
  type='Monthly' AND deleted_at IS NULL AND period <> ''`
  (`0039_invoice_period_unique.sql:22-24`). Non-Monthly, soft-deleted, and blank-period rows
  are exempt. The cron adds `ON CONFLICT DO NOTHING`; a zero-row result means a concurrent
  run won, so it skips **both** the count and the email -- otherwise a parent is told about
  an invoice this run did not create (`cron.go:572-594`).
- **Dedup preloads fail closed.** A query error aborts the whole run rather than proceeding
  with an empty set, which would re-bill every active student (`cron.go:330-336`).
- Dedup keys on `period`, not `created_on`: "an invoice raised in September for August is an
  August invoice" (`cron.go:649-655`).
- **`period` is set only for `type='Monthly'`** (`createdOn[:7]`); everything else gets `''`.
  Stamping a period on a registration fee would block that student's tuition invoice
  (`handlers_invoices.go:196-201`).
- **Re-paying an already-Paid invoice is a no-op** -- the UPDATE carries `AND status<>'Paid'`,
  and both the referral milestone and the confirmation email are gated on `rowsChanged > 0`
  (`handlers_invoices.go:441-443, 485`).
- **Receipt numbers** (`RCPT-000001` from `receipt_no_seq`) are minted only on the first
  transition to Paid, guarded by `status='Paid' AND (receipt_no IS NULL OR receipt_no='')`.
  The same guard is duplicated in both webhook handlers (`handlers_invoices.go:468-470`,
  `payments.go:222, 388`).

### Stale comment warning

`0024_payroll_unique_month.sql:8-10` states a unique index was "Deliberately NOT added to
invoices". Migration `0039` later adds exactly that index. **`0039` is current.** Reading
only `0024` leads to the wrong conclusion.

## Statuses

`Unpaid`, `Paid`, `Pending Verification`, `Pending`, `Overdue`. Creation always forces
`Unpaid` (`handlers_invoices.go:152`). Parents may transition **only** to
`Pending Verification` -- never to Paid, even with an empty body
(`handlers_invoices.go:396-415`).

A non-cash payment (any method except `Cash` or empty) requires a reference number, validated
against the **effective post-update state** -- body value falling back to the stored value.
This closes the bypass where an admin marked Paid with an empty body on an invoice that
already had `method="Bank Transfer", ref=""` (`handlers_invoices.go:418-434`).

Bulk delete only removes `Unpaid` and `Overdue`. Paid rows are financial records and
Pending Verification rows carry parent-submitted proof; both are silently kept even when
their ids are in the request, and the response reports deleted vs skipped
(`handlers_invoices.go:293-321`).

The "payment received" email fires **only** when confirming a payment the parent themselves
submitted (`submitted_by_parent`, set exclusively by the parent pay path). Admin cash entry
and bulk mark-paid stay silent so reconciliation does not blast every parent
(`handlers_invoices.go:452-455, 489-505`).

## Payment webhooks

Both **fail closed** when the signing secret is unset -- 503, never skip verification.
"Skipping verification would turn this public route into an unauthenticated 'mark any
invoice paid' endpoint" (`payments.go:184-191, 331-336`). If you are debugging webhook
failures, do not make verification optional.

Webhook updates run with **no tenant scope** (there are no Claims -- it is a system action)
and are idempotent via `AND status<>'Paid'`. `reference_no` becomes the gateway transaction
id, which is what satisfies the non-cash reference rule (`payments.go:39-41, 207-210`).

Stripe signatures outside a 5-minute tolerance are rejected as replays, checked in both
directions of clock skew (`payments.go:46, 429`).

## Referrals

State machine `pending -> earned -> exhausted`. The milestone is exactly **3 Paid Monthly
invoices** for the referred student, which sets `status='earned', credits_remaining=3`
(`store/referrals.go:21-32`; the same milestone logic is duplicated in
`handlers_referrals.go:102-105`). Counting Adhoc or unpaid invoices breaks the reward contract.

Referral hook errors are swallowed **by design**: "referral logic must never break payment
processing" (`store/referrals.go:9`). Propagating a referral failure would abort a real payment.

Consumption is a single atomic UPDATE guarded by `status='earned' AND credits_remaining > 0`,
flipping to `exhausted` at zero, explicitly to prevent the TOCTOU double-spend where two
concurrent requests both read `remaining=1` (`handlers_referrals.go:129-135`). A
read-then-write rewrite reintroduces it.

Manual invoice creation zeroes a claimed `referralCredit` only when the family verifiably has
no earned rewards -- **not** on a transient DB error, which would silently strip legitimate
credits during a blip (`handlers_invoices.go:155-171`).

## PDFs

All printed text must pass through the cp1252 `UnicodeTranslator` or em-dashes and accented
names print as mojibake -- but `LogoPath` must NOT be translated because it is a filesystem
path (`pdf_invoices.go:120-125`). Adding a new printed field without translating it corrupts
the PDF.

Legacy invoices with no `line_items` have their gross reverse-engineered:
`gross = amount / (1 - discountPct/100) + siblingDiscount + referralCredit`, and any
unexplained positive leftover is labelled "Early bird discount" only when `discountPct == 0`
(`pdf_invoices.go:227-244`). Changing how discounts are stored breaks this reconstruction.

## Frontend notes

- Cash confirmation requires the typed amount to match within 0.005
  (`billing.js:701`).
- A manual sibling invoice is **one** invoice attached to `children[0].id`, with the rest
  stored as a JSON array in `siblingIds` -- not one invoice per child (`billing.js:1587-1589`).
  Generating per-child invoices here would double-bill.
- The edit modal appends the invoice's existing type to the Monthly/Adhoc picker, because
  reclassifying a system type (`Self-study`, `Self-study Overflow`) to Monthly "would corrupt
  billing reports + the overflow dedup" (`billing.js:1112-1117`).
- `_checkReferralMilestoneClient` is an explicitly redundant safety net for the server-side
  hook, not the primary mechanism (`billing.js:738-743`). Do not delete either half on the
  assumption the other covers it.

## Open conflicts (verified, unresolved in code)

**Sibling discount has two different shapes.** The cron applies a flat RM10 per child
(`cron.go:31-33, 518-519`); the frontend's Sibling invoice tab applies a **percentage** per
child, `perChild * (1 - discount/100)` (`billing.js:1550`). Unifying "the" sibling discount
changes one of them. Confirm which is intended before touching either.

**Self-study allowance -- FIXED 2026-08-26.** The frontend self-study tab now reads each
student's `packageSelfStudyHours` (default 4), matching the cron's per-student
`COALESCE(s.package_self_study_hours,4)` (`cron.go:360-363`).

Manual self-study billing is restricted to drop-in students because package students are
auto-billed for overflow by the cron -- mixing them double-charges (`billing.js:1169-1172`).
