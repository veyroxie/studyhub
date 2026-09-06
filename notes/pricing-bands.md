# Pricing rework: level moves onto the enrolment, two bands become three

Status: PLAN. No code written. Audit complete; ready to implement.

Agreed with Ely 2026-09-06, from Nadine's requirements:

- A student has no single level. One child can be Level 1 Mandarin and Level 2
  Math, so level belongs to the ENROLMENT (student x class), not the student.
- Three bands now: `1-2`, `3-4`, `5-6`. Split the shape today even though every
  band prices identically at the Level 4 rate to begin with; Nadine adjusts the
  numbers herself later.
- Bands and rates editable from settings, which also stops living on the
  Schedule page.
- September is invoiced retroactively for students the current run skipped.

## Why this is happening now

Production, 2026-09-06 18:59 (`docker logs api`):

```
monthly billing: class has no priced tier — line skipped        (x4)
monthly billing: student skipped — no priced classes and no package amount
  student_id=STU_20260419163110116
```

That student is enrolled in four classes and invoiced nothing. A class prices
by joining `pricing_tiers` on `(class_type, level_band)` (`cron.go:345`); a
class whose pair has no matching tier resolves to a fee of 0, and 0 is treated
as "skip this line" (`cron.go:491`) rather than "stop and say so". A student
left with no priced lines and no package amount is skipped entirely
(`cron.go:506`).

Same failure shape as the announcements audience and the backup upload: a value
that matches nothing, answered with silence, healthy-looking from outside.

## 0. Audit — production, 2026-09-06

Answered. The queries that produced this are kept at the end of this section
so any claim below can be re-verified rather than trusted.

**37 classes. 34 of them cannot be priced.** Only three can: one Private class
with a band, and two Group classes carrying a `monthly_fee_override`.

```
  Group    26 ---- "Level 3 & 4"      8   band derivable FROM THE NAME
                   "Self-Study"      10   not tuition -- own category
                   "Level 1 & 2"      3   band derivable FROM THE NAME
                   "Mandarin"         2   subject, no level anywhere
                   Teacher Nadine     1   1-to-1, session_rate 30
                   (2 more)           2   priced by monthly_fee_override
  Private  11 ---- "Teacher X (Name)"10   1-to-1, negotiated; 1 has a rate
                   (1 more)           1   the only banded class in the system
```

Five findings that change the plan:

1. **The level is in the class NAME, not the band column.** "Level 1 & 2" and
   "Level 3 & 4" — 11 classes — can be banded automatically by the migration.
   No hand-mapping, and no question for Nadine: the names already match the
   new band vocabulary exactly.

2. **Self-Study is 10 of the 34 and is not tuition.** It has no level and
   should never price off a level matrix; it is what the subscription's
   self-study credits cover. It needs its own category, not a band.

3. **Private is 1-to-1, named `Teacher X (Student)`, individually
   negotiated.** Two carry a `session_rate` (80 and 30), the rest carry
   nothing. A level matrix is the wrong instrument for these — they need a
   per-class rate that is REQUIRED rather than optional.

4. **`students.level_band` is empty for all 66 students.** Removing the
   student-band fallback from `rates.go:45-48` is therefore a genuine no-op
   today, not a behaviour change. This de-risks the largest part of the work.

5. **No September invoice exists for `STU_20260419163110116`.** Retroactive
   billing is a clean insert. No void-and-reissue, no amended invoice for a
   parent to be confused by.

### What is actually billing today

The matrix prices almost nothing, yet only one student was skipped. So the
money is coming from `students.package_amount` — the flat subscription — and
the class matrix is close to vestigial. The one skipped student is the one who
has neither a package nor a priceable class.

That matters for scope: this rework is not repairing the main billing path. It
is making per-class pricing work so that add-ons and prorating (the confirmed
subscription model) have something correct to price against.

### Data hygiene found on the way

- `Self-Study` (8) and `Self-study` (2) differ only in case.
- `Teacher Chiying (Aria)` has `session_rate` 80; `Teacher Chiying Aria)` —
  missing the opening bracket — has 0. Almost certainly a mistyped duplicate
  of the same class. Needs a human decision, not a migration.

### The queries

Read-only; `make psql`.

