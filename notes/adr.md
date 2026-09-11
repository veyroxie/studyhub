# Architecture decision record

One file, newest last. Each entry states what was decided, why, and what it
costs. A decision that turns out wrong gets a new entry superseding it rather
than an edit -- the reasoning at the time is the point.

Status: `Accepted` = agreed and either built or queued. `Proposed` = written
down, not yet agreed. `Interim` = deliberately temporary, with the trigger for
revisiting named.

---

## ADR-001 -- Editing lives in context; creating vocabulary lives in setup
**2026-09-08 · Accepted**

**Context.** Six surfaces were found offering more than one route to the same
outcome (`ux-overlapping-surfaces.md`). The proposal on the table was to move
all editing to a single admin page.

**Decision.** Split by what the thing is, not by where it appears.

- **Creating vocabulary** -- subjects, pricing categories and tiers,
  classrooms, packages, teachers -- lives on one setup page. Rare, deliberate,
  needs the whole picture.
- **Editing an instance** -- this class, this student, this enrolment, this
  session -- stays wherever the thing is shown. Frequent, contextual.
- **One component per concept, reused.** The enrolment picker is written once
  and appears on both the student modal and the class modal.

**Why not one editing page.** The problem was never the number of doorways; it
was that the same edit behaved differently depending on which one you used.
Four enrolment surfaces would be harmless if all four behaved identically. A
single setup page also makes the common case worse: spotting a wrong classroom
while looking at Thursday's schedule would mean leaving, finding the class in a
list, fixing it and coming back.

**Consequences.** Every screen that shows a thing may offer to edit it, so
consistency has to be enforced by sharing components rather than by convention.
Adding a surface is cheap; adding a *variant* of a surface is the thing to
refuse.

---

## ADR-002 -- Billing state collapses to one concept
**2026-09-08 · Accepted**

**Context.** `students.subscription_status` holds `active | paused | frozen`,
but `pause` and `freeze` are behaviourally identical -- `handlers_students.go`
says so outright. Two UIs drive the same field with three vocabularies:
"Auto-bill On/Off" on the list (no confirm) and "Pause / Freeze / Resume" in
the profile (with confirm). Separately, `status` is a different field, so a
frozen student displays a green **Active** badge next to a blue **Frozen** chip.

**Decision.** Delete `pause`. Keep one off-state. Show billing in the badge
itself -- `Active`, `Active (not billed)`, `Inactive` -- rather than as a
second chip beside a contradictory one. One vocabulary across both surfaces.

**Why not make pause and freeze differ.** That is the better long-term model
and it needs the dated `student_billing_periods` table in
`student-lifecycle-dates.md`. It also needs Nadine to say what she means by
each, and there is no evidence she wants two concepts -- only that the UI
offered two words.

**Consequences.** `paused` rows in production must migrate to `frozen`. The
dated-periods model stays on the table as a later ADR; this decision does not
block it.

---

## ADR-003 -- Subject is a label, never a pricing key
**2026-09-08 · Accepted**

**Context.** Skooly priced by subject x class type x level. The question was
whether to mirror that and tie pricing tiers to subjects.

**Decision.** No. `classes.subject` stays a display label for the invoice line
and reporting, picked from a managed list. Pricing keys off the catalogue's
free-form **category**, which already carries the subject where it matters:
`Group`, `Private`, `Mandarin`, `Phonics`.

**Why.** A subject x type x level grid is the shape that failed: Mandarin has
no level, Phonics is group-only, English is private-only, Self-Study has
neither. A grid forces a cell for every combination and most come back blank,
which is how 34 of 37 classes became unpriceable. A category owns whatever
tiers make sense -- six for Math, one for Mandarin, none for Self-Study -- so
no combination is forced to exist. This also preserves migration 0034's
decision rather than quietly reversing it.

**Consequences.** Subject is blank on 32 of 37 classes today and needs
backfilling before it is useful for reporting. Two categories that differ only
by subject (`Mandarin` vs `Group`) will look redundant until someone reads why.

---

## ADR-004 -- The pricing catalogue screen blocks the switchover
**2026-09-08 · Accepted**

**Context.** Migrations 0051 and 0054 created `pricing_categories` and
`pricing_plans` with the real prices. There is no API route and no UI for
either. The only pricing screen edits `pricing_tiers`, the table being
replaced, and it lives on the Schedule page.

**Decision.** Build the full editor -- add a category, name its tiers, set
prices -- **before** the billing switchover, not as the follow-up commit
section 5 of `pricing-bands.md` originally planned.

