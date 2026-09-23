-- The scheduled-jobs worker.
--
-- The worker has the app role's problem from the other side. Every job runs
-- inside one tenant's transaction, under that tenant's policies, like any
-- request — but deciding *which* tenants to visit means reading across all of
-- them, which the app role cannot do and must not be given a policy for.
-- jobs_tenant_ids is the narrow way through, in the shape of the auth lookup
-- functions: SECURITY DEFINER, a pinned search_path, and nothing returned but
-- what the scheduler needs to decide when a tenant's day has turned.
--
-- job_runs is what makes every job safe to run twice. Before a job does its
-- work for a tenant it claims (job, run key) — for a daily job, the tenant's
-- local date — in the same transaction as the work. A second worker, a restart
-- mid-sweep or an operator running a job by hand all find the claim and do
-- nothing; a failed run rolls its claim back with its work and is retried.

-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION jobs_tenant_ids()
RETURNS TABLE (tenant_id uuid, timezone text)
LANGUAGE sql
STABLE
SECURITY DEFINER
SET search_path = public, pg_temp
AS $$
  SELECT id, timezone FROM tenants ORDER BY id;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
  REVOKE EXECUTE ON FUNCTION jobs_tenant_ids() FROM PUBLIC;
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'coachpulse_app') THEN
    GRANT EXECUTE ON FUNCTION jobs_tenant_ids() TO coachpulse_app;
  END IF;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE job_runs (
  id          uuid PRIMARY KEY,
  tenant_id   uuid        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  job         text        NOT NULL,
  run_key     text        NOT NULL,
  -- What the run did, for an operator reading the table: rows touched.
  affected    integer     NOT NULL DEFAULT 0,
  finished_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, job, run_key)
);
CREATE INDEX job_runs_recent_idx ON job_runs (tenant_id, job, finished_at DESC);
ALTER TABLE job_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE job_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY job_runs_isolation ON job_runs
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id());
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS job_runs;
DROP FUNCTION IF EXISTS jobs_tenant_ids();
-- +goose StatementEnd
