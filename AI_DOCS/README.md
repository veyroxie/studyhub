# AI_DOCS

Per-subsystem spec sheets for AI coding assistants working in this repo.

`CLAUDE.md` holds the stack and the repo-wide invariants; it loads into every conversation, so
it stays terse. These files hold the deep, subsystem-specific rules and are read on demand.

## Read this one before touching that code

| File | Covers |
|---|---|
| [auth-and-tenancy.md](auth-and-tenancy.md) | JWT, refresh rotation, MFA, roles, CSRF, and why tenant isolation has no database backstop |
| [billing.md](billing.md) | Money representation, the price-precedence chain, discount stacking, invoice idempotency, webhooks, referrals |
| [calendar-and-sessions.md](calendar-and-sessions.md) | Why there is no sessions table, per-date exception rows, cancellations, holidays, iCal, attendance |
| [jobs-and-outbound.md](jobs-and-outbound.md) | The in-process job roster, the outbound kill switch, email queue semantics, push, websockets |
| [database.md](database.md) | The `?` placeholder trap, migration rules, schema conventions, snapshot caching, connection pool |
| [frontend-contract.md](frontend-contract.md) | The `window.App` module contract, `App.Api` / `Store` / `Utils`, XSS boundaries, the Docker build step |

## What these are for

The point is **not** generic best practice -- a model already knows that. The payload is the
set of facts that are true about this repo and unguessable from whichever file you happen to
open first: that `monthly_fee_override = 0` means "unset" and not "free", that `esc()` does not
protect a JS string literal, that RLS exists but is inert, that a `?` in a jsonb operator gets
silently renumbered.

## How to use them

- Every non-obvious claim carries a `path/file:line` anchor. If a claim looks wrong, **check
  the citation** -- it is there so you can, and so the doc can be re-verified rather than
  trusted on faith.
- Sections headed "Open conflicts" describe places where two parts of the code genuinely
  disagree today. They are not bugs to fix in passing; resolving one is its own decision.
- A doc that contradicts the code means the doc is stale. Fix it in the same change.

## Provenance

Built 2026-08-20 by having subagents extract cited evidence from the source, then writing
these docs from that evidence. **The citations were sampled, not exhaustively verified.**

What that means concretely, so you can calibrate how much to lean on a line number:

- 247 line-level citations across these six files.
- All 247 were mechanically checked: every cited file exists and every line number is within
  that file's length. Three out-of-range citations were found and corrected this way.
- Roughly 15 were verified by hand, chosen for blast radius -- the money constants and
  rounding, tenant scoping, the RLS-is-dormant note, `esc()`, and both flagged conflicts.
  One of those 15 was off by eight lines and was corrected.
- The remainder are agent-extracted. The **claims** were each tied to a quoted snippet, but a
  line number may still be off by a few, and the code drifts anyway.

So: **grep the symbol or the quoted phrase, do not trust the line number as an address.** If a
claim does not match what you find, the doc is wrong -- fix it in the same change.
