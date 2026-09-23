/**
 * The local mirror of the server's schema.
 *
 * Only the tables a trainer needs on a gym floor with no signal are kept
 * locally. Reads always come from here, never from the network, which is what
 * makes the app usable in a basement — the sync engine's job is to keep this
 * honest in the background.
 *
 * Column names match the server's sync payload exactly, so a pulled row is
 * written without translation. Anything the server chooses not to send — an
 * encrypted medical note, a share token — simply has no column here.
 */

/** Tables the pull loop writes into, keyed by the collection name the server sends. */
export const SYNC_TABLES = [
  'clients',
  'biometric_entries',
  'parq_responses',
  'waiver_signatures',
  'packages',
  'credit_transactions',
  'session_types',
  'sessions',
  'session_attendees',
  'invoices',
  'payments',
  'exercises',
  'programs',
  'program_assignments',
  'workout_sessions',
  'set_logs',
  'settings',
  'locations',
  'package_offers',
] as const;

export type SyncTable = (typeof SYNC_TABLES)[number];

/**
 * The schema every device started with — version 1.
 *
 * Frozen. A device that already has these tables will never run a changed
 * CREATE TABLE IF NOT EXISTS, so editing a statement here would silently fork
 * old devices from new ones. Changes go in UPGRADES instead.
 *
 * Deliberately loose about types: SQLite is dynamically typed anyway, and a
 * local schema that rejects a row the server considers valid would strand a
 * device. Money and loads stay integers, as they are everywhere else.
 */
