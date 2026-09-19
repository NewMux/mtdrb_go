# Running CoachPulse

Two processes: the Go API with its Postgres, and the Expo app. The app is
offline-first, so once it has synced once it keeps working with the API
stopped — which is worth trying deliberately, because it is the whole point of
the product.

## 1. The backend

```bash
make up      # Postgres + MinIO via deploy/docker-compose.yml, then migrations
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
it, so you also need something S3-shaped on `:9000`. MinIO is the intended one;
any S3-compatible server will satisfy the check. Media (progress photos) is the
only feature that needs it — nothing in the walkthrough below does.

</details>

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
| `check bucket coachpulse` at boot | Object storage is not running; `make up` starts MinIO |
| CORS errors in a browser | Add that exact origin to `CORS_ORIGINS` |
| Today is empty | Nothing is booked for today — see step 4 |
| Badge says "needs attention" | An operation was refused. Tap it: the reason is in plain words, with *Try again* or *Discard* |

## Demo mode — no server at all

```bash
cd app
EXPO_PUBLIC_DEMO=1 npx expo start
```

Seeds a practice into the device database on first launch — three clients, a
day of sessions, packages, invoices, last week's training — and never calls the
network. Useful for showing someone the app without standing up a backend.

It is not a mock: every screen already reads local SQLite, so this is the real
app with the sync engine idle. What it cannot do is anything the server
decides. Credits are not really burned, revenue is not recognised and invoices
are not settled — those happen in the ledger, behind the API. The screens say
"waiting on the server" and nothing ever answers, which is exactly what a phone
in a basement sees.

To build it for hosting under a path:

```bash
EXPO_PUBLIC_DEMO=1 npx expo export --platform web --clear --output-dir dist
```

Set `expo.experiments.baseUrl` in `app.json` to the path it will be served
from, or the router will 404 on load.
