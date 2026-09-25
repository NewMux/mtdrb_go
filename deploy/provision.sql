-- Provision the roles CoachPulse runs as, on a production database.
--
-- Run once, as the database's owner (the role that will run migrations),
-- BEFORE the first migration:
--
--   psql "$OWNER_DATABASE_URL" -v app_password="$APP_DB_PASSWORD" -f deploy/provision.sql
--
-- The owner needs CREATEROLE, which a managed Postgres gives its
-- administrative user. Running it again only changes the password, so it is
-- also how the app role's password is rotated.
--
-- Why before the migrations: they grant the app role EXECUTE on the few
-- cross-tenant functions, and withhold DELETE on tenants, only if the role
-- exists when they run. Table rights come from the default privileges set
-- below, which cover whatever the owner creates from now on.
--
-- deploy/postgres-init/00-app-role.sql is the development equivalent, with
-- a password everyone knows. This file has none.

\set ON_ERROR_STOP on

\if :{?app_password}
\else
  \echo 'usage: psql "$OWNER_DATABASE_URL" -v app_password=... -f deploy/provision.sql'
  \quit
\endif

SELECT length(:'app_password') < 24 AS too_short \gset
\if :too_short
  \echo 'the app password must be at least 24 characters; generate one with: openssl rand -hex 24'
  \quit
\endif

-- The API's role: may log in, may not see past row-level security.
SELECT format('CREATE ROLE coachpulse_app LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE PASSWORD %L',
              :'app_password')
 WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'coachpulse_app') \gexec
SELECT format('ALTER ROLE coachpulse_app PASSWORD %L', :'app_password') \gexec

SELECT format('GRANT CONNECT ON DATABASE %I TO coachpulse_app', current_database()) \gexec
GRANT USAGE ON SCHEMA public TO coachpulse_app;

-- For everything the migrating role (this session's user) creates.
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO coachpulse_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO coachpulse_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT EXECUTE ON FUNCTIONS TO coachpulse_app;

\echo 'coachpulse_app is ready. Next: DATABASE_URL="$OWNER_DATABASE_URL" migrate up'
