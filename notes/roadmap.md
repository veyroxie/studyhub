# StudyHub Roadmap (from Nadine/Ely feedback, Jun 2026)

Decisions locked: early-bird cutoff = **7th**; pricing = **type×level matrix** (Group/Private ×
Level 1–3/4–6 = 240/260/480/520); class feedback → **4-monthly progress reports** everywhere.

## ✅ Done (live)
- Early-bird flat RM10 (cutoff 7th), shown on invoice + banner
- Auto monthly subscription per student; Monthly Batch removed
- Referral RM10 × 3 months
- Reference/receipt mandatory for bank transfer + QR (cash optional)
- Invoice auto-issued; receipt only after payment; PDF download
- Self-study: round-up RM10/hr + credit-back the unused slice
- Pricing matrix + Settings screen; non-academic category removed (workshops kept)
- RLS enforcing via non-superuser role; alerts; deploy/harden tooling

## Batch A — quick UX wins
- Students: clickable stat-cards → filter; remove View button → clickable rows;
  debounce search (no reload per keystroke); editable student number
- Parent billing: show only Pending + Overdue cards
- Parent schedule: week-only; drop teacher filter / active-staff / enrolled-count cards
- Analytics: show teacher names (not labels/placeholders)

## Batch B — billing/invoice depth
- Invoice line items: 4 lessons + self-study, included self-study shown as a discount line
- Freeze / pause / resume tied to each student's package (subscription_status)
- Analytics: revenue pending→paid reflects on payment; invoice download reflects in analytics

## Batch C — progress reports (cross-panel, big)
- Replace per-class feedback with 4-monthly progress report (template) in admin/teacher/parent

## Batch D — access control (big)
- Parent gets invoice/receipt/progress/check-in-notifs ONLY if the month's fee is paid;
  "no access" banner otherwise

## Batch E — teacher panel privacy + replacements
- Teachers see only their own students' health + DOB + classes + replacements
  (no contact info; remove active/new/total count cards)
- Absence: support "absent without replacement"; simplify the 3-hr replacement-rule wording;
  show credits in replacements tab; log-extension limited to the child's enrolled levels
- Teacher sees pending class replacements for their students

## Batch F — analytics by level
- Attendance rate + filters grouped by level (1–6); teacher analytics by teacher

## Batch G — staff/payroll
- Hourly part-time payroll auto-generated from classes taught that month

## Batch H — performance (investigate)
- Slow login; slow schedule/actions; deletes feel non-instant — profile snapshot/load path

## Batch I — research / hardware (write-ups, not pure code)
- Check-in hardware: NFC vs QR scanner vs kiosk tablet — need immediate parent notify,
  real-time, no kid-distracting device. Recommend an approach.
- Notify parents on BOTH check-in and check-out
- Google Calendar sync / phone widget for the schedule — feasibility + approach
- One-time: import current students' real data after changes land

## Suggested order
A → B → F+G → C+D → E → H → I

## Centre requests, 2026-09-12 (Ying Quah, Nadine)

**Birthday wishes to a parent's WhatsApp (Ying Quah).** Not possible on the
current stack without a new dependency and a cost: WhatsApp Business API
messages to a user who has not messaged us first must be pre-approved template
messages sent through a Meta-approved BSP, billed per conversation. That is a
vendor, an approval process and a per-message cost, not a feature flag.
`students.dob` is already stored, so the trigger is trivial; the delivery is the
whole job. Cheaper alternatives that need no vendor: the existing web-push
channel, or an in-app notice on the parent portal. Decision needed before any
work.

**Teacher-only account for Rose (Nadine).** Already supported. Users are created
with role `teacher` (`HandleUsers` POST accepts parent, teacher, admin and
explicitly rejects superadmin). A teacher's reach is their own classes, not the
whole centre -- see "Teacher scoping" in AI_DOCS/auth-and-tenancy.md. Two things
to confirm before creating it: a teacher sees the timetable for classes they
teach, so Rose must be on `classes.teacher_ids` for whatever she needs to view;
and with the 2026-09-10 front-desk change a teacher can check ANY student in or
out, which is wider than "view the timetable". If Rose is meant to be read-only,
that gap is real and is not yet closed.

**Are parents receiving email? (Nadine).** No. `OUTBOUND_ALLOWLIST` is
restricted and no parent address is on it (ADR-015). Nothing has gone to a
parent and nothing will until that is deliberately changed.
