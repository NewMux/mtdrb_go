-- +goose Up
-- Client records, intake compliance and body measurements.
--
-- Note the policy shape used throughout this file:
--
--   tenant_id = current_tenant_id()
--   AND (current_client_id() IS NULL OR <this row belongs to that client>)
--
-- A trainer session leaves app.client_id unset and sees the whole roster. A
-- portal session binds it and can reach only its own row. The PRD requires
-- that clients have zero access to other clients' records; this makes that
-- structural rather than something each handler must remember.

-- +goose StatementBegin
CREATE TYPE client_status AS ENUM ('lead', 'active', 'paused', 'archived');
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE clients (
  id                uuid PRIMARY KEY,
  tenant_id         uuid          NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  full_name         text          NOT NULL CHECK (length(btrim(full_name)) > 0),
  email             citext,
  phone             text,
  date_of_birth     date,
  status            client_status NOT NULL DEFAULT 'active',

  emergency_contact_name  text,
  emergency_contact_phone text,

  -- Encrypted with pgcrypto under the application's column key (ADR 0003).
  -- Stored as bytea and never indexed: this is the one field where losing
  -- queryability is the right trade.
  medical_notes_encrypted bytea,

  -- Per-client override of the tenant default. Lets a trainer extend credit to
  -- a client they trust without opening overdraft for everyone.
  allow_overdraft   boolean,
  -- Default per-session rate in minor units, used to price renewal invoices.
  default_rate_minor bigint CHECK (default_rate_minor IS NULL OR default_rate_minor >= 0),

  notes             text          NOT NULL DEFAULT '',
  created_at        timestamptz   NOT NULL DEFAULT now(),
  updated_at        timestamptz   NOT NULL DEFAULT now(),
  -- Soft delete: a client with financial history cannot be erased without
  -- orphaning journal entries, so removal hides rather than deletes.
  deleted_at        timestamptz,
  server_seq        bigint        NOT NULL DEFAULT nextval('sync_seq')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX clients_tenant_idx ON clients (tenant_id) WHERE deleted_at IS NULL;
CREATE INDEX clients_tenant_seq_idx ON clients (tenant_id, server_seq);
CREATE INDEX clients_name_idx ON clients (tenant_id, full_name);
CREATE UNIQUE INDEX clients_tenant_email_key ON clients (tenant_id, email)
  WHERE email IS NOT NULL AND deleted_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER clients_set_updated_at
  BEFORE UPDATE ON clients FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER clients_bump_seq
  BEFORE INSERT OR UPDATE ON clients FOR EACH ROW EXECUTE FUNCTION bump_sync_seq();
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE clients ENABLE ROW LEVEL SECURITY;
ALTER TABLE clients FORCE ROW LEVEL SECURITY;
CREATE POLICY clients_isolation ON clients
  USING (tenant_id = current_tenant_id()
         AND (current_client_id() IS NULL OR id = current_client_id()))
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Tags
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TABLE tags (
  id         uuid PRIMARY KEY,
  tenant_id  uuid        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  name       text        NOT NULL CHECK (length(btrim(name)) > 0),
  colour     text,
  created_at timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX tags_tenant_name_key ON tags (tenant_id, lower(name));
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE tags ENABLE ROW LEVEL SECURITY;
ALTER TABLE tags FORCE ROW LEVEL SECURITY;
CREATE POLICY tags_isolation ON tags
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE client_tags (
  tenant_id  uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  client_id  uuid NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
  tag_id     uuid NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (client_id, tag_id)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX client_tags_tag_idx ON client_tags (tenant_id, tag_id);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE client_tags ENABLE ROW LEVEL SECURITY;
ALTER TABLE client_tags FORCE ROW LEVEL SECURITY;
CREATE POLICY client_tags_isolation ON client_tags
  USING (tenant_id = current_tenant_id()
         AND (current_client_id() IS NULL OR client_id = current_client_id()))
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Media objects
-- ---------------------------------------------------------------------------
-- Rows describe objects in private storage. The bytes never pass through the
-- API; clients upload and download through short-lived presigned URLs issued
-- after an authorisation check (ADR 0003).
-- +goose StatementBegin
CREATE TYPE media_kind AS ENUM ('progress_photo', 'receipt', 'signature', 'form_check_video', 'exercise_demo', 'other');
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TYPE media_sensitivity AS ENUM ('standard', 'sensitive');
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE media_objects (
  id           uuid PRIMARY KEY,
  tenant_id    uuid              NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  -- Object key in the bucket. Prefixed by tenant so a misconfigured policy
  -- still cannot produce a cross-tenant key collision.
  storage_key  text              NOT NULL,
  kind         media_kind        NOT NULL,
  sensitivity  media_sensitivity NOT NULL DEFAULT 'standard',
  content_type text              NOT NULL,
  byte_size    bigint            NOT NULL CHECK (byte_size >= 0),
  -- Set when the object depicts a specific client, which drives both the
  -- portal policy below and deletion when a client is purged.
  client_id    uuid REFERENCES clients(id) ON DELETE CASCADE,
  uploaded_by  uuid REFERENCES users(id),
  -- NULL until the client confirms the upload completed; unconfirmed rows are
  -- swept, so an abandoned upload does not leave a dangling reference.
  confirmed_at timestamptz,
  created_at   timestamptz       NOT NULL DEFAULT now(),
  deleted_at   timestamptz
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX media_objects_key_key ON media_objects (storage_key);
CREATE INDEX media_objects_client_idx ON media_objects (tenant_id, client_id, kind);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE media_objects ENABLE ROW LEVEL SECURITY;
ALTER TABLE media_objects FORCE ROW LEVEL SECURITY;
CREATE POLICY media_objects_isolation ON media_objects
  USING (tenant_id = current_tenant_id()
         AND (current_client_id() IS NULL OR client_id = current_client_id()))
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- PAR-Q and waivers
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TABLE parq_responses (
  id           uuid PRIMARY KEY,
  tenant_id    uuid        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  client_id    uuid        NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
  -- The seven standard PAR-Q answers plus any follow-up detail, kept as jsonb
  -- because the questionnaire's wording is revised periodically and old
  -- responses must remain readable exactly as they were answered.
  answers      jsonb       NOT NULL,
  -- True if any answer indicates the client should seek medical clearance.
  requires_clearance boolean NOT NULL DEFAULT false,
  cleared_at   timestamptz,
  completed_at timestamptz NOT NULL DEFAULT now(),
  created_at   timestamptz NOT NULL DEFAULT now(),
  server_seq   bigint      NOT NULL DEFAULT nextval('sync_seq')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX parq_responses_client_idx ON parq_responses (tenant_id, client_id, completed_at DESC);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE parq_responses ENABLE ROW LEVEL SECURITY;
ALTER TABLE parq_responses FORCE ROW LEVEL SECURITY;
CREATE POLICY parq_responses_isolation ON parq_responses
  USING (tenant_id = current_tenant_id()
         AND (current_client_id() IS NULL OR client_id = current_client_id()))
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- +goose StatementBegin
-- A waiver's text is versioned and immutable once signed: a signature is only
-- meaningful against the exact wording that was agreed to.
CREATE TABLE waivers (
  id         uuid PRIMARY KEY,
  tenant_id  uuid        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  title      text        NOT NULL CHECK (length(btrim(title)) > 0),
  body       text        NOT NULL CHECK (length(btrim(body)) > 0),
  version    integer     NOT NULL DEFAULT 1 CHECK (version > 0),
  is_active  boolean     NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX waivers_tenant_title_version_key ON waivers (tenant_id, lower(title), version);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE waivers ENABLE ROW LEVEL SECURITY;
ALTER TABLE waivers FORCE ROW LEVEL SECURITY;
CREATE POLICY waivers_isolation ON waivers
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE waiver_signatures (
  id          uuid PRIMARY KEY,
  tenant_id   uuid        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  waiver_id   uuid        NOT NULL REFERENCES waivers(id),
  client_id   uuid        NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
  -- The drawn signature image, held in private storage like any other media.
  signature_media_id uuid REFERENCES media_objects(id),
  -- Snapshot of the exact text signed. Kept even though waiver_id points at
  -- the row, because a waiver row could later be archived or edited by an
  -- owner and the evidentiary value is in what was actually shown.
  signed_body text        NOT NULL,
  signed_name text        NOT NULL,
  signed_at   timestamptz NOT NULL DEFAULT now(),
  -- Recorded for evidentiary weight; inet is the right type for an address.
  signed_ip   inet,
  created_at  timestamptz NOT NULL DEFAULT now(),
  server_seq  bigint      NOT NULL DEFAULT nextval('sync_seq')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX waiver_signatures_client_idx ON waiver_signatures (tenant_id, client_id);
CREATE UNIQUE INDEX waiver_signatures_unique ON waiver_signatures (client_id, waiver_id);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE waiver_signatures ENABLE ROW LEVEL SECURITY;
ALTER TABLE waiver_signatures FORCE ROW LEVEL SECURITY;
CREATE POLICY waiver_signatures_isolation ON waiver_signatures
  USING (tenant_id = current_tenant_id()
         AND (current_client_id() IS NULL OR client_id = current_client_id()))
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Biometrics
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TABLE biometric_entries (
  id             uuid PRIMARY KEY,
  tenant_id      uuid        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  client_id      uuid        NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
  measured_on    date        NOT NULL,
  -- Grams, so a kilogram-or-pound display choice is a presentation concern and
  -- the stored value never rounds.
  weight_grams   integer CHECK (weight_grams IS NULL OR weight_grams > 0),
  -- Basis points: 1550 is 15.50%. Integers again, for the same reason.
  body_fat_bp    integer CHECK (body_fat_bp IS NULL OR (body_fat_bp >= 0 AND body_fat_bp <= 10000)),
  -- Circumferences in millimetres, keyed by site (waist, hips, arm...). Open
  -- because trainers measure different sites for different goals.
  circumferences jsonb       NOT NULL DEFAULT '{}'::jsonb,
  notes          text        NOT NULL DEFAULT '',
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now(),
  deleted_at     timestamptz,
  server_seq     bigint      NOT NULL DEFAULT nextval('sync_seq')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX biometric_entries_client_idx ON biometric_entries (tenant_id, client_id, measured_on DESC);
CREATE INDEX biometric_entries_seq_idx ON biometric_entries (tenant_id, server_seq);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER biometric_entries_set_updated_at
  BEFORE UPDATE ON biometric_entries FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER biometric_entries_bump_seq
  BEFORE INSERT OR UPDATE ON biometric_entries FOR EACH ROW EXECUTE FUNCTION bump_sync_seq();
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE biometric_entries ENABLE ROW LEVEL SECURITY;
ALTER TABLE biometric_entries FORCE ROW LEVEL SECURITY;
CREATE POLICY biometric_entries_isolation ON biometric_entries
  USING (tenant_id = current_tenant_id()
         AND (current_client_id() IS NULL OR client_id = current_client_id()))
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Grants
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'coachpulse_app') THEN
    GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO coachpulse_app;
    GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO coachpulse_app;
  END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS biometric_entries;
DROP TABLE IF EXISTS waiver_signatures;
DROP TABLE IF EXISTS waivers;
DROP TABLE IF EXISTS parq_responses;
DROP TABLE IF EXISTS media_objects;
DROP TYPE IF EXISTS media_sensitivity;
DROP TYPE IF EXISTS media_kind;
DROP TABLE IF EXISTS client_tags;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS clients;
DROP TYPE IF EXISTS client_status;
-- +goose StatementEnd
