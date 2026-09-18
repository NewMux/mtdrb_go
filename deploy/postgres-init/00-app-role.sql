-- The API must never be able to see across tenants, so it connects as a role
-- that is neither superuser nor BYPASSRLS. Migrations run as the owner
-- (postgres); the running server uses coachpulse_app, and every row-level
-- security policy therefore actually applies to it.
--
-- Production provisions the equivalent role; this file only bootstraps local
-- development so the two environments fail the same way.

CREATE ROLE coachpulse_app WITH LOGIN PASSWORD 'coachpulse_app' NOBYPASSRLS;

GRANT CONNECT ON DATABASE coachpulse TO coachpulse_app;

\connect coachpulse

GRANT USAGE ON SCHEMA public TO coachpulse_app;

-- Table-level rights are granted as migrations create objects; these defaults
-- cover everything the owner creates from here on.
ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO coachpulse_app;
ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO coachpulse_app;
ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA public
  GRANT EXECUTE ON FUNCTIONS TO coachpulse_app;

-- pgcrypto backs column-level encryption for the narrow set of genuinely
-- sensitive free-text fields (emergency medical notes).
CREATE EXTENSION IF NOT EXISTS pgcrypto;
