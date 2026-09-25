-- Locations and package offers: the practice's places and its price list.
--
-- A personal trainer works in several places — their own studio, a gym that
-- rents them floor time, a beach at dawn, a client's living room, a video
-- call — and until now a session said where only as free text. Text cannot
-- be counted, filtered or held to a plan's limit, and "Studio A" and
-- "studio a" are two places to a report. Sessions gain a location_id; the
-- text stays beside it as a snapshot of the name at booking, so renaming a
-- studio does not rewrite where last year's sessions happened.
--
-- Existing sessions are carried over: one location per distinct place name
-- already in use, matched case- and space-insensitively, linked back to the
-- sessions that named it. Nothing a trainer typed is lost.
--
-- Package offers are the price list the sell screen picks from. Until now
-- every sale was typed afresh — name, credits, price, validity — which is
-- slow at a front desk and invites the same pack at three prices. An offer
-- is a template: selling one copies its terms onto the invoice line, so
-- changing an offer's price never reprices anything already sold.

-- +goose Up
-- +goose StatementBegin
CREATE TYPE location_kind AS ENUM ('studio', 'gym', 'outdoor', 'client_home', 'online');

CREATE TABLE locations (
  id          uuid PRIMARY KEY,
  tenant_id   uuid          NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  name        text          NOT NULL CHECK (length(btrim(name)) > 0),
  kind        location_kind NOT NULL DEFAULT 'studio',
  address     text          NOT NULL DEFAULT '',
  -- Emirate or region. The UAE VAT return splits supplies by emirate.
  region      text          NOT NULL DEFAULT '',
  colour      text          CHECK (colour IS NULL OR colour ~ '^#[0-9a-fA-F]{6}$'),
  is_primary  boolean       NOT NULL DEFAULT false,
  archived_at timestamptz,
  created_at  timestamptz   NOT NULL DEFAULT now(),
  updated_at  timestamptz   NOT NULL DEFAULT now(),
  server_seq  bigint        NOT NULL DEFAULT nextval('sync_seq')
);
CREATE UNIQUE INDEX locations_name_key ON locations (tenant_id, lower(btrim(name))) WHERE archived_at IS NULL;
-- One primary place per practice: the default for bookings and, later, for
-- which emirate an invoice's supply happened in.
CREATE UNIQUE INDEX locations_primary_key ON locations (tenant_id) WHERE is_primary AND archived_at IS NULL;
CREATE INDEX locations_seq_idx ON locations (tenant_id, server_seq);
CREATE TRIGGER locations_set_updated_at BEFORE UPDATE ON locations
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER locations_bump_seq BEFORE INSERT OR UPDATE ON locations
  FOR EACH ROW EXECUTE FUNCTION bump_sync_seq();
ALTER TABLE locations ENABLE ROW LEVEL SECURITY;
ALTER TABLE locations FORCE ROW LEVEL SECURITY;
CREATE POLICY locations_isolation ON locations
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE sessions ADD COLUMN location_id uuid REFERENCES locations(id) ON DELETE SET NULL;
CREATE INDEX sessions_location_idx ON sessions (tenant_id, location_id, starts_at);

-- Carry over the places already typed. Run as the owner, which row-level
-- security does not bind, so this sees every tenant at once.
INSERT INTO locations (id, tenant_id, name, kind)
SELECT gen_random_uuid(), tenant_id, min(btrim(location)), 'studio'
  FROM sessions
 WHERE btrim(location) <> ''
 GROUP BY tenant_id, lower(btrim(location));

UPDATE sessions s
   SET location_id = l.id
  FROM locations l
 WHERE l.tenant_id = s.tenant_id
   AND lower(btrim(l.name)) = lower(btrim(s.location))
   AND btrim(s.location) <> '';

-- Each carried-over practice's most used place becomes its primary.
UPDATE locations l SET is_primary = true
 WHERE l.id IN (
   SELECT DISTINCT ON (tenant_id) location_id
     FROM sessions
    WHERE location_id IS NOT NULL
    GROUP BY tenant_id, location_id
    ORDER BY tenant_id, count(*) DESC, location_id);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TYPE offer_kind AS ENUM ('session_pack', 'monthly_coaching', 'online_coaching', 'semi_private');
CREATE TYPE billing_cycle AS ENUM ('one_off', 'weekly', 'monthly', 'annual');

CREATE TABLE package_offers (
  id              uuid PRIMARY KEY,
  tenant_id       uuid          NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  name            text          NOT NULL CHECK (length(btrim(name)) > 0),
  description     text          NOT NULL DEFAULT '',
  kind            offer_kind    NOT NULL DEFAULT 'session_pack',
  -- Sessions included. Null for coaching sold by time rather than by session.
  credits         integer       CHECK (credits IS NULL OR credits > 0),
  price_minor     bigint        NOT NULL CHECK (price_minor >= 0),
  currency        char(3)       NOT NULL,
  -- Whether price_minor already contains VAT. UAE and KSA consumer prices
  -- are quoted inclusive; the VAT slice reads this.
  price_includes_vat boolean    NOT NULL DEFAULT true,
  -- Days a pack's credits last from the sale. Null never expires.
  validity_days   integer       CHECK (validity_days IS NULL OR validity_days > 0),
  cycle           billing_cycle NOT NULL DEFAULT 'one_off',
  -- The kind of session the credits are for, when it matters.
  session_type_id uuid          REFERENCES session_types(id) ON DELETE SET NULL,
  sort_order      integer       NOT NULL DEFAULT 0,
  archived_at     timestamptz,
  created_at      timestamptz   NOT NULL DEFAULT now(),
  updated_at      timestamptz   NOT NULL DEFAULT now(),
  server_seq      bigint        NOT NULL DEFAULT nextval('sync_seq'),
  -- A pack is sold once; a membership recurs. A pack must say how many.
  CHECK (kind <> 'session_pack' OR credits IS NOT NULL)
);
CREATE INDEX package_offers_seq_idx ON package_offers (tenant_id, server_seq);
CREATE TRIGGER package_offers_set_updated_at BEFORE UPDATE ON package_offers
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER package_offers_bump_seq BEFORE INSERT OR UPDATE ON package_offers
  FOR EACH ROW EXECUTE FUNCTION bump_sync_seq();
ALTER TABLE package_offers ENABLE ROW LEVEL SECURITY;
ALTER TABLE package_offers FORCE ROW LEVEL SECURITY;
CREATE POLICY package_offers_isolation ON package_offers
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS package_offers;
DROP TYPE IF EXISTS billing_cycle;
DROP TYPE IF EXISTS offer_kind;
DROP INDEX IF EXISTS sessions_location_idx;
ALTER TABLE sessions DROP COLUMN IF EXISTS location_id;
DROP TABLE IF EXISTS locations;
DROP TYPE IF EXISTS location_kind;
-- +goose StatementEnd
