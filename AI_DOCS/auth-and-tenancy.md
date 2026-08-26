# Auth, roles, and tenant isolation

Read this before touching anything under `internal/auth/`, `internal/core/middleware.go`,
`internal/store/scope.go`, or any handler that reads tenant-owned data. This is the
highest-blast-radius subsystem in the repo: a mistake here is a cross-tenant data leak,
not a rendering glitch.

## The one thing to internalise

**Tenant isolation is a per-handler convention. Nothing enforces it.**

`server.go:144-149` says so directly:

> NOTE: RLSScope is currently a no-op passthrough (the app connects as a Postgres
> superuser and a per-request GUC is unsafe on a shared pool), so there is NO
> database-level backstop: every handler MUST apply `store.ScopeTenant` itself.
> Do not rely on RLS to catch a missing tenant WHERE clause -- it will not.

Postgres RLS policies exist (migrations `0004`, `0015`) but are dormant for two
independent reasons documented in `internal/store/rls.go:15-21`: the app connects as a
superuser (which bypasses RLS even under `FORCE ROW LEVEL SECURITY`), and the
`app.tenant_id` GUC cannot be safely set on a pooled connection. Activation is a real
refactor, planned in `notes/rls-activation.md`.

`rls_test.go` does NOT prove the codebase is safe. It simulates a non-superuser role via
`SET ROLE` and checks that the policies *would* constrain reads on the `students` table.
Its own header (`rls_test.go:14-17`) calls the policies "INERT" in production. Treat it as
a test of the future backstop, not of current query hygiene.

## Scoping a query

```go
tw, args := store.ScopeTenant(claims, "s")   // alias optional; "" for unaliased
db.Query(`SELECT ... FROM students s WHERE s.deleted_at IS NULL` + tw, args...)
```

The returned fragment **starts with " AND "**, so the base query must already have a
`WHERE` -- the house idiom is `WHERE 1=1` or `WHERE deleted_at IS NULL`
(`snapshot_bounded.go:51,84`).

`store.TenantID` (`scope.go:8-10, 31-40`) has two traps:

- **`nil` claims resolve to tenant 1**, not 0. Background and pre-auth callers land on
  tenant 1 rather than getting global visibility.
- **Tenant 0 means superadmin**, and `ScopeTenant` returns an empty fragment for it --
  a deliberate, fully unscoped cross-tenant query. Do not "fix" tenant 0 as invalid.

Do not reintroduce the older `(tenant_id=? OR ?=0)` form. It was replaced because it
prevented Postgres from using the composite `(tenant_id, deleted_at)` indexes
(`scope.go:27-31`).

A second scoping style also exists: explicit `if c.TenantID == 0 { ... } else { ... }`
branches, as in `handlers_admin_unlock.go:30-34`. Grepping for `ScopeTenant` alone
therefore undercounts tenant-checked handlers.

After a tenant-scoped write, check `RowsAffected` and 404 on zero
(`handlers_users.go:197-201`) -- a wrong or cross-tenant id previously returned a
false-success 204.

## Roles

Exactly four, carried in the JWT `role` claim (`core/claims.go:16-18`):
`superadmin`, `admin`, `teacher`, `parent`.

Use `core.IsAdminRole(c)`, never `c.Role == "admin"`. The bare comparison locks
superadmins out of routine work and is called out as "a recurring drift across handlers"
(`handlers.go:18-27`).

Authorization is enforced in **both** places, so route placement tells you nothing on its
own. `auth.RequireAdmin` wraps only the user-management / import / registrations / audit
subgroup (`server.go:311-312`); other admin-only endpoints check inside the handler body
(e.g. the monthly cron trigger, `jobs/cron.go:747-748`).

`HandleUsers` POST accepts only `parent`, `teacher`, `admin` -- rejecting `superadmin`
explicitly so an admin cannot self-provision a higher-privilege account
(`handlers_users.go:70-76`).

### Parent scoping

A parent is tied to their children by **email string match**: `students.contact = claims.Email`
(`handlers_students.go:133`). There is no ownership table. Two consequences:

- Changing a student's `contact` silently changes which parent account owns them.
- Any handler that applies only `ScopeTenant` and forgets the parent branch shows a parent
  every student in the tenant.

Announcement visibility needs SQL **and** a Go post-filter: `store.ParentAnnouncementFilter`
(`parent_scope.go:61-87`) drops class-targeted rows whose class is not in the parent's
enrolment set, "enforced here, not just in the client, so the snapshot never leaks them".

`users.email` is globally UNIQUE across tenants (`database.go:68`), which is why auth flows
can look up by bare email. `staff.email` is NOT unique, so staff-by-email lookups must be
tenant-scoped or a teacher can inherit another tenant's staff identity and class
permissions (`auth.go:232-237`, `handlers_students.go:33-35`).

## Tokens

**Access token**: HS256 JWT in the `sh_token` cookie -- HttpOnly, `SameSite=Lax`, `Path=/`,
`Secure` when TLS or `X-Forwarded-Proto: https` (`auth.go:268-280`). Expiry 24h, or 30 days
with "Remember me" (`auth.go:56-59`). Accepted from the cookie first, then an
`Authorization: Bearer` header; non-HMAC signing methods are rejected (`auth.go:518-540`).

