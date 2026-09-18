# ADR 0004: Split conflict resolution by data class

Status: Accepted
Date: 2026-09-18

## Context

Trainers work in basements with no signal. The PRD requires offline logging of
workouts, attendance and cash payments, with auto-sync on reconnection, and
specifies "last-write-wins for workout logging; strict transaction
serialization for financial ledger adjustments".

A single conflict strategy cannot serve both. Last-write-wins applied to money
silently destroys payments: two devices recording the same $500 cash settlement
would leave one entry, or worse, LWW on a balance field would lose one of two
genuinely distinct payments.

## Decision

Resolve conflicts by data class:

- **Workout logs, biometrics, notes** — last-write-wins on `updated_at`. Losing
  a redundant edit to a set log is acceptable; blocking the trainer is not.
- **Financial and attendance operations** — never LWW. These are append-only
  *intents* applied serially on the server. A genuine conflict (an invoice
  already settled, a credit already burned) returns a typed conflict that the
  app surfaces to the trainer rather than resolving on its own.

Safety rests on two mechanisms:
- **Client-minted UUIDv7 primary keys**, so a row created offline keeps its
  identity and a replay is recognisable as the same row.
- **`Idempotency-Key` on every mutating endpoint**, backed by a table storing
  the request hash and stored response. Replaying an outbox batch three times
  produces exactly one journal entry.

## Consequences

Good:
- Money is never silently lost or duplicated by a sync race.
- The offline outbox can retry aggressively, which is what makes flaky gym
  connectivity tolerable.

Costs:
- The client must implement real conflict UI for financial operations instead
  of resolving everything automatically.
- Every mutating endpoint carries idempotency bookkeeping. This is not optional
  polish; it is load-bearing.
