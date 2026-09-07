# Prorated add-on classes

Status: PLAN. No code. Depends on the pricing switchover (step 3) landing first.

Nadine, 2026-09-08: "she already came for 4 classes and wants to add on, but I
don't know how to add her into a one time schedule". Confirmed as a PRORATED
ADDITIONAL CLASS, explicitly not a replacement and not credit-covered.

## 1. Why credits are the wrong mechanism

Nadine asked whether this uses class credits. It must not.

A credit means "you paid for a lesson you did not get". It is minted only by
absence -- the button is "Mark absent (+ credit)" (`students.js:595`) -- and
spending one writes a ledger row and never touches an invoice
(`students.js:1365`). So routing add-ons through credits would (a) require
falsifying an absence to mint the credit and (b) bill nothing.

An add-on is the opposite: a new purchase. The two ledgers stay separate.

```
  credit  = compensation, earned by absence, spending bills 0
  add-on  = purchase beyond the subscription, bills prorated
```

## 2. The prorate rule, and why the awkward number is right

Ely 09-08: apply the rule automatically rather than typing a price each time.

Ying Quah 02/09 already fixes the divisor without anyone guessing: "if they
come for 5 lessons, we'll consider that as add on lesson". That means the
subscription BUYS four sessions per week-slot per month, and the fifth is
extra. So:

```
  included sessions per month = sessions_per_week x 4
  add-on unit price           = monthly_fee / (sessions_per_week x 4)
```

| Plan | Monthly | Included | Add-on |
| --- | --- | --- | --- |
| Group Level 1-2, 1x | 240 | 4 | 60.00 |
| Group Level 1-2, 2x | 450 | 8 | 56.25 |
| Group Level 3-4, 1x | 260 | 4 | 65.00 |
| Private Level 3-4, 1x | 520 | 4 | 130.00 |
| Private Level 3-4, 2x | 1010 | 8 | 126.25 |

This closes the question left open in section 10 of `pricing-bands.md`. The
56.25 looked wrong there; it is right. A twice-weekly student already gets a
bulk discount, so their marginal session is cheaper than a once-weekly
student's. Deriving it from the plan means there is no second set of numbers to
maintain, and it moves automatically when Nadine edits a price.

**Overridable per booking.** The derived figure fills the field; Nadine can
change it for a trial or a goodwill session. Derived default, stored result.

## 3. Shape

```
  class_addons
    id, tenant_id, student_id, class_id, date,
    unit_price  NUMERIC(12,2) NOT NULL CHECK (unit_price > 0),
    invoice_id  TEXT,          -- NULL until billed
    created_by, created_on, deleted_at
```

Three decisions carry the design:

**Store the resolved price, do not re-derive at invoice time.** If Nadine edits
the Group Level 1-2 price in October, September's add-on must keep the price it
was sold at. Same rule `0047` set for schedules and section 5 of the pricing
plan set for rates: a change applies forward, never backwards.

**`invoice_id` IS the consumption marker. Not a boolean.** A `billed BOOLEAN`
can be set true when no invoice was created -- the mechanism ran, the work did
not, which is the failure shape behind six bugs this month. A column that holds
the invoice's id can only be filled if an invoice exists. The monthly cron
inserts with `ON CONFLICT DO NOTHING` and already skips its count and its email
when zero rows come back (`cron.go:572-594`); stamping `invoice_id` belongs in
that same guarded path, in the same transaction.

**Do not store a count.** "How many add-ons this month" is
`COUNT(*) WHERE student_id=? AND date LIKE 'YYYY-MM'`. A stored counter is a
second source of truth that drifts -- the same reasoning `0051` used for not
storing sessions-per-week.

## 4. Where it appears

**Roster.** `App.Utils.rosterFor` (added 09-08) already unions enrolled
students with anyone holding an attendance record. An add-on is a third
source: enrolled, or has a record, or has an add-on for that class and date.
The student then appears for check-in on that date only.

**Invoice.** Add-ons for September are billed on the October invoice, as their
own line items beneath the subscription. Consumed in arrears while the
subscription is charged in advance, on one invoice. Ying Quah 02/09 asked for
one line per thing with the description carrying the meaning, so:

```
  Group Class Level 1-2 (Twice a week)      RM 450.00
  Additional class - 12 Sep                 RM  56.25
```

**Capacity.** Booking checks `classes.capacity` against the roster for that
date, and refuses rather than silently over-filling.

## 5. Open questions

For Nadine:

1. If an add-on is booked and the student does not come, is it still charged?
   Ely put three options to her 09-08: charge it, charge it and grant a class
   credit, or do not charge. Awaiting her answer.

ANSWERED, Ely 09-08: **early bird applies to the monthly subscription only,
not to add-on lines.** So the add-on total is added AFTER the stacking chain in
`AI_DOCS/billing.md` rather than inside it -- early bird is computed on the
subscription base and stored as the exact difference, and add-on lines sit
outside that arithmetic. Putting them inside would let a bigger add-on silently
enlarge the discount `applyEarlyBirdExpiry` later has to claw back.

For Ely:

2. A student with no live enrolment in that category has no plan, so no
   derived price. Refuse the booking, or fall back to the class's own rate?
3. Add-ons for a student on `package_amount` (the flat subscription) have no
   plan to divide either. Seven students are on packages today.

## 6. Sequencing

Add-ons resolve their price from `pricing_plans`, so this lands AFTER the
switchover (step 3 of `pricing-bands.md`). Building it first would mean
deriving prices from `pricing_tiers`, which prices exactly one class in
production today.
