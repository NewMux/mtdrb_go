/**
 * A practice with a day in it, seeded straight into the local database.
 *
 * Demo mode exists because the app is offline-first all the way down: every
 * screen reads local SQLite, and the server's only job is to keep that mirror
 * honest. So a build with no server is not a mock of the app — it *is* the
 * app, with the sync engine idle. The queries, the credit arithmetic, the
 * outbox and the floor logger are the real ones.
 *
 * What it cannot show is anything the server decides: credits are not really
 * burned, revenue is not really recognised, and an invoice is not really
 * settled. Those refusals and postings live in the ledger, behind an API. The
 * screens say "waiting on the server" and, here, nothing ever answers.
 */

import type { Database } from '@/db/types';
import { newId } from '@/lib/id';
import type { Account } from '@/api/types';

export const DEMO_ACCOUNT: Account = {
  user_id: 'demo-user',
  tenant_id: 'demo-tenant',
  email: 'sam@riverastrength.example',
  display_name: 'Sam Rivera',
  role: 'trainer',
  currency: 'EUR',
};

/** Exercises, by movement pattern — the way a coach checks a session is balanced. */
const EXERCISES: [string, string, string][] = [
  ['Back Squat', 'squat', 'barbell'],
  ['Front Squat', 'squat', 'barbell'],
  ['Goblet Squat', 'squat', 'dumbbell'],
  ['Bulgarian Split Squat', 'lunge', 'dumbbell'],
  ['Walking Lunge', 'lunge', 'dumbbell'],
  ['Deadlift', 'hinge', 'barbell'],
  ['Romanian Deadlift', 'hinge', 'barbell'],
  ['Hip Thrust', 'hinge', 'barbell'],
  ['Kettlebell Swing', 'hinge', 'kettlebell'],
  ['Bench Press', 'horizontal push', 'barbell'],
  ['Incline Dumbbell Press', 'horizontal push', 'dumbbell'],
  ['Push-up', 'horizontal push', 'bodyweight'],
  ['Overhead Press', 'vertical push', 'barbell'],
  ['Dumbbell Shoulder Press', 'vertical push', 'dumbbell'],
  ['Barbell Row', 'horizontal pull', 'barbell'],
  ['Seated Cable Row', 'horizontal pull', 'cable'],
  ['Pull-up', 'vertical pull', 'bodyweight'],
  ['Lat Pulldown', 'vertical pull', 'cable'],
  ['Plank', 'core', 'bodyweight'],
  ['Pallof Press', 'core', 'cable'],
];

interface Person {
  name: string;
  email: string;
  hour: number;
  credits: number;
  /** Whether their pack is still owed for, which is what Money shows. */
  owes: boolean;
}

const PEOPLE: Person[] = [
  { name: 'Dana Rivers', email: 'dana@example.com', hour: 9, credits: 7, owes: false },
  { name: 'Morgan Hale', email: 'morgan@example.com', hour: 11, credits: 3, owes: true },
  { name: 'Priya Raman', email: 'priya@example.com', hour: 17, credits: 0, owes: true },
];

const iso = (d: Date) => d.toISOString();
const day = (d: Date) => d.toISOString().slice(0, 10);

/**
 * Fills an empty database.
 *
 * Returns immediately if anything is already there, so a reload keeps whatever
 * the person did last time rather than wiping it.
 */
