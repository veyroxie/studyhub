# Invoice engine rebuild: plan

Written 2026-09-10 against the 12-question research brief, with every claim
about our own code re-verified rather than taken from the report.

## What the research got right about us, and what it did not

Correct, and already the case -- do not redo these:

- **Idempotency (Q7) is already enforced by the database.** Migration `0039`
  puts a partial UNIQUE index on `(tenant_id, student_id, period)` for
  `type='Monthly' AND deleted_at IS NULL AND period <> ''`. The report's main
  Q7 recommendation is already shipped, for the reason it gives.
- **Per-student transactions (Q7)** shipped in `8fe1d67`.
- **Session-count proration on half-open `[start, end)` windows (Q6)** is what
  `EnrollmentWindowsIn` already does.

Wrong or incomplete about us, found by checking:

- **We have float money in the SCHEMA, not just in Go.** `discount_pct REAL`,
  `sibling_discount REAL`, `referral_credit DOUBLE PRECISION`. `amount` is
  NUMERIC(12,2), so the total is exact while its own components are binary
  floats. This is worse than the report assumed and it is cheap to fix.
- **`pricing_plans` has no effective dating at all** (`0051`). `CatalogPrices`
  takes an `asOf`, but it only dates the ENROLMENT windows -- the price rows
  themselves have no history, so re-rating a past month silently uses today's
  prices. Q5 lands squarely.
- **There is no draft state.** Production holds only Paid, Unpaid and Overdue.
  The immutability boundary of Q1 does not exist to be tightened; it has to be
  introduced.
- **Discounts have two competing representations**: dedicated columns on
  `invoices` AND `line_items` entries. Q4's model has to replace both, not sit
  beside them.

## Sequencing

Each stage is independently shippable and independently verifiable. Nothing in
a later stage is needed to make an earlier one correct. Stage 5 is the only one
that changes what a parent receives.

### Stage 0 -- decisions, no code

Blocking, and not mine to make:

1. **SST and e-Invoice scope**, confirmed against RMCD and LHDN directly, not
   against the report. Two specific questions: does the centre's revenue sit
   under RM3m with no corporate parent at or above it, and is a music/enrichment
   model assessed under education (Group M) or coaching (Group G)?
2. **Rounding**: banker's or half-up. Either is defensible; it must be written
   down and pinned by a test.
3. **Numbering**: gapless or sequence-with-gaps. At single-operator concurrency
   gapless is nearly free, so the question is whether we want it at all.
4. **Which corrections we support**: void-and-reissue, credit note, debit note.

### Stage 1 -- foundations, no behaviour change

- **1a. Money types.** Migrate `discount_pct`, `sibling_discount`,
  `referral_credit` and any other REAL/DOUBLE money column to NUMERIC(12,2).
  On the Go side, decide between `int64` sen and `shopspring/decimal` and apply
  it in the rating path only. `round2` on float64 stays wrong until this lands.
- **1b. Effective-dated catalogue.** Add `effective_from` / `effective_to` to
  `pricing_plans`, backfill existing rows as open-ended from their creation,
  and add a PostgreSQL exclusion constraint so two versions of the same plan
  cannot overlap. Price rows become insert-only.

Verified by: existing tests still green, plus a new test that re-rating a past
month after a price change returns the OLD price.

### Stage 2 -- one rating engine

- **2a. Extract `internal/rating`.** A pure function, no I/O, no transactions,
  no invoice numbers: `Rate(enrolment, period, catalogue) -> ([]LineItem, error)`.
  It returns priced line items, NOT an invoice. Both the monthly run and the
  manual builder call it.
- **2b. Typed discounts.** `discount_type` (kind, exclusivity, best-of group)
  and `applied_discount` (target line, type, source reference, sequence,
  amount, state). This replaces the discount columns AND my early-bird line-name
  tag. The early bird becomes a conditional discount with
  pending/earned/forfeited state, which is what it always was.
- **2c. Delete the frontend price list.** `_packageCatalog()` in `billing.js`
  reads the superseded `pricingTiers` table and cannot express Level 0,
  Mandarin, Phonics or twice-weekly. It goes; the builder asks the engine.

Verified by: the shadow diff of Stage 5a, run early and often from here on.

### Stage 3 -- the invoice as a document

- **3a. Draft / issued boundary.** New `draft` status. Draft is freely editable
  and deletable and carries no number. Issue is an atomic transition that
  assigns the number, snapshots the priced lines, and freezes the row.
- **3b. Numbering at finalization only**, per the Stage 0 decision, so an
  abandoned draft burns no number.
- **3c. Corrections.** `void` as a terminal state plus `superseded_by` and
  `credit_note_of` references. Editing an issued invoice stops being possible;
  the existing paid-invoice freeze becomes the general rule rather than a
  special case.
- **3d. Status lifecycle.** draft, open, paid, partially_paid, void,
  uncollectible. **Overdue stops being stored** and is derived from
  `due_date < today AND status = open`. This also removes the current oddity
  where the early-bird job is the only thing that ever writes 'Overdue'.

### Stage 4 -- the workflow Nadine actually uses

- **4a. Build-and-review.** One screen: pick the month, the engine drafts every
  student's invoice, the list shows totals with unpriceable students called out,
  she edits or deletes drafts, then one "issue" action finalizes each in its own
  transaction.
- **4b. Outbox.** An `outbox` row written in the same transaction as the issue,
  with a relay for the side effects. Decouples "invoice issued" from "email
  sent", and is where MyInvois submission would attach if we ever opt in.

### Stage 5 -- cutover

- **5a. Shadow run.** The new engine rates every student for the current month
  and produces line items only. Diff against what was actually issued, for every
  student, including the hard cases: mid-month joins, the Level 3 hand
  discounts, siblings, Aria's mixed private-plus-group week, the boundary
  session. The differ already exists (`/api/billing/price-preview`).
- **5b. Written go/no-go, decided before looking at numbers.** Target: zero
  unexplained per-student diffs across one full cycle. Every remaining diff
  categorised as "old was wrong" or "new is wrong" and resolved.
- **5c. Tests.** Golden files for a handful of representative invoices; property
  tests for the invariants that matter here -- lines sum to the total, a
  discount never inverts a total, a full period rates to the full price,
  allocated sibling shares sum exactly to the household discount.
- **5d. Switch**, keeping a rollback path until the gate passes.

## Deliberately not doing

- **Bitemporal pricing.** Single-axis effective dating answers "what did March
  cost". Reconstructing "what did we believe in April that March cost" has no
  reader here.
- **Stripe-style revision machinery.** A void status and a supersede pointer
  cover a single operator's monthly cycle.
- **Building for MyInvois now.** Store the seam (TIN, MSIC, per-line
  classification, an SST amount defaulting to zero) so opting in is
  configuration rather than migration, but do not build submission.

## Open risk

Stage 3 changes what an admin can do to an invoice they have already sent, and
Stage 2b changes how every discount is stored. Both touch live money. Neither
ships without the Stage 5a diff coming back clean.
