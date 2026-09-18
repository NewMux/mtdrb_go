# ADR 0002: Enforce tenant isolation in Postgres, not in Go

Status: Accepted
Date: 2026-09-18

## Context

CoachPulse is multi-tenant and holds medical notes, progress photos and
financial records. A single forgotten `WHERE tenant_id = $1` leaks one
trainer's client roster to another. Application-layer scoping fails open: the
bug is invisible until someone reports it.

## Decision

Every domain table carries `tenant_id uuid not null` and has a row-level
security policy gating on `current_setting('app.tenant_id', true)::uuid`.

The API connects as `coachpulse_app`, a role that is **neither superuser nor
BYPASSRLS**. Migrations run as the owner. Each request opens a transaction and
issues `SET LOCAL app.tenant_id` before any query; `SET LOCAL` scopes the
setting to the transaction, so a pooled connection cannot carry one tenant's
identity into the next request.

## Consequences

Good:
- Isolation fails *closed*. A handler that forgets to scope gets zero rows, not
  another tenant's rows.
- Cross-tenant probes return 404 rather than 403, so existence is not leaked.
- The guarantee is testable directly: run every read path as a second tenant
  and assert empty results.

Costs:
- All queries must run inside a transaction that has set the tenant GUC. The
  `tenancy` package owns this; bypassing it is a review-blocking error.
- Migrations must remember to enable RLS and grant to the app role on each new
  table. A schema test enumerates tables and fails on any that lack a policy,
  so this cannot be forgotten silently.
