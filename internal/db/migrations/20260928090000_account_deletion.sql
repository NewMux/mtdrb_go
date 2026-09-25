-- Deleting an account.
--
-- The app stores must offer it, and a practice's records are its clients'
-- personal and medical data, so "delete" has to mean the rows go. Two steps:
--
-- When the owner deletes the practice, the API stamps tenants.deleted_at,
-- deactivates every user and revokes every session. Nothing can sign in, and
-- nothing is removed yet: a deletion made by mistake, or by someone holding a
-- stolen phone, can still be undone by an operator during the grace period.
--
-- When the grace period has passed, the worker calls purge_tenant_if_due,
-- which deletes the tenant and, by cascade, everything it owns.
--
-- The journal, credit ledger and payments are append-only, enforced by
-- triggers that refuse DELETE, and that is the right rule for as long as the
-- business exists. The purge is the one exception, and it is narrow: the
-- triggers let a delete through only while app.purging_tenant names that row's
-- tenant AND the statement runs with another role's rights than the session's
-- — true inside purge_tenant_if_due, a SECURITY DEFINER function, and the
-- cascades it sets off, and never for a statement the app role issues itself.
-- The app role also loses DELETE on tenants, which nothing in the API ever
-- needed, so it cannot start that cascade.

-- +goose Up
-- +goose StatementBegin
ALTER TABLE tenants ADD COLUMN deleted_at timestamptz;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION purging(p_tenant uuid) RETURNS boolean
LANGUAGE sql STABLE
AS $$
  SELECT current_setting('app.purging_tenant', true) = p_tenant::text
     AND current_user <> session_user;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION forbid_journal_mutation() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF purging(OLD.tenant_id) THEN
      RETURN OLD;
    END IF;
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
CREATE OR REPLACE FUNCTION forbid_credit_mutation() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE' AND purging(OLD.tenant_id) THEN
    RETURN OLD;
  END IF;
  RAISE EXCEPTION 'credit transactions are immutable; append a compensating entry instead'
    USING ERRCODE = 'check_violation';
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION forbid_payment_mutation() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF purging(OLD.tenant_id) THEN
      RETURN OLD;
    END IF;
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
-- Deletes a tenant whose deletion is older than p_grace at p_now, and
-- reports whether it did. Called by the worker inside that tenant's own
-- transaction, so a purge and its job claim commit or fail together.
--
-- A handful of foreign keys between a tenant's own tables are RESTRICT, which
-- is checked row by row in whatever order the cascade reaches them; those
-- children go first, explicitly. Everything else follows the tenant row.
CREATE FUNCTION purge_tenant_if_due(p_tenant uuid, p_now timestamptz, p_grace interval)
RETURNS boolean
LANGUAGE plpgsql
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
DECLARE
  v_deleted timestamptz;
BEGIN
  -- Row-level security is forced even for the owner, so the owner reads and
  -- deletes as this tenant, like everyone else.
  PERFORM set_config('app.tenant_id', p_tenant::text, true);
  SELECT deleted_at INTO v_deleted FROM tenants WHERE id = p_tenant;
  IF v_deleted IS NULL OR v_deleted > p_now - p_grace THEN
    RETURN false;
  END IF;

  PERFORM set_config('app.purging_tenant', p_tenant::text, true);
  DELETE FROM set_logs            WHERE tenant_id = p_tenant;
  DELETE FROM program_exercises   WHERE tenant_id = p_tenant;
  DELETE FROM program_assignments WHERE tenant_id = p_tenant;
  DELETE FROM payments            WHERE tenant_id = p_tenant;
  DELETE FROM invoices            WHERE tenant_id = p_tenant;
  DELETE FROM tenants             WHERE id = p_tenant;
  PERFORM set_config('app.purging_tenant', '', true);
  RETURN true;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
  REVOKE EXECUTE ON FUNCTION purge_tenant_if_due(uuid, timestamptz, interval) FROM PUBLIC;
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'coachpulse_app') THEN
    GRANT EXECUTE ON FUNCTION purge_tenant_if_due(uuid, timestamptz, interval) TO coachpulse_app;
    REVOKE DELETE ON tenants FROM coachpulse_app;
  END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'coachpulse_app') THEN
    GRANT DELETE ON tenants TO coachpulse_app;
  END IF;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
DROP FUNCTION IF EXISTS purge_tenant_if_due(uuid, timestamptz, interval);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION forbid_journal_mutation() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    RAISE EXCEPTION 'journal entries are immutable; post a reversing entry instead'
      USING ERRCODE = 'check_violation';
  END IF;

  IF TG_TABLE_NAME = 'journal_entries' THEN
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
CREATE OR REPLACE FUNCTION forbid_credit_mutation() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
  RAISE EXCEPTION 'credit transactions are immutable; append a compensating entry instead'
    USING ERRCODE = 'check_violation';
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION forbid_payment_mutation() RETURNS trigger
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
DROP FUNCTION IF EXISTS purging(uuid);
ALTER TABLE tenants DROP COLUMN IF EXISTS deleted_at;
-- +goose StatementEnd