**Why.** On the day billing switches over, the old screen edits a table nothing
reads and the new catalogue has no screen at all. Nadine would be unable to
change any price without a developer, which is the exact outcome the rework
exists to end.

**Consequences.** The switchover's critical path grows by a screen. Level 0 and
Mandarin tiers cannot be created until it exists, even though their prices are
known (RM320 and RM240).

---

## ADR-005 -- A one-off class replaces the add-on feature
**2026-09-08 · Accepted**

**Context.** `addon-classes.md` specified a separate add-on object with a
prorated price derived from the catalogue. Separately, classes needed a
recurring-vs-one-off distinction.

**Decision.** One "add class" flow that forks on its first question. Recurring
classes price from the catalogue. **One-off classes carry a rate typed at
creation, with no derived default**, and land on the following month's invoice
as their own line. An add-on *is* a one-off class with a student and a price.

**Why no default.** Ely's call. The derived figure -- `monthly_fee /
(sessions_per_week x 4)` -- is confirmed twice over (Ying Quah's "fifth lesson"
rule, and every one of Skooly's seven prorated rates) and stays recorded in
`addon-classes.md` in case it is wanted later as a suggestion. But a typed rate
cannot be silently wrong, and one-offs are rare enough that typing is cheap.

**Consequences.** One fewer concept for Nadine to learn. The invoice line still
needs the consumption marker from `addon-classes.md`: `invoice_id` on the
charge, never a `billed` boolean, so a charge cannot be marked billed when no
invoice was created.

---

## ADR-006 -- Free-text fields that name things become managed lists
**2026-09-08 · Accepted**

**Context.** Audit of production, 2026-09-08:

| Field | Reality |
| --- | --- |
| `classes.classroom` | four rooms stored as six spellings: `Classroom 1` x12, `classroom 1` x1, `Classroom 2` x14, `2` x2, `Classroom 3` x12, `Classroom 4` x1, blank x1 |
| `classes.subject` | blank on 32 of 37 |
| `students.package_amount` | eight loose numbers that are plainly products |

**Decision.** Classroom, subject and package become managed lists created on
the setup page. Existing values are normalised by migration.

**Why classroom is urgent and the others are not.** Clash detection compares
classroom strings, so a class in `2` and a class in `Classroom 2` at the same
hour **do not collide** -- the double-booking guard misses them. That is a live
correctness bug, not tidiness. Subject and package are unused and untidy
respectively.

**Consequences.** A migration must map the six spellings onto four rooms, and
that mapping is a judgement call (`2` almost certainly means `Classroom 2`, but
"almost certainly" is doing work). Package becoming a product changes how
`package_amount` is read at billing time; that is a later ADR.

---

## ADR-007 -- Level 3 is priced at the Level 4 rate and discounted by hand
**2026-09-08 · Interim**

**Context.** Nadine's per-level list (07-02) prices Level 3 at the Level 1-3
rate: Group RM240, Private RM480. The three-band model puts Level 3 into
`Level 3-4`, priced at RM260 / RM520. Gareth is Level 3 and private, so the
catalogue will price him RM520 where her own list says RM480.

**Decision.** Leave the catalogue as seeded. Level 3 students are billed at the
Level 4 rate and discounted by hand until the three-tier pricing is confirmed.

**Why interim.** Ely raised three tiers with Ying Quah and the pricing is not
settled. Encoding a Level 3 discount now would bake in a rule nobody has
agreed, and the plan already anticipated hand-discounting for exactly this.

**Revisit when** the three-tier pricing is confirmed. The question to settle
then: if Level 3 always costs less than Level 4, `Level 3-4` is two prices
wearing one name, which is the same flaw the three-band model was meant to fix.

---

## ADR-008 -- An enrolment start date is asked for, never assumed
**2026-09-08 · Accepted, built**

**Context.** `SyncEnrollments` stamped `today` on every insert, so an enrolment
recorded when the row was created rather than when the student joined. On 30-31
August a bulk enrolment recorded 35 join dates as that day, hiding 85 real
attendance records behind windows that had not opened yet.

**Decision.** The enrol flow asks "Starting from", defaulting to today. A
malformed date is refused on both create and update rather than falling back to
today. Migration 0055 repaired the existing rows from attendance evidence.
Removals still end today, since the field is labelled a start date.

**Why refuse rather than default.** Silently defaulting is what produced the
original bug. `started_on` is TEXT compared lexically, so an unvalidated
`05/08/2026` would store happily and sort wrong for the life of the row.

**Consequences.** The date currently exists on only one of the four enrolment
surfaces. Until it is on all of them, ADR-001's "one component" rule is
violated and the same bug can recur through Add or Edit Student.

---

## ADR-009 -- The attendance roster trusts evidence over the enrolment window
**2026-09-08 · Accepted, built**

**Context.** See ADR-008. A wrong enrolment date made existing attendance rows
invisible with no error.

**Decision.** `App.Utils.rosterFor` draws everyone enrolled on that date **plus
anyone already holding an attendance record for it**. The window decides who
should be there; evidence decides who was.

**Consequences.** The guard reaches only as far as the snapshot carries
attendance, which is 90 days. It is defence in depth, not the fix -- correct
`started_on` values are what make older dates render.

---

## ADR-010 -- A class's pricing category is derived on write; its tier never is
**2026-09-08 · Accepted, built**

**Context.** Migration 0053 categorised every class that existed when it ran.
The create path never wrote the column, so three classes created in the
following two days had none, two of them with live students, each silently
unpriceable.

**Decision.** `resolvePricingCategory` applies 0053's rule on every create and
every edit. Category is **derived** when omitted; tier is **never** derived --
a blank tier stays blank and is surfaced in the "needs a tier" list.

**Why the asymmetry.** Category is structural and the class already answers it:
group, one-to-one, or self-study. A tier is the priced thing, and guessing it
means inventing a bill.

**Consequences.** A tenant with no catalogue at all passes through with a blank
category rather than being blocked, because `store.TenantID` returns 0 for a
superadmin and the catalogue is seeded at tenant 1.

---

## ADR-011 -- Attending and being invoiced are two separate facts
**2026-09-08 · Accepted · supersedes ADR-002**

**Context.** ADR-002 read three visible states (`active | paused | frozen`)
against two behaviours and concluded the model should collapse to one concept.
That was wrong, and Nadine said why on 09-08:

> "I did freeze all of their auto payments I think"
> "yes cause I was trying to learn how to use it hahaha"

She was not freezing students. She turned auto-billing off across the roster
while she learned the system. All 21 were attending normally throughout -- 19
of them have attendance since 1 September.

**Decision.** Two independent axes, two values each.

```
  attendance_state   active | on a break     does the student come?
  billing_mode       auto   | manual         how is the invoice made?
