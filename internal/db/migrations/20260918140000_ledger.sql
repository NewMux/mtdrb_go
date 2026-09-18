-- +goose Up
-- The double-entry ledger (ADR 0001).
--
-- Every figure the product reports about money — profit, receivables ageing,
-- how much unearned training a trainer still owes — is derived from these
-- three tables. Nothing else records an amount as authoritative.

-- +goose StatementBegin
CREATE TYPE account_type AS ENUM ('asset', 'liability', 'equity', 'revenue', 'expense');
-- +goose StatementEnd

-- +goose StatementBegin
-- normal_balance says which side increases an account. Assets and expenses
-- grow on the debit side; liabilities, equity and revenue on the credit side.
-- Storing it makes reports readable without hard-coding the rule per query.
CREATE TYPE normal_balance AS ENUM ('debit', 'credit');
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Chart of accounts
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TABLE accounts (
  id             uuid PRIMARY KEY,
  tenant_id      uuid           NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  code           text           NOT NULL CHECK (code ~ '^[0-9]{4}$'),
  name           text           NOT NULL CHECK (length(btrim(name)) > 0),
  type           account_type   NOT NULL,
  normal_balance normal_balance NOT NULL,
  -- System accounts are created at signup and referenced by the posting rules
  -- in code. They cannot be renamed away or deleted, or a posting rule would
  -- have nowhere to land.
  is_system      boolean        NOT NULL DEFAULT false,
  -- Identifies a system account to application code independently of its
  -- display name, which a trainer may localise.
  slug           text,
  archived_at    timestamptz,
  created_at     timestamptz    NOT NULL DEFAULT now(),
  updated_at     timestamptz    NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX accounts_tenant_code_key ON accounts (tenant_id, code);
CREATE UNIQUE INDEX accounts_tenant_slug_key ON accounts (tenant_id, slug) WHERE slug IS NOT NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER accounts_set_updated_at
  BEFORE UPDATE ON accounts
  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE accounts FORCE ROW LEVEL SECURITY;
CREATE POLICY accounts_isolation ON accounts
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Journal entries
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
-- source_type/source_id link an entry back to the business event that caused
-- it, so an invoice screen can show its postings and a report can be traced to
-- the session or payment behind it.
CREATE TYPE journal_source AS ENUM (
  'invoice_issued',
  'payment_received',
  'session_delivered',
  'late_cancellation',
  'expense',
  'package_expired',
  'adjustment',
  'opening_balance'
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE journal_entries (
  id          uuid PRIMARY KEY,
  tenant_id   uuid           NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  -- The date the event belongs to for reporting, which is not necessarily the
  -- date the row was written: a trainer logging Friday's cash on Monday must
  -- still see it in Friday's figures.
  entry_date  date           NOT NULL,
  memo        text           NOT NULL DEFAULT '',
  source_type journal_source NOT NULL,
  source_id   uuid,
  currency    char(3)        NOT NULL,
  -- Set on the entry that reverses this one, and on the reversal pointing back.
  -- Corrections never mutate; they add an equal and opposite entry.
  reverses_id uuid REFERENCES journal_entries(id),
  reversed_by uuid REFERENCES journal_entries(id),
  created_by  uuid REFERENCES users(id),
  created_at  timestamptz    NOT NULL DEFAULT now(),
  server_seq  bigint         NOT NULL DEFAULT nextval('sync_seq')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX journal_entries_tenant_date_idx ON journal_entries (tenant_id, entry_date);
CREATE INDEX journal_entries_source_idx ON journal_entries (tenant_id, source_type, source_id);
CREATE INDEX journal_entries_seq_idx ON journal_entries (tenant_id, server_seq);
-- An entry may be reversed at most once; a second reversal would double-count.
CREATE UNIQUE INDEX journal_entries_reverses_key ON journal_entries (reverses_id) WHERE reverses_id IS NOT NULL;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE journal_entries ENABLE ROW LEVEL SECURITY;
ALTER TABLE journal_entries FORCE ROW LEVEL SECURITY;
CREATE POLICY journal_entries_isolation ON journal_entries
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Journal lines
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TABLE journal_lines (
  id           uuid PRIMARY KEY,
  tenant_id    uuid    NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  entry_id     uuid    NOT NULL REFERENCES journal_entries(id) ON DELETE CASCADE,
  account_id   uuid    NOT NULL REFERENCES accounts(id),
  -- Amounts are integer minor units. Never a float: see platform/money.
  debit_minor  bigint  NOT NULL DEFAULT 0 CHECK (debit_minor >= 0),
  credit_minor bigint  NOT NULL DEFAULT 0 CHECK (credit_minor >= 0),
  currency     char(3) NOT NULL,
  memo         text    NOT NULL DEFAULT '',
  -- A line is either a debit or a credit, never both and never neither. A
  -- zero-amount line carries no information and would silently pass the
  -- balance check, so it is rejected outright.
  CONSTRAINT journal_lines_one_sided CHECK (
    (debit_minor > 0 AND credit_minor = 0) OR
    (credit_minor > 0 AND debit_minor = 0)
  )
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX journal_lines_entry_idx ON journal_lines (entry_id);
CREATE INDEX journal_lines_account_idx ON journal_lines (tenant_id, account_id);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE journal_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE journal_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY journal_lines_isolation ON journal_lines
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- The balance assertion
-- ---------------------------------------------------------------------------
-- This is the guarantee that makes the ledger trustworthy: debits equal
-- credits for every entry, with no exception and no way for application code
-- to opt out.
--
-- It is DEFERRABLE INITIALLY DEFERRED because an entry is written one line at
-- a time and is legitimately unbalanced in between. Checking at COMMIT means
-- the intermediate states are allowed but the committed state never is.
-- Enforcing this in Go instead would leave it one forgotten call site away
-- from a corrupt ledger.

-- +goose StatementBegin
CREATE FUNCTION assert_entry_balanced() RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
  v_entry_id uuid := COALESCE(NEW.entry_id, OLD.entry_id);
  v_debits   bigint;
  v_credits  bigint;
  v_lines    integer;
BEGIN
  SELECT COALESCE(sum(debit_minor), 0), COALESCE(sum(credit_minor), 0), count(*)
    INTO v_debits, v_credits, v_lines
    FROM journal_lines
   WHERE entry_id = v_entry_id;

  -- An entry deleted outright (cascade from its header) leaves no lines and
  -- nothing to assert.
  IF v_lines = 0 THEN
    RETURN NULL;
  END IF;

  -- Single-sided entries are always unbalanced; catching this here gives a
  -- clearer failure than a mismatched total.
  IF v_lines < 2 THEN
    RAISE EXCEPTION 'journal entry % has % line(s); a double-entry needs at least 2',
      v_entry_id, v_lines
      USING ERRCODE = 'check_violation';
  END IF;

  IF v_debits <> v_credits THEN
    RAISE EXCEPTION 'journal entry % is unbalanced: debits % <> credits %',
      v_entry_id, v_debits, v_credits
      USING ERRCODE = 'check_violation';
  END IF;

  RETURN NULL;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE CONSTRAINT TRIGGER journal_lines_balanced
  AFTER INSERT OR UPDATE OR DELETE ON journal_lines
  DEFERRABLE INITIALLY DEFERRED
  FOR EACH ROW EXECUTE FUNCTION assert_entry_balanced();
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Immutability
-- ---------------------------------------------------------------------------
-- An audit trail that can be edited is not an audit trail. Entries and lines
-- are append-only; the sole permitted update is linking an entry to the
-- reversal that cancels it.
-- +goose StatementBegin
CREATE FUNCTION forbid_journal_mutation() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'journal entries are immutable; post a reversing entry instead'
      USING ERRCODE = 'check_violation';
  END IF;

  IF TG_TABLE_NAME = 'journal_entries' THEN
    -- Allow only the reversed_by back-reference to change.
    IF (NEW.id, NEW.tenant_id, NEW.entry_date, NEW.memo, NEW.source_type,
        NEW.currency, NEW.reverses_id)
       IS DISTINCT FROM
       (OLD.id, OLD.tenant_id, OLD.entry_date, OLD.memo, OLD.source_type,
        OLD.currency, OLD.reverses_id)
       OR NEW.source_id IS DISTINCT FROM OLD.source_id
    THEN
      RAISE EXCEPTION 'journal entries are immutable; post a reversing entry instead'
        USING ERRCODE = 'check_violation';
    END IF;
    IF OLD.reversed_by IS NOT NULL AND NEW.reversed_by IS DISTINCT FROM OLD.reversed_by THEN
      RAISE EXCEPTION 'journal entry % has already been reversed', OLD.id
        USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
  END IF;

  RAISE EXCEPTION 'journal lines are immutable; post a reversing entry instead'
    USING ERRCODE = 'check_violation';
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER journal_entries_immutable
  BEFORE UPDATE OR DELETE ON journal_entries
  FOR EACH ROW EXECUTE FUNCTION forbid_journal_mutation();
-- +goose StatementEnd

-- +goose StatementBegin
-- Lines admit no exception at all: any UPDATE or DELETE is refused.
-- ON DELETE CASCADE from journal_entries can never fire, because deleting an
-- entry is itself refused above.
CREATE TRIGGER journal_lines_immutable
  BEFORE UPDATE OR DELETE ON journal_lines
  FOR EACH ROW EXECUTE FUNCTION forbid_journal_mutation();
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
DROP TRIGGER IF EXISTS journal_lines_immutable ON journal_lines;
DROP TRIGGER IF EXISTS journal_entries_immutable ON journal_entries;
DROP FUNCTION IF EXISTS forbid_journal_mutation();
DROP TRIGGER IF EXISTS journal_lines_balanced ON journal_lines;
DROP FUNCTION IF EXISTS assert_entry_balanced();
DROP TABLE IF EXISTS journal_lines;
DROP TABLE IF EXISTS journal_entries;
DROP TYPE IF EXISTS journal_source;
DROP TABLE IF EXISTS accounts;
DROP TYPE IF EXISTS normal_balance;
DROP TYPE IF EXISTS account_type;
-- +goose StatementEnd
