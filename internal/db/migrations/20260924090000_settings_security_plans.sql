-- Settings, account security and plan state: what makes the product a thing a
-- trainer can pay for and trust with their business.
--
-- Settings live on the tenant row. A trainer's practice has one of each —
-- one language, one working week, one set of targets — and a separate
-- key/value table would trade every CHECK below for stringly-typed reads.
-- The shapes that genuinely vary (weekly availability, targets, which
-- automations run) are jsonb, validated in Go where their rules live.
--
-- The tenant row joins the sync sequence, so a device mirrors the settings it
-- needs offline: the week start the calendar draws, the working hours that
-- bound a booking, and the plan state that decides whether the outbox may send.
--
-- Plan state is only state. No payment provider writes it yet; cmd/admin does,
-- and a provider's webhook will write the same columns later. A trial is
-- fourteen days of everything, and a lapsed one goes read-only rather than
-- locking anyone out of their own records.
--
-- Two-factor secrets are encrypted with the column key, like medical notes;
-- recovery codes and password-reset tokens are stored as hashes only, like
-- refresh tokens, so a database read yields nothing that signs anyone in.

-- +goose Up
-- +goose StatementBegin
ALTER TABLE tenants
  -- ISO 3166 country of the practice. Decides the week's shape (a Saudi week
  -- starts on Sunday) and, later, which VAT rules apply. Unknown for tenants
  -- that predate it.
  ADD COLUMN country char(2) CHECK (country ~ '^[A-Z]{2}$'),
  ADD COLUMN language text NOT NULL DEFAULT 'en' CHECK (language IN ('en', 'ar')),
  -- What invoices and receipts are written in; a UAE tax invoice is commonly
  -- bilingual.
  ADD COLUMN document_language text NOT NULL DEFAULT 'en'
    CHECK (document_language IN ('en', 'ar', 'bilingual')),
  ADD COLUMN digits text NOT NULL DEFAULT 'latn' CHECK (digits IN ('latn', 'arab')),
  -- 0 = Sunday … 6 = Saturday, as in JavaScript and Go.
  ADD COLUMN week_start smallint NOT NULL DEFAULT 1 CHECK (week_start BETWEEN 0 AND 6),
  -- {"1": [["06:00","12:00"],["16:00","20:00"]], …} keyed by weekday. The
  -- denominator for utilisation: an hour nobody could book is not an empty one.
  ADD COLUMN working_hours jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN targets jsonb NOT NULL DEFAULT '{}'::jsonb,
  ADD COLUMN automations jsonb NOT NULL DEFAULT '{}'::jsonb,
  -- How long a device stays signed in without being used.
  ADD COLUMN session_timeout_days integer NOT NULL DEFAULT 30
    CHECK (session_timeout_days BETWEEN 1 AND 365),
  ADD COLUMN plan text NOT NULL DEFAULT 'trial' CHECK (plan IN ('trial', 'starter', 'pro')),
  ADD COLUMN plan_status text NOT NULL DEFAULT 'active'
    CHECK (plan_status IN ('active', 'past_due', 'cancelled')),
  -- Signup sets this from the service clock; the default is what starts a
  -- fresh trial for tenants that predate plans, rather than lapsing them the
  -- moment this runs.
  ADD COLUMN trial_ends_at timestamptz DEFAULT now() + interval '14 days',
  ADD COLUMN plan_renews_on date,
  ADD COLUMN cancel_at_period_end boolean NOT NULL DEFAULT false,
  ADD COLUMN server_seq bigint NOT NULL DEFAULT nextval('sync_seq'),
  ADD CONSTRAINT tenants_trial_has_end CHECK (plan <> 'trial' OR trial_ends_at IS NOT NULL);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX tenants_seq_idx ON tenants (server_seq);
