-- +goose Up
-- Authentication has a bootstrapping problem: row-level security needs a
-- tenant, but the tenant is exactly what login is trying to discover from an
-- email address, and what refresh is trying to discover from a token hash.
--
-- The wrong fixes are to grant the app role BYPASSRLS, or to add a permissive
-- SELECT policy on users — both would open every row to every request for the
-- sake of two lookups.
--
-- Instead these two SECURITY DEFINER functions run as the owner and return
-- exactly one row's worth of authentication material, keyed on a value the
-- caller must already possess. They are the only sanctioned way to read across
-- tenants, they are small enough to audit in full, and each pins its
-- search_path so a caller cannot shadow `public` with its own objects and
-- capture owner privileges.

-- +goose StatementBegin
CREATE FUNCTION auth_lookup_user_by_email(p_email citext)
RETURNS TABLE (
  user_id        uuid,
  tenant_id      uuid,
  email          citext,
  password_hash  text,
  display_name   text,
  role           text,
  deactivated_at timestamptz,
  currency       char(3)
)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
  SELECT u.id, u.tenant_id, u.email, u.password_hash, u.display_name,
         u.role::text, u.deactivated_at, t.default_currency
    FROM users u
    JOIN tenants t ON t.id = u.tenant_id
   WHERE u.email = p_email;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- Keyed on the token hash, so a caller can only reach a row whose 32-byte
-- secret they already hold. The token itself is never returned.
CREATE FUNCTION auth_lookup_refresh_token(p_token_hash bytea)
RETURNS TABLE (
  token_id   uuid,
  tenant_id  uuid,
  user_id    uuid,
  family_id  uuid,
  expires_at timestamptz,
  used_at    timestamptz,
  revoked_at timestamptz
)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
  SELECT id, tenant_id, user_id, family_id, expires_at, used_at, revoked_at
    FROM refresh_tokens
   WHERE token_hash = p_token_hash;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
-- EXECUTE is revoked from PUBLIC first: a SECURITY DEFINER function is
-- world-executable by default, and these must be reachable only by the app.
DO $$
BEGIN
  REVOKE EXECUTE ON FUNCTION auth_lookup_user_by_email(citext) FROM PUBLIC;
  REVOKE EXECUTE ON FUNCTION auth_lookup_refresh_token(bytea) FROM PUBLIC;

  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'coachpulse_app') THEN
    GRANT EXECUTE ON FUNCTION auth_lookup_user_by_email(citext) TO coachpulse_app;
    GRANT EXECUTE ON FUNCTION auth_lookup_refresh_token(bytea) TO coachpulse_app;
  END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP FUNCTION IF EXISTS auth_lookup_refresh_token(bytea);
DROP FUNCTION IF EXISTS auth_lookup_user_by_email(citext);
-- +goose StatementEnd
