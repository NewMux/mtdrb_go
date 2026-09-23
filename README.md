# CoachPulse

A business operating system for independent personal trainers: client CRM,
workout programming, scheduling, and a self-contained double-entry accounting
ledger — with no card gateway, because the trainers this is built for settle in
cash, local bank wires and P2P transfers.

## Why the ledger is real

Money and service delivery are decoupled in time. A client pays $500 for ten
sessions in January and trains through April. Treating that payment as January
revenue overstates January, understates the rest, and yields a tax export that
does not match reality.

So prepaid packs credit **Deferred Revenue**, a liability. Revenue is
recognized when a session is delivered:

| Event | Debit | Credit |
|---|---|---|
| Invoice issued (10-session pack) | Accounts Receivable | Deferred Revenue |
| Payment recorded | Cash / Bank / Wallet | Accounts Receivable |
| **Session completed** | Deferred Revenue | **Training Revenue** |
| Expense logged | Expense account | Cash / Bank |

The P&L, receivables ageing and tax export are all derived from the journal, so
they agree by construction. See [ADR 0001](docs/adr/0001-double-entry-ledger.md).

## Running it

[RUNNING.md](RUNNING.md) — the backend, the app on your phone, and a
walkthrough of both journeys.

## Architecture

- **API** — Go 1.24, chi, pgx, Postgres 16; `api/openapi.yaml` is the contract, and the client's wire types are generated from it
- **Tenant isolation** — Postgres row-level security; the API connects as a role
  that is neither superuser nor `BYPASSRLS`, so isolation fails *closed*
  ([ADR 0002](docs/adr/0002-tenant-isolation-via-rls.md))
- **Client** — one Expo / React Native codebase for iOS, Android and web
  ([ADR 0005](docs/adr/0005-expo-single-codebase.md))
- **Offline** — SQLite + an outbox; last-write-wins for workout logs, strict
  serialization for money ([ADR 0004](docs/adr/0004-offline-sync-conflict-policy.md))

```
cmd/          api, worker and migrate binaries
internal/
  app/        the service graph, wired once for every binary and the HTTP tests
  platform/   money, ids, clock, errors, logger
  db/         migrations and the pool
  ledger/     accounts, journal entries, posting rules
  billing/    invoices, payments, packages, credits
  crm/        clients, waivers, assessments
  scheduling/ sessions, attendance state machine
  programming/ exercises, mesocycles, workout logs
  sync/       offline pull and push
app/          Expo client
```

## Getting started

Requires Go 1.24+, Docker and Node 20+.

```bash
cp .env.example .env
make up          # Postgres + MinIO, then migrations
make run         # API on :8080

make app-install # once
make app-start   # Expo client
```

## Development

```bash
make test              # unit tests
make test-integration  # RLS, ledger and journey tests (needs Postgres)
make verify            # everything CI runs
make migrate-status    # schema state
```

Migrations run as the database owner; the API runs as `coachpulse_app`. Keeping
those separate is what makes the isolation tests meaningful — see the Makefile
`OWNER_DATABASE_URL` and `APP_DATABASE_URL` variables.

## Documentation

Architecture decisions live in [`docs/adr/`](docs/adr/). Start with
[0001 (ledger)](docs/adr/0001-double-entry-ledger.md) and
[0002 (tenant isolation)](docs/adr/0002-tenant-isolation-via-rls.md) — they
constrain everything else.