export async function seedDemo(db: Database): Promise<void> {
  const existing = await db.selectOne<{ n: number }>('SELECT count(*) AS n FROM clients');
  if ((existing?.n ?? 0) > 0) return;

  const now = new Date();
  const at = (hour: number, offsetDays = 0) => {
    const d = new Date(now);
    d.setDate(d.getDate() + offsetDays);
    d.setHours(hour, 0, 0, 0);
    return d;
  };

  await db.transaction(async () => {
    const exerciseIds = new Map<string, string>();
    for (const [name, category, equipment] of EXERCISES) {
      const id = newId();
      exerciseIds.set(name, id);
      await db.execute(
        `INSERT INTO exercises (id, name, category, equipment, is_custom) VALUES (?, ?, ?, ?, 0)`,
        [id, name, category, equipment],
      );
    }

    const typeId = newId();
    await db.execute(
      `INSERT INTO session_types (id, name, duration_minutes, capacity, credit_cost)
       VALUES (?, '1-on-1', 60, 1, 1)`,
      [typeId],
    );

    let invoiceNumber = 1;

    for (const person of PEOPLE) {
      const clientId = newId();
      await db.execute(
        `INSERT INTO clients (id, full_name, email, status, notes, created_at, updated_at)
         VALUES (?, ?, ?, 'active', '', ?, ?)`,
        [clientId, person.name, person.email, iso(now), iso(now)],
      );

      // A ten-session pack: ten credits at 50.00 each.
      await db.execute(
        `INSERT INTO packages
           (id, client_id, name, credits_total, credits_remaining, unit_price_minor,
            currency, purchased_on, status)
         VALUES (?, ?, '10-session personal training package', 10, ?, 5000, 'EUR', ?, ?)`,
        [newId(), clientId, person.credits, day(at(9, -30)), person.credits > 0 ? 'active' : 'exhausted'],
      );

      const invoiceId = newId();
      await db.execute(
        `INSERT INTO invoices
           (id, client_id, number, status, currency, total_minor, issue_date, due_date,
            notes, created_at, updated_at)
         VALUES (?, ?, ?, ?, 'EUR', 50000, ?, ?, '', ?, ?)`,
        [
          invoiceId, clientId,
          `INV-2026-${String(invoiceNumber++).padStart(4, '0')}`,
          person.owes ? 'issued' : 'settled',
          day(at(9, -30)),
          // One is deliberately past due, so the ageing has something to show.
          day(at(9, person.name === 'Priya Raman' ? -9 : 12)),
          iso(now), iso(now),
        ],
      );
      if (!person.owes) {
        await db.execute(
          `INSERT INTO payments
             (id, invoice_id, client_id, amount_minor, currency, instrument, received_on, reference, notes)
           VALUES (?, ?, ?, 50000, 'EUR', 'bank_transfer', ?, 'SEPA transfer', '')`,
          [newId(), invoiceId, clientId, day(at(9, -28))],
        );
      }

      // Today's session.
      const sessionId = newId();
      await db.execute(
        `INSERT INTO sessions (id, session_type_id, starts_at, ends_at, status, location, notes, created_at, updated_at)
         VALUES (?, ?, ?, ?, 'scheduled', 'Studio', '', ?, ?)`,
        [sessionId, typeId, iso(at(person.hour)), iso(at(person.hour + 1)), iso(now), iso(now)],
      );
      await db.execute(
        `INSERT INTO session_attendees (id, session_id, client_id, status, credits_charged, notes, created_at, updated_at)
         VALUES (?, ?, ?, 'scheduled', 0, '', ?, ?)`,
        [newId(), sessionId, clientId, iso(now), iso(now)],
      );

      // Last week's training, so "Repeat last session" has something to bring
      // forward and the profile is not an empty page.
      const previous = newId();
      await db.execute(
        `INSERT INTO workout_sessions (id, client_id, week_number, performed_on, status, notes, created_at, updated_at)
         VALUES (?, ?, 1, ?, 'completed', '', ?, ?)`,
        [previous, clientId, day(at(9, -7)), iso(now), iso(now)],
      );

      const squat = exerciseIds.get('Back Squat')!;
      const bench = exerciseIds.get('Bench Press')!;
      const load = person.name === 'Dana Rivers' ? 80_000 : 60_000;

      // A warm-up that cloning must skip, then the working sets.
      await db.execute(
        `INSERT INTO set_logs
           (id, workout_session_id, exercise_id, set_index, reps, load_grams, rpe_tenths, is_warmup, completed, notes)
         VALUES (?, ?, ?, 1, 10, ?, 50, 1, 1, '')`,
        [newId(), previous, squat, Math.round(load * 0.5)],
      );
      for (let set = 2; set <= 4; set++) {
        await db.execute(
          `INSERT INTO set_logs
             (id, workout_session_id, exercise_id, set_index, reps, load_grams, rpe_tenths, is_warmup, completed, notes)
           VALUES (?, ?, ?, ?, 8, ?, ?, 0, 1, '')`,
          [newId(), previous, squat, set, load, 70 + set * 5],
        );
      }
      for (let set = 1; set <= 3; set++) {
        await db.execute(
          `INSERT INTO set_logs
             (id, workout_session_id, exercise_id, set_index, reps, load_grams, rpe_tenths, is_warmup, completed, notes)
           VALUES (?, ?, ?, ?, 10, ?, 75, 0, 1, '')`,
          [newId(), previous, bench, set, Math.round(load * 0.7)],
        );
      }

      await db.execute(
        `INSERT INTO biometric_entries (id, client_id, measured_on, weight_grams, body_fat_bp, circumferences, notes)
         VALUES (?, ?, ?, ?, ?, '{}', '')`,
        [newId(), clientId, day(at(9, -30)), 84_000, 1700],
      );
      await db.execute(
        `INSERT INTO biometric_entries (id, client_id, measured_on, weight_grams, body_fat_bp, circumferences, notes)
         VALUES (?, ?, ?, ?, ?, '{}', '')`,
        [newId(), clientId, day(at(9, -3)), 82_400, 1550],
      );
    }
  });
}
