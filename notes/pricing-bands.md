# Pricing rework: level moves onto the enrolment, two bands become three

Status: PLAN. No code written. Blocked on the two discovery queries in step 0.

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

## 0. Prerequisites — run these BEFORE writing any migration

The backfill differs completely depending on the answers, so these are
prerequisites, not curiosities. Read-only; `make psql`.

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

Resolution order, single definition, used by both paths:

```
  enrollment.level_band          <- the answer, per student per class
    -> class.level_band          <- fallback while enrolments are unbanded
      -> UNPRICEABLE (error)     <- never 0, never silently skipped
```

`students.level_band` leaves the chain. It cannot express "Level 1 Mandarin,
Level 2 Math", so keeping it as a fallback would keep producing a confidently
wrong answer for exactly the case Nadine described.

**It is not dropped in this change.** Stop writing it, stop reading it, leave
the column and its data in place. A dropped column cannot be rolled back, and
the values in it are the only record of what someone believed a student's level
was — useful as the source for the enrolment backfill.

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

Driven by Q1. `1-3` maps to... `1-2` or `3-4`? It straddles the new boundary,
so **this is Nadine's call per class, not a rule I can write.** The migration
maps what is unambiguous and leaves the rest empty for step 3d.

### 3d. Make unpriceable loud

`cron.go:491` and `:506` currently `Warn` and continue. Warning into a log
nobody reads is how a student went a month without an invoice. These become a
visible admin surface: a "classes needing a price" list, shown where Nadine
will see it. No new alert email — she is not the on-call for this.

### 3e. Switch both pricing paths to the enrolment band

`rates.go:45-48` and `cron.go:345`, to the chain in section 2.

## 4. Retroactive September

Depends on Q3.

- **No September invoice exists:** delete nothing. Once pricing lands, run
  `generateMonthlyInvoices` for the month; the dedup guard
  (`cron.go:333`) lets it through precisely because no row exists.
- **A zero or partial invoice exists:** the guard will silently refuse to
  issue a second one, and "retroactive" would quietly do nothing. The row is
  voided and reissued rather than amended in place — an amended invoice that a
  parent has already seen is a support conversation; a voided one with a
  replacement has an audit trail.

Either way this runs as an explicit one-off with the output checked against
Nadine's expectation before anything is sent, and while `OUTBOUND_ALLOWLIST` is
still set, so no parent receives a surprise bill mid-verification.

## 5. Settings page (separate commit, lands after)

`_editPricingModal` and the matrix render currently live in
`calendar.js:1064-1094, 1208`. Nobody looks for pricing on the Schedule page.

This has no data dependency on anything above — it moves existing UI to a new
home. Kept as its own commit so a rollback of the pricing model does not drag
the relocation with it, and vice versa.

## 6. Risks

| Risk | Handling |
| --- | --- |
| A backfilled band is wrong, so a parent is billed the wrong amount | Every band written by the backfill is derived, never invented. Nadine reviews the mapping before the first run. |
| Rollback loses the level data | `students.level_band` keeps its data; nothing is dropped in this change. |
| The `1-3` to `1-2`/`3-4` split is ambiguous | Not decided in code. Left empty and surfaced for Nadine. |
| Retroactive run double-bills | Dedup guard plus an explicit dry-run check before sending. |
| Bands drift out of sync between the two billing paths again | One resolution chain, one place. |

## Open question for Nadine

Which band does each current `1-3` class become — `1-2` or `3-4`? The old band
straddles the new boundary, so there is no correct automatic answer, and
guessing changes what a parent pays.
