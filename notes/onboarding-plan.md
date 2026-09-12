# Account onboarding: where it stands and what is left

Written 2026-09-12, after the seeded teacher password turned out to be live on
production. [[studyhub-deploy-runbook]]

## Shipped

- **`must_change_credentials`** (0068). A password an admin typed is temporary
  by construction: that session gets 428 on every route except the setup
  endpoint, logout and health. Enforced in middleware, not the UI.
- **`POST /api/auth/complete-setup`.** The account's owner picks their own email
  and password. The staff row's email moves in the same transaction, because a
  teacher's classes resolve through `staff WHERE email = <signed-in email>`.
- **`PUT /api/users/{id}/credentials`.** Admin sets email, password and role.
  Refuses superadmin. Revokes every existing session.
- **`POST /api/users`** sets the flag, so creating an account is one step.
- **`scripts/issue-temp-password.sh`** for an existing account.

Proven end to end on production: Ely's own account went through it.

## 1. Teacher onboarding, on the platform

Today this is a shell script. It should be a screen, because Nadine will be
adding teachers and will not run curl.

**Where:** the existing user-management surface, admin only.

**The flow:** name, email, role, and a generated password shown ONCE with a copy
button. No password field for the admin to invent one, and no emailing it: the
admin reads it out or messages it, and it dies at first sign-in.

**Also create the staff row.** This is the part a naive form gets wrong. A
teacher with a `users` row but no `staff` row signs in successfully and sees an
empty timetable, because classes resolve through `staff.email`. The form must
create both, with the same email, in one transaction -- the same rule the
credentials endpoint already enforces on the way out.

**Then assign classes**, or she still sees nothing. Worth doing in the same
flow rather than leaving it as a separate step nobody remembers.

**Open:** whether a "teacher" who only needs to view the timetable should be a
distinct read-only role. Right now a teacher can check ANY student in or out
(the front-desk exemption, 2026-09-10), which is wider than Nadine pictures when
she says "only teacher's view".

## 2. Parent onboarding

**The state now:** 51 parent accounts, 27 active, and **zero have ever verified
an email**. They were created by the importer and by `ensureParentUserAccount`
with random unusable passwords, each with a set-password token emailed to them.
The design is right. The delivery is dead, because `OUTBOUND_ALLOWLIST` is one
address (ADR-015). So 51 people have accounts they have never once been able to
sign into.

**Why a public sign-up page is not the answer on its own.** A parent's view is
scoped by `students.contact = their email`. So the question a sign-up page has
to answer is "is this person really the parent of that child", and the usual
answer is email verification. That is exactly what is switched off. Without it,
anyone who knows or guesses a parent's email address can register it and read
that family's children, attendance and invoices. A sign-up page is safe only
once verified email works.

**Recommended: an admin-issued invite link.** `store.CreateEmailToken` with
`TokenPurposeSetPassword`, the `/api/set-password` endpoint and the set-password
page all exist and are tested. What is missing is one endpoint that RETURNS the
link to the admin instead of mailing it, so Nadine can paste it into the
WhatsApp thread she is already having with that parent.

It is better than a temporary password for parents specifically:

- Forty-two families. Reading passwords aloud forty-two times is worse than
  sending a link that expires.
- The trust comes from the channel. Nadine is messaging a number already on
  file, which is a stronger check than an email nobody can receive.
- It degrades correctly. When outbound mail is turned on, the same token gets
  emailed automatically and the copy-by-hand step just stops being needed.

**So: two flows, deliberately.** Not because parents are lesser, but because
staff you hand a password to in person and parents you message.

**Then:** a sign-up page becomes worth building once email verification works,
as the self-service path for a parent who was never invited. Until then it is a
hole with a form in front of it.

## 3. Birthday wishes

Already recorded in `notes/roadmap.md` under the 2026-09-12 centre requests.
Summary: `students.dob` is stored so the trigger is trivial; the delivery is the
whole job. WhatsApp messages to someone who has not messaged first must be
pre-approved templates through a Meta-approved BSP, billed per conversation --
a vendor and a cost, not a feature flag. Web push or a parent-portal notice
costs nothing and needs no vendor. Decision needed before any work.

## Order

1. Teacher onboarding screen. Nadine needs it and it has no dependencies.
2. Parent invite link endpoint plus a copy button. Unblocks 51 dead accounts.
3. Read-only role, if Nadine wants Rose to be view-only.
4. Public sign-up page, only after outbound mail is on.
