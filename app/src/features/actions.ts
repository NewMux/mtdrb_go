/**
 * What the trainer can do, and what happens locally when they do it.
 *
 * Each action writes the local mirror optimistically and queues an operation.
 * The UI reads the local write immediately, so tapping "completed" between
 * sets is instant whether or not there is signal.
 *
 * The optimistic write is deliberately conservative about anything the server
 * owns. Marking attendance shows the new status at once, because that is what
 * the trainer just chose — but the credit balance is *not* decremented locally,
 * because whether a credit is available, which pack it comes from and what
 * revenue it earns are the server's decisions. Guessing would mean showing a
 * balance that a refusal later contradicts.
 */

import type { Database } from '@/db/types';
import type { AttendanceStatus, OperationType, PaymentInstrument } from '@/api/types';
import { enqueue } from '@/sync/outbox';
import { previousSets } from './queries';
import { newId } from '@/lib/id';

/** Queues an operation and applies its local effect in one transaction. */
async function act(
  db: Database,
  type: OperationType,
  data: Record<string, unknown> | ((id: string) => Record<string, unknown>),
  localEffect?: (id: string) => Promise<void>,
): Promise<string> {
  const id = newId();
  const queuedAt = new Date();
  await db.transaction(async () => {
    if (localEffect) await localEffect(id);
    await enqueue(db, id, type, typeof data === 'function' ? data(id) : data, queuedAt);
  });
  return id;
}

/**
 * Marks one attendee's outcome.
 *
 * The status is applied locally at once. The credit balance is not: the server
 * decides whether a credit was available and which pack it came from, and a
 * local guess would be contradicted the moment the sync answers.
 */
export async function markAttendance(
  db: Database,
  attendeeId: string,
  status: AttendanceStatus,
  opts: { notes?: string; allowOverdraft?: boolean } = {},
): Promise<string> {
  return act(
    db,
    'attendance.mark',
    {
      attendee_id: attendeeId,
      status,
      notes: opts.notes ?? '',
      allow_overdraft: opts.allowOverdraft ?? false,
    },
    async () => {
      await db.execute(
        `UPDATE session_attendees SET status = ?, marked_at = ?, notes = CASE WHEN ? = '' THEN notes ELSE ? END
          WHERE id = ?`,
        [status, new Date().toISOString(), opts.notes ?? '', opts.notes ?? '', attendeeId],
      );
    },
  );
}

/** Opens a workout to log sets against. */
export async function startWorkout(
  db: Database,
  clientId: string,
  opts: { dayId?: string; sessionId?: string; weekNumber?: number; performedOn?: Date } = {},
): Promise<string> {
  const performedOn = (opts.performedOn ?? new Date()).toISOString().slice(0, 10);

  return act(
    db,
    'workout.start',
    (id) => ({
      // The id travels with the operation. Without it the server mints its own
      // and every set queued behind this workout — in this same push batch —
      // references a workout the server has never heard of.
      id,
      client_id: clientId,
      day_id: opts.dayId ?? null,
      session_id: opts.sessionId ?? null,
      week_number: opts.weekNumber ?? 1,
      performed_on: performedOn,
    }),
    async (id) => {
      // The workout exists locally under the id the device minted, so sets can
      // reference it before the server has ever heard of it.
      await db.execute(
        `INSERT INTO workout_sessions
           (id, client_id, day_id, session_id, week_number, performed_on, status, notes)
         VALUES (?, ?, ?, ?, ?, ?, 'in_progress', '')`,
        [
          id,
          clientId,
          opts.dayId ?? null,
          opts.sessionId ?? null,
          opts.weekNumber ?? 1,
          performedOn,
        ],
      );
    },
  );
}

export interface SetInput {
  reps?: number | null;
  loadGrams?: number | null;
  rpeTenths?: number | null;
  rir?: number | null;
  restSeconds?: number | null;
  tempo?: string;
  isWarmup?: boolean;
  completed?: boolean;
  notes?: string;
}

/**
 * Logs a set.
 *
 * Keyed locally on (workout, exercise, set index) exactly as on the server, so
 * correcting a mistyped weight overwrites the set rather than adding another —
 * and a replay converges on the same row.
 */
export async function logSet(
  db: Database,
  workoutId: string,
  exerciseId: string,
  setIndex: number,
  input: SetInput = {},
): Promise<string> {
  const completed = input.completed ?? true;

  // The row id is resolved before the operation is queued, not inside the
  // local effect, because the server has to be told the same id. Correcting a
  // typed weight reuses the existing row's id so the correction lands on the
  // set already there, on the device and on the server alike.
  const existing = await db.selectOne<{ id: string }>(
    `SELECT id FROM set_logs WHERE workout_session_id = ? AND exercise_id = ? AND set_index = ?`,
    [workoutId, exerciseId, setIndex],
  );

  return act(
    db,
    'workout.log_set',
    (id) => ({
      id: existing?.id ?? id,
      workout_id: workoutId,
      exercise_id: exerciseId,
      set_index: setIndex,
      reps: input.reps ?? null,
      load_grams: input.loadGrams ?? null,
      rpe_tenths: input.rpeTenths ?? null,
      rir: input.rir ?? null,
      rest_seconds: input.restSeconds ?? null,
      tempo: input.tempo ?? '',
      is_warmup: input.isWarmup ?? false,
      completed,
      notes: input.notes ?? '',
    }),
    async (id) => {
      const rowId = existing?.id ?? id;

      await db.execute(
        `INSERT INTO set_logs
           (id, workout_session_id, exercise_id, set_index, reps, load_grams,
            rpe_tenths, rir, rest_seconds, tempo, is_warmup, completed, notes)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
         ON CONFLICT (id) DO UPDATE SET
           reps = excluded.reps, load_grams = excluded.load_grams,
           rpe_tenths = excluded.rpe_tenths, rir = excluded.rir,
           rest_seconds = excluded.rest_seconds, tempo = excluded.tempo,
           is_warmup = excluded.is_warmup, completed = excluded.completed,
           notes = excluded.notes`,
        [
          rowId, workoutId, exerciseId, setIndex,
          input.reps ?? null, input.loadGrams ?? null,
          input.rpeTenths ?? null, input.rir ?? null, input.restSeconds ?? null,
          input.tempo ?? '', input.isWarmup ? 1 : 0, completed ? 1 : 0,
          input.notes ?? '',
        ],
      );
    },
  );
}

