-- Who the SECURITY DEFINER functions run as.
--
-- Signing in, refreshing a session, opening a shared invoice, resetting a
-- password and listing tenants for the worker all start without a tenant,
-- so they go through a handful of narrow SECURITY DEFINER functions that
-- read across tenants. They were owned by whoever ran the migrations, on the
-- assumption that the owner reads past row-level security. It does not:
-- every table FORCEs row-level security, which binds the owner too. Only a
-- superuser gets past it, and a superuser is what development and CI migrate
-- as. On a managed Postgres, where the owner is an ordinary role, every one
-- of those functions returned nothing, so nobody could sign in.
--
-- FORCE stays: it is what keeps an API accidentally connected as the owner
-- from seeing every tenant. The functions now belong to coachpulse_definer,
-- a role that cannot log in, and only that role gets a policy reading across
-- tenants. It is reachable only through the functions it owns, which are
-- small enough to audit in full, or by an operator who deliberately SETs
-- ROLE to it (cmd/admin does).
--
-- The migrating role needs CREATEROLE, which managed Postgres gives its
-- administrative user.

-- +goose Up
-- +goose StatementBegin
DO $$
DECLARE
  r record;
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'coachpulse_definer') THEN
    CREATE ROLE coachpulse_definer NOLOGIN NOBYPASSRLS;
  END IF;
  -- Handing a function to a role requires being able to act as it.
  EXECUTE format('GRANT coachpulse_definer TO %I', current_user);

  GRANT USAGE ON SCHEMA public TO coachpulse_definer;
  GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO coachpulse_definer;
  ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO coachpulse_definer;
  -- Writes bump the sync sequence.
  GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO coachpulse_definer;
  ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO coachpulse_definer;

  FOR r IN
    SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
     WHERE n.nspname = 'public' AND c.relkind = 'r' AND c.relrowsecurity
  LOOP
    EXECUTE format(
      'CREATE POLICY definer_access ON %I TO coachpulse_definer USING (true) WITH CHECK (true)',
      r.relname);
  END LOOP;

  -- A function's new owner must be able to create in its schema, for the
  -- moment of the hand-over only. A later migration adding a SECURITY
  -- DEFINER function repeats these three steps; the schema test fails until
  -- it does.
  GRANT CREATE ON SCHEMA public TO coachpulse_definer;
  FOR r IN
    SELECT p.oid::regprocedure AS fn FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
     WHERE n.nspname = 'public' AND p.prosecdef
  LOOP
    EXECUTE format('ALTER FUNCTION %s OWNER TO coachpulse_definer', r.fn);
  END LOOP;
  REVOKE CREATE ON SCHEMA public FROM coachpulse_definer;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
DECLARE
  r record;
BEGIN
  FOR r IN
    SELECT p.oid::regprocedure AS fn FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
     WHERE n.nspname = 'public' AND p.prosecdef
  LOOP
    EXECUTE format('ALTER FUNCTION %s OWNER TO %I', r.fn, current_user);
  END LOOP;
  FOR r IN
    SELECT tablename FROM pg_policies WHERE schemaname = 'public' AND policyname = 'definer_access'
  LOOP
    EXECUTE format('DROP POLICY definer_access ON %I', r.tablename);
  END LOOP;
  ALTER DEFAULT PRIVILEGES IN SCHEMA public
    REVOKE SELECT, INSERT, UPDATE, DELETE ON TABLES FROM coachpulse_definer;
  ALTER DEFAULT PRIVILEGES IN SCHEMA public
    REVOKE USAGE, SELECT ON SEQUENCES FROM coachpulse_definer;
  REVOKE ALL ON ALL TABLES IN SCHEMA public FROM coachpulse_definer;
  REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM coachpulse_definer;
  REVOKE USAGE ON SCHEMA public FROM coachpulse_definer;
  -- The role itself is cluster-wide and may serve another database, so it
  -- stays; nothing in this one refers to it any more.
END
$$;
-- +goose StatementEnd
