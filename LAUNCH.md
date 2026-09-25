# Launching CoachPulse

Everything needed to run CoachPulse in production and put the app in the
stores. [RUNNING.md](RUNNING.md) covers running it on your own machine.

## What you have to supply

These need your accounts or your decisions, so they are not in the repository.

| | What | Notes |
|---|---|---|
| ☐ | A domain | `app.example.com`, pointed at the server. Add `files.app.example.com` for a single-box install |
| ☐ | A server with Docker | 2 vCPU and 4 GB is plenty to start |
| ☐ | Postgres 16 | Managed is recommended (RDS, Cloud SQL, Neon, Supabase…), with point-in-time recovery switched on |
| ☐ | An S3-compatible bucket | Cloudflare R2, S3, or the bundled SeaweedFS on a single box |
| ☐ | An SMTP account | Postmark, SES, Resend, Mailgun… Used for password resets |
| ☐ | Legal text reviewed | `/legal/privacy` and `/legal/terms` are drafts written from what the software does. Have a lawyer read them, then fill in the `LEGAL_*` settings |
| ☐ | Apple Developer Program | $99 a year, for the App Store |
| ☐ | Google Play Console | $25 once, for Play |
| ☐ | An Expo account | For EAS Build and Submit |
| ☐ | Sentry (optional) | `SENTRY_DSN`, for error reports from the API and worker |

## 1. The database

The API never connects as the database's owner. It uses `coachpulse_app`,
which row-level security binds, so a bug that forgets to filter by practice
returns nothing instead of another trainer's clients.

