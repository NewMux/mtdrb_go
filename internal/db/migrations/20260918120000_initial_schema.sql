-- +goose Up
-- +goose StatementBegin

-- ---------------------------------------------------------------------------
-- Extensions
-- ---------------------------------------------------------------------------
-- pgcrypto backs column-level encryption for the narrow set of sensitive
-- free-text fields (see ADR 0003) and gen_random_bytes for share tokens.
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;

-- ---------------------------------------------------------------------------
-- Tenant context helpers
-- ---------------------------------------------------------------------------
-- Every row-level security policy routes through current_tenant_id(). Keeping
-- it in one function means the isolation rule is defined once rather than
-- copy-pasted into dozens of policies where one typo would open a hole.
--
-- The 'true' argument to current_setting makes a missing GUC return NULL
-- instead of raising. A NULL tenant matches no row, so a query that forgets to
-- establish tenant context returns nothing — it fails closed.

-- +goose StatementEnd
-- +goose StatementBegin
CREATE FUNCTION current_tenant_id() RETURNS uuid
LANGUAGE sql STABLE
AS $$
  SELECT NULLIF(current_setting('app.tenant_id', true), '')::uuid;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Portal sessions carry a client identity in addition to a tenant. Trainer
-- sessions leave this unset, which is how policies distinguish the two.
CREATE FUNCTION current_client_id() RETURNS uuid
LANGUAGE sql STABLE
AS $$
  SELECT NULLIF(current_setting('app.client_id', true), '')::uuid;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Maintains updated_at on write. Sync depends on this being reliable: a row
-- whose updated_at did not move is a row the client will never pull.
CREATE FUNCTION set_updated_at() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  NEW.updated_at := now();
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Monotonic per-row change counter driving incremental sync pulls. A single
-- global sequence is sufficient and avoids per-tenant sequence sprawl; clients
-- only ever see their own tenant's rows, so gaps are invisible and harmless.
CREATE SEQUENCE sync_seq AS bigint START 1;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION bump_sync_seq() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  NEW.server_seq := nextval('sync_seq');
  RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Tenants
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TABLE tenants (
  id                  uuid PRIMARY KEY,
  name                text        NOT NULL CHECK (length(btrim(name)) > 0),
  -- Single currency per tenant in Phase 1. The column exists on money-bearing
  -- rows too, so Phase 3 multi-currency is additive rather than a migration.
  default_currency    char(3)     NOT NULL DEFAULT 'USD',
  timezone            text        NOT NULL DEFAULT 'UTC',
  -- Operational defaults the scheduling and billing engines read.
  buffer_minutes      integer     NOT NULL DEFAULT 15 CHECK (buffer_minutes >= 0),
  allow_overdraft     boolean     NOT NULL DEFAULT false,
  no_show_is_billable boolean     NOT NULL DEFAULT true,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER tenants_set_updated_at
  BEFORE UPDATE ON tenants
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE tenants ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenants FORCE ROW LEVEL SECURITY;
-- +goose StatementEnd

-- +goose StatementBegin
-- A tenant may only ever see itself.
CREATE POLICY tenants_isolation ON tenants
  USING (id = current_tenant_id())
  WITH CHECK (id = current_tenant_id());
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Users (trainers and staff)
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TYPE user_role AS ENUM ('owner', 'trainer');
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE users (
  id            uuid PRIMARY KEY,
  tenant_id     uuid        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  email         citext      NOT NULL,
  -- argon2id encoded hash; never a reversible form.
  password_hash text        NOT NULL,
  display_name  text        NOT NULL CHECK (length(btrim(display_name)) > 0),
  role          user_role   NOT NULL DEFAULT 'owner',
  last_login_at timestamptz,
  deactivated_at timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
-- Email is globally unique so login can resolve a tenant before any tenant
-- context exists. Scoping it per tenant would make login ambiguous.
CREATE UNIQUE INDEX users_email_key ON users (email);
CREATE INDEX users_tenant_idx ON users (tenant_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER users_set_updated_at
  BEFORE UPDATE ON users
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE users FORCE ROW LEVEL SECURITY;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE POLICY users_isolation ON users
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Refresh tokens
-- ---------------------------------------------------------------------------
-- Rotating refresh tokens. Only a hash is stored, so a database read does not
-- yield usable credentials. Rotation is tracked as a family: presenting an
-- already-rotated token indicates theft and revokes the whole chain.
-- +goose StatementBegin
CREATE TABLE refresh_tokens (
  id          uuid PRIMARY KEY,
  tenant_id   uuid        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  user_id     uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  family_id   uuid        NOT NULL,
  token_hash  bytea       NOT NULL,
  expires_at  timestamptz NOT NULL,
  used_at     timestamptz,
  revoked_at  timestamptz,
  user_agent  text,
  created_at  timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX refresh_tokens_hash_key ON refresh_tokens (token_hash);
CREATE INDEX refresh_tokens_family_idx ON refresh_tokens (family_id);
CREATE INDEX refresh_tokens_user_idx ON refresh_tokens (user_id);
-- Sweeping expired tokens is a routine job; this keeps that scan cheap.
CREATE INDEX refresh_tokens_expires_idx ON refresh_tokens (expires_at);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE refresh_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE refresh_tokens FORCE ROW LEVEL SECURITY;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE POLICY refresh_tokens_isolation ON refresh_tokens
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Idempotency
-- ---------------------------------------------------------------------------
-- Every mutating endpoint records its key here. This is what makes the offline
-- outbox safe to replay: a repeated push returns the stored response instead
-- of posting a second journal entry (see ADR 0004).
-- +goose StatementBegin
CREATE TABLE idempotency_keys (
  tenant_id     uuid        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  key           text        NOT NULL CHECK (length(key) BETWEEN 8 AND 255),
  -- Guards against a client reusing one key for a different request.
  request_hash  bytea       NOT NULL,
  endpoint      text        NOT NULL,
  status_code   integer,
  response_body jsonb,
  -- NULL while the original request is still in flight; a concurrent replay
  -- sees the row and waits rather than racing it.
  completed_at  timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, key)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idempotency_keys_created_idx ON idempotency_keys (created_at);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE idempotency_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE idempotency_keys FORCE ROW LEVEL SECURITY;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE POLICY idempotency_keys_isolation ON idempotency_keys
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Grants
-- ---------------------------------------------------------------------------
-- The app role gets DML only. It cannot alter schema, and because it is
-- NOBYPASSRLS every policy above genuinely applies to it (see ADR 0002).
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'coachpulse_app') THEN
    GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO coachpulse_app;
    GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO coachpulse_app;
    GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO coachpulse_app;
  END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS refresh_tokens;
DROP TABLE IF EXISTS users;
DROP TYPE IF EXISTS user_role;
DROP TABLE IF EXISTS tenants;
DROP FUNCTION IF EXISTS bump_sync_seq();
DROP SEQUENCE IF EXISTS sync_seq;
DROP FUNCTION IF EXISTS set_updated_at();
DROP FUNCTION IF EXISTS current_client_id();
DROP FUNCTION IF EXISTS current_tenant_id();
-- +goose StatementEnd
