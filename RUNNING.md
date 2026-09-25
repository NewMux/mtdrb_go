# Running CoachPulse

This is running it on your own machine. For production — servers, the app
stores, backups — see [LAUNCH.md](LAUNCH.md).

Two processes: the Go API with its Postgres, and the Expo app. The app is
offline-first, so once it has synced once it keeps working with the API
stopped — which is worth trying deliberately, because it is the whole point of
the product.

## 1. The backend

```bash
cp .env.example .env
make up      # Postgres + object storage via deploy/docker-compose.yml, then migrations
make run     # API on :8080
```

Check it:

```bash
curl localhost:8080/readyz     # {"status":"ready"}
```

<details>
<summary>No Docker?</summary>

Run Postgres yourself, create the RLS-bound role, then migrate:

```bash
createdb coachpulse
psql -d coachpulse -f deploy/postgres-init/00-app-role.sql
make migrate
```

The API checks its object-storage bucket at boot and exits if it cannot reach
it, so you also need something S3-shaped on `:9000`. The compose file runs
SeaweedFS there (MinIO no longer publishes server images); any S3-compatible
server will do, and with `STORAGE_CREATE_BUCKET=true` the API creates the
bucket itself. Media (progress photos) is the
only feature that needs it — nothing in the walkthrough below does.

</details>

### The worker

```bash
make worker   # scheduled jobs, against the local stack
```

Today it retires packs whose expiry date has passed, a quarter past midnight
in each practice's own time zone, moving their unused value out of Deferred
Revenue. Run as many as you like: one leads (a Postgres advisory lock) and
every job claims its run in `job_runs`, so nothing happens twice. It needs
only `DATABASE_URL`; `JOB_INTERVAL` (default `1m`) is how often it looks.

### Email, plans and the admin tool

**Password-reset email.** With no `SMTP_HOST` set, the API writes each email
to its log instead of sending it — the reset link is right there in the
terminal running `make run`. Production refuses to start without SMTP. The
link points at `APP_URL` (default `http://localhost:8081`, the Expo web dev
server).

| Variable | Default | Purpose |
|---|---|---|
| `SMTP_HOST`, `SMTP_PORT` | —, 587 | Mail server |
| `SMTP_TLS` | `starttls` (`implicit` on 465) | `starttls` refuses a server that does not offer it; `none` is for a local mail catcher and refused in production |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | — | Authentication, if the server wants it |
| `MAIL_FROM` | `CoachPulse <no-reply@coachpulse.io>` | Sender |
| `APP_URL` | `http://localhost:8081` | Where reset links point; https in production |
| `TRUST_PROXY` | `false` | Take the caller's address from the right-most `X-Forwarded-For` entry, the one the proxy in front appended. Only when every request arrives through that proxy |

**Plans.** Every new practice starts a fourteen-day trial of everything. When
it lapses the account is read-only: every read keeps working, and writes
(sync included) answer `402 subscription_inactive` until the plan changes.
There is no payment provider yet; plans are changed with `cmd/admin`, which
connects as the database owner and acts as `coachpulse_definer`, the one role
that reads across practices:

```bash
export OWNER_DATABASE_URL=postgres://postgres:postgres@localhost:5432/coachpulse?sslmode=disable
go run ./cmd/admin list-tenants
go run ./cmd/admin set-plan <tenant-id> pro -renews-on 2026-12-31
go run ./cmd/admin set-plan <tenant-id> starter      # 25 active clients, 1 location
go run ./cmd/admin extend-trial <tenant-id> 7
go run ./cmd/admin restore-tenant <tenant-id>       # undo a deletion within 30 days
```

**Deleting an account.** Settings → Security → *Delete account*. For an owner
it deactivates the practice at once; the worker removes it for good after
`ACCOUNT_PURGE_AFTER` (30 days). `restore-tenant` undoes it until then.

A change reaches the trainer's devices at their next token refresh (within
fifteen minutes) and their next sync.

## 2. The app

```bash
cd app
npm install
EXPO_PUBLIC_API_URL=http://192.168.1.50:8080 npx expo start
```

Scan the QR code with **Expo Go** on your phone.

**Use your machine's LAN IP, not `localhost`.** On a phone, `localhost` is the
phone — it will look like the API is down. Find yours with:

```bash
ipconfig getifaddr en0                          # macOS
hostname -I | awk '{print $1}'                  # Linux
```

Phone and computer have to be on the same network, and a firewall prompting
about port 8080 is a real thing to allow rather than dismiss.

### Or in a browser

```bash
cd app && npx expo start --web
```

The web build is subject to CORS, which the phone is not. The API allows
`http://localhost:8081` by default; anything else needs adding:

```bash
CORS_ORIGINS=http://localhost:8081,http://192.168.1.50:8081 make run
```

The layouts are built for a phone. The browser is the quicker look; the phone
is the honest one.

## 3. Walk through both journeys

Roughly ten minutes, and it exercises the ledger end to end.

### Journey A — a session becomes revenue

1. **Create an account.** *Start a new practice*, any email and password. Signup
   seeds your chart of accounts and 66 exercises, so nothing is empty.
2. **Add a client.** Clients → *Add client*. They exist the instant you save,
   with or without signal.
3. **Sell them a package.** Open the client → *Sell a package*. Ten sessions at
   50.00 issues an invoice and grants 10 credits. The credits appear on the
   profile.

   What just happened in the books: `DR Accounts Receivable / CR Deferred
   Revenue`. You are owed money and you owe ten sessions. **Nothing has been
   earned.**