```

The cron bills when `billing_mode = auto` AND the student is not on a break. A
break implies no invoice regardless of mode; manual billing says nothing about
whether the child attends.

`pause` is still deleted. That half of ADR-002 stands: `pause` and `freeze` were
the same thing on the attendance axis, and one word is enough there.

**Why ADR-002 was wrong.** It counted the states and inferred the model. The
real fault was not three words for one state -- it was **one control doing two
jobs**. Nadine reached for the attendance axis because it was the only switch
that stopped invoices. Collapsing to one concept would have removed the symptom
and kept the cause, leaving her with no way to say "keep billing this one by
hand" except by pretending the student had stopped coming.

**Consequences.** The migration changes shape entirely. The 21 are not frozen
students to be unfrozen; they are `attending + manual billing`, which is what
she meant. Moving them to auto is then a decision she makes when she trusts the
pricing, per student or in bulk, rather than a correction of a wrong state.

It also means `on a break` currently has **no users at all**, so a screen must
not be designed around the assumption that it is common.

**Note on method.** ADR-002 was written from a correct count and an assumed
cause -- the same mistake as the September invoice scope in
`pricing-bands.md` section 12a, made twice in one day. Counting a thing and
explaining it are separate steps, and the second one needs its own evidence.

---

## ADR-012 -- One switch, named for what it does
**2026-09-08 · Accepted · supersedes ADR-011 and ADR-002**

**Context.** Nadine, 09-08, asked for freezing and auto-billing NOT to be
separated: "if they don't come they don't have to pay."

That reasoning is sound but does not describe what she did. She switched off 21
students who WERE attending, because she wanted to invoice by hand while
learning the system. Her one-sentence model does not cover her own use case,
which is why ADR-011 read it as two axes.

**Decision.** One field, one control, named for the only thing it has ever
done: **does the monthly run raise this student's invoice.**

Both cases fit without a second axis:

- The student is on a break, so nobody wants an invoice -> off.
- The admin wants to raise it by hand -> off.

`pause` is deleted; it was a third word for the second of two behaviours. The
API still accepts it and maps it to the same state, so a stale client cannot
break, and no production row carries it.

**Why ADR-011 was wrong.** It treated a naming problem as a schema problem.
The field never touched attendance -- freezing has never removed anyone from a
roster, and the handler comment claiming it did was simply false. Attendance is
decided by the enrolment's start and end dates, which is what Ely used for
Zhang Zhan He. Once the switch is labelled honestly, one control covers both
cases and the second axis buys nothing.

**Consequences.** The Active badge now carries a "Not billed" chip beside it
rather than a "Frozen" chip elsewhere in the row: both facts are true and they
belong together, since it was reading them apart that let 21 attending students
sit switched off. The profile says "Monthly invoicing ON/OFF" with one button,
and the confirmation states plainly that the student stays in every class,
roster and report.

**Note on method.** This is the third position on the same question in one day:
collapse (002), split (011), collapse-with-honest-naming (012). Each turn came
from new evidence rather than a change of mind -- 002 from counting states, 011
from Nadine's actual usage, 012 from her stating the intent. The lesson is not
to decide slower; it is that a model argued from the shape of the data, without
the operator's intent, will be wrong in a way the data cannot reveal.

---

## ADR-013 -- A hand-typed discount becomes a recorded discount
**2026-09-08 · Accepted**

**Context.** The first real differ run found Rui Xiang, Sukie Ren and Jiho Choi
each invoiced exactly RM10 below the catalogue, with every discount column at
zero: `early_bird_discount`, `sibling_discount`, `referral_credit` and
`discount_pct` alike. The reduction exists only as a smaller number typed into
the amount. Utaha differs by 30 -- the same RM10 plus the Level 3 band question
in ADR-007.

Left alone, the switchover RAISES those bills by RM10 and nothing in the data
explains why, to Nadine or to a parent who asks.

**Decision.** Record it. A standing per-student monthly discount, carried as an
amount and a reason, applied by the cron as its own invoice line.

**Why a line and not a lower price.** The tier price is what the centre charges
for that tier; the RM10 is what this family was given. Folding it into the tier
would reprice everyone on it, and folding it into a typed total is the state we
are leaving. A line says who got it and why, survives a price change, and shows
up on the invoice where a parent can see it.

**Why not the existing discount fields.** Early bird is a mutation with a
clawback (`applyEarlyBirdExpiry` restores the exact RM removed), sibling and
referral are derived from family and referral state. This is none of those --
it is a standing arrangement with one student, and reusing a field whose
semantics are already load-bearing is how the sibling discount ended up with
two different shapes.

**Consequences.** Every affected student needs the discount entered before the
switchover, or their bill rises. The differ then compares like with like,
because the computed side can subtract the same line.

**Open:** whether Nadine wants these as a fixed RM amount or a percentage. The
known cases are flat, so flat is the assumption until she says otherwise.

---

### CORRECTED 09-09 -- the premise was wrong for three of the five

Ely asked why any of this needed asking when the records were already to hand.
Checking them settles it, and settles it against me.

Nadine, 08-07: "the early bird discount, can change to RM10 instead of
discount?" -- and `cron.go:21` carries `EarlyBirdRM = 10.0`, applied to any
monthly invoice raised on or before the cutoff. Against her own per-level list
the September arithmetic is exact:

| Student | Her price | Invoiced | What the gap is |
| --- | --- | --- | --- |
| Rui Xiang | Level 2 = 240 | 230 | early bird |
| Sukie Ren | Level 2 = 240 | 230 | early bird |
| Jiho Choi | Group 3-4 2x = 490 | 480 | early bird |
| Utaha Luo | Level 3 = 240 | 230 | early bird, plus 20 of band gap |
| Gareth Lee | Level 3 private = 480 | 480 | band gap only, no early bird |

So **three of the five need nothing**. The cron already applies that RM10; they
only showed as differences because Nadine hand-made September's invoices and
typed the early bird into a smaller total, while the differ compares against
the catalogue's gross.

**What survives.** The standing discount is still the right mechanism, for a
smaller and different reason: ADR-007 has Level 3 billed at the Level 4 rate
and discounted by hand until the three-tier pricing is settled. That is Gareth
at 40 and Utaha at 20 -- two students, not five, and a hand-discount made
visible rather than typed into a total.

**What I got wrong.** I read "five invoices below the catalogue with every
discount column at zero" and concluded the discounts were invisible. Zero in
those columns was true; the inference was not. The differ compares gross to
net, and I did not check its arithmetic against Nadine's own price list before
writing the ADR -- a list I had already read. Same failure as the September
invoice scope and as ADR-002: a correct measurement, an assumed cause.

**Consequence for the switchover.** Two bills change at switchover, not five,
and both are the Level 3 question rather than anything new.

---

## ADR-014 -- Only three students should have invoicing off
**2026-09-08 · Accepted, not yet applied**

**Context.** 60 of 70 students have monthly invoicing switched off, from
Nadine's bulk action in August while she was learning the system (ADR-012).
Asked which should genuinely be off, she named three: **Zhang Zhan He, Stella
Kim and Joy Kim.**

**Decision.** Those three stay off. The rest go back to automatic invoicing.

**Not yet applied, deliberately.** Switching 57 students back on while the cron
still prices from `pricing_tiers` -- which can price exactly one class in the
whole estate -- would reproduce September: a run that bills the package
students and skips everyone else with a warning. The order recorded in
`pricing-bands.md` section 13 stands: fix the pricing, prove it with the
differ, then switch them on. Unfreezing is the LAST step of the switchover, not
a precondition for it.

**Consequences.** The 1 October run bills almost nobody unless the switchover
lands first. That is the deadline this work is actually against.

---

## ADR-015 -- Outbound mail stays restricted
**2026-09-09 · Accepted**

**Context.** `OUTBOUND_ALLOWLIST=etee3001@gmail.com` has been live in production
since before this work started. Every message to any other address is dropped
and marked `suppressed`. The original plan listed clearing it as a precondition
for the system opening to parents.

**Decision.** It stays. Ely, 09-09: no opening the mail.

**Consequences, and they are the point.**

- **No parent receives anything.** Not invoices, not payment confirmations, not
  announcements, not password links. The system runs internally only.
- **The billing switchover is therefore reversible in a way it would not
  otherwise be.** A wrong invoice raised by the cron is a row in a table that
  can be corrected or soft-deleted; a wrong invoice *emailed* is a conversation
  with a parent. Keeping the allowlist on through the switchover removes the
  one consequence that cannot be undone.
- **Parent accounts still work.** Fifty-one exist and the portal is unaffected;
  they simply are not notified. A parent who logs in sees their invoices.
- **`email_queue` keeps filling.** Twelve rows are already permanently failed.
  Suppressed messages are recorded rather than sent, so the queue is a log of
  what would have gone out, and worth reading before the allowlist is ever
  lifted.

**Revisit when** the centre actually wants parents contacted by the system.
Until then, treat any feature whose value depends on a parent receiving mail as
not yet delivering that value, and say so rather than reporting it as done.

## ADR-016 -- The four Stage 0 decisions for the invoice engine

Decided by Ely, 2026-09-11, before Stage 2 work began.

**e-Invoice and SST scope.** Revenue under RM3m with no corporate parent at or
above it, so the centre sits outside the LHDN MyInvois mandate (threshold raised
from RM1m to RM3m effective 1 September 2026) and outside SST on education
(RM60,000 per student per year, Malaysian citizens exempt). We build the SEAM
and not the submission: customer TIN, our MSIC code, a per-line tax
classification and an SST amount defaulting to zero, so opting in later is
configuration rather than a migration. Re-check before each academic year and
immediately if revenue approaches RM3m.

**Corrections: void-and-reissue only.** No credit notes, no debit notes. This
covers every case where money has not yet been received, which includes the one
that prompted the question: an early-bird invoice unpaid past the 7th is voided
and reissued at full price, so the parent sees a fresh correct invoice instead
of an amount that silently changed underneath them.

Consequence, accepted: correcting an invoice that has ALREADY been paid stays a
manual job. Voiding it would strand the payment and the parent's receipt against
a cancelled document. Credit notes are a small addition if that case turns out
to be common; we are not building a document type for a case that may not arise.

Consequence, structural: `idx_invoices_monthly_unique` is
`(tenant_id, student_id, period) WHERE type='Monthly' AND deleted_at IS NULL`.
A voided invoice still fills that slot, so the reissue would be refused by the
index that exists to prevent double-billing. Stage 3 must widen the predicate to
exclude voided rows, or a routine clawback returns a 409 nobody can explain.

**Numbering: gapless, resetting each year.** INV-2026-0001 upward with no
missing numbers. Gaplessness normally costs write throughput because it
serialises number assignment; at one operator issuing once a month that cost is
nil, and it is the safer answer against any future auditor. The number is
assigned at finalization only, so an abandoned draft burns nothing.

**Rounding: half-up.** RM0.125 becomes RM0.13. Chosen over banker's rounding
because Nadine checks figures by hand and the system must agree with her
calculator; the cumulative upward bias is immaterial at this volume. Round ONCE
per line after all stacked discounts are applied in their recorded order, then
sum the rounded lines to get the invoice total, so lines always add up to what
is charged.
