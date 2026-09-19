/**
 * Reads.
 *
 * Every one of these hits local SQLite, never the network. That is what makes
 * the app work in a basement, and it is why the sync engine's only job is to
 * keep this mirror honest in the background.
 */

import type { Database } from '@/db/types';
import type { AttendanceStatus } from '@/api/types';

export interface RosterEntry {
  attendeeId: string;
  sessionId: string;
  clientId: string;
  clientName: string;
  startsAt: string;
  endsAt: string;
  sessionTypeName: string;
  status: AttendanceStatus;
  location: string;
  /** From the local mirror; stale between syncs, and labelled as such in the UI. */
  creditsRemaining: number;
}

/** The day's roster, in time order — the first screen a trainer sees. */
export async function todaysRoster(db: Database, day: Date = new Date()): Promise<RosterEntry[]> {
  const start = new Date(day.getFullYear(), day.getMonth(), day.getDate()).toISOString();
  const end = new Date(day.getFullYear(), day.getMonth(), day.getDate() + 1).toISOString();

  return db.select<RosterEntry>(
    `SELECT sa.id AS attendeeId, s.id AS sessionId, c.id AS clientId,
            c.full_name AS clientName, s.starts_at AS startsAt, s.ends_at AS endsAt,
            coalesce(st.name, '') AS sessionTypeName, sa.status AS status,
            coalesce(s.location, '') AS location,
            coalesce((SELECT sum(p.credits_remaining) FROM packages p
                       WHERE p.client_id = c.id AND p.status IN ('active','exhausted')), 0) AS creditsRemaining
       FROM session_attendees sa
       JOIN sessions s ON s.id = sa.session_id
       JOIN clients c ON c.id = sa.client_id
       LEFT JOIN session_types st ON st.id = s.session_type_id
      WHERE s.starts_at >= ? AND s.starts_at < ?
        AND s.status != 'cancelled' AND c.deleted_at IS NULL
      ORDER BY s.starts_at, c.full_name`,
    [start, end],
  );
}

export interface ClientSummary {
  id: string;
  fullName: string;
  status: string;
  creditsRemaining: number;
  email: string | null;
  phone: string | null;
}

/** The client roster, searchable. */
export async function listClients(db: Database, search = ''): Promise<ClientSummary[]> {
  const term = search.trim();
  return db.select<ClientSummary>(
    `SELECT c.id, c.full_name AS fullName, c.status,
            coalesce((SELECT sum(p.credits_remaining) FROM packages p
                       WHERE p.client_id = c.id AND p.status IN ('active','exhausted')), 0) AS creditsRemaining,
            c.email, c.phone
       FROM clients c
      WHERE c.deleted_at IS NULL
        AND (? = '' OR c.full_name LIKE '%' || ? || '%'
             OR coalesce(c.email,'') LIKE '%' || ? || '%'
             OR coalesce(c.phone,'') LIKE '%' || ? || '%')
      ORDER BY c.full_name`,
    [term, term, term, term],
  );
}

export interface LowBalanceClient {
  id: string;
  fullName: string;
  creditsRemaining: number;
  nextExpiry: string | null;
  lastSessionOn: string | null;
}

/**
 * Clients at or under a credit threshold — the renewal list.
 *
 * Computed on the device as well as the server, unlike the rest of the
 * dashboard. The numbers beside it (revenue, receivables) are ledger
 * arithmetic and are meaningless stale, but this one drives a conversation
 * held standing next to the person it concerns, which is exactly where the
 * signal is worst.
 *
 * Ordered by who runs out first. A client with no pack at all counts as zero
 * and belongs here: they are the plainest renewal of the lot.
 */
export async function lowBalanceClients(
  db: Database, threshold = 2,
): Promise<LowBalanceClient[]> {
  return db.select<LowBalanceClient>(
    `SELECT c.id, c.full_name AS fullName,
            coalesce((SELECT sum(p.credits_remaining) FROM packages p
                       WHERE p.client_id = c.id
                         AND p.status IN ('active','exhausted')
                         AND p.credits_remaining <> 0), 0) AS creditsRemaining,
            (SELECT min(p.expires_at) FROM packages p
               WHERE p.client_id = c.id AND p.status = 'active'
                 AND p.credits_remaining > 0 AND p.expires_at IS NOT NULL) AS nextExpiry,
            (SELECT max(s.starts_at) FROM session_attendees sa
               JOIN sessions s ON s.id = sa.session_id
              WHERE sa.client_id = c.id AND sa.status = 'completed') AS lastSessionOn
       FROM clients c
      WHERE c.deleted_at IS NULL AND c.status = 'active'
        AND coalesce((SELECT sum(p.credits_remaining) FROM packages p
                       WHERE p.client_id = c.id
                         AND p.status IN ('active','exhausted')
                         AND p.credits_remaining <> 0), 0) <= ?
      ORDER BY creditsRemaining, nextExpiry IS NULL, nextExpiry, c.full_name`,
    [threshold],
  );
}

