-- Low-balance alerting.
--
-- Renewal is the business model, so the moment a client is nearly out of
-- credits is the moment worth surfacing. The threshold is a tenant setting
-- rather than a constant: a trainer selling ten-packs and one selling
-- forty-packs do not want warning at the same number.
--
-- Nothing here stores *who* is low. That is derived on read from the packages
-- a client holds, for the same reason overdue invoices are derived: a stored
-- flag is only as truthful as the last job that refreshed it, and a renewal
-- prompt that is wrong until cron runs is worse than no prompt.

-- +goose Up
ALTER TABLE tenants
  ADD COLUMN low_balance_threshold integer NOT NULL DEFAULT 2
    CHECK (low_balance_threshold >= 0 AND low_balance_threshold <= 100);

COMMENT ON COLUMN tenants.low_balance_threshold IS
  'Credits at or below which a client is flagged for renewal. Derived on read; never stored per client.';

-- The alert scans packages by client, which is the same shape BalanceFor uses.
CREATE INDEX IF NOT EXISTS packages_low_balance_idx
  ON packages (tenant_id, client_id)
  WHERE status IN ('active', 'exhausted');

-- +goose Down
DROP INDEX IF EXISTS packages_low_balance_idx;
ALTER TABLE tenants DROP COLUMN IF EXISTS low_balance_threshold;