CREATE TRIGGER tenants_bump_seq
  BEFORE INSERT OR UPDATE ON tenants FOR EACH ROW EXECUTE FUNCTION bump_sync_seq();
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE users
  -- pgp_sym_encrypt(base32 secret, column key). Set on setup, before the
  -- trainer has proved they can read it; totp_enabled_at marks that they have.
  ADD COLUMN totp_secret_encrypted bytea,
  ADD COLUMN totp_enabled_at timestamptz,
  -- The last 30-second step a code was accepted for, so a code overheard at
  -- the front desk cannot be replayed within its window.
  ADD COLUMN totp_last_step bigint,
  ADD COLUMN password_changed_at timestamptz;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE mfa_recovery_codes (
  id         uuid PRIMARY KEY,
  tenant_id  uuid        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  user_id    uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  code_hash  bytea       NOT NULL,
  used_at    timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id, code_hash)
);
CREATE INDEX mfa_recovery_codes_tenant_idx ON mfa_recovery_codes (tenant_id);
ALTER TABLE mfa_recovery_codes ENABLE ROW LEVEL SECURITY;
ALTER TABLE mfa_recovery_codes FORCE ROW LEVEL SECURITY;
CREATE POLICY mfa_recovery_codes_isolation ON mfa_recovery_codes
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE password_resets (
  id         uuid PRIMARY KEY,
  tenant_id  uuid        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  user_id    uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash bytea       NOT NULL UNIQUE,
  expires_at timestamptz NOT NULL,
  used_at    timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX password_resets_tenant_idx ON password_resets (tenant_id);
CREATE INDEX password_resets_user_idx ON password_resets (user_id);
ALTER TABLE password_resets ENABLE ROW LEVEL SECURITY;
ALTER TABLE password_resets FORCE ROW LEVEL SECURITY;
CREATE POLICY password_resets_isolation ON password_resets
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- +goose StatementBegin
-- The reset link has the same bootstrapping problem as a refresh token: its
-- hash is all the caller holds, and the tenant is what must be discovered.
-- Same shape as auth_lookup_refresh_token, and for the same reasons.
CREATE FUNCTION auth_lookup_password_reset(p_token_hash bytea)
RETURNS TABLE (
  reset_id   uuid,
  tenant_id  uuid,
  user_id    uuid,
  expires_at timestamptz,
  used_at    timestamptz
)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
  SELECT id, tenant_id, user_id, expires_at, used_at
    FROM password_resets
   WHERE token_hash = p_token_hash;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
  REVOKE EXECUTE ON FUNCTION auth_lookup_password_reset(bytea) FROM PUBLIC;
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'coachpulse_app') THEN
    GRANT EXECUTE ON FUNCTION auth_lookup_password_reset(bytea) TO coachpulse_app;
  END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP FUNCTION IF EXISTS auth_lookup_password_reset(bytea);
DROP TABLE IF EXISTS password_resets;
DROP TABLE IF EXISTS mfa_recovery_codes;
ALTER TABLE users
  DROP COLUMN IF EXISTS totp_secret_encrypted,
  DROP COLUMN IF EXISTS totp_enabled_at,
  DROP COLUMN IF EXISTS totp_last_step,
  DROP COLUMN IF EXISTS password_changed_at;
DROP TRIGGER IF EXISTS tenants_bump_seq ON tenants;
DROP INDEX IF EXISTS tenants_seq_idx;
ALTER TABLE tenants
  DROP CONSTRAINT IF EXISTS tenants_trial_has_end,
  DROP COLUMN IF EXISTS country,
  DROP COLUMN IF EXISTS language,
  DROP COLUMN IF EXISTS document_language,
  DROP COLUMN IF EXISTS digits,
  DROP COLUMN IF EXISTS week_start,
  DROP COLUMN IF EXISTS working_hours,
  DROP COLUMN IF EXISTS targets,
  DROP COLUMN IF EXISTS automations,
  DROP COLUMN IF EXISTS session_timeout_days,
  DROP COLUMN IF EXISTS plan,
  DROP COLUMN IF EXISTS plan_status,
  DROP COLUMN IF EXISTS trial_ends_at,
  DROP COLUMN IF EXISTS plan_renews_on,
  DROP COLUMN IF EXISTS cancel_at_period_end,
  DROP COLUMN IF EXISTS server_seq;
-- +goose StatementEnd