1. Create an empty database, for example `coachpulse`. The user that owns it
   (a managed Postgres's admin user) runs migrations. It needs `CREATEROLE`,
   which managed providers give their admin user. It does **not** need to be
   a superuser, and ideally is not.
2. Create the app role, **before the first migration**:

   ```bash
   psql "$OWNER_DATABASE_URL" -v app_password="$(openssl rand -hex 24)" -f deploy/provision.sql
   ```

   Keep that password; it goes into `DATABASE_URL`. Running the script again
   only rotates the password.
3. Migrations run by themselves on every deploy (step 3). They create a
   second role, `coachpulse_definer`, which cannot log in. It owns the few
   functions that must read across practices: sign-in, token refresh,
   shared invoices and the worker's tenant list.

**Backups.** Turn on the provider's point-in-time recovery, and keep backups
for **no more than 35 days**. The privacy policy promises that a deleted
practice is gone from backups within that time. Restore into a scratch
database at least once before launch; a backup never restored is a hope.

## 2. Object storage

- **R2 or S3.** Create a private bucket (no public access, no public bucket
  policy; the API refuses to start on a bucket anyone can read). Create a key
  limited to that bucket with object read, write and delete, and list on the
  bucket. The API never creates the bucket in production.
- **CORS.** Browsers upload straight to the bucket, so allow your domain:

  ```json
  [{ "AllowedOrigins": ["https://app.example.com"],
     "AllowedMethods": ["GET", "PUT"],
     "AllowedHeaders": ["Content-Type", "Content-Length"],
     "MaxAgeSeconds": 3600 }]
  ```

## 3. Deploy

```bash
git clone https://github.com/NewMux/mtdrb_go && cd mtdrb_go
cp deploy/.env.production.example deploy/.env.production
$EDITOR deploy/.env.production        # every value; secrets from openssl rand -hex 32
docker compose --env-file deploy/.env.production -f deploy/compose.prod.yml up -d --build
```

What starts, in order:

1. `migrate` applies migrations as the owner, then exits.
2. `api` and `worker` start only if the migrations succeeded.
3. `web` (Caddy) gets a certificate for `DOMAIN`, serves the web app, and
   proxies the API on the same origin.

The web build's sign-in cookie depends on that shared origin, so do not put
the API on a different host.

The API refuses to start on a production misconfiguration and lists every
problem at once. It checks for:

- the example keys, or the same key used twice;
- a database connection that could fall back to plaintext;
- localhost anywhere, or plain http;
- a missing SMTP server, or mail sent unencrypted;
- missing legal details.

Read the log (`docker compose … logs api`) and fix what it names.

**Check it:**

```bash
curl https://app.example.com/readyz          # {"status":"ready"}
curl -I https://app.example.com/legal/privacy
```

Then, in a browser:

1. Create a practice and go through onboarding.
2. Add a client and sell a pack.
3. Mark the invoice paid.
4. Request a password reset and check the email arrives.
5. Delete that test account (Settings → Security).

**Updating.** Run `git pull`, then the same `up -d --build`. Migrations run
first; if one fails, the old containers keep serving. Every migration can be
rolled back:

```bash
docker compose --env-file deploy/.env.production -f deploy/compose.prod.yml run --rm migrate migrate down
```

**Pre-built images.** Tagging a release `v1.2.3` makes CI publish
`ghcr.io/newmux/coachpulse-server:v1.2.3`. Set `SERVER_IMAGE` to it to skip
building the server on the machine. The web image is always built on the
machine, because it has the domain compiled in.

### A single machine for everything

Add `--profile local` to run Postgres and SeaweedFS (an S3-compatible store)
on the same machine. The bottom of `.env.production.example` lists the
settings this needs. The database is set up the way a managed one is: an
owner that is not a superuser, and the app role from `provision.sql`.

In return, backups are yours:

```bash
# nightly, off the machine
docker compose -f deploy/compose.prod.yml exec -T postgres \
  pg_dump -U postgres -Fc coachpulse > coachpulse-$(date +%F).dump
```

Also back up the `seaweedfs-data` volume. Delete dumps after 35 days.

## 4. Running it

**Plans are sold by invoice** for now; there is no payment provider. A new
practice gets a 14-day trial, after which it is read-only (nothing is lost)
until you set a plan:

```bash
C="docker compose --env-file deploy/.env.production -f deploy/compose.prod.yml run --rm api"
$C admin list-tenants
$C admin set-plan <tenant-id> pro -renews-on 2026-12-31
$C admin extend-trial <tenant-id> 14
$C admin restore-tenant <tenant-id>      # undo an owner's deletion within 30 days
```

**Account deletion** is immediate for sign-in, and permanent after
`ACCOUNT_PURGE_AFTER` (30 days). At that point the worker removes every row
and every file. Requests by email go through the same flow: sign in as
support, or ask the owner to do it.

**The worker** runs:

- pack expiry, just after midnight in each practice's own time zone;
- housekeeping at 03:00, which deletes expired sessions, reset links and
  old idempotency keys;
- the purge of deleted practices at 04:00.

Several workers can run at once; one leads.

**Watching it.** Point an uptime check at `/readyz`. Logs are JSON on stdout,
with medical notes, tokens and bank details redacted. With `SENTRY_DSN` set,
every error-level log line from the API and worker is also sent to Sentry,
redacted the same way.

## 5. The app stores

### Build

```bash
cd app
npm install -g eas-cli
eas login
eas init                                 # creates the project; note its id
export EAS_PROJECT_ID=<id> EXPO_OWNER=<your expo account>
$EDITOR eas.json                         # production EXPO_PUBLIC_API_URL → https://app.example.com
eas build --platform all --profile production
eas submit --platform ios --latest
eas submit --platform android --latest
```

A production build stops if `EXPO_PUBLIC_API_URL` is missing, not https, or
still the example. Build numbers go up by themselves.

The icon is `app/assets/icon.svg`. Edit it and run
`node scripts/render-icons.mjs` to regenerate every size.

### Store listings

| | App Store Connect | Play Console |
|---|---|---|
| Privacy policy URL | `https://app.example.com/legal/privacy` | same |
| Account deletion | In the app: Settings → Security → Delete account | Same, plus the web link Play asks for: `https://app.example.com/dashboard/settings/security` |
| Sign-in for review | A demo practice with a few clients (create one on production and put its login in the review notes) | same, under App access |
| Payments | Plans are billed to businesses by invoice outside the app, with no purchase inside it. Say so in the review notes | No in-app products |
| Encryption | Exempt (HTTPS only); already declared in the build | — |

**Data declarations** (App Privacy, Play Data safety). The app collects:

- contact info (name, email);
- health and fitness data, which trainers record about their clients;
- photos (progress photos);
- financial info (invoices and payments recorded; no card data);
- user content;
- identifiers (account ID);
- diagnostics (crash logs, if Sentry is on).

None of it is used for tracking or advertising, none is sold, all of it is
encrypted in transit, and all of it can be deleted.

## Before you announce it

- [ ] Legal text reviewed and the `LEGAL_*` values filled in.
- [ ] A backup restored into a scratch database.
- [ ] Password reset email received from production.
- [ ] Uptime check on `/readyz`; Sentry receiving a test error.
- [ ] A test practice deleted, and gone after the grace period (or check
      `ACCOUNT_PURGE_AFTER` works by setting it to `1h` on staging).
- [ ] The app from TestFlight / internal testing signed in against production.

## Not in this launch

These are deliberately out, and hidden from the app so nothing half-built
shows:

- calendar booking;
- the programme builder;
- analytics, insights, tasks and the shop;
- a payment provider;
- data export.

`EXPO_PUBLIC_SHOW_UNBUILT=1` shows the placeholders in a build, for demos.
[STATUS.md](STATUS.md) has the full list of what is next.
