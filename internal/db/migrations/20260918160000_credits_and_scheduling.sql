-- +goose Up
-- Prepaid credit packs, the calendar, and the attendance state machine that
-- ties the two to the ledger.

-- btree_gist lets an exclusion constraint mix uuid equality with range
-- overlap, which is what makes double-booking impossible rather than merely
-- unlikely (see sessions_no_overlap below).
-- +goose StatementBegin
CREATE EXTENSION IF NOT EXISTS btree_gist;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Packages: prepaid blocks of sessions
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TYPE package_status AS ENUM ('active', 'exhausted', 'expired', 'void');
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE packages (
  id                uuid PRIMARY KEY,
  tenant_id         uuid           NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  client_id         uuid           NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
  -- Set when the pack was sold via an invoice. Null for a pack granted
  -- directly, such as a comped block or an opening balance migration.
  invoice_id        uuid,
  name              text           NOT NULL DEFAULT '',
  credits_total     integer        NOT NULL CHECK (credits_total > 0),
  -- Maintained alongside credit_transactions in the same transaction. It is
  -- redundant by design: the append-only log is the truth, and this column is
  -- what makes "does this client have credit?" a single indexed read on a gym
  -- floor. A test asserts the two never disagree.
  credits_remaining integer        NOT NULL,
  -- Revenue recognised per credit burned. Held per package because the same
  -- client may hold packs bought at different rates.
  unit_price_minor  bigint         NOT NULL CHECK (unit_price_minor >= 0),
  currency          char(3)        NOT NULL,
  purchased_on      date           NOT NULL,
  expires_at        date,
  status            package_status NOT NULL DEFAULT 'active',
  created_at        timestamptz    NOT NULL DEFAULT now(),
  updated_at        timestamptz    NOT NULL DEFAULT now(),
  server_seq        bigint         NOT NULL DEFAULT nextval('sync_seq')
);
-- +goose StatementEnd

-- +goose StatementBegin
-- Consumption is oldest-first among active packs, and a pack that expires
-- sooner is consumed before one that expires later, so a client never loses
-- credit they could have used. This index serves that lookup.
CREATE INDEX packages_consumption_idx
  ON packages (tenant_id, client_id, status, expires_at NULLS LAST, purchased_on);
