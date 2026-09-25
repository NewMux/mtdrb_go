#!/bin/sh
# First start of the "local" profile's Postgres. It is set up the way a
# managed Postgres is: the database belongs to an ordinary role, not the
# superuser, so row-level security binds it exactly as it will elsewhere.
# Then the app role is provisioned with the same script production uses.
set -eu
psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d postgres \
  -v owner_password="$OWNER_DB_PASSWORD" <<'SQL'
SELECT format('CREATE ROLE coachpulse_owner LOGIN CREATEROLE PASSWORD %L', :'owner_password') \gexec
CREATE DATABASE coachpulse OWNER coachpulse_owner;
SQL
psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d coachpulse -c "ALTER SCHEMA public OWNER TO coachpulse_owner"
psql -v ON_ERROR_STOP=1 -U coachpulse_owner -d coachpulse \
  -v app_password="$APP_DB_PASSWORD" -f /provision.sql
