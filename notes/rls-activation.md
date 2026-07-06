# RLS activation runbook

Tenant isolation is currently enforced at the **query layer** by `store.ScopeTenant()`,
applied to every tenant-scoped query and covered by `TestRLSTenantIsolation`. The
Postgres row-level-security policies (migrations `0004`/`0015`) are a dormant
**second line of defence**. This document is the plan to activate them.

## Why RLS is dormant today

1. **The app connects as a Postgres superuser.** Superusers bypass RLS entirely —
   even `FORCE ROW LEVEL SECURITY`. The policies do nothing until the app connects
   as a non-superuser role. Migration `0026` stages that role (`studyhub_app`,
   `NOSUPERUSER NOLOGIN`).
2. **The tenant GUC must be set on the serving connection.** RLS reads
   `current_setting('app.tenant_id')`. `database/sql` hands each query an arbitrary
   pooled connection, so setting the GUC on one connection does not affect queries
   that run on another. Reliable per-request RLS needs every query in a request to
   run on a single pinned `*sql.Conn` (or a per-request transaction) that has the
   GUC set with `SET LOCAL`.

The old `RLSScope` middleware ran `SET app.tenant_id` on a random pooled connection.
That protected nothing and left the GUC stuck on that connection. It has been
reduced to a passthrough so it can no longer mis-set a pooled connection.

## Activation steps (do all, in order)

1. **Implement per-request connection binding.** Give each request a pinned
   `*sql.Conn`, run `SET LOCAL app.tenant_id = <tid>` on it inside a transaction (or
   a `SET` that is `RESET` on release), and route that request's queries through it.
   The cleanest shape: a request-scoped `*store.DB` that wraps the pinned conn,
   fetched by each handler via a small `store.FromRequest(r)` accessor. This is the
   real work — it touches how handlers obtain their DB handle. Load-test the
   connection pool afterwards (watch for pool exhaustion and handler `BeginTx`
   re-entrancy on the pinned conn).
2. **Provision the app role for login:** `ALTER ROLE studyhub_app LOGIN PASSWORD '<strong>';`
   (migration `0026` created it `NOLOGIN` so it can't be used prematurely).
3. **Switch `DATABASE_URL`** to connect as `studyhub_app` instead of the superuser.
4. **Verify** `TestRLSTenantIsolation` still passes and add an end-to-end test that a
   handler with a deliberately unscoped query cannot read another tenant's rows.

Until step 1 is done, do **not** point the app at `studyhub_app`: with `FORCE` RLS and
no GUC set, every write would be rejected.