CREATE INDEX packages_seq_idx ON packages (tenant_id, server_seq);
CREATE INDEX packages_invoice_idx ON packages (tenant_id, invoice_id) WHERE invoice_id IS NOT NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER packages_set_updated_at
  BEFORE UPDATE ON packages FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER packages_bump_seq
  BEFORE INSERT OR UPDATE ON packages FOR EACH ROW EXECUTE FUNCTION bump_sync_seq();
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE packages ENABLE ROW LEVEL SECURITY;
ALTER TABLE packages FORCE ROW LEVEL SECURITY;
CREATE POLICY packages_isolation ON packages
  USING (tenant_id = current_tenant_id()
         AND (current_client_id() IS NULL OR client_id = current_client_id()))
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Credit transactions: the append-only credit log
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TYPE credit_reason AS ENUM (
  'purchase',
  'session_completed',
  'late_cancellation',
  'no_show',
  'undo',
  'expiry',
  'manual_adjustment'
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE credit_transactions (
  id               uuid PRIMARY KEY,
  tenant_id        uuid          NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  package_id       uuid          NOT NULL REFERENCES packages(id) ON DELETE CASCADE,
  client_id        uuid          NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
  -- Negative to consume, positive to restore. Never zero: a movement of no
  -- credits is not an event.
  delta            integer       NOT NULL CHECK (delta <> 0),
  reason           credit_reason NOT NULL,
  -- The attendance row that caused this movement, when there was one.
  session_attendee_id uuid,
  -- The journal entry posted alongside. Credit movement and revenue
  -- recognition happen in one transaction, so this is how the two are tied
  -- together for audit.
  journal_entry_id uuid REFERENCES journal_entries(id),
  memo             text          NOT NULL DEFAULT '',
  created_at       timestamptz   NOT NULL DEFAULT now(),
  server_seq       bigint        NOT NULL DEFAULT nextval('sync_seq')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX credit_transactions_package_idx ON credit_transactions (package_id);
CREATE INDEX credit_transactions_client_idx ON credit_transactions (tenant_id, client_id, created_at DESC);
CREATE INDEX credit_transactions_attendee_idx ON credit_transactions (session_attendee_id)
  WHERE session_attendee_id IS NOT NULL;
CREATE INDEX credit_transactions_seq_idx ON credit_transactions (tenant_id, server_seq);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE credit_transactions ENABLE ROW LEVEL SECURITY;
ALTER TABLE credit_transactions FORCE ROW LEVEL SECURITY;
CREATE POLICY credit_transactions_isolation ON credit_transactions
  USING (tenant_id = current_tenant_id()
         AND (current_client_id() IS NULL OR client_id = current_client_id()))
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- +goose StatementBegin
-- The credit log is append-only for the same reason the journal is: a balance
-- that can be edited is a balance nobody can trust. Restoring a credit is a
-- positive row, not the deletion of a negative one.
CREATE FUNCTION forbid_credit_mutation() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  RAISE EXCEPTION 'credit transactions are immutable; append a compensating entry instead'
    USING ERRCODE = 'check_violation';
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER credit_transactions_immutable
  BEFORE UPDATE OR DELETE ON credit_transactions
  FOR EACH ROW EXECUTE FUNCTION forbid_credit_mutation();
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Session types
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TABLE session_types (
  id                 uuid PRIMARY KEY,
  tenant_id          uuid        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  name               text        NOT NULL CHECK (length(btrim(name)) > 0),
  duration_minutes   integer     NOT NULL CHECK (duration_minutes > 0 AND duration_minutes <= 600),
  -- 1 is one-to-one; anything higher is semi-private or small group.
  capacity           integer     NOT NULL DEFAULT 1 CHECK (capacity > 0 AND capacity <= 50),
  -- Credits consumed per attendee. Usually 1, but a 90-minute slot might cost
  -- 2 against a pack priced for 60-minute sessions.
  credit_cost        integer     NOT NULL DEFAULT 1 CHECK (credit_cost >= 0),
  colour             text,
  archived_at        timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX session_types_tenant_name_key ON session_types (tenant_id, lower(name))
  WHERE archived_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER session_types_set_updated_at
  BEFORE UPDATE ON session_types FOR EACH ROW EXECUTE FUNCTION set_updated_at();
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE session_types ENABLE ROW LEVEL SECURITY;
ALTER TABLE session_types FORCE ROW LEVEL SECURITY;
CREATE POLICY session_types_isolation ON session_types
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Recurrence series
-- ---------------------------------------------------------------------------
-- A series is a lightweight header. Occurrences are materialised as ordinary
-- session rows rather than computed on read, so a single occurrence can be
-- moved, cancelled or attended without the series having to model exceptions.
-- +goose StatementBegin
CREATE TABLE session_series (
  id              uuid PRIMARY KEY,
  tenant_id       uuid        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  session_type_id uuid        NOT NULL REFERENCES session_types(id),
  -- ISO weekday numbers, 1 = Monday.
  weekdays        smallint[]  NOT NULL CHECK (array_length(weekdays, 1) BETWEEN 1 AND 7),
  starts_on       date        NOT NULL,
  ends_on         date        NOT NULL,
  time_of_day     time        NOT NULL,
  timezone        text        NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  CHECK (ends_on >= starts_on)
);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE session_series ENABLE ROW LEVEL SECURITY;
ALTER TABLE session_series FORCE ROW LEVEL SECURITY;
CREATE POLICY session_series_isolation ON session_series
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Sessions
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TYPE session_status AS ENUM ('scheduled', 'cancelled');
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE sessions (
  id              uuid PRIMARY KEY,
  tenant_id       uuid           NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  session_type_id uuid           NOT NULL REFERENCES session_types(id),
  series_id       uuid REFERENCES session_series(id) ON DELETE SET NULL,
  starts_at       timestamptz    NOT NULL,
  ends_at         timestamptz    NOT NULL,
  -- The slot the trainer is actually unavailable for: the session plus the
  -- tenant's buffer, applied to the end only. Padding one side gives exactly
  -- "N minutes between sessions"; padding both would double-count the gap.
  -- Stored rather than computed so the exclusion constraint below can index
  -- it, and so a later change to the tenant buffer does not retroactively
  -- invalidate bookings already made.
  blocked_range   tstzrange      NOT NULL,
  status          session_status NOT NULL DEFAULT 'scheduled',
  location        text           NOT NULL DEFAULT '',
  notes           text           NOT NULL DEFAULT '',
  cancelled_at    timestamptz,
  created_at      timestamptz    NOT NULL DEFAULT now(),
  updated_at      timestamptz    NOT NULL DEFAULT now(),
  server_seq      bigint         NOT NULL DEFAULT nextval('sync_seq'),
  CHECK (ends_at > starts_at)
);
-- +goose StatementEnd

-- +goose StatementBegin
-- Double-booking is refused by the database, not by a read-then-write check in
-- application code. Two devices booking the same slot while offline and
-- syncing together is exactly the race that check would lose.
ALTER TABLE sessions ADD CONSTRAINT sessions_no_overlap
  EXCLUDE USING gist (tenant_id WITH =, blocked_range WITH &&)
  WHERE (status <> 'cancelled');
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX sessions_calendar_idx ON sessions (tenant_id, starts_at);
CREATE INDEX sessions_seq_idx ON sessions (tenant_id, server_seq);
CREATE INDEX sessions_series_idx ON sessions (series_id) WHERE series_id IS NOT NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER sessions_set_updated_at
  BEFORE UPDATE ON sessions FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER sessions_bump_seq
  BEFORE INSERT OR UPDATE ON sessions FOR EACH ROW EXECUTE FUNCTION bump_sync_seq();
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Session attendees: where attendance actually lives
-- ---------------------------------------------------------------------------
-- Attendance is per attendee, not per session. In a semi-private slot one
-- client can complete while another no-shows, and each outcome has its own
-- credit and revenue consequence. Modelling attendance on the session would
-- make that unrepresentable.
-- +goose StatementBegin
CREATE TYPE attendance_status AS ENUM (
  'scheduled',
  'completed',
  'late_cancel',
  'early_cancel',
  'no_show'
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE session_attendees (
  id              uuid PRIMARY KEY,
  tenant_id       uuid              NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  session_id      uuid              NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  client_id       uuid              NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
  status          attendance_status NOT NULL DEFAULT 'scheduled',
  -- Credits actually consumed by this attendance. Recorded here so that undo
  -- restores exactly what was taken, even if the session type's cost or the
  -- pack's price changed in between.
  credits_charged integer           NOT NULL DEFAULT 0,
  marked_at       timestamptz,
  marked_by       uuid REFERENCES users(id),
  notes           text              NOT NULL DEFAULT '',
  created_at      timestamptz       NOT NULL DEFAULT now(),
  updated_at      timestamptz       NOT NULL DEFAULT now(),
  server_seq      bigint            NOT NULL DEFAULT nextval('sync_seq')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX session_attendees_unique ON session_attendees (session_id, client_id);
CREATE INDEX session_attendees_client_idx ON session_attendees (tenant_id, client_id);
CREATE INDEX session_attendees_seq_idx ON session_attendees (tenant_id, server_seq);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER session_attendees_set_updated_at
  BEFORE UPDATE ON session_attendees FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER session_attendees_bump_seq
  BEFORE INSERT OR UPDATE ON session_attendees FOR EACH ROW EXECUTE FUNCTION bump_sync_seq();
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE session_attendees ENABLE ROW LEVEL SECURITY;
ALTER TABLE session_attendees FORCE ROW LEVEL SECURITY;
CREATE POLICY session_attendees_isolation ON session_attendees
  USING (tenant_id = current_tenant_id()
         AND (current_client_id() IS NULL OR client_id = current_client_id()))
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- +goose StatementBegin
-- Deferred until session_attendees exists, because the policy reads it: a
-- portal client may see a session only if they are on its roster.
ALTER TABLE sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE sessions FORCE ROW LEVEL SECURITY;
CREATE POLICY sessions_isolation ON sessions
  USING (tenant_id = current_tenant_id()
         AND (current_client_id() IS NULL OR EXISTS (
              SELECT 1 FROM session_attendees sa
               WHERE sa.session_id = sessions.id
                 AND sa.client_id = current_client_id())))
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- +goose StatementBegin
-- Enforces the session type's capacity. A check constraint cannot span rows,
-- so this runs as a trigger; it takes a lock on the session row first so two
-- concurrent bookings cannot both see a free seat.
CREATE FUNCTION enforce_session_capacity() RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  v_capacity integer;
  v_booked   integer;
BEGIN
  SELECT st.capacity INTO v_capacity
    FROM sessions s
    JOIN session_types st ON st.id = s.session_type_id
   WHERE s.id = NEW.session_id
     FOR UPDATE OF s;

  IF v_capacity IS NULL THEN
    RAISE EXCEPTION 'session % does not exist', NEW.session_id
      USING ERRCODE = 'foreign_key_violation';
  END IF;

  -- Cancelled attendees free their seat.
  SELECT count(*) INTO v_booked
    FROM session_attendees
   WHERE session_id = NEW.session_id
     AND status <> 'early_cancel'
     AND id <> NEW.id;

  IF v_booked >= v_capacity THEN
    RAISE EXCEPTION 'session % is full (capacity %)', NEW.session_id, v_capacity
      USING ERRCODE = 'check_violation';
  END IF;

  RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER session_attendees_capacity
  BEFORE INSERT ON session_attendees
  FOR EACH ROW EXECUTE FUNCTION enforce_session_capacity();
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
-- The sessions policy reads session_attendees, so it has to go first or the
-- drop below is refused for a dependency that is not obvious from the DDL.
DROP POLICY IF EXISTS sessions_isolation ON sessions;
DROP TRIGGER IF EXISTS session_attendees_capacity ON session_attendees;
DROP FUNCTION IF EXISTS enforce_session_capacity();
DROP TABLE IF EXISTS session_attendees;
DROP TYPE IF EXISTS attendance_status;
DROP TABLE IF EXISTS sessions;
DROP TYPE IF EXISTS session_status;
DROP TABLE IF EXISTS session_series;
DROP TABLE IF EXISTS session_types;
DROP TRIGGER IF EXISTS credit_transactions_immutable ON credit_transactions;
DROP FUNCTION IF EXISTS forbid_credit_mutation();
DROP TABLE IF EXISTS credit_transactions;
DROP TYPE IF EXISTS credit_reason;
DROP TABLE IF EXISTS packages;
DROP TYPE IF EXISTS package_status;
-- +goose StatementEnd
