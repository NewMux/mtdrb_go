-- +goose Up
-- Workout programming and performance logging.
--
-- The shape here mirrors how coaches actually talk: a programme is a
-- macrocycle, divided into blocks (phases), each holding training days, each
-- prescribing exercises. What a client actually did is recorded separately —
-- the prescription and the performance are different facts, and comparing them
-- is the whole point of progression tracking.

-- ---------------------------------------------------------------------------
-- Exercise library
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TYPE exercise_category AS ENUM (
  'squat', 'hinge', 'lunge', 'horizontal_push', 'vertical_push',
  'horizontal_pull', 'vertical_pull', 'carry', 'core', 'olympic',
  'conditioning', 'mobility', 'other'
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE exercises (
  id             uuid PRIMARY KEY,
  -- Seeded per tenant at signup, like the chart of accounts. A shared global
  -- library would be smaller, but a trainer renaming "Romanian Deadlift" to
  -- their own cue must not rename it for everyone else, and every other table
  -- in this system gates on tenant_id — keeping that uniform is worth the rows.
  tenant_id      uuid              NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  name           text              NOT NULL CHECK (length(btrim(name)) > 0),
  category       exercise_category NOT NULL DEFAULT 'other',
  primary_muscle text              NOT NULL DEFAULT '',
  equipment      text              NOT NULL DEFAULT '',
  instructions   text              NOT NULL DEFAULT '',
  -- A YouTube or Vimeo link, or a trainer's own uploaded demo.
  video_url      text              NOT NULL DEFAULT '',
  demo_media_id  uuid REFERENCES media_objects(id) ON DELETE SET NULL,
  -- False for the seeded library, true for anything the trainer added.
  is_custom      boolean           NOT NULL DEFAULT true,
  archived_at    timestamptz,
  created_at     timestamptz       NOT NULL DEFAULT now(),
  updated_at     timestamptz       NOT NULL DEFAULT now(),
  server_seq     bigint            NOT NULL DEFAULT nextval('sync_seq')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX exercises_tenant_name_key ON exercises (tenant_id, lower(name))
  WHERE archived_at IS NULL;
CREATE INDEX exercises_category_idx ON exercises (tenant_id, category) WHERE archived_at IS NULL;
CREATE INDEX exercises_seq_idx ON exercises (tenant_id, server_seq);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER exercises_set_updated_at
  BEFORE UPDATE ON exercises FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER exercises_bump_seq
  BEFORE INSERT OR UPDATE ON exercises FOR EACH ROW EXECUTE FUNCTION bump_sync_seq();
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE exercises ENABLE ROW LEVEL SECURITY;
ALTER TABLE exercises FORCE ROW LEVEL SECURITY;
-- Readable by a portal client too: they need to see what they are being asked
-- to do, and the library carries nothing private.
CREATE POLICY exercises_isolation ON exercises
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id() AND current_client_id() IS NULL);
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Programmes
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TABLE programs (
  id          uuid PRIMARY KEY,
  tenant_id   uuid        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  name        text        NOT NULL CHECK (length(btrim(name)) > 0),
  description text        NOT NULL DEFAULT '',
  -- A template is a reusable skeleton the trainer copies for each client,
  -- rather than something assigned directly.
  is_template boolean     NOT NULL DEFAULT false,
  archived_at timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  server_seq  bigint      NOT NULL DEFAULT nextval('sync_seq')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX programs_tenant_idx ON programs (tenant_id) WHERE archived_at IS NULL;
CREATE INDEX programs_seq_idx ON programs (tenant_id, server_seq);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER programs_set_updated_at
  BEFORE UPDATE ON programs FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER programs_bump_seq
  BEFORE INSERT OR UPDATE ON programs FOR EACH ROW EXECUTE FUNCTION bump_sync_seq();
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE programs ENABLE ROW LEVEL SECURITY;
ALTER TABLE programs FORCE ROW LEVEL SECURITY;
CREATE POLICY programs_isolation ON programs
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id() AND current_client_id() IS NULL);
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Blocks: the phases of a mesocycle
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TABLE program_blocks (
  id          uuid PRIMARY KEY,
  tenant_id   uuid        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  program_id  uuid        NOT NULL REFERENCES programs(id) ON DELETE CASCADE,
  name        text        NOT NULL CHECK (length(btrim(name)) > 0),
  -- How many weeks this phase runs for. A four-week hypertrophy block repeats
  -- its days four times rather than storing twenty-eight copies of them.
  weeks       integer     NOT NULL DEFAULT 1 CHECK (weeks > 0 AND weeks <= 52),
  sort_order  integer     NOT NULL DEFAULT 0,
  notes       text        NOT NULL DEFAULT '',
  created_at  timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX program_blocks_program_idx ON program_blocks (program_id, sort_order);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE program_blocks ENABLE ROW LEVEL SECURITY;
ALTER TABLE program_blocks FORCE ROW LEVEL SECURITY;
CREATE POLICY program_blocks_isolation ON program_blocks
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id() AND current_client_id() IS NULL);
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Training days
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TABLE program_days (
  id         uuid PRIMARY KEY,
  tenant_id  uuid        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  block_id   uuid        NOT NULL REFERENCES program_blocks(id) ON DELETE CASCADE,
  name       text        NOT NULL CHECK (length(btrim(name)) > 0),
  sort_order integer     NOT NULL DEFAULT 0,
  notes      text        NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX program_days_block_idx ON program_days (block_id, sort_order);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE program_days ENABLE ROW LEVEL SECURITY;
ALTER TABLE program_days FORCE ROW LEVEL SECURITY;
CREATE POLICY program_days_isolation ON program_days
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id() AND current_client_id() IS NULL);
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Prescriptions
-- ---------------------------------------------------------------------------
-- What the trainer asked for. What the client actually did lives in set_logs;
-- keeping the two apart is what makes progression measurable.
-- +goose StatementBegin
CREATE TABLE program_exercises (
  id           uuid PRIMARY KEY,
  tenant_id    uuid    NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  day_id       uuid    NOT NULL REFERENCES program_days(id) ON DELETE CASCADE,
  exercise_id  uuid    NOT NULL REFERENCES exercises(id) ON DELETE RESTRICT,
  sort_order   integer NOT NULL DEFAULT 0,

  target_sets  integer NOT NULL DEFAULT 3 CHECK (target_sets > 0 AND target_sets <= 50),
  -- Reps are a range because "8 to 12" is how coaches prescribe. A fixed
  -- prescription sets both bounds to the same number.
  target_reps_min integer CHECK (target_reps_min IS NULL OR target_reps_min >= 0),
  target_reps_max integer CHECK (target_reps_max IS NULL OR target_reps_max >= 0),
  -- Grams, like biometrics: kilograms or pounds is a display choice, and the
  -- stored value should never round.
  target_load_grams integer CHECK (target_load_grams IS NULL OR target_load_grams >= 0),
  -- RPE in tenths: 85 is RPE 8.5. Integers again, for the same reason.
  target_rpe_tenths integer CHECK (target_rpe_tenths IS NULL OR (target_rpe_tenths BETWEEN 0 AND 100)),
  target_rir        integer CHECK (target_rir IS NULL OR target_rir BETWEEN 0 AND 10),
  rest_seconds      integer CHECK (rest_seconds IS NULL OR rest_seconds >= 0),
  -- Tempo notation such as "3-1-1-0". Text because it is a notation, not a
  -- quantity to compute with.
  tempo        text NOT NULL DEFAULT '',
  -- Exercises sharing a group letter are performed back to back.
  superset_group text NOT NULL DEFAULT '',
  notes        text NOT NULL DEFAULT '',
  created_at   timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT program_exercises_rep_range CHECK (
    target_reps_min IS NULL OR target_reps_max IS NULL OR target_reps_max >= target_reps_min
  )
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX program_exercises_day_idx ON program_exercises (day_id, sort_order);
CREATE INDEX program_exercises_exercise_idx ON program_exercises (tenant_id, exercise_id);
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE program_exercises ENABLE ROW LEVEL SECURITY;
ALTER TABLE program_exercises FORCE ROW LEVEL SECURITY;
CREATE POLICY program_exercises_isolation ON program_exercises
  USING (tenant_id = current_tenant_id())
  WITH CHECK (tenant_id = current_tenant_id() AND current_client_id() IS NULL);
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Assignments
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TYPE assignment_status AS ENUM ('active', 'completed', 'paused', 'cancelled');
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE program_assignments (
  id         uuid PRIMARY KEY,
  tenant_id  uuid              NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  program_id uuid              NOT NULL REFERENCES programs(id) ON DELETE RESTRICT,
  client_id  uuid              NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
  starts_on  date              NOT NULL,
  ends_on    date,
  status     assignment_status NOT NULL DEFAULT 'active',
  notes      text              NOT NULL DEFAULT '',
  created_at timestamptz       NOT NULL DEFAULT now(),
  updated_at timestamptz       NOT NULL DEFAULT now(),
  server_seq bigint            NOT NULL DEFAULT nextval('sync_seq'),
  CHECK (ends_on IS NULL OR ends_on >= starts_on)
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX program_assignments_client_idx ON program_assignments (tenant_id, client_id, status);
CREATE INDEX program_assignments_seq_idx ON program_assignments (tenant_id, server_seq);
-- A client follows one programme at a time; two active at once is a mistake,
-- not a feature.
CREATE UNIQUE INDEX program_assignments_one_active
  ON program_assignments (client_id) WHERE status = 'active';
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER program_assignments_set_updated_at
  BEFORE UPDATE ON program_assignments FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER program_assignments_bump_seq
  BEFORE INSERT OR UPDATE ON program_assignments FOR EACH ROW EXECUTE FUNCTION bump_sync_seq();
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE program_assignments ENABLE ROW LEVEL SECURITY;
ALTER TABLE program_assignments FORCE ROW LEVEL SECURITY;
CREATE POLICY program_assignments_isolation ON program_assignments
  USING (tenant_id = current_tenant_id()
         AND (current_client_id() IS NULL OR client_id = current_client_id()))
  WITH CHECK (tenant_id = current_tenant_id() AND current_client_id() IS NULL);
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Workout sessions: what actually happened
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TYPE workout_status AS ENUM ('in_progress', 'completed', 'skipped');
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE workout_sessions (
  id            uuid PRIMARY KEY,
  tenant_id     uuid           NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  client_id     uuid           NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
  assignment_id uuid REFERENCES program_assignments(id) ON DELETE SET NULL,
  -- Which prescribed day this was. Null for an ad-hoc workout, because
  -- training happens that does not follow a plan.
  day_id        uuid REFERENCES program_days(id) ON DELETE SET NULL,
  -- The calendar session this was performed in, when there was one. Optional
  -- because a client may log a workout they did alone.
  session_id    uuid REFERENCES sessions(id) ON DELETE SET NULL,
  -- Which repeat of the block this was, so week 3 of a four-week block is
  -- distinguishable from week 1 without duplicating the prescription.
  week_number   integer        NOT NULL DEFAULT 1 CHECK (week_number > 0),
  performed_on  date           NOT NULL,
  status        workout_status NOT NULL DEFAULT 'in_progress',
  notes         text           NOT NULL DEFAULT '',
  created_at    timestamptz    NOT NULL DEFAULT now(),
  updated_at    timestamptz    NOT NULL DEFAULT now(),
  deleted_at    timestamptz,
  server_seq    bigint         NOT NULL DEFAULT nextval('sync_seq')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX workout_sessions_client_idx ON workout_sessions (tenant_id, client_id, performed_on DESC)
  WHERE deleted_at IS NULL;
CREATE INDEX workout_sessions_seq_idx ON workout_sessions (tenant_id, server_seq);
CREATE INDEX workout_sessions_session_idx ON workout_sessions (session_id) WHERE session_id IS NOT NULL;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER workout_sessions_set_updated_at
  BEFORE UPDATE ON workout_sessions FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER workout_sessions_bump_seq
  BEFORE INSERT OR UPDATE ON workout_sessions FOR EACH ROW EXECUTE FUNCTION bump_sync_seq();
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE workout_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE workout_sessions FORCE ROW LEVEL SECURITY;
-- A portal client logs their own sets, so this is one of the few tables they
-- may write to — but only rows that are their own.
CREATE POLICY workout_sessions_isolation ON workout_sessions
  USING (tenant_id = current_tenant_id()
         AND (current_client_id() IS NULL OR client_id = current_client_id()))
  WITH CHECK (tenant_id = current_tenant_id()
              AND (current_client_id() IS NULL OR client_id = current_client_id()));
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- Set logs
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE TABLE set_logs (
  id                 uuid PRIMARY KEY,
  tenant_id          uuid    NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  workout_session_id uuid    NOT NULL REFERENCES workout_sessions(id) ON DELETE CASCADE,
  exercise_id        uuid    NOT NULL REFERENCES exercises(id) ON DELETE RESTRICT,
  -- The prescription this set answers, when it followed one.
  program_exercise_id uuid REFERENCES program_exercises(id) ON DELETE SET NULL,
  set_index          integer NOT NULL CHECK (set_index > 0),

  reps         integer CHECK (reps IS NULL OR reps >= 0),
  load_grams   integer CHECK (load_grams IS NULL OR load_grams >= 0),
  rpe_tenths   integer CHECK (rpe_tenths IS NULL OR (rpe_tenths BETWEEN 0 AND 100)),
  rir          integer CHECK (rir IS NULL OR rir BETWEEN 0 AND 10),
  rest_seconds integer CHECK (rest_seconds IS NULL OR rest_seconds >= 0),
  tempo        text    NOT NULL DEFAULT '',
  -- A warm-up set counts as work done but not as a working set for
  -- progression, so it is flagged rather than inferred from the load.
  is_warmup    boolean NOT NULL DEFAULT false,
  completed    boolean NOT NULL DEFAULT true,
  notes        text    NOT NULL DEFAULT '',

  -- A clip the client attached for asynchronous form feedback.
  form_check_media_id uuid REFERENCES media_objects(id) ON DELETE SET NULL,

  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  server_seq   bigint      NOT NULL DEFAULT nextval('sync_seq')
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX set_logs_unique ON set_logs (workout_session_id, exercise_id, set_index);
CREATE INDEX set_logs_session_idx ON set_logs (workout_session_id);
-- Serves previous-session lookup: the last time this client did this lift.
CREATE INDEX set_logs_history_idx ON set_logs (tenant_id, exercise_id, created_at DESC);
CREATE INDEX set_logs_seq_idx ON set_logs (tenant_id, server_seq);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER set_logs_set_updated_at
  BEFORE UPDATE ON set_logs FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER set_logs_bump_seq
  BEFORE INSERT OR UPDATE ON set_logs FOR EACH ROW EXECUTE FUNCTION bump_sync_seq();
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE set_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE set_logs FORCE ROW LEVEL SECURITY;
CREATE POLICY set_logs_isolation ON set_logs
  USING (tenant_id = current_tenant_id()
         AND (current_client_id() IS NULL OR EXISTS (
              SELECT 1 FROM workout_sessions ws
               WHERE ws.id = set_logs.workout_session_id
                 AND ws.client_id = current_client_id())))
  WITH CHECK (tenant_id = current_tenant_id()
              AND (current_client_id() IS NULL OR EXISTS (
                   SELECT 1 FROM workout_sessions ws
                    WHERE ws.id = set_logs.workout_session_id
                      AND ws.client_id = current_client_id())));
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
DROP TABLE IF EXISTS set_logs;
DROP TABLE IF EXISTS workout_sessions;
DROP TYPE IF EXISTS workout_status;
DROP TABLE IF EXISTS program_assignments;
DROP TYPE IF EXISTS assignment_status;
DROP TABLE IF EXISTS program_exercises;
DROP TABLE IF EXISTS program_days;
DROP TABLE IF EXISTS program_blocks;
DROP TABLE IF EXISTS programs;
DROP TABLE IF EXISTS exercises;
DROP TYPE IF EXISTS exercise_category;
-- +goose StatementEnd
