# CoachPulse — Build Status

**Branch:** `claude/charming-lamport-mlpjxw` · **Last updated:** 2026-09-18

PRD Phase 1's **backend is complete**. Both user journeys from the PRD work end
to end, asserted through the real HTTP router against real Postgres.

| | |
|---|---|
| Milestones done | 7 of 8 (M0–M6) |
| Tests | 173, green under `-race` |
| Production Go | ~12,700 lines |
| Test Go | ~7,400 lines |
| Migrations | 7, each verified to roll back and reapply |
| Tables | 35, every one under row-level security |

---

## What was built

### M0 — Foundation

Go 1.24 + chi + Postgres 16. Structured errors with stable machine codes the
client branches on, a logger that redacts medical notes and payment details
by key, UUIDv7 ids so offline rows keep identity, and a `money` type that is
integer minor units and refuses cross-currency arithmetic. Six ADRs in
[`docs/adr/`](docs/adr/) record the decisions that constrain everything after.

### M1 — Tenancy & authentication

Tenant isolation is enforced in Postgres, not Go. Every table carries
`tenant_id` with a row-level security policy, and the API connects as a role
that is **neither superuser nor `BYPASSRLS`** — so a handler that forgets to
scope gets zero rows rather than another trainer's clients.

Two bootstrapping problems were solved by tightening rather than loosening:
signup binds its transaction to the tenant it is about to create; login and
refresh resolve an email or token hash through narrow `SECURITY DEFINER`
functions with pinned `search_path`, rather than granting the app role a
blanket read.

Refresh tokens rotate and are tracked as a family — replaying a spent token
revokes the whole chain.

### M2 — Double-entry ledger

The module the product is built around. Prepaid packs credit **Deferred
Revenue**, a liability; revenue is recognised one session at a time, when the
training is actually delivered. Receiving money moves it between assets — it
does not earn it.

Guarantees live in the database because a rule in application code is one
forgotten call site from a corrupt ledger:

- A deferred constraint trigger asserts debits equal credits per entry at
  commit. A one-cent imbalance is physically rejected.
- Entries are append-only. Corrections are reversing entries carrying the
  original's date.

Verified by property tests over randomised sequences of sell / collect /
deliver / expense / undo.

### M3 — CRM, intake and private media

Client records, custom tags, PAR-Q, versioned waivers, biometrics, progress
photos. Medical notes are pgcrypto-encrypted and excluded from ordinary
profile fetches; a wrong key fails loudly rather than returning an empty field
that reads as "no conditions".

Client-to-client isolation is structural: policies narrow to the caller's own
row when a portal client id is bound, and show the whole roster when it isn't.

Media bytes never pass through the API — presigned URLs are minted only after
the row is read under RLS, so authorisation happens before the URL exists.

### M4 — Credits, scheduling and attendance · **Journey A**

Mark a session Completed → the credit burns → `DR Deferred / CR Training
Revenue` → the balance hits zero → the refusal on the next session carries
`remaining: 0, required: 1` so the app can offer a renewal.

Attendance lives on the **attendee**, not the session: in a semi-private slot
one client can complete while another no-shows, each with its own credit and
revenue consequence.

Double-booking is refused by a Postgres exclusion constraint over a buffered
time range, so two devices syncing offline bookings cannot both win. With a
15-minute buffer, 10:10 after a 09:00–10:00 session is refused and 10:15 is
accepted.

### M5 — Invoices, payments and sharing · **Journey B**

Issue → share a link over WhatsApp carrying the trainer's IBAN → client pays
off-platform → *Mark as Paid* → `settled`, receivables zero, 10 credits
available.

- **Gap-free invoice numbers**, required by tax law across much of the EU. A
  counter row taken `FOR UPDATE`, not a sequence — sequences are
  non-transactional, so a rolled-back issue would leave a hole.
- **Overdue is derived**, not stored. A stored flag needs a nightly job to
  stay truthful.
- The share link is the only unauthenticated surface: 32 random bytes, only
  the hash stored, revocable, with a payload assembled field by field so a
  field added later cannot leak by default.
- **Idempotency** finally uses the table that had sat unused since M0.

### M6 — Programming and the floor logger

Prescription and performance are kept as **separate facts** — collapsing them
would destroy the one thing progression tracking exists to show.