export const BASE_SCHEMA: string[] = [
  `CREATE TABLE IF NOT EXISTS clients (
     id TEXT PRIMARY KEY NOT NULL,
     full_name TEXT NOT NULL,
     email TEXT, phone TEXT, date_of_birth TEXT,
     status TEXT NOT NULL DEFAULT 'active',
     emergency_contact_name TEXT, emergency_contact_phone TEXT,
     allow_overdraft INTEGER, default_rate_minor INTEGER,
     notes TEXT NOT NULL DEFAULT '',
     created_at TEXT, updated_at TEXT, deleted_at TEXT
   )`,
  `CREATE INDEX IF NOT EXISTS clients_name_idx ON clients (full_name)`,

  `CREATE TABLE IF NOT EXISTS biometric_entries (
     id TEXT PRIMARY KEY NOT NULL, client_id TEXT NOT NULL,
     measured_on TEXT NOT NULL, weight_grams INTEGER, body_fat_bp INTEGER,
     circumferences TEXT, notes TEXT NOT NULL DEFAULT '',
     created_at TEXT, updated_at TEXT, deleted_at TEXT
   )`,
  `CREATE INDEX IF NOT EXISTS biometrics_client_idx ON biometric_entries (client_id, measured_on DESC)`,

  `CREATE TABLE IF NOT EXISTS parq_responses (
     id TEXT PRIMARY KEY NOT NULL, client_id TEXT NOT NULL,
     answers TEXT, requires_clearance INTEGER, cleared_at TEXT, completed_at TEXT
   )`,

  `CREATE TABLE IF NOT EXISTS waiver_signatures (
     id TEXT PRIMARY KEY NOT NULL, waiver_id TEXT, client_id TEXT NOT NULL,
     signed_name TEXT, signed_at TEXT, signature_media_id TEXT
   )`,

  `CREATE TABLE IF NOT EXISTS packages (
     id TEXT PRIMARY KEY NOT NULL, client_id TEXT NOT NULL, invoice_id TEXT,
     name TEXT, credits_total INTEGER, credits_remaining INTEGER,
     unit_price_minor INTEGER, currency TEXT, purchased_on TEXT,
     expires_at TEXT, status TEXT
   )`,
  `CREATE INDEX IF NOT EXISTS packages_client_idx ON packages (client_id, status)`,

  `CREATE TABLE IF NOT EXISTS credit_transactions (
     id TEXT PRIMARY KEY NOT NULL, package_id TEXT, client_id TEXT,
     delta INTEGER, reason TEXT, session_attendee_id TEXT,
     journal_entry_id TEXT, memo TEXT, created_at TEXT
   )`,

  `CREATE TABLE IF NOT EXISTS session_types (
     id TEXT PRIMARY KEY NOT NULL, name TEXT NOT NULL,
     duration_minutes INTEGER, capacity INTEGER, credit_cost INTEGER,
     colour TEXT, archived_at TEXT
   )`,

  `CREATE TABLE IF NOT EXISTS sessions (
     id TEXT PRIMARY KEY NOT NULL, session_type_id TEXT, series_id TEXT,
     starts_at TEXT NOT NULL, ends_at TEXT, status TEXT,
     location TEXT, notes TEXT, created_at TEXT, updated_at TEXT
   )`,
  `CREATE INDEX IF NOT EXISTS sessions_calendar_idx ON sessions (starts_at)`,

  `CREATE TABLE IF NOT EXISTS session_attendees (
     id TEXT PRIMARY KEY NOT NULL, session_id TEXT NOT NULL, client_id TEXT NOT NULL,
     status TEXT NOT NULL DEFAULT 'scheduled', credits_charged INTEGER DEFAULT 0,
     marked_at TEXT, notes TEXT, created_at TEXT, updated_at TEXT
   )`,
  `CREATE INDEX IF NOT EXISTS attendees_session_idx ON session_attendees (session_id)`,

  `CREATE TABLE IF NOT EXISTS invoices (
     id TEXT PRIMARY KEY NOT NULL, client_id TEXT NOT NULL, number TEXT,
     status TEXT, currency TEXT, total_minor INTEGER,
     issue_date TEXT, due_date TEXT, notes TEXT,
     payment_instructions_snapshot TEXT,
     issued_at TEXT, settled_at TEXT, voided_at TEXT,
     created_at TEXT, updated_at TEXT, deleted_at TEXT
   )`,
  `CREATE INDEX IF NOT EXISTS invoices_client_idx ON invoices (client_id)`,

  `CREATE TABLE IF NOT EXISTS payments (
     id TEXT PRIMARY KEY NOT NULL, invoice_id TEXT NOT NULL, client_id TEXT,
     amount_minor INTEGER, currency TEXT, instrument TEXT, received_on TEXT,
     reference TEXT, notes TEXT, journal_entry_id TEXT,
     reversed_by TEXT, reverses_id TEXT, created_at TEXT
   )`,
  `CREATE INDEX IF NOT EXISTS payments_invoice_idx ON payments (invoice_id)`,

  `CREATE TABLE IF NOT EXISTS exercises (
     id TEXT PRIMARY KEY NOT NULL, name TEXT NOT NULL, category TEXT,
     primary_muscle TEXT, equipment TEXT, instructions TEXT,
     video_url TEXT, demo_media_id TEXT, is_custom INTEGER, archived_at TEXT
   )`,
  `CREATE INDEX IF NOT EXISTS exercises_name_idx ON exercises (name)`,

  `CREATE TABLE IF NOT EXISTS programs (
     id TEXT PRIMARY KEY NOT NULL, name TEXT NOT NULL, description TEXT,
     is_template INTEGER, archived_at TEXT, created_at TEXT, updated_at TEXT
   )`,

  `CREATE TABLE IF NOT EXISTS program_assignments (
     id TEXT PRIMARY KEY NOT NULL, program_id TEXT, client_id TEXT,
     starts_on TEXT, ends_on TEXT, status TEXT, notes TEXT,
     created_at TEXT, updated_at TEXT
   )`,

  `CREATE TABLE IF NOT EXISTS workout_sessions (
     id TEXT PRIMARY KEY NOT NULL, client_id TEXT NOT NULL, assignment_id TEXT,
     day_id TEXT, session_id TEXT, week_number INTEGER,
     performed_on TEXT, status TEXT, notes TEXT,
     created_at TEXT, updated_at TEXT, deleted_at TEXT
   )`,
  `CREATE INDEX IF NOT EXISTS workouts_client_idx ON workout_sessions (client_id, performed_on DESC)`,

  `CREATE TABLE IF NOT EXISTS set_logs (
     id TEXT PRIMARY KEY NOT NULL, workout_session_id TEXT NOT NULL,
     exercise_id TEXT NOT NULL, program_exercise_id TEXT,
     set_index INTEGER NOT NULL, reps INTEGER, load_grams INTEGER,
     rpe_tenths INTEGER, rir INTEGER, rest_seconds INTEGER, tempo TEXT,
     is_warmup INTEGER DEFAULT 0, completed INTEGER DEFAULT 1,
     notes TEXT, form_check_media_id TEXT, created_at TEXT, updated_at TEXT
   )`,
  `CREATE INDEX IF NOT EXISTS set_logs_workout_idx ON set_logs (workout_session_id)`,
  `CREATE INDEX IF NOT EXISTS set_logs_history_idx ON set_logs (exercise_id)`,

  /**
   * The outbox.
   *
   * Every write the trainer makes lands here first and is applied to the local
   * tables optimistically, so the UI responds at once whether or not there is
   * signal. The engine drains it in queued order.
   *
   * `status` is not a nicety: an operation the server refused must stop being
   * retried and start being shown to the trainer, or the queue jams behind it
   * forever.
   */
  `CREATE TABLE IF NOT EXISTS outbox (
     id TEXT PRIMARY KEY NOT NULL,
     type TEXT NOT NULL,
     data TEXT NOT NULL,
     queued_at TEXT NOT NULL,
     status TEXT NOT NULL DEFAULT 'pending',
     attempts INTEGER NOT NULL DEFAULT 0,
     last_attempt_at TEXT,
     error_code TEXT,
     error_message TEXT
   )`,
  `CREATE INDEX IF NOT EXISTS outbox_pending_idx ON outbox (status, queued_at)`,

  /** Key/value for the sync cursor and similar device-local state. */
  `CREATE TABLE IF NOT EXISTS meta (
     key TEXT PRIMARY KEY NOT NULL,
     value TEXT NOT NULL
   )`,
];