export interface LoggedSet {
  id: string;
  exerciseId: string;
  exerciseName: string;
  setIndex: number;
  reps: number | null;
  loadGrams: number | null;
  rpeTenths: number | null;
  isWarmup: number;
  completed: number;
}

/** The sets logged in a workout so far. */
export async function workoutSets(db: Database, workoutId: string): Promise<LoggedSet[]> {
  return db.select<LoggedSet>(
    `SELECT sl.id, sl.exercise_id AS exerciseId, coalesce(e.name, '') AS exerciseName,
            sl.set_index AS setIndex, sl.reps, sl.load_grams AS loadGrams,
            sl.rpe_tenths AS rpeTenths, sl.is_warmup AS isWarmup, sl.completed
       FROM set_logs sl
       LEFT JOIN exercises e ON e.id = sl.exercise_id
      WHERE sl.workout_session_id = ?
      ORDER BY e.name, sl.set_index`,
    [workoutId],
  );
}

/**
 * What this client did last time on this lift.
 *
 * The local half of the PRD's single-tap cloning: the numbers are already on
 * the device, so the prefill appears instantly and the server call that makes
 * it durable happens behind it.
 *
 * Only completed working sets count — prefilling from a warm-up would put
 * last week's 40kg opener in as this week's working weight.
 */
export async function previousSets(
  db: Database, clientId: string, exerciseId: string, excludeWorkoutId?: string,
): Promise<LoggedSet[]> {
  const previous = await db.selectOne<{ id: string }>(
    `SELECT ws.id
       FROM workout_sessions ws
       JOIN set_logs sl ON sl.workout_session_id = ws.id
      WHERE ws.client_id = ? AND sl.exercise_id = ?
        AND sl.completed = 1 AND sl.is_warmup = 0
        AND ws.deleted_at IS NULL AND (? IS NULL OR ws.id != ?)
      ORDER BY ws.performed_on DESC LIMIT 1`,
    [clientId, exerciseId, excludeWorkoutId ?? null, excludeWorkoutId ?? null],
  );
  if (!previous) return [];

  return db.select<LoggedSet>(
    `SELECT sl.id, sl.exercise_id AS exerciseId, coalesce(e.name,'') AS exerciseName,
            sl.set_index AS setIndex, sl.reps, sl.load_grams AS loadGrams,
            sl.rpe_tenths AS rpeTenths, sl.is_warmup AS isWarmup, sl.completed
       FROM set_logs sl
       LEFT JOIN exercises e ON e.id = sl.exercise_id
      WHERE sl.workout_session_id = ? AND sl.exercise_id = ? AND sl.is_warmup = 0
      ORDER BY sl.set_index`,
    [previous.id, exerciseId],
  );
}

export interface InvoiceSummary {
  id: string;
  number: string | null;
  clientName: string;
  status: string;
  currency: string;
  totalMinor: number;
  paidMinor: number;
  dueDate: string | null;
}

/**
 * Outstanding invoices, oldest due first — the chase list.
 *
 * Overdue is derived here exactly as it is on the server: an invoice is late
 * because the date passed, not because a job ran.
 */
export async function outstandingInvoices(
  db: Database, clientId?: string,
): Promise<InvoiceSummary[]> {
  return db.select<InvoiceSummary>(
    `SELECT i.id, i.number, c.full_name AS clientName, i.status, i.currency,
            i.total_minor AS totalMinor,
            coalesce((SELECT sum(p.amount_minor) FROM payments p WHERE p.invoice_id = i.id), 0) AS paidMinor,
            i.due_date AS dueDate
       FROM invoices i
       JOIN clients c ON c.id = i.client_id
      WHERE i.deleted_at IS NULL AND i.status IN ('issued', 'partially_paid')
        AND (? IS NULL OR i.client_id = ?)
      ORDER BY i.due_date IS NULL, i.due_date, i.issue_date`,
    [clientId ?? null, clientId ?? null],
  );
}

