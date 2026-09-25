# CoachPulse — Build Status

**Branch:** `claude/charming-heisenberg-973web` · **Last updated:** 2026-09-25

**Ready to launch**, pending what only the operator can supply: a domain, the
database and bucket, store accounts, and a lawyer's read of the privacy
policy and terms. [LAUNCH.md](LAUNCH.md) is the runbook, and its first table
is the list of those things.

The production deployment ran end to end in Docker, in production mode,
behind TLS. It signed up, signed in, uploaded through presigned URLs,
downloaded and deleted, and the web build signed in and stayed signed in
across a reload. Getting there found a bug that would have stopped anyone
signing in on a managed Postgres; see
[what launch preparation found](#what-launch-preparation-found).

| | |
|---|---|
| Server tests | 263 test functions, green under `-race`, on a superuser-migrated database and on one migrated the way production is |
| Client tests | 137, plus 7 against a live server |
| Production Go | ~20,400 lines |
| Test Go | ~11,500 lines |
| Migrations | 15, each verified to roll back and reapply |
| Tables | 39, every one under forced row-level security |
| Client routes | 31, every one bundles, on Expo SDK 57 |

---

## Since Phase 1

### Product (September 20–23)

Built on a separate branch and brought together here:

- **Foundations.** Pack revenue no longer strands a rounding remainder. The
  API contract is enforced: every route must be in `api/openapi.yaml`, and
  the client's wire types are generated from it. Money renders in its
  currency's precision.
- **Design system and shell.** Light and dark, English and Arabic
  (right-to-left), and one navigation that serves the gym floor and the desk.
- **The demo is recorded from the real API**, byte for byte reproducible,
  and replays in the browser with no server.
- **Settings and account security.** Two-step sign-in, recovery codes,
  password reset by email, the devices list, and rate limits. Plans are
  state only, and a lapsed account goes read-only.
- **The worker**, with a leader lock, idempotent claims, and pack expiry at
  each practice's midnight.
- **Locations and a price list; onboarding and VAT** for the Gulf states.
- **Expo SDK 57**, merged in from its own branch.

### Launch preparation (September 25)

- **Configuration refuses to launch like a laptop.** `APP_ENV` must be set.
  Production refuses:
  - the example keys, or one key used for both jobs;
  - a database connection without enforced TLS;
  - localhost URLs;
  - plaintext mail;
  - missing legal details.
- **Media.** Upload URLs sign content type and length. Confirming an upload
  checks the real store. The API never creates a bucket in production, and
  refuses a public one. The presigner has run against a live S3 server.
- **Server hardening.**
  - The client address comes from the right-most proxy hop.
  - Public invoice links are rate limited.
  - HSTS is on.
  - Postgres enforces `statement_timeout`.
  - SMTP requires TLS.
  - Housekeeping deletes expired sessions and keys.
  - Error-level logs can go to Sentry, redacted.
- **Account deletion**, which both stores require. It deactivates at once,
  and after 30 days the worker purges every row and file. This is the one
  narrow exception to the append-only ledger, and the app role cannot use it.
- **Privacy policy and terms** at `/legal/privacy` and `/legal/terms`.
- **Docker deployment.** One server image, migrations before every start,
  and Caddy for TLS on one origin. It works with managed Postgres and S3/R2,
  or on a single box. CI builds the images and publishes on release tags.
- **The app for the stores.**
  - A delete-account screen, which also clears the device.
  - Links to the legal pages.
  - Unbuilt modules hidden, and the plan screen no longer sells them.
  - Icons and a splash screen.
  - `eas.json`, and a production build that refuses a missing or
    non-https API URL.
  - A crash screen instead of a white page.

### What launch preparation found

| Found | What would have happened |
|---|---|
| Every SECURITY DEFINER function belonged to the migrating role, and every table forces RLS on its owner. Only a superuser reads past that, and dev and CI migrated as one | On any managed Postgres, **nobody could sign in**: sign-in, refresh, password reset and shared invoices all failed. Found by migrating as an ordinary owner; seven API tests failed. The functions now belong to a `NOLOGIN` definer role, and CI migrates like production |
| The development role script granted default privileges for the role named `postgres` | A managed database's owner is rarely called that, so the app role would have had no rights at all |
| Presigned uploads signed neither type nor size | Any client could put an HTML page, or a gigabyte, at a key served as a progress photo |
| `EnsureBucket` created missing buckets, and never checked the policy its comment promised to | A typo in the bucket name would have created an empty second bucket. A public bucket would have started without complaint |
| `TRUST_PROXY` believed the left-most `X-Forwarded-For` | Behind a proxy, a guesser picks a fresh address for every sign-in attempt. Waiver signatures recorded the proxy's own address |
| STARTTLS only "when offered" | Anyone on the path strips the offer and reads reset links in the clear |
| An unset `APP_ENV` meant development | Forgetting one variable switched every production check off |
| The `.env.example` keys pass the 32-byte check | A server signing tokens with a key published in the repository |
| Caddy would not start with an empty ACME email | The first production deploy fails at the proxy |
| `minio/minio` no longer pulls | `make up` was broken for anyone without a cached image. Development and single-box installs now use SeaweedFS |
| A release build with no API URL falls back to `http://localhost` | The store build looks permanently offline on every phone |
| Nothing ever deleted expired refresh tokens, reset links or idempotency keys | Tables on the sign-in path grow for ever |

---

## Phase 1, as built (September 18–19)

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

**Offline-minted ids are honoured by the server.** A device with no signal
queues `workout.start` and the sets logged against it in one batch, all
referencing the id the device chose. The server keeps that id, because there is
no way for an outbox to rewrite a queued operation's foreign keys from an
earlier operation's result — and because a server-minted id would bring the
same client back from the next pull as a second person. UUIDv7 on both sides is
what makes this safe to do.

Supplied ids are inserted `ON CONFLICT (id) DO NOTHING` and then re-read under
RLS, so a retried push converges instead of failing on a primary key, and an id
belonging to another tenant is refused rather than silently reported as
created.

---

### Bugs the tests caught in Phase 1

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

#### The five the live run caught

Running the client against a real server for the first time found these. Every
one had passed both suites, because each side's tests asserted a wire format
that side had invented — the Go tests marshalled Go values, the TypeScript
tests asserted the JSON the client happened to send, and neither checked
`api/openapi.yaml`. Five of six offline operations were refused on the first
real push.

| Defect | Why it survived until now |
|---|---|
| The API published `format: date` but decoded into `time.Time`, which accepts only RFC 3339 — so every date a conforming client sent was rejected | The Go tests passed `time.Time` values, which marshal as timestamps. No test ever sent the format the spec promises |
| Offline-minted ids were discarded for workouts, clients, sets and measurements, so sets pushed in the same batch referenced a workout the server had never heard of | The integration test pushed **twice**, reading the server's id out of the first response — shaped around the flaw instead of exposing it. A real outbox drains in one batch |
| Sets came back from a pull under a different id and appeared **twice** on the floor logger, for ever | The server upserts sets on their natural key, so the server was consistent; only the device accumulated the duplicates |
| Selling a ten-session pack granted **100 credits**, and set revenue recognition to a tenth of the real price — so Deferred Revenue would never drain | `package_credits` is credits *per unit* and multiplies with quantity. The client test asserted the shape the client sent rather than what the server does with it |
| The web build could not make **a single network request**: `ApiClient` held `fetch` in a field and called it as a method, which a browser rejects as an illegal invocation | Every client test injects a `fetchImpl` stub, so the default path was never taken; the live test runs in Node, whose `fetch` does not care about its receiver. Only a real browser fails — and it surfaced as "No connection", so the app just looked permanently offline |

The credits one matters most: it corrupts the ledger, quietly, which is the
single thing this product exists to get right.

Driving the real UI in a browser also showed `synchronise` pulling *before*
pushing and then stopping — so a recorded payment left the invoice looking
unpaid until the next interval, up to 45 seconds later. It now pulls again
when a push changed something, because that is when the server decides
anything.

Two design self-corrections mid-build: `MarkDay` originally skipped
out-of-credit clients silently (a trainer would believe it worked); the credit
service originally had a broken `LinkJournalEntry` that could never execute,
replaced by a plan/apply split so the journal link is written at insert.

---


---

## What should be done next

### Before or soon after launch

- **A payment provider.** Plans are set by hand with `cmd/admin`. The
  `subscription.Provider` interface is waiting. Selling to businesses
  outside the app keeps App Store rule 3.1.3 on our side.
- **Data export.** The terms and deletion flow ask trainers to keep what
  they need for their taxes; a CSV of the books would make that one tap.
- **Abandoned uploads.** Unconfirmed media rows are never swept, and neither
  are their objects.
- **Rate limits across instances.** The limiters are per process, which is
  right for one API container and generous for several.
- **Crash reporting inside the app.** The server reports to Sentry; the
  app has a crash screen but sends nothing.

### Hidden until built

- **Calendar and booking.** Creating and rescheduling sessions is still
  server-only. It was a phone tab, so it is the first to bring back.
- **The programme builder.** The server side is complete (M6).
- **Analytics, insights, tasks, the shop.** The plan tiers already gate them.

### Carried over

- Responses still send dates as timestamps where the spec says `date`.
- PDF invoices ([ADR 0006](docs/adr/0006-invoice-pdf-rendering.md)).
- Recurring invoice drafts and overdue reminders, for the worker.

---

## Running it

[RUNNING.md](RUNNING.md) for development, [LAUNCH.md](LAUNCH.md) for
production.

```bash
make verify              # Go: format, vet, lint, unit tests
make test-integration    # RLS, ledger, journeys, deletion, jobs (needs Postgres)
make app-verify          # client: typecheck, tests, bundle every route
```

Two integration runs are worth knowing about. The first is the live S3
test, which is skipped unless a store is given:

```bash
STORAGE_TEST_ENDPOINT=localhost:9000 STORAGE_TEST_ACCESS_KEY=minioadmin \
STORAGE_TEST_SECRET_KEY=minioadmin go test ./internal/media -run RealStore
```

The second is the suite against a database set up like production: an
owner that is not a superuser, and the app role from
`deploy/provision.sql`. CI does this on every push.

Start with [ADR 0001 (ledger)](docs/adr/0001-double-entry-ledger.md) and
[ADR 0002 (tenant isolation)](docs/adr/0002-tenant-isolation-via-rls.md).
