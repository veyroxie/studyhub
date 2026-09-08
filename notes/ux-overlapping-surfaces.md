# Where the system offers more than one way to do one thing

Audit requested 2026-09-08 after Aria turned out to simply not be enrolled.
Findings only; nothing changed.

Ordered by how likely each is to cause a wrong action, not by effort.

## 1. Enrolling a student: four surfaces, none of them on the class

A student joins a class from:

| Surface | Where |
| --- | --- |
| Add Student modal | `students.js:764` |
| Edit Student modal | `students.js:675` |
| Classes tab, "Enrol / manage classes" | `students.js:338` |
| Registration approval | `handlers_registrations.go:541` |

All four write `students.enrolled_classes` and PUT `/api/students`. The class
detail modal lists who is in the class (`calendar.js:607-620`) and lets you
click through to a student, but has **no way to add one**. So the obvious
mental model -- open the class, add the student -- dead-ends, which is exactly
how Aria ended up with a class named after her that she was not in.

**And the four surfaces are no longer equivalent.** The "Starting from" date
added on 09-08 exists only on the Classes tab. Enrolling through Add Student or
Edit Student still silently uses today, which is the behaviour migration 0055
had to repair. Same bug, narrower door.

## 2. A frozen student is labelled Active

Three indicators, two fields, and the loudest one is the wrong one:

```
  status              Active | Inactive | New | Waitlisted   -> green "Active" badge
  subscription_status active | paused | frozen               -> small chip + On/Off toggle
```

A student who is frozen shows a green **Active** badge (`students.js:180`), a
blue **Frozen** chip beside their name (`students.js:162`), and an **Off**
toggle. All three are correct and they read as contradictory.

This is not cosmetic: 21 students with live enrolments are frozen right now and
will not be invoiced in October, and the list they appear in labels them Active.

## 3. The same field has two UIs and three vocabularies

Both write `subscription_status` through the same endpoint:

| Surface | Words | Confirms? |
| --- | --- | --- |
| List row toggle (`_applyActive`) | Auto-bill **On / Off** | no |
| Profile modal (`_subscriptionAction`) | **Pause / Freeze / Resume** | yes |

"Off" and "Freeze" are the same action. "Pause" and "Freeze" are the same
BEHAVIOUR -- `handlers_students.go:495` says so outright, "freeze is a separate
flag with the same effect" -- but they render different chips, so the UI
promises a distinction the system does not implement. One path asks for
confirmation and the other does not, for the identical state change.

Three visible states, two behaviours.

## 4. The class modal's count and its roster disagree by design

- **Count** `c.enrolled`: every student whose `enrolled_classes` holds the id,
  no status filter (`handlers_students.go:637`).
- **Roster** below it: only `status` Active or New (`calendar.js:609`).

So "Enrolled 8/10" can sit above a list of seven names. Checked against
production: **zero classes disagree today**, because no Inactive student
currently keeps a class in their JSON. Latent, not live -- but it will surface
the first time someone is marked Inactive without being unenrolled.

`calendar.js:574` computes an `enrolled` list that nothing uses; the roster is
recomputed 30 lines later under different rules. That dead variable is probably
how the two rules drifted apart.

## 5. The only pricing screen edits the table being replaced

`/api/pricing/{id}` (`server.go:252`) is the sole pricing route and it writes
`pricing_tiers`. There is **no route and no UI** for `pricing_categories` or
`pricing_plans` -- the catalogue migrations 0051 and 0054 created.

So today Nadine can only edit the matrix that prices exactly one class, and on
the day the switchover lands she will be able to edit nothing at all without a
developer. That is precisely the outcome the rework exists to end, and it makes
the settings screen in section 5 of `pricing-bands.md` a blocker on the
switchover rather than a follow-up.

The editor also still lives on the Schedule page (`calendar.js:1065`), which
section 5 already flagged as the wrong place to look for pricing.

## 6. Two sources of truth for enrolment, agreeing by luck

`students.enrolled_classes` (JSON) drives every screen. The `enrollments` table
drives the attendance roster and, after the switchover, billing. They agree on
counts for all 30 enrolled students today, and nothing enforces that: only
`SyncEnrollments` keeps them aligned, and it is called from four places that
each have to remember to.

## What I would fix first

1. **(2) and (3) together** -- one word for one state. Either delete "pause" or
   make it mean something. Show the billing state where the Active badge is,
   not beside it.
2. **(1)** -- add an "Add student" button to the class modal that opens the
   same enrol flow, and put the "Starting from" date on all four surfaces.
3. **(5)** -- the catalogue needs a screen before the switchover, not after.
4. (4) and (6) are latent; worth a guard, not a rewrite.