/**
 * One step forward from the frozen base.
 *
 * `resync` names the collections whose rows are already on the device without
 * the columns this step adds. Pull drops columns a device does not know, and
 * the device's cursor is already past those rows, so without a resync the new
 * fields would reach a device only when each row next changed — possibly
 * never. The server restarts just those collections on the next pull.
 */
export interface SchemaUpgrade {
  version: number;
  statements: string[];
  resync?: SyncTable[];
}

/** Every step after version 1, in order. Append only. */
export const UPGRADES: SchemaUpgrade[] = [
  {
    // The practice's own row: the week the calendar draws, the hours a
    // booking must fit, and the plan state that decides whether the outbox
    // may send. One row, keyed by the tenant id.
    version: 2,
    statements: [
      `CREATE TABLE IF NOT EXISTS settings (
         id TEXT PRIMARY KEY NOT NULL,
         business_name TEXT, currency TEXT, timezone TEXT, country TEXT,
         language TEXT, document_language TEXT, digits TEXT,
         week_start INTEGER, working_hours TEXT,
         session_timeout_days INTEGER, buffer_minutes INTEGER,
         allow_overdraft INTEGER, no_show_is_billable INTEGER, low_balance_threshold INTEGER,
         plan TEXT, plan_status TEXT, trial_ends_at TEXT, plan_renews_on TEXT,
         cancel_at_period_end INTEGER, updated_at TEXT
       )`,
    ],
  },
  {
    // Where the practice works and what it sells. Sessions gain the place
    // they were booked at; the device already holds its sessions without
    // the column, so they are pulled again.
    version: 3,
    statements: [
      `CREATE TABLE IF NOT EXISTS locations (
         id TEXT PRIMARY KEY NOT NULL,
         name TEXT NOT NULL, kind TEXT, address TEXT, region TEXT, colour TEXT,
         is_primary INTEGER, archived_at TEXT, updated_at TEXT
       )`,
      `CREATE TABLE IF NOT EXISTS package_offers (
         id TEXT PRIMARY KEY NOT NULL,
         name TEXT NOT NULL, description TEXT, kind TEXT, credits INTEGER,
         price_minor INTEGER, currency TEXT, price_includes_vat INTEGER,
         validity_days INTEGER, cycle TEXT, session_type_id TEXT,
         sort_order INTEGER, archived_at TEXT, updated_at TEXT
       )`,
      `ALTER TABLE sessions ADD COLUMN location_id TEXT`,
    ],
    resync: ['sessions'],
  },
];

/** The version a device is at once every upgrade has run. */
export const SCHEMA_VERSION = UPGRADES.reduce((v, u) => Math.max(v, u.version), 1);
