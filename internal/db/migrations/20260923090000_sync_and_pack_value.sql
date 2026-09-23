-- Two foundations the SaaS build stands on.
--
-- Session types join the sync sequence. They were sent whole on a device's
-- first sync and never again, on the reasoning that they are small and rarely
-- change — which meant a type created after that first sync never reached the
-- device at all, and any booking against it rendered with no name. Every
-- catalogue table this build adds (locations, package offers, products) would
-- have inherited the same bug, so the special case goes rather than spreads.
--
-- Packs learn their own value. Revenue is recognised per credit at
-- unit_price_minor, which was derived as floor(line amount / credits); the
-- floor's remainder was credited to Deferred Revenue at sale and never
-- recognised by anything, so 100.00 for three sessions left 0.01 in the
-- liability for ever. VAT-inclusive pricing makes that the ordinary case —
-- 500 AED gross is 476.19 net, which does not divide by ten. value_minor is
-- what the pack is worth in total; the credit that empties it carries the
-- remainder, so the liability drains to exactly zero.

-- +goose Up
-- +goose StatementBegin
ALTER TABLE session_types
  ADD COLUMN server_seq bigint NOT NULL DEFAULT nextval('sync_seq');
CREATE INDEX session_types_seq_idx ON session_types (tenant_id, server_seq);
CREATE TRIGGER session_types_bump_seq
  BEFORE INSERT OR UPDATE ON session_types FOR EACH ROW EXECUTE FUNCTION bump_sync_seq();
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE packages ADD COLUMN value_minor bigint;

-- A pack sold on an invoice is worth what its line said, where that line can
-- be identified unambiguously and the pack still has credit to recognise it
-- against. Everything else — granted packs, and packs already emptied or
-- expired, whose history is settled — is worth exactly its credits at price,
-- which is what was recognised for it.
UPDATE packages p
   SET value_minor = l.amount_minor
  FROM invoice_lines l
 WHERE l.invoice_id = p.invoice_id
   AND l.kind = 'package'
   AND l.description = p.name
   AND l.package_credits * l.quantity = p.credits_total
   AND l.amount_minor / p.credits_total = p.unit_price_minor
   AND p.status = 'active'
   AND p.credits_remaining > 0;

UPDATE packages SET value_minor = unit_price_minor * credits_total
 WHERE value_minor IS NULL;

ALTER TABLE packages
  ALTER COLUMN value_minor SET NOT NULL,
  ADD CONSTRAINT packages_value_covers_credits CHECK (
    value_minor - unit_price_minor * credits_total >= 0
    AND value_minor - unit_price_minor * credits_total < credits_total
  );

COMMENT ON COLUMN packages.value_minor IS
  'Total value of the pack. The credit that empties it recognises value_minor - unit_price_minor * credits_total on top of its unit price.';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE packages DROP CONSTRAINT IF EXISTS packages_value_covers_credits;
ALTER TABLE packages DROP COLUMN IF EXISTS value_minor;
DROP TRIGGER IF EXISTS session_types_bump_seq ON session_types;
DROP INDEX IF EXISTS session_types_seq_idx;
ALTER TABLE session_types DROP COLUMN IF EXISTS server_seq;
-- +goose StatementEnd
