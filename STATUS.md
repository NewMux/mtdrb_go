# CoachPulse — Build Status

**Branch:** `claude/charming-lamport-mlpjxw` · **Last updated:** 2026-09-18

**PRD Phase 1 is complete.** Both user journeys work end to end — asserted
through the real HTTP router against real Postgres on the server, and buildable
as an app a trainer can hold on the client.

| | |
|---|---|
| Milestones done | 8 of 8 |
| Server tests | 185, green under `-race` |
| Client tests | 72, against real SQLite |
| Production Go | ~12,700 lines |
| Test Go | ~7,400 lines |
| Migrations | 7, each verified to roll back and reapply |
| Tables | 35, every one under row-level security |
| Client routes | 14, every one bundles |

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

Sixty-six exercises seeded per tenant at signup, organised by movement
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

### M7 — The sync engine and the app

**Server side.** Pull mirrors rows into local SQLite; **push carries
operations, not rows.**

That asymmetry is the load-bearing decision. A generic row upsert would be far
less code — the client sends an attendance row with status `completed`, the
server writes it — and it would be wrong. Marking attendance burns a credit and
posts revenue; writing the row directly does neither, so the calendar would say
the session happened while the books said nothing was earned. So the outbox
replays *intents*, each dispatching to the same service the online path uses.
There is no route around the rules.

Covered by a test that drains a morning of offline work — attendance, a
workout, cash in an envelope, a measurement — and then asserts the credit
burned, the revenue posted, the invoice settled and the trial balance still
balances.

Per-collection cursors behind an opaque token. A conflict is reported per
operation with the request still `200`, because a blanket failure would have
the outbox retry the eleven operations that succeeded.

**Client side.** Expo + expo-router, TypeScript strict, over a local SQLite
mirror. Every read hits the device; nothing on a gym floor waits for a network.

- **Today** — the roster, with the outcome buttons opening in place rather than
  pushing a screen. Journey A's hinge lives here: completing a session for a
  client at zero credits offers the renewal or an explicit overdraft, decided
  before the client leaves.
- **The floor logger** — one exercise at a time, numbers large enough to read
  at arm's length. *Repeat last session* clones last week's working sets as
  **logged but not completed**: greyed, waiting for a tap. Warm-ups are not
  cloned.
- **Clients** — searchable roster, profile with credits, receivables,
  measurements and recent volume.
- **Money** — Journey B's second half. The chase list, and two taps to record
  what arrived and how.
- **Sell a package** — Journey B's first half, and the one screen that is
  deliberately **online-only**: a gap-free invoice number comes from a counter
  the server holds under a row lock, and two devices issuing offline would both
  mint `2026-014`. The screen says so before the trainer starts typing. It ends
  by handing them a share link for WhatsApp.
- **Sync** — what is waiting and what was refused, in plain words, with the
  choice to send again or let it go.

Two rules the client keeps everywhere:

- **Optimistic about what the trainer chose, never about what the server
  owns.** An attendance mark shows instantly; the credit balance does not move,
  because which pack it comes from and what revenue it recognises are the
  server's decisions. A payment is recorded; the invoice is *not* marked
  settled, because whether it settles depends on what else has been paid and on
  the server's refusal to accept an overpayment.
- **A refused operation stops being retried and starts being shown.** Silently
  parking it leaves the trainer believing a session was marked when it was not.

The local write and the queued operation are one transaction, so a set can
never exist on the device without an operation to carry it — verified by a test
that fails the enqueue and asserts the set rolled back with it.

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
| M7 | A single global sync cursor advanced past rows in other collections — data would have **silently never synced** |
| M7 | `withTransactionAsync` resolves to `void`, so the driver's `transaction()` returned undefined where callers expected a value |

Two design self-corrections mid-build: `MarkDay` originally skipped
out-of-credit clients silently (a trainer would believe it worked); the credit
service originally had a broken `LinkJournalEntry` that could never execute,
replaced by a plan/apply split so the journal link is written at insert.

---

## What should be done next

### 1. M5.5 — Jobs and the worker *(the largest remaining gap)*

`cmd/worker` is a stub and `internal/jobs/` is empty. Needs advisory-lock
leader election, then:

- Recurring invoice drafts (PRD 4.1)
- Package expiry sweeps — `billing.ExpirePackages` exists and is unit-tested
  but nothing calls it on a schedule
- Overdue reminders (PRD Phase 3)

### 2. Close the known gaps

- **`media.S3Presigner` has never run against a live bucket.** It compiles and
  the media service is covered by API tests using a fake presigner, but Docker
  is unavailable in this sandbox and the MinIO download is proxy-blocked. This
  is the single largest untested surface.
- **The client has never talked to a live server.** Its unit tests drive the
  API client against a stubbed fetch and the offline logic against real SQLite,
  and every route bundles — but nothing has yet run the whole loop against a
  running API. That is the first thing to do with a machine that can run both.
- **PDF invoice export.** Deliberately deferred; the share page is one HTML
  template so PDF renders the same source rather than a second layout
  ([ADR 0006](docs/adr/0006-invoice-pdf-rendering.md)).

### 3. Screens the PRD asks for that are not built

- The **programme builder** — mesocycles, blocks, days and prescriptions. The
  whole server side exists (M6) and the client can log against a programme, but
  building one is a sit-down, wide-screen job and wants the desktop web layout
  rather than a phone.
- **Financial reporting** — P&L, trial balance, receivables ageing. The ledger
  computes all of it; nothing displays it.
- **Calendar and booking** — the client mirrors sessions and marks attendance,
  but creating and rescheduling a session is still server-only.

### 4. PRD Phase 2 and 3

Expense entry and receipt capture (accounts and posting rules already seeded,
so this is additive), tax-ready CSV export, two-way Google/Apple calendar sync,
the client companion portal (tokens and policies are designed and tested; the
screens are not built), multi-currency.

---

## Running it

```bash
# Postgres (Docker is unavailable in this sandbox, so a direct cluster)
pg_ctl -D /var/lib/postgresql/coachpulse-test -o '-p 5432 -k /tmp -h 127.0.0.1' start
make migrate

make test                                  # unit
make test-integration                      # RLS, ledger, journeys
make verify                                # everything CI runs for the server

make app-install                           # once
make app-start                             # Expo dev server
make app-verify                            # typecheck, tests, bundle every route
```

The client reads its API base URL from `expo.extra.apiBaseUrl` in
`app/app.json`; point it at the machine running `make run`.

Integration tests need `-p 1`: they share one database and reset it between
tests, so concurrent packages corrupt each other's assertions.

Start with [ADR 0001 (ledger)](docs/adr/0001-double-entry-ledger.md) and
[ADR 0002 (tenant isolation)](docs/adr/0002-tenant-isolation-via-rls.md) —
they constrain everything else.