Sixty-eight exercises seeded per tenant at signup, organised by movement
pattern rather than muscle, because that is how a coach checks a session is
balanced. Mesocycles hold blocks hold days hold prescriptions; a four-week
block repeats its days rather than storing twenty-eight copies.

Gym-floor behaviour:

- Logging a set upserts on (workout, exercise, set index), so a corrected
  typo does not create a second set three and an outbox replay converges.
- The PRD's single-tap cloning writes last session's numbers as
  **logged-but-not-completed** — prefilled numbers that silently counted as
  performed would put lifts in a client's history that never happened.
- Cloning skips warm-ups; progression counts working sets only, so a deload
  cannot read as a personal best.

---

## Bugs the tests caught

Worth recording, because each was a real defect in shipped-looking code:

| Milestone | Bug |
|---|---|
| M2 | An account whose only entries fell after the report date returned *no row* instead of a zero balance |
| M3 | `LatestParQ` ordered only by timestamp — ambiguous when offline rows sync together |
| M4 | A pack could exist without its matching liability, driving Deferred Revenue negative and **overstating profit** |
| M4 | Overdraft could not draw against an exhausted pack, so the first overdrawn session was wrongly refused |
| M4 | An overdrawn balance displayed `0` instead of `−1`, hiding the debt |
| M5 | Idempotent replays stored as `jsonb` came back with keys reordered, so a replay was not byte-identical |

Two design self-corrections mid-build: `MarkDay` originally skipped
out-of-credit clients silently (a trainer would believe it worked); the credit
service originally had a broken `LinkJournalEntry` that could never execute,
replaced by a plan/apply split so the journal link is written at insert.

---

## What should be done next

### 1. M7 — Expo client *(the only Phase 1 item left)*

The backend is complete and unused. Everything below it is lower priority than
having something a trainer can hold.

- `expo-sqlite` + Drizzle mirroring the server schema
- Outbox sync engine against `POST /sync/push` and `GET /sync/pull`
- Generated API client from `api/openapi.yaml`
- Trainer screens for M3–M6; desktop web layouts for the programme builder
  and financial reporting

**The server side of sync does not exist yet.** `internal/sync/` is an empty
directory. Every table already carries `server_seq`, `updated_at`, soft
deletes and client-minted UUIDv7 ids, so the design ([ADR
0004](docs/adr/0004-offline-sync-conflict-policy.md)) is ready — but the pull
and push endpoints are still to write. That is the first task of M7, not the
last.

### 2. M5.5 — Jobs and the worker

`cmd/worker` is a stub and `internal/jobs/` is empty. Needs advisory-lock
leader election, then:

- Recurring invoice drafts (PRD 4.1)
- Package expiry sweeps — `billing.ExpirePackages` exists and is unit-tested
  but nothing calls it on a schedule
- Overdue reminders (PRD Phase 3)

### 3. Close the known gaps

- **`media.S3Presigner` has never run against a live bucket.** It compiles and
  the media service is covered by API tests using a fake presigner, but Docker
  is unavailable in this sandbox and the MinIO download is proxy-blocked. This
  is the single largest untested surface.
- **PDF invoice export.** Deliberately deferred; the share page is one HTML
  template so PDF renders the same source rather than a second layout
  ([ADR 0006](docs/adr/0006-invoice-pdf-rendering.md)).
- **`api/openapi.yaml` is a skeleton.** M7's generated client depends on it.

### 4. PRD Phase 2 and 3

Expense entry and receipt capture (accounts and posting rules already seeded,
so this is additive), the P&L dashboard, tax-ready CSV export, two-way
Google/Apple calendar sync, the client companion portal (tokens and policies
are designed and tested; the screens are not built), multi-currency.

---

## Running it

```bash
# Postgres (Docker is unavailable in this sandbox, so a direct cluster)
pg_ctl -D /var/lib/postgresql/coachpulse-test -o '-p 5432 -k /tmp -h 127.0.0.1' start
make migrate

make test                                  # unit
make test-integration                      # RLS, ledger, journeys
make verify                                # everything CI runs
```

Integration tests need `-p 1`: they share one database and reset it between
tests, so concurrent packages corrupt each other's assertions.

Start with [ADR 0001 (ledger)](docs/adr/0001-double-entry-ledger.md) and
[ADR 0002 (tenant isolation)](docs/adr/0002-tenant-isolation-via-rls.md) —
they constrain everything else.
