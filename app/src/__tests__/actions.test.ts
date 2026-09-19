/**
 * The write path, against real SQLite.
 *
 * What these assert is the discipline the actions exist to enforce: a local
 * effect the trainer sees at once, an operation queued for the server, and
 * *nothing guessed* about the numbers the server owns.
 */

import { MemoryDatabase } from './support';
import {
  cloneLastSession, completeWorkout, createClient, logSet,
  markAttendance, recordPayment, startWorkout,
} from '@/features/actions';
import { previousSets, workoutSets } from '@/features/queries';
import * as outbox from '@/sync/outbox';

describe('actions', () => {
  let db: MemoryDatabase;

  beforeEach(async () => {
    db = new MemoryDatabase();
    await db.execute(
      `INSERT INTO clients (id, full_name, status, notes) VALUES ('c1','Dana Rivers','active','')`,
    );
    await db.execute(
      `INSERT INTO exercises (id, name, category) VALUES ('e1','Back squat','squat')`,
    );
  });
  afterEach(() => { db.close(); });

  it('marks attendance locally and queues the intent', async () => {
    await db.execute(`INSERT INTO sessions (id, starts_at, status) VALUES ('s1','2026-05-01T09:00:00Z','scheduled')`);
    await db.execute(`INSERT INTO session_attendees (id, session_id, client_id, status)
      VALUES ('a1','s1','c1','scheduled')`);
    await db.execute(`INSERT INTO packages (id, client_id, credits_total, credits_remaining, status)
      VALUES ('p1','c1',10,7,'active')`);

    await markAttendance(db, 'a1', 'completed');

    const attendee = await db.selectOne<{ status: string }>(
      'SELECT status FROM session_attendees WHERE id = ?', ['a1'],
    );
    expect(attendee?.status).toBe('completed');

    const queued = await outbox.pending(db);
    expect(queued).toHaveLength(1);
    expect(queued[0]?.type).toBe('attendance.mark');
    expect(queued[0]?.data.attendee_id).toBe('a1');

    // The credit is deliberately NOT burned locally: which pack it comes from
    // and what revenue it recognises are the server's decisions, and a guess
    // here would be contradicted the moment the sync answers.
    const pack = await db.selectOne<{ credits_remaining: number }>(
      'SELECT credits_remaining FROM packages WHERE id = ?', ['p1'],
    );
    expect(pack?.credits_remaining).toBe(7);
  });

  it('leaves the invoice unsettled when a payment is recorded', async () => {
    await db.execute(`INSERT INTO invoices (id, client_id, number, status, currency, total_minor)
      VALUES ('i1','c1','2026-001','issued','EUR',50000)`);

    await recordPayment(db, 'i1', 50000, 'bank_transfer', { currency: 'EUR' });

    const invoice = await db.selectOne<{ status: string }>(
      'SELECT status FROM invoices WHERE id = ?', ['i1'],
    );
    // Whether this settles depends on what else has been paid, possibly from
    // another device, and on the server's overpayment rule.
    expect(invoice?.status).toBe('issued');
    expect((await outbox.pending(db))[0]?.type).toBe('payment.record');
  });

  it('overwrites a set rather than adding a second one at the same index', async () => {
    const workout = await startWorkout(db, 'c1');
    await logSet(db, workout, 'e1', 1, { reps: 8, loadGrams: 80_000 });
    await logSet(db, workout, 'e1', 1, { reps: 8, loadGrams: 82_500 });

    const sets = await workoutSets(db, workout);
    expect(sets).toHaveLength(1);
    expect(sets[0]?.loadGrams).toBe(82_500);
  });

  it('rolls the local write back when queueing fails', async () => {
    const workout = await startWorkout(db, 'c1');
    const broken = Object.create(db) as MemoryDatabase & { execute: typeof db.execute };
    broken.execute = async (sql: string, params?: unknown[]) => {
      if (sql.includes('INSERT INTO outbox')) throw new Error('disk full');
      return db.execute(sql, params);
    };

    await expect(logSet(broken, workout, 'e1', 1, { reps: 8 })).rejects.toThrow('disk full');

    // A set that exists locally but was never queued would be lost silently at
    // the next pull, which is worse than the write failing loudly.
    expect(await workoutSets(db, workout)).toHaveLength(0);
  });

  describe('single-tap cloning', () => {
    async function lastWeek(): Promise<void> {
      await db.execute(
        `INSERT INTO workout_sessions (id, client_id, performed_on, status)
         VALUES ('w0','c1','2026-04-24','completed')`,
      );
      await db.execute(
        `INSERT INTO set_logs (id, workout_session_id, exercise_id, set_index, reps, load_grams, rpe_tenths, is_warmup, completed)
         VALUES ('s0','w0','e1',1,20,20000,60,1,1),
                ('s1','w0','e1',2,8,80000,80,0,1),
                ('s2','w0','e1',3,8,80000,85,0,1)`,
      );
    }

    it('brings last session forward as logged but not completed', async () => {
      await lastWeek();
      const workout = await startWorkout(db, 'c1');

      expect(await cloneLastSession(db, workout, 'c1', 'e1')).toBe(2);

      const sets = await workoutSets(db, workout);
      expect(sets.map((s) => s.loadGrams)).toEqual([80_000, 80_000]);
      // The whole point: prefilled numbers that silently counted as performed
      // would put lifts in the client's history that never happened.
      expect(sets.every((s) => s.completed === 0)).toBe(true);
    });

    it('numbers the cloned sets from one', async () => {
      await lastWeek();
      const workout = await startWorkout(db, 'c1');
      await cloneLastSession(db, workout, 'c1', 'e1');

      // Last week's working sets were indices 2 and 3 — index 1 was the
      // warm-up, which is not cloned. Carrying the indices across opened a
      // fresh workout at "set 2".
      expect((await workoutSets(db, workout)).map((s) => s.setIndex)).toEqual([1, 2]);
    });

    it('skips the warm-up, so a light opener is not next week’s working weight', async () => {
      await lastWeek();
      const workout = await startWorkout(db, 'c1');
      await cloneLastSession(db, workout, 'c1', 'e1');

      const sets = await workoutSets(db, workout);
      expect(sets).toHaveLength(2);
      expect(sets.some((s) => s.loadGrams === 20_000)).toBe(false);
    });

    it('does not clone from the workout in progress', async () => {
      const workout = await startWorkout(db, 'c1');
      await logSet(db, workout, 'e1', 1, { reps: 5, loadGrams: 100_000 });

      expect(await previousSets(db, 'c1', 'e1', workout)).toHaveLength(0);
      expect(await cloneLastSession(db, workout, 'c1', 'e1')).toBe(0);
      expect(await workoutSets(db, workout)).toHaveLength(1);
    });

    it('queues one operation per cloned set', async () => {
      await lastWeek();
      const workout = await startWorkout(db, 'c1');
      await cloneLastSession(db, workout, 'c1', 'e1');

      const queued = await outbox.pending(db);
      expect(queued.filter((e) => e.type === 'workout.log_set')).toHaveLength(2);
      expect(queued.every((e) => e.type !== 'workout.log_set' || e.data.completed === false)).toBe(true);
    });
  });

  it('queues a client before the server has ever heard of them', async () => {
    const id = await createClient(db, 'Morgan Hale', { email: 'morgan@example.com' });

    const client = await db.selectOne<{ full_name: string }>(
      'SELECT full_name FROM clients WHERE id = ?', [id],
    );
    expect(client?.full_name).toBe('Morgan Hale');
    expect((await outbox.pending(db))[0]?.type).toBe('client.create');
  });

  it('completes a workout locally', async () => {
    const workout = await startWorkout(db, 'c1');
    await completeWorkout(db, workout, 'strong session');

    const row = await db.selectOne<{ status: string; notes: string }>(
      'SELECT status, notes FROM workout_sessions WHERE id = ?', [workout],
    );
    expect(row?.status).toBe('completed');
    expect(row?.notes).toBe('strong session');
  });
});