export interface ClientDetail extends ClientSummary {
  dateOfBirth: string | null;
  notes: string;
  allowOverdraft: number | null;
  emergencyContactName: string | null;
  emergencyContactPhone: string | null;
  /** The soonest pack expiry with credits left on it, or null. */
  nextExpiry: string | null;
}

/** One client, for their profile screen. */
export async function clientDetail(db: Database, id: string): Promise<ClientDetail | null> {
  return db.selectOne<ClientDetail>(
    `SELECT c.id, c.full_name AS fullName, c.status, c.email, c.phone,
            c.date_of_birth AS dateOfBirth, coalesce(c.notes, '') AS notes,
            c.allow_overdraft AS allowOverdraft,
            c.emergency_contact_name AS emergencyContactName,
            c.emergency_contact_phone AS emergencyContactPhone,
            coalesce((SELECT sum(p.credits_remaining) FROM packages p
                       WHERE p.client_id = c.id AND p.status IN ('active','exhausted')), 0) AS creditsRemaining,
            (SELECT min(p.expires_at) FROM packages p
               WHERE p.client_id = c.id AND p.status = 'active'
                 AND p.credits_remaining > 0 AND p.expires_at IS NOT NULL) AS nextExpiry
       FROM clients c
      WHERE c.id = ? AND c.deleted_at IS NULL`,
    [id],
  );
}

export interface WorkoutSummary {
  id: string;
  performedOn: string;
  status: string;
  setCount: number;
  /** Total load moved, in grams — reps times weight, summed over working sets. */
  volumeGrams: number;
}

/** A client's recent training, newest first. */
export async function recentWorkouts(
  db: Database, clientId: string, limit = 10,
): Promise<WorkoutSummary[]> {
  return db.select<WorkoutSummary>(
    `SELECT ws.id, ws.performed_on AS performedOn, ws.status,
            coalesce((SELECT count(*) FROM set_logs sl
                       WHERE sl.workout_session_id = ws.id AND sl.completed = 1), 0) AS setCount,
            coalesce((SELECT sum(sl.reps * sl.load_grams) FROM set_logs sl
                       WHERE sl.workout_session_id = ws.id
                         AND sl.completed = 1 AND sl.is_warmup = 0), 0) AS volumeGrams
       FROM workout_sessions ws
      WHERE ws.client_id = ? AND ws.deleted_at IS NULL
      ORDER BY ws.performed_on DESC, ws.id DESC
      LIMIT ?`,
    [clientId, limit],
  );
}

export interface BiometricEntry {
  id: string;
  measuredOn: string;
  weightGrams: number | null;
  bodyFatBP: number | null;
  notes: string;
}

/** A client's measurements, newest first. */
export async function biometricHistory(
  db: Database, clientId: string, limit = 12,
): Promise<BiometricEntry[]> {
  return db.select<BiometricEntry>(
    `SELECT id, measured_on AS measuredOn, weight_grams AS weightGrams,
            body_fat_bp AS bodyFatBP, coalesce(notes, '') AS notes
       FROM biometric_entries
      WHERE client_id = ? AND deleted_at IS NULL
      ORDER BY measured_on DESC, id DESC
      LIMIT ?`,
    [clientId, limit],
  );
}

/** A workout that was opened and never finished, so the floor logger can resume it. */
export async function openWorkout(
  db: Database, clientId: string,
): Promise<{ id: string; performedOn: string } | null> {
  return db.selectOne<{ id: string; performedOn: string }>(
    `SELECT id, performed_on AS performedOn
       FROM workout_sessions
      WHERE client_id = ? AND status = 'in_progress' AND deleted_at IS NULL
      ORDER BY performed_on DESC, id DESC LIMIT 1`,
    [clientId],
  );
}

/** A client's exercise library, for picking a lift on the floor. */
export async function searchExercises(db: Database, search = '', limit = 50) {
  const term = search.trim();
  return db.select<{ id: string; name: string; category: string; equipment: string }>(
    `SELECT id, name, coalesce(category,'') AS category, coalesce(equipment,'') AS equipment
       FROM exercises
      WHERE archived_at IS NULL
        AND (? = '' OR name LIKE '%' || ? || '%' OR coalesce(equipment,'') LIKE '%' || ? || '%')
      ORDER BY name LIMIT ?`,
    [term, term, term, limit],
  );
}
