# Student lifecycle dates: start, drop, pause, freeze

Status: BRAINSTORM. No code beyond migration 0055 and the roster guard.

Written 2026-09-08, after the August attendance bug: 85 attendance rows across
19 students were invisible because 35 enrolments recorded when the ROW was
created rather than when the student joined.

## 1. The diagnosis: two kinds of time, one column

Every lifecycle date in this system is one of two things, and the schema does
not distinguish them:

- **Valid time** -- when the fact was true in the world. "Chase joined the
  Wednesday class on 5 August."
- **Transaction time** -- when we wrote it down. "The row was created on 30
  August."

The August bug was `enrollments.started_on` holding transaction time while
`enrolledOn` read it as valid time. `students.paused_at` has the identical
shape: `handlers_students.go:524` stamps `time.Now()`, so it records the
moment the button was clicked, never the date the pause applies from.

**The codebase already solved this once and got it right.** Migration `0047`
gives a class edit an optional `scheduleFrom`: the change records the date the
new slot APPLIES, and resolution for date `d` is "the version with the greatest
`effective_from <= d`". That is valid-time modelling, already shipped, already
tested (`TestClassUpdate_ScheduleFromWritesVersion`).

The answer to "what is the best model" is largely: apply the pattern this repo
already proved, to the student.

## 2. Where each concept lives today

| Concept | Stored as | Kind of time | Can answer "on 5 Aug?" |
| --- | --- | --- | --- |
| Joined the centre | `students.registered_on` | valid | yes |
| Joined a class | `enrollments.started_on` | transaction (was the bug) | after 0055, back to first evidence |
| Left a class | `enrollments.ended_on` | transaction | no |
| Left the centre | `status='Inactive'` + `inactive_on` | state + date | partly |
| Billing paused | `subscription_status` + `paused_at` | state + click timestamp | no |
| Billing frozen | `subscription_status='frozen'` | state only | no |

Only the first row is unambiguously right.

## 3. Pause and freeze are the same thing today

`handlers_students.go:493-496` says so outright: "freeze is a separate flag
with the same effect that's surfaced differently in reports." Both set
`subscription_status` to a non-active value, both stamp `paused_at`, and the
cron excludes both identically (`cron.go:233, 362, 424`).

So the honest answer to "how do I differentiate the freeze date from the start
date" is that today there is no freeze date at all, only the timestamp of a
click, and freeze does not differ from pause.

**This has to go to Nadine before anything is modelled.** Two words for one
behaviour is exactly how a user comes to believe in a distinction the system
does not implement. Likely intended meanings, to put to her as options:

- **Pause** -- a known short break (a holiday). Seat held, billing stops,
  expected return date.
- **Freeze** -- open-ended. Seat may be released, billing stops, no return date.

If she means the same thing by both, delete one.

## 4. Recommended model: three axes, each dated

Keep them separate. They answer different questions and change independently.

```
  1. MEMBERSHIP   student <-> centre    registered_on .. left_on
  2. ENROLMENT    student <-> class     started_on .. ended_on      (exists)
  3. BILLING      student <-> money     active | paused | frozen, dated
```

Axis 2 already has the right shape. Axis 3 is the gap, and it should mirror
axis 2 rather than invent a third idea:

```
  student_billing_periods
    student_id, state, started_on, ended_on, reason, created_by
```

State on date `d` is the row where `started_on <= d < COALESCE(ended_on, ...)`,
the same half-open window `enrollments` and `enrolledOn` already use.

### Why this shape

**Not `paused_from` / `paused_until` columns on `students`.** Cheapest change,
but it holds exactly one pause. A student who pauses in June and again in
October overwrites the first, so recomputing June later gives the wrong answer.
Money you cannot recompute is money you cannot defend to a parent.

**Not an append-only event log** (`paused`/`resumed` events folded into state).
Most flexible and the most faithful record, but every existing
`subscription_status='active'` check -- three in the cron alone -- becomes a
join or a projection. Real cost, no benefit Nadine can see.

**The interval table wins because it is the third instance of a pattern already
here**, not a new idea: `enrollments` stints, `class_schedule_versions`, this.
One mental model instead of three.

### Keep the flag as the "now" mirror

`students.subscription_status` stays, mirroring the newest period, exactly the
invariant `0047` established for `classes.day`: the row is the source of truth
for "now" so the existing readers keep working; the table answers "what was it
on date `d`". That makes this incremental rather than a rewrite.

## 5. The one change that prevents the whole bug class

**Any UI that sets a lifecycle date must ask for the date, and must never
silently use `now()`.**

Class edits already work this way -- a payload carrying `scheduleFrom` records
a version, and its absence means a retroactive correction (`0047`). The
enrolment form should match: "Enrol from [date]", defaulting to today but
editable. Pause and freeze the same: "from [date]", optionally "until [date]".

Had the enrolment form asked "from when?" on 30 August, none of this week's bug
would exist. That single affordance is worth more than the table.

## 6. Decisions needed

For Nadine:

1. What is the difference between pause and freeze? If none, one goes.
2. Does a paused student keep their seat against class capacity?
3. When a student pauses mid-month, is that month prorated or billed whole?
   (Section 9 of the pricing plan says a month is flat, not four weeks -- the
   same argument probably applies, but it has not been asked.)

For Ely:

4. **Does a paused student's enrolment stay live?** This is now a money
   question: the new catalogue derives sessions-per-week from COUNT of live
   enrolments, so a paused twice-weekly student either still counts as 2x or
   drops to 1x. Nobody has decided.
5. Should an enrolment be allowed to start before the student's
   `registered_on`? Probably clamp, with a warning rather than a hard refusal.
6. Future-dated states ("freeze from 1 October") mean the cron must resolve
   state BY DATE rather than reading a flag. That is the point of the change,
   but it is also the part that touches billing, so it lands last.

## 7. Staged path, each stage shippable alone

| Stage | Change | Risk |
| --- | --- | --- |
| 1 (done) | `0055` backdates enrolment starts from attendance evidence; `rosterFor` guard | none, no money reads it |
| 2 | "from" date on the enrolment form and handler | low, UI + one handler |
| 3 | `student_billing_periods` + backfill from current state; nothing reads it | none, additive |
| 4 | Cron resolves billing state by date instead of by flag | HIGH -- money |

Stage 4 is the only dangerous one and it should be sequenced the way the
pricing switchover is: an old-vs-new comparison for every student before any
invoice is issued.

## 8. What NOT to build

- **Do not delete past attendance to "clean up" a departed student.** Raised
  09-08 for Zhang Zhan He. His records are the evidence that he was there, and
  `enrolledOn` exists precisely because an earlier version hid past students.
  When a specific marking is wrong, `_undoAttendance` already fixes one row.
- **Do not drop `paused_at` / `resumed_at`** when the periods table lands.
  Stop writing them, stop reading them, leave the data -- the same rule the
  pricing plan applied to `students.level_band`.
