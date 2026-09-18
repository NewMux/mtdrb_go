-- +goose Up
-- Invoices, manual payments and the shareable link.
--
-- CoachPulse processes no cards. An invoice is a document that tells a client
-- how to pay off-platform, and a payment is the trainer's record that money
-- arrived. The ledger already knows how to account for both; this migration
-- gives them somewhere to live.

-- ---------------------------------------------------------------------------
-- Payment methods: how this trainer gets paid
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TYPE payment_method_kind AS ENUM ('bank_transfer', 'digital_wallet', 'cash', 'cheque', 'other');
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE payment_methods (
  id           uuid PRIMARY KEY,
  tenant_id    uuid                NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  kind         payment_method_kind NOT NULL,
  label        text                NOT NULL CHECK (length(btrim(label)) > 0),
  -- Structured settlement details: iban, swift_bic, account_holder,
  -- account_number, bank_name, handle. Kept as jsonb because the fields that
  -- matter differ by country and by app, and a column per variant would be a
  -- migration every time a trainer starts taking a new local wallet.
  details      jsonb               NOT NULL DEFAULT '{}'::jsonb,
  -- Free text alongside the structured fields, because "send it to my Revolut
  -- tag @sam" is as real a payment method as an IBAN.
  instructions text                NOT NULL DEFAULT '',
  is_default   boolean             NOT NULL DEFAULT false,
  sort_order   integer             NOT NULL DEFAULT 0,
  archived_at  timestamptz,
  created_at   timestamptz         NOT NULL DEFAULT now(),
  updated_at   timestamptz         NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX payment_methods_tenant_idx ON payment_methods (tenant_id, sort_order)
  WHERE archived_at IS NULL;
-- At most one default, so rendering an invoice never has to break a tie.
CREATE UNIQUE INDEX payment_methods_one_default ON payment_methods (tenant_id)
  WHERE is_default AND archived_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER payment_methods_set_updated_at
  BEFORE UPDATE ON payment_methods FOR EACH ROW EXECUTE FUNCTION set_updated_at();
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE payment_methods ENABLE ROW LEVEL SECURITY;
ALTER TABLE payment_methods FORCE ROW LEVEL SECURITY;
-- Trainer-only: these are the trainer's own banking details, and a portal
-- client sees them only as the snapshot rendered onto their own invoice.
CREATE POLICY payment_methods_isolation ON payment_methods
  USING (tenant_id = current_tenant_id() AND current_client_id() IS NULL)
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Invoice numbering
-- ---------------------------------------------------------------------------
-- Gap-free sequential numbering, per tenant per year. Tax law in much of the
-- EU requires an unbroken run, and the PRD's persona settles by IBAN/SWIFT.
--
-- A Postgres sequence is explicitly the wrong tool here: sequences are
-- non-transactional by design, so a rolled-back issue would burn a number and
-- leave a hole. A counter row taken FOR UPDATE rolls back with its
-- transaction. Contention is nil for a single operator.
-- +goose StatementBegin
CREATE TABLE invoice_counters (
  tenant_id   uuid    NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  year        integer NOT NULL CHECK (year BETWEEN 2000 AND 2999),
  next_number integer NOT NULL DEFAULT 1 CHECK (next_number > 0),
  PRIMARY KEY (tenant_id, year)
);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE invoice_counters ENABLE ROW LEVEL SECURITY;
ALTER TABLE invoice_counters FORCE ROW LEVEL SECURITY;
CREATE POLICY invoice_counters_isolation ON invoice_counters
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Invoices
-- ---------------------------------------------------------------------------
-- Note what is absent: `overdue`. It is derived as (due_date < today AND
-- balance > 0) rather than stored, because a stored flag needs a nightly job
-- to stay truthful and an invoice that only becomes overdue once a cron has
-- run is a liability in a receivables report.
-- +goose StatementBegin
CREATE TYPE invoice_status AS ENUM ('draft', 'issued', 'partially_paid', 'settled', 'void');
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE invoices (
  id          uuid PRIMARY KEY,
  tenant_id   uuid           NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  client_id   uuid           NOT NULL REFERENCES clients(id) ON DELETE RESTRICT,
  -- Assigned at issue, never at creation: a draft that is deleted must not
  -- consume a number.
  number      text,
  status      invoice_status NOT NULL DEFAULT 'draft',
  currency    char(3)        NOT NULL,
  total_minor bigint         NOT NULL DEFAULT 0 CHECK (total_minor >= 0),
  issue_date  date,
  due_date    date,
  notes       text           NOT NULL DEFAULT '',

  -- The settlement instructions exactly as they stood when this invoice was
  -- issued. Same reasoning as a waiver's signed_body: a trainer who changes
  -- their IBAN in June must not retroactively change what a March invoice told
  -- a client to pay.
  payment_instructions_snapshot jsonb,

  -- Only the hash of the share token is stored, like a refresh token: a
  -- database read must not yield a working link to someone's invoice.
  share_token_hash bytea,
  share_created_at timestamptz,
  share_revoked_at timestamptz,

  journal_entry_id uuid REFERENCES journal_entries(id),

  issued_at   timestamptz,
  settled_at  timestamptz,
  voided_at   timestamptz,
  void_reason text,

  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  deleted_at  timestamptz,
  server_seq  bigint      NOT NULL DEFAULT nextval('sync_seq'),

  -- An issued invoice has a number and a date; a draft has neither.
  CONSTRAINT invoices_issued_has_number CHECK (
    (status = 'draft' AND number IS NULL) OR
    (status <> 'draft' AND number IS NOT NULL AND issue_date IS NOT NULL)
  )
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX invoices_tenant_number_key ON invoices (tenant_id, number)
  WHERE number IS NOT NULL;
CREATE UNIQUE INDEX invoices_share_token_key ON invoices (share_token_hash)
  WHERE share_token_hash IS NOT NULL;
CREATE INDEX invoices_client_idx ON invoices (tenant_id, client_id, issue_date DESC);
CREATE INDEX invoices_seq_idx ON invoices (tenant_id, server_seq);
-- Serves receivables ageing: everything still owed, oldest first.
CREATE INDEX invoices_outstanding_idx ON invoices (tenant_id, due_date)
  WHERE status IN ('issued', 'partially_paid') AND deleted_at IS NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER invoices_set_updated_at
  BEFORE UPDATE ON invoices FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER invoices_bump_seq
  BEFORE INSERT OR UPDATE ON invoices FOR EACH ROW EXECUTE FUNCTION bump_sync_seq();
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE invoices ENABLE ROW LEVEL SECURITY;
ALTER TABLE invoices FORCE ROW LEVEL SECURITY;
-- A portal client sees their own invoices, never another client's.
CREATE POLICY invoices_isolation ON invoices
  USING (tenant_id = current_tenant_id()
         AND (current_client_id() IS NULL OR client_id = current_client_id()))
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Invoice lines
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
-- A package line sells prepaid credits and posts to Deferred Revenue; a
-- service line bills work already done and posts straight to revenue. The
-- distinction is what decides the accounting, so it is explicit rather than
-- inferred from whether credits happen to be set.
CREATE TYPE invoice_line_kind AS ENUM ('package', 'service');
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE invoice_lines (
  id               uuid PRIMARY KEY,
  tenant_id        uuid              NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  invoice_id       uuid              NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
  kind             invoice_line_kind NOT NULL DEFAULT 'service',
  description      text              NOT NULL CHECK (length(btrim(description)) > 0),
  quantity         integer           NOT NULL DEFAULT 1 CHECK (quantity > 0),
  unit_price_minor bigint            NOT NULL CHECK (unit_price_minor >= 0),
  amount_minor     bigint            NOT NULL CHECK (amount_minor >= 0),
  -- Credits granted when this line is issued. Required on a package line and
  -- meaningless on a service line.
  package_credits  integer CHECK (package_credits IS NULL OR package_credits > 0),
  -- Optional expiry for the credits this line sells.
  credits_expire_on date,
  sort_order       integer           NOT NULL DEFAULT 0,
  created_at       timestamptz       NOT NULL DEFAULT now(),

  CONSTRAINT invoice_lines_package_has_credits CHECK (
    (kind = 'package' AND package_credits IS NOT NULL) OR
    (kind = 'service' AND package_credits IS NULL)
  ),
  -- The line total must actually be the product. Storing it and letting it
  -- disagree would put a number on a tax document that nothing computed.
  CONSTRAINT invoice_lines_amount_is_product CHECK (amount_minor = quantity * unit_price_minor)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX invoice_lines_invoice_idx ON invoice_lines (invoice_id, sort_order);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE invoice_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE invoice_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY invoice_lines_isolation ON invoice_lines
  USING (tenant_id = current_tenant_id()
         AND (current_client_id() IS NULL OR EXISTS (
              SELECT 1 FROM invoices i
               WHERE i.id = invoice_lines.invoice_id
                 AND i.client_id = current_client_id())))
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Payments
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TYPE payment_instrument AS ENUM ('cash', 'bank_transfer', 'cheque', 'digital_wallet');
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE payments (
  id               uuid PRIMARY KEY,
  tenant_id        uuid               NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  invoice_id       uuid               NOT NULL REFERENCES invoices(id) ON DELETE RESTRICT,
  client_id        uuid               NOT NULL REFERENCES clients(id) ON DELETE RESTRICT,
  amount_minor     bigint             NOT NULL CHECK (amount_minor > 0),
  currency         char(3)            NOT NULL,
  instrument       payment_instrument NOT NULL,
  received_on      date               NOT NULL,
  -- The trainer's own reference: a transfer id, a cheque number, "cash in
  -- envelope". Free text because the world is.
  reference        text               NOT NULL DEFAULT '',
  notes            text               NOT NULL DEFAULT '',
  journal_entry_id uuid REFERENCES journal_entries(id),
  -- Set when this payment has been reversed by a later correcting row.
  reversed_by      uuid REFERENCES payments(id),
  reverses_id      uuid REFERENCES payments(id),
  created_by       uuid REFERENCES users(id),
  created_at       timestamptz        NOT NULL DEFAULT now(),
  server_seq       bigint             NOT NULL DEFAULT nextval('sync_seq')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX payments_invoice_idx ON payments (invoice_id);
CREATE INDEX payments_client_idx ON payments (tenant_id, client_id, received_on DESC);
CREATE INDEX payments_seq_idx ON payments (tenant_id, server_seq);
CREATE UNIQUE INDEX payments_reverses_key ON payments (reverses_id) WHERE reverses_id IS NOT NULL;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE payments ENABLE ROW LEVEL SECURITY;
ALTER TABLE payments FORCE ROW LEVEL SECURITY;
CREATE POLICY payments_isolation ON payments
  USING (tenant_id = current_tenant_id()
         AND (current_client_id() IS NULL OR client_id = current_client_id()))
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- +goose StatementBegin
-- Payments are append-only, like journal entries and credit movements. A
-- mis-keyed payment is corrected by recording a reversal, so the trail shows
-- what happened rather than what someone wishes had happened. The one
-- permitted change is linking a payment to the reversal that cancels it.
CREATE FUNCTION forbid_payment_mutation() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'payments are immutable; record a reversing payment instead'
      USING ERRCODE = 'check_violation';
  END IF;

  IF (NEW.id, NEW.tenant_id, NEW.invoice_id, NEW.client_id, NEW.amount_minor,
      NEW.currency, NEW.instrument, NEW.received_on, NEW.reverses_id)
     IS DISTINCT FROM
     (OLD.id, OLD.tenant_id, OLD.invoice_id, OLD.client_id, OLD.amount_minor,
      OLD.currency, OLD.instrument, OLD.received_on, OLD.reverses_id)
  THEN
    RAISE EXCEPTION 'payments are immutable; record a reversing payment instead'
      USING ERRCODE = 'check_violation';
  END IF;

  IF OLD.reversed_by IS NOT NULL AND NEW.reversed_by IS DISTINCT FROM OLD.reversed_by THEN
    RAISE EXCEPTION 'payment % has already been reversed', OLD.id
      USING ERRCODE = 'check_violation';
  END IF;

  RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER payments_immutable
  BEFORE UPDATE OR DELETE ON payments
  FOR EACH ROW EXECUTE FUNCTION forbid_payment_mutation();
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Close the loop on packages
-- ---------------------------------------------------------------------------
-- M4 had to leave packages.invoice_id as a bare uuid because invoices did not
-- exist yet. Now it can be a real reference.
-- +goose StatementBegin
ALTER TABLE packages
  ADD CONSTRAINT packages_invoice_fk
  FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE SET NULL;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Idempotent replay storage
-- ---------------------------------------------------------------------------
-- M5 is the first milestone to actually use idempotency_keys, and using it
-- exposed a problem with the original column type.
--
-- A replayed request must return byte-for-byte what the original returned.
-- jsonb does not preserve that: it normalises key order and discards
-- insignificant whitespace, so the replay came back reordered and a client
-- comparing responses would see two different answers to the same request.
-- The column is never queried into — it is stored and handed back verbatim —
-- so bytea is the honest type for it.
-- +goose StatementBegin
ALTER TABLE idempotency_keys
  ALTER COLUMN response_body TYPE bytea
  USING convert_to(response_body::text, 'UTF8');
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Share-token lookup
-- ---------------------------------------------------------------------------
-- Reading an invoice by its share token has the same bootstrapping problem as
-- login: the caller is unauthenticated, so there is no tenant context, but
-- row-level security needs one. It gets the same answer as
-- auth_lookup_user_by_email — one narrow SECURITY DEFINER function returning
-- exactly the identifiers needed, keyed on a 32-byte secret the caller must
-- already hold, with search_path pinned so `public` cannot be shadowed.
--
-- It returns ids only. The caller then binds the tenant and reads the invoice
-- through the ordinary policies, so this widens nothing beyond the lookup.
-- +goose StatementBegin
CREATE FUNCTION invoice_lookup_by_share_token(p_token_hash bytea)
RETURNS TABLE (invoice_id uuid, tenant_id uuid)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
  SELECT id, tenant_id
    FROM invoices
   WHERE share_token_hash = p_token_hash
     AND share_revoked_at IS NULL
     AND deleted_at IS NULL
     AND status <> 'draft';
$$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
  REVOKE EXECUTE ON FUNCTION invoice_lookup_by_share_token(bytea) FROM PUBLIC;
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'coachpulse_app') THEN
    GRANT EXECUTE ON FUNCTION invoice_lookup_by_share_token(bytea) TO coachpulse_app;
    GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO coachpulse_app;
    GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO coachpulse_app;
  END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Guarded on the current type: the column is only bytea if this migration's
-- Up actually ran, and an unconditional cast fails when it did not.
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
     WHERE table_name = 'idempotency_keys'
       AND column_name = 'response_body'
       AND data_type = 'bytea'
  ) THEN
    ALTER TABLE idempotency_keys
      ALTER COLUMN response_body TYPE jsonb
      USING convert_from(response_body, 'UTF8')::jsonb;
  END IF;
END
$$;
DROP FUNCTION IF EXISTS invoice_lookup_by_share_token(bytea);
ALTER TABLE packages DROP CONSTRAINT IF EXISTS packages_invoice_fk;
DROP TRIGGER IF EXISTS payments_immutable ON payments;
DROP FUNCTION IF EXISTS forbid_payment_mutation();
DROP TABLE IF EXISTS payments;
DROP TYPE IF EXISTS payment_instrument;
-- The invoices policy is dropped explicitly before invoice_lines, whose own
-- policy reads invoices.
DROP POLICY IF EXISTS invoice_lines_isolation ON invoice_lines;
DROP TABLE IF EXISTS invoice_lines;
DROP TYPE IF EXISTS invoice_line_kind;
DROP TABLE IF EXISTS invoices;
DROP TYPE IF EXISTS invoice_status;
DROP TABLE IF EXISTS invoice_counters;
DROP TABLE IF EXISTS payment_methods;
DROP TYPE IF EXISTS payment_method_kind;
-- +goose StatementEnd
