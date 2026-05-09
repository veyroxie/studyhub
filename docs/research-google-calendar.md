# Google Calendar integration — options for parents

Parents want to see their child's class schedule on their phone without having to log into the website. Today they keep a separate Google Calendar where they manually enter classes, then put a Google Calendar widget on their home screen. They want StudyHub to feed that widget directly.

This doc lays out three approaches in order of effort.

## Recommendation: ICS feed per parent

Ship this. Lowest effort, biggest payoff, no Google API account needed.

- Backend exposes `/api/parents/{id}/calendar.ics?token=...` — a static `.ics` file generated on each request from the parent's children's classes (and one-off cancellations / replacements).
- The token is a per-parent random string (regenerable on demand) so the URL itself is the auth — no cookies, since Google's calendar fetcher won't carry them.
- Parent subscribes once on their phone:
  - **iOS**: Settings → Calendar → Accounts → Add → Other → Add Subscribed Calendar → paste URL.
  - **Android (Google Calendar app)**: open `calendar.google.com` in a browser → "Other calendars" → "From URL" → paste URL → it then syncs to the Android Google Calendar app and any home-screen widget already pointed at it.
- Google polls the URL every ~6 hours (iOS configurable). Acceptable for class-schedule changes; not for same-day cancellations — surface those in StudyHub itself.
- Cancel a class → next refresh the event disappears.

**Effort**: ~1 day. One handler, one tiny ICS template (RFC 5545; `BEGIN:VEVENT` blocks). No third-party libs needed; the format is plain text.

**Caveats**:
- The token is bearer auth in the URL. Document that "anyone with this URL can see your child's schedule" — same trust model as a Google Calendar share link.
- Sub-day refresh isn't possible. Same-day class cancellations stay in StudyHub's notif bell.

## Two-way OAuth sync (Google Calendar API)

Mirror StudyHub events into the parent's primary Google Calendar by writing events directly with the Google Calendar API.

- Parent OAuths once (Google Sign-in scope `https://www.googleapis.com/auth/calendar.events.owned`).
- StudyHub holds an access + refresh token per parent and pushes inserts/updates/deletes whenever a class is added or rescheduled.
- Realtime — changes appear within seconds. Cancellations and reschedules feel correct.

**Effort**: 4–6 days. Token lifecycle, refresh handling, Google Cloud project, brand verification for OAuth consent screen, retry/back-off, idempotency keys to avoid duplicate events on re-sync. Worth it eventually if parents complain about ICS lag, but not the first move.

## Public web link (already works)

Parent bookmarks `studyhub.fit/calendar` on their phone home screen. iOS turns it into an icon that looks like a native app. Works today; no widget, no system calendar.

**Effort**: zero. Just remind parents this option exists.

## Decision

Ship the ICS feed in the next sprint after the current batch settles. Re-evaluate two-way sync after a month if parents push for same-day accuracy.