```sql
-- Q1. Are the unpriceable classes missing a band, or carrying one the tier
-- table has never heard of? Backfill strategy hinges on this.
SELECT COALESCE(class_type,'(null)') AS class_type,
       COALESCE(NULLIF(level_band,''),'(empty)') AS level_band,
       COUNT(*)
  FROM classes
 WHERE deleted_at IS NULL
 GROUP BY 1,2
 ORDER BY 3 DESC;

-- Q2. Is students.level_band actually carrying anything today? It decides
-- whether removing the student-level fallback is a no-op or a behaviour change.
SELECT COALESCE(NULLIF(level_band,''),'(empty)') AS level_band, COUNT(*)
  FROM students
 WHERE deleted_at IS NULL
 GROUP BY 1
 ORDER BY 2 DESC;

-- Q3. Does a September invoice already exist for the skipped student? The
-- dedup guard will refuse to issue a second one, so "retroactive" needs to
-- know whether it is creating a row or amending one.
SELECT id, type, amount, status, created_on
  FROM invoices
 WHERE student_id = 'STU_20260419163110116'
   AND created_on LIKE '2026-09%'
   AND deleted_at IS NULL;
```

## 1. Current shape (verified, not remembered)

| Thing | Where | Note |
| --- | --- | --- |
| `pricing_tiers(class_type, level_band, monthly_fee, hourly_rate)` | `0016`, `0045` | `UNIQUE (tenant_id, class_type, level_band)`. Bands seeded `1-3` and `4-6` only. |
| `classes.level_band` | `0016:10` | `DEFAULT ''`. Editable per class (`handlers_classes.go:359`). |
| `classes.monthly_fee_override`, `classes.session_rate` | `0037`, `0045` | Per-class escape hatches. `0` = unset. |
| `students.level_band` | `0045` | The student's own band. |
| `enrollments(student_id, class_id, started_on, ended_on)` | `0044` | Already the join row. Has no level column. |
| Monthly pricing | `cron.go:345` | Class band only. Ignores the student band entirely. |
| Session pricing | `rates.go:18-67` | Student band, falling back to class band. |

**The two billing paths already disagree.** Monthly billing prices by the
class's band; session billing prefers the student's. That inconsistency is
invisible today because the two never run on the same invoice, and it is
exactly what this rework resolves — one band source for both.

## 2. Target shape

The audit rules out a fixed grid. Three of the four class families do not fit
one: Self-Study has no level, Private is negotiated per student, and Mandarin
is a subject with no level recorded anywhere. A wider grid would leave the same
holes in different places.

So the matrix becomes user-defined, which is also what Ely asked for on 09-06 —
Nadine adds a category, names its tiers, sets their prices, with no migration
and no developer.

```
  CATEGORY          TIERS (named, ordered, priced)
  ---------------   ---------------------------------------------
  Group             Level 1-2 (240)  Level 3-4 (260)  Level 5-6 (260)
  Private           per class, rate REQUIRED at save time
  Self-Study        covered by subscription credits (0, deliberately)
  Music, ...        whatever she creates next
```

A class picks a category. An enrolment picks a tier within that category —
which is what makes "Level 1 Mandarin and Level 2 Math for one student"
expressible, and is the whole reason level cannot live on the student.

Resolution order, one definition, used by both billing paths:

```
  enrollment.tier          <- the answer, per student per class
    -> class default tier  <- for a category whose tier does not vary
      -> class rate        <- Private: negotiated, required, no tier
        -> UNPRICEABLE     <- an error at SAVE time, never a 0 at bill time
```

`students.level_band` leaves the chain. It cannot express two subjects at two
levels, so keeping it as a fallback would keep producing a confidently wrong
answer for exactly the case Nadine described. All 66 rows are empty, so
removing it changes no one's bill today.

**It is not dropped in this change.** Stop writing it, stop reading it, leave
the column and its data. A dropped column cannot be rolled back.

## 3. Migration path

### 3a. `0051_enrollment_level_band.sql`

```sql
ALTER TABLE enrollments ADD COLUMN IF NOT EXISTS level_band TEXT NOT NULL DEFAULT '';
```

Backfill from the best available source, in order: the student's band where
set, else the class's. Records what we believe today without inventing
anything; `''` stays `''` and surfaces in step 3d rather than guessing.

### 3b. `0052_three_bands.sql`

Insert six tiers (`Group`/`Private` x `1-2`/`3-4`/`5-6`), all at the Level 4
rate as agreed. Soft-delete the old four rather than renaming them.