/** Finishes a workout. */
export async function completeWorkout(
  db: Database,
  workoutId: string,
  notes = '',
): Promise<string> {
  return act(
    db,
    'workout.complete',
    { workout_id: workoutId, notes },
    async () => {
      await db.execute(
        `UPDATE workout_sessions SET status = 'completed',
            notes = CASE WHEN ? = '' THEN notes ELSE ? END
          WHERE id = ?`,
        [notes, notes, workoutId],
      );
    },
  );
}

/**
 * Records money received off-platform.
 *
 * The local invoice is *not* marked settled. Whether this payment settles it
 * depends on what else has been paid — possibly from another device — and on
 * the server's overpayment rule. Showing "paid" optimistically and then
 * discovering it was refused is precisely the kind of error that costs a
 * trainer money, so the payment is queued and the invoice waits for the answer.
 */
export async function recordPayment(
  db: Database,
  invoiceId: string,
  amountMinor: number,
  instrument: PaymentInstrument,
  opts: { currency?: string; receivedOn?: Date; reference?: string; notes?: string } = {},
): Promise<string> {
  return act(db, 'payment.record', {
    invoice_id: invoiceId,
    amount_minor: amountMinor,
    currency: opts.currency ?? '',
    instrument,
    received_on: (opts.receivedOn ?? new Date()).toISOString().slice(0, 10),
    reference: opts.reference ?? '',
    notes: opts.notes ?? '',
  });
}

/** Adds a client. */
export async function createClient(
  db: Database,
  fullName: string,
  opts: { email?: string; phone?: string; notes?: string } = {},
): Promise<string> {
  return act(
    db,
    'client.create',
    (id) => ({
      // Same id as the local row, so the client does not come back from the
      // next pull as a second person with the same name.
      id,
      full_name: fullName,
      email: opts.email ?? null,
      phone: opts.phone ?? null,
      notes: opts.notes ?? '',
    }),
    async (id) => {
      await db.execute(
        `INSERT INTO clients (id, full_name, email, phone, status, notes)
         VALUES (?, ?, ?, ?, 'active', ?)`,
        [id, fullName, opts.email ?? null, opts.phone ?? null, opts.notes ?? ''],
      );
    },
  );
}

/** Records a measurement. */
export async function recordBiometrics(
  db: Database,
  clientId: string,
  input: {
    measuredOn?: Date;
    weightGrams?: number;
    bodyFatBP?: number;
    circumferences?: Record<string, number>;
    notes?: string;
  },
): Promise<string> {
  const measuredOn = (input.measuredOn ?? new Date()).toISOString().slice(0, 10);

  return act(
    db,
    'biometrics.record',
    (id) => ({
      id,
      client_id: clientId,
      measured_on: measuredOn,
      weight_grams: input.weightGrams ?? null,
      body_fat_bp: input.bodyFatBP ?? null,
      circumferences: input.circumferences ?? {},
      notes: input.notes ?? '',
    }),
    async (id) => {
      await db.execute(
        `INSERT INTO biometric_entries
           (id, client_id, measured_on, weight_grams, body_fat_bp, circumferences, notes)
         VALUES (?, ?, ?, ?, ?, ?, ?)`,
        [
          id, clientId, measuredOn,
          input.weightGrams ?? null, input.bodyFatBP ?? null,
          JSON.stringify(input.circumferences ?? {}), input.notes ?? '',
        ],
      );
    },
  );
}

/**
 * The PRD's single-tap cloning.
 *
 * Writes last session's working sets into this workout as *logged but not
 * completed*. The distinction is the whole point: prefilled numbers that
 * silently counted as performed would put lifts in a client's history that
 * never happened. The trainer confirms each set as they do it.
 *
 * Warm-ups are not cloned — `previousSets` already excludes them, so a deload
 * opener cannot arrive as this week's working weight.
 */
export async function cloneLastSession(
  db: Database,
  workoutId: string,
  clientId: string,
  exerciseId: string,
): Promise<number> {
  const previous = await previousSets(db, clientId, exerciseId, workoutId);

  // Renumbered from one rather than copied. Last session's warm-up held index
  // 1, so carrying the indices across opened a fresh workout at "set 2".
  let index = 0;
  for (const set of previous) {
    index += 1;
    await logSet(db, workoutId, exerciseId, index, {
      reps: set.reps,
      loadGrams: set.loadGrams,
      rpeTenths: set.rpeTenths,
      isWarmup: false,
      completed: false,
    });
  }
  return previous.length;
}