4. **Book a session.** This one is still server-only — no screen for it yet:

   ```bash
   TOKEN=...   # from the app, or POST /v1/auth/login

   curl -s localhost:8080/v1/sessions/session-types \
     -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
     -d '{"name":"1-on-1","duration_minutes":60,"capacity":1,"credit_cost":1}'

   curl -s localhost:8080/v1/sessions \
     -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
     -d '{"session_type_id":"<id from above>","starts_at":"2026-09-19T09:00:00Z",
          "client_ids":["<client id>"],"location":"Studio"}'
   ```

   Pull to refresh **Today** and it appears.
5. **Mark it completed.** Tap the client on Today, then *Completed*.

   The credit burns and `DR Deferred Revenue / CR Training Revenue` posts. The
   balance drops to 9. That is the moment revenue is recognised — when the
   training was delivered, not when it was paid for.
6. **Run the client to zero** and mark one more. The app offers a renewal or an
   explicit overdraft. It will not quietly train someone for free.

### Journey B — getting paid

1. **Money** shows the invoice outstanding, with its ageing.
2. **Mark as paid** → amount prefilled with the balance → pick *Bank* → record.
3. Receivables go to zero.

   `DR Bank / CR Accounts Receivable`. Revenue is untouched — receiving money
   moves it between assets, it does not earn it. It was earned in step 5 above.

### The part worth testing properly

**Turn off wifi on the phone** and keep using it. Mark attendance, log a
workout, record a payment. Everything responds instantly and the badge in the
corner counts what is waiting.

Turn wifi back on. The queue drains, and the numbers the server owns — credit
balances, invoice status — update when it answers rather than before.

That asymmetry is deliberate. Your own choices apply immediately; anything the
server decides waits for the server, because showing "paid" and then
discovering it was refused is the kind of error that costs a trainer money.

## Tests

```bash
make verify              # Go: format, vet, lint, unit tests
make test-integration    # RLS, ledger and both journeys, against real Postgres
make app-verify          # client: typecheck, tests, bundle every route
```

There is also an end-to-end test that runs the client's real sync engine
against a running API:

```bash
make run &
cd app && COACHPULSE_LIVE_API=http://127.0.0.1:8080 npx jest live
```

It is skipped unless that variable is set, so CI stays hermetic. It is the test
that caught four bugs the stubbed suites on both sides had missed — see the
bugs table in [STATUS.md](STATUS.md).

## When something is wrong

| Symptom | Cause |
|---|---|
| App hangs on sign-in | `EXPO_PUBLIC_API_URL` points at `localhost`, or the phone is on another network |
| `check bucket coachpulse` at boot | Object storage is not running, or the bucket is missing: `make up` starts it, and `STORAGE_CREATE_BUCKET=true` (in `.env.example`) creates the bucket |
| CORS errors in a browser | Add that exact origin to `CORS_ORIGINS` |
| Today is empty | Nothing is booked for today — see step 4 |
| Badge says "needs attention" | An operation was refused. Tap it: the reason is in plain words, with *Try again* or *Discard* |

## Demo mode — no server at all

```bash
cd app
EXPO_PUBLIC_DEMO=1 npx expo start
```

Every launch seeds the device with Sam Rivera's studio in Dubai: twelve
clients over twelve weeks, a Wednesday with four sessions (one already done),
packs, invoices in AED (a renewal just sent, one on net-30 terms, one overdue)
and three weeks of logged training. It never calls the network. Useful for
showing someone the app without standing up a backend.

It is not a mock. The practice was recorded from the real API: `cmd/demo`
runs a scenario against a throwaway database with a fixed clock and writes
what a device would receive — a full sync pull and the answers to the reads
the app makes — to `app/src/demo/fixtures/recording.json`. The demo seeds the
device by pulling that through the real sync engine, and serves the recorded
reads, so every balance and earnings figure is real ledger output. Dates move
so the recorded Wednesday is always today, at the studio's wall-clock hours.

What it cannot do is anything the server decides next. Credits are not
really burned, revenue is not recognised and invoices are not settled. Offline
actions queue, exactly as on a phone in a basement; the online-only ones
(issuing an invoice) say this is the demo.

To change the practice, edit `cmd/demo/scenario.go` and re-record against the
local stack (the owner connection needs `CREATEDB`; the scratch database is
dropped afterwards). The same scenario gives the same file, byte for byte:

```bash
make demo-record
```

To build it for hosting under a path:

```bash
EXPO_PUBLIC_DEMO=1 npx expo export --platform web --clear --output-dir dist
```

Set `expo.experiments.baseUrl` in `app.json` to the path it will be served
from, or the router will 404 on load.

**Embedding it in an iframe needs one more thing.** A frame sandboxed without
`allow-same-origin` has an opaque origin, and `expo-sqlite` on web needs both a
Worker (refused: "cannot be accessed from origin 'null'") and OPFS (refused
outright). The app would sit on its launch spinner for ever.

So a demo build on the web uses sql.js's asm.js engine instead — no worker, no
WebAssembly to fetch, no storage handle, nothing the sandbox denies. It is
in-memory only, which for a seeded demo costs nothing: the seed runs again on
reload. See `app/src/demo/sqljs.ts`; the real app keeps `expo-sqlite`, which is
the right driver on a device, and the engine is absent from a non-demo build.

**And inline the bundle.** An embedded page may have no origin its subresources
can be fetched from, and the failure is invisible: the pre-rendered HTML holds
a loading spinner, so a bundle that never loads looks exactly like one that is
still loading. Fold the emitted JS into the HTML as a single `<script>`, inline
the navigation PNGs as `data:` URIs, `history.replaceState` to `/` before it
runs so the router does not depend on the hosting path, and add a timer that
replaces the spinner with the captured error if the app has not mounted. A
silent spinner is the one failure mode that tells nobody anything.