**Why soft-delete, not rename:** `deleted_at` is already on this table, and an
invoice raised in August referenced the `4-6` rate. Renaming rewrites what a
past invoice was priced against; soft-deleting leaves that history intact and
readable. Rename is cheaper and wrong for money.

**Add a CHECK on `level_band`.** The current constraint is `UNIQUE`, which
stops duplicates but permits a typo: `'3-4 '` with a trailing space stores
happily, matches no class, and prices at zero. That is the announcement-
audience bug again, in the money path. A CHECK closes the category rather than
the instance. Cost, accepted: adding a band later needs a migration, which is
correct for something that changes what people are charged.

### 3c. Band mapping for the existing classes

No hand-mapping needed, and no question for Nadine. Confirmed by Ely 09-06:

| New tier | Price | From |
| --- | --- | --- |
| Level 1-2 | 240 / 480 | today's `1-3` |
| Level 3-4 | 260 / 520 | today's `4-6`; Level 3 discounted by hand |
| Level 5-6 | 260 / 520 | today's `4-6` |

The 11 classes named "Level 1 & 2" and "Level 3 & 4" map straight onto the new
tier names, so the migration derives them from the class name.

The remaining 23 do NOT get a guess: Self-Study moves to its own category,
Private to a required per-class rate, and Mandarin has no level recorded
anywhere and is surfaced for a human. Deriving a band for those would be
inventing a price.

### 3d. No class may exist without a price (confirmed 09-06)

`cron.go:491` and `:506` currently `Warn` and carry on. Warning into a log
nobody reads is how a student went a full month uninvoiced.

The rule moves EARLIER, to where the class is saved: a class whose category,
tier and rate do not resolve to a price is refused at save time
(`handlers_classes.go:238` and `:359`). The bad state then cannot be created,
rather than being discovered a month later.

Because the 34 existing classes predate the rule, the same check also drives a
"classes needing a price" list in the admin UI so the backlog is visible and
finite. No alert email — Nadine is not on-call for this.

### 3e. Switch both pricing paths to the enrolment band

`rates.go:45-48` and `cron.go:345`, to the chain in section 2.

## 4. Retroactive September

Confirmed by the audit: **no September invoice exists** for
`STU_20260419163110116`. So this is a clean insert — nothing to void, nothing
to amend, no revised invoice for a parent to query.

Once pricing lands, `generateMonthlyInvoices` runs for the month and the dedup
guard (`cron.go:333`) lets it through precisely because no row exists.

Run as an explicit one-off with the output checked against Nadine's
expectation before anything is sent, and while `OUTBOUND_ALLOWLIST` is still
set, so no parent can receive a surprise bill mid-verification.

## 5. Settings page and the category editor (separate commit, lands after)

`_editPricingModal` and the matrix render currently live in
`calendar.js:1064-1094, 1208`. Nobody looks for pricing on the Schedule page.

The new screen does more than move: it is where Nadine adds a category, names
its tiers and sets their prices. Editing a rate takes effect from the change
forward, never backwards — an invoice already raised keeps the price it was
raised at, the same rule migration `0047` established for schedules.

Kept as its own commit so a rollback of the pricing model does not drag the
relocation with it, and vice versa.

## 6. Risks

| Risk | Handling |
| --- | --- |
| A derived tier is wrong, so a parent is billed the wrong amount | Only the 11 classes whose NAME states the level are derived. The other 23 are surfaced, never guessed. |
| Rollback loses the level data | `students.level_band` keeps its data; nothing is dropped in this change. |
| Retroactive run double-bills | Dedup guard, plus a dry run checked before anything is sent, with the allowlist still on. |
| The two billing paths drift apart again | One resolution chain, one place, used by both. |
| A user-defined tier is created with no price, recreating the hole | A tier with no price cannot be saved, and a class cannot reference a tier that has none. |
| Self-Study priced as tuition, double-charging on top of the subscription | Its own category at 0 by design, with a comment saying why 0 is correct here and nowhere else. |

## 7. Not in this change

- **Duplicate class.** `Teacher Chiying Aria)` looks like a mistyped copy of
  `Teacher Chiying (Aria)`. A human decides whether to delete it; a migration
  must not.
- **`Self-Study` vs `Self-study`.** Case-inconsistent names. Harmless while
  nothing keys on the name; worth tidying when someone is in there.
- **Mandarin's level.** Two classes with no level recorded anywhere. Surfaced
  in the "needs a price" list for Nadine to set.