A cryptographically valid JWT is still rejected on three further gates (`auth.go:548-571`):
user status is not `active`; the token was issued before `users.sessions_invalid_before`;
or its `jti` is in `revoked_tokens`. Both caches have a 30s TTL. These exist because
"refresh-token revocation alone left stolen access tokens alive for up to 30 days after the
victim reset their password" (`auth.go:443-445`). `revokeToken` must also delete the
in-memory cache entry or the killed token survives 30 more seconds (`auth.go:415-419`).

**Refresh token**: `sh_refresh` cookie, HttpOnly, `SameSite=Strict`, `Path=/api/auth`, 7-day
TTL, stored server-side only as SHA-256 (`refresh_tokens.go:16-19, 39-40`). Setting or
clearing it with a different `Path` silently fails to overwrite. Rotated on every use within
a `token_family`; presenting an already-used or revoked token **burns the entire family** as
suspected theft (`refresh_tokens.go:111-114`). Refresh always reissues with
`rememberMe=false` -- a deliberate conservative downgrade, not a bug (`refresh_tokens.go:148-151`).

Not every session has a refresh family: email verification and set-password auto-login issue
only the 24h access cookie (`handlers_verify.go:84-98`, `handlers_password.go:138-152`).

## Passwords, lockout, enumeration

Argon2id in PHC format, m=64MiB, t=1, p=4, 16-byte salt, 32-byte key (`passwords.go:30-36`).
Legacy bcrypt hashes still verify and are transparently rehashed on next successful login
(`auth.go:184-190`) -- removing that path locks out every pre-migration account.

Lockout is 5 failures then 15 minutes, incremented with an atomic `UPDATE ... RETURNING`
under the row lock so concurrent failures cannot all read the same pre-increment value
(`auth.go:146-160`). Login additionally rate-limits 5/minute per IP
(`core/middleware.go:49`), and `X-Real-IP` / `X-Forwarded-For` are trusted only from
loopback/private peers -- otherwise an attacker reaching the app directly could spoof past
the limiter (`middleware.go:52-57, 93-95`).

Two deliberate anti-enumeration measures, both easy to destroy by "simplifying":

- Unknown-email logins verify against a dummy Argon2 hash so the timing matches
  (`auth.go:77-88, 122-128`).
- Forgot-password and resend-verification return an identical generic message whether or
  not the account exists (`handlers_password.go:20-25`, `handlers_verify.go:124-126`).

Password reset kills sessions three ways -- refresh family revoked, `sessions_invalid_before`
stamped, `ical_token_version` bumped (`handlers_password.go:214-223`). Drop any one and a
live intruder channel survives account recovery.

## MFA

TOTP, RFC 6238, 6 digits, 30s step, plus/minus one step drift, hand-rolled with no external
library (`mfa.go:76-78`). On login, an MFA user gets **no auth cookie** -- only a single-use
5-minute intermediate token (SHA-256 stored, consumed via `DELETE ... RETURNING`) to be
POSTed with a code to `/api/auth/mfa/verify` (`auth.go:200-218`, `mfa.go:380-401`). Issuing
the cookie directly on password success bypasses MFA entirely.

## CSRF and public routes

Layered: an Origin/host **exact** match (substring matching is unsafe because
`studyhub.fit.attacker.com` contains `studyhub.fit` -- `server.go:64-67`), plus a
double-submit `sh_csrf` cookie compared constant-time against the `X-CSRF-Token` header
(`csrf.go:95, 119`). That cookie is **intentionally not HttpOnly** -- the frontend must read
it to set the header. "Hardening" it breaks the whole scheme. There is a fixed exempt-path
list including the webhooks and pre-session auth endpoints (`csrf.go:43-54`).

Deliberately public (`server.go:108-139`): `/api/health`, `/metrics`, `/api/openapi.yaml`,
`/api/branding`, `/api/push/vapid-key`, `/api/calendar/{userID}/{token}` (signed-URL auth),
the `/api/auth/login|mfa/verify|refresh|logout` set, `/api/register`, `/api/register-teacher`,
`/api/forgot-password`, `/api/reset-password`, `/api/set-password`, `/api/verify-email`,
`/api/resend-verification`, `/ws`, and the two payment webhooks (signature-verified
internally). **A new route added above the JWT group is public.**

Logout is mounted outside the JWT middleware on purpose so an expired or malformed cookie
can still log out; it re-parses with `jwt.WithoutClaimsValidation()` to recover the `jti`
for revocation (`auth.go:286-288, 306-320`).

`HandleMe` re-validates name, role, and status against the live `users` row rather than
trusting the JWT, so role changes and suspensions take effect without re-login
(`auth.go:346-363`).

## Do not

- Write a query against tenant-owned data without `ScopeTenant` or an explicit tenant branch.
- Assume `rls_test.go` passing means your new query is isolated.
- Compare `c.Role == "admin"`.
- Make `sh_csrf` HttpOnly, or match Origin by substring.
- Pass a caller-controlled string as the `table` argument to `store.tenantOf` -- it is
  concatenated into SQL and all current callers are hardcoded constants
  (`tenant_lookup.go:19`).
