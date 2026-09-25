-- Onboarding, and the VAT registration it asks about.
--
-- A new practice used to land on an empty Today screen with every setting at
-- its default: UTC unless the device said otherwise, no country, no idea
-- whether its prices include VAT. onboarded_at records that the owner has
-- been through the few questions that matter before the first sale; until
-- then the app walks them through those instead of the empty roster. Practices
-- that predate it are treated as onboarded — they have been running for weeks
-- and must not be stopped at a wizard.
--
-- The VAT registration lives here rather than waiting for the invoicing that
-- uses it, because it is a question a trainer answers on day one: whether
-- they are registered, their TRN, the rate their country charges, and whether
-- they quote prices with VAT in (UAE and KSA consumer prices are quoted
-- inclusive). The rate follows the country, as the week does, unless the
-- trainer says otherwise.

-- +goose Up
-- +goose StatementBegin
ALTER TABLE tenants
  ADD COLUMN onboarded_at timestamptz,
  ADD COLUMN vat_registered boolean NOT NULL DEFAULT false,
  -- Tax registration number: 15 digits in the UAE and Saudi Arabia.
  ADD COLUMN trn text CHECK (trn IS NULL OR trn ~ '^[0-9]{15}$'),
  -- Basis points: 500 is 5%.
  ADD COLUMN vat_rate_bp integer NOT NULL DEFAULT 500 CHECK (vat_rate_bp BETWEEN 0 AND 10000),
  ADD COLUMN prices_include_vat boolean NOT NULL DEFAULT true,
  ADD CONSTRAINT tenants_registered_has_trn CHECK (NOT vat_registered OR trn IS NOT NULL);

UPDATE tenants SET onboarded_at = created_at;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE tenants
  DROP CONSTRAINT IF EXISTS tenants_registered_has_trn,
  DROP COLUMN IF EXISTS onboarded_at,
  DROP COLUMN IF EXISTS vat_registered,
  DROP COLUMN IF EXISTS trn,
  DROP COLUMN IF EXISTS vat_rate_bp,
  DROP COLUMN IF EXISTS prices_include_vat;
-- +goose StatementEnd
