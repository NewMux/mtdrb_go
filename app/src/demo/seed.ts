/**
 * A practice with some history in it, seeded straight into the local database.
 *
 * Demo mode exists because the app is offline-first all the way down: every
 * screen reads local SQLite, and the server's only job is to keep that mirror
 * honest. So a build with no server is not a mock of the app — it *is* the
 * app, with the sync engine idle. The queries, the outbox, the programme
 * builder and the floor logger are the real ones.
 *
 * What it cannot show is anything the server decides: credits are not really
 * burned, revenue is not really recognised, and an invoice is not really
 * settled. Those live in the ledger, behind an API.
 *
 * The history is shaped to make each screen worth looking at: a busy week on
 * the calendar, six weeks of lifts that trend upward, body weight coming down,
 * six months of payments, someone running out of sessions, someone gone quiet.
 */

import type { Database } from '@/db/types';
import { newId } from '@/lib/id';
import type { Account } from '@/api/types';
import { EXERCISE_LIBRARY, TEMPLATES } from '@/features/library';

export const DEMO_ACCOUNT: Account = {
  user_id: 'demo-user',
  tenant_id: 'demo-tenant',
  email: 'sam@riverastrength.example',
  display_name: 'Sam Rivera',
  role: 'trainer',
  currency: 'EUR',
};

interface Person {
  name: string;
  phone: string;
  /** Hour of their regular slot, and the weekdays they come in (1 = Monday). */
  hour: number;
  days: number[];
  packSize: number;
  creditsLeft: number;
  /** Whether the current pack is still unpaid. */
  owes: boolean;
  /** Days since they last trained; large for someone who has gone quiet. */
  lastSeen: number;
  /** Starting squat, in kg, for the lift history. */
  squat: number;
  bench: number;
  weight: number;
  template?: string;
  notes: string;
}

const PEOPLE: Person[] = [
  { name: 'Dana Rivers', phone: '+353871234501', hour: 7, days: [1, 3, 5], packSize: 20, creditsLeft: 12, owes: false, lastSeen: 2, squat: 80, bench: 55, weight: 72, template: 'strength-5x5', notes: 'Goal: 100 kg squat by March. Loves heavy days.' },
  { name: 'Morgan Hale', phone: '+353871234502', hour: 9, days: [2, 4], packSize: 10, creditsLeft: 2, owes: true, lastSeen: 3, squat: 60, bench: 40, weight: 88, template: 'foundations', notes: 'Goal: lose 8 kg for the wedding in June. Left knee — no deep lunges.' },
  { name: 'Priya Raman', phone: '+353871234503', hour: 17, days: [1, 4], packSize: 10, creditsLeft: 0, owes: true, lastSeen: 4, squat: 50, bench: 32, weight: 61, template: 'glutes', notes: 'Runner. Building strength for a marathon block.' },
  { name: 'Leo Martins', phone: '+353871234504', hour: 12, days: [2, 5], packSize: 10, creditsLeft: 6, owes: false, lastSeen: 1, squat: 110, bench: 85, weight: 84, template: 'upper-lower', notes: 'Experienced lifter. Wants size — eat more!' },
  { name: 'Aisha Kone', phone: '+353871234505', hour: 18, days: [3], packSize: 5, creditsLeft: 4, owes: false, lastSeen: 6, squat: 40, bench: 25, weight: 66, notes: 'New this month. Nervous about the gym — keep it fun.' },
  { name: "Tom O'Brien", phone: '+353871234506', hour: 8, days: [], packSize: 10, creditsLeft: 3, owes: false, lastSeen: 23, squat: 90, bench: 70, weight: 95, notes: 'Travels for work. Has gone quiet — check in.' },
  { name: 'Sofia Lindqvist', phone: '+353871234507', hour: 10, days: [1, 3], packSize: 20, creditsLeft: 15, owes: false, lastSeen: 5, squat: 55, bench: 35, weight: 58, template: 'mobility', notes: 'Desk job, tight hips. Goal: pain-free running.' },
  { name: 'Jamal Wright', phone: '+353871234508', hour: 16, days: [5], packSize: 10, creditsLeft: 1, owes: false, lastSeen: 7, squat: 100, bench: 75, weight: 79, template: 'power', notes: 'Plays rugby. In-season — keep volume moderate.' },
];

const SESSION_TYPES = [
  { name: '1-on-1', minutes: 60, capacity: 1, colour: '#007AFF' },
  { name: 'Duo', minutes: 60, capacity: 2, colour: '#AF52DE' },
  { name: 'Assessment', minutes: 45, capacity: 1, colour: '#34C759' },
];

const iso = (d: Date) => d.toISOString();
const ymd = (d: Date) => d.toISOString().slice(0, 10);

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
  const at = (offsetDays: number, hour = 9, minute = 0) => {
    const d = new Date(now);
    d.setDate(d.getDate() + offsetDays);
    d.setHours(hour, minute, 0, 0);
    return d;
  };

  await db.transaction(async () => {
    const exercise = new Map<string, string>();
    for (const [name, category, muscle, equipment] of EXERCISE_LIBRARY) {
      const id = newId();
      exercise.set(name, id);
      await db.execute(
        `INSERT INTO exercises (id, name, category, primary_muscle, equipment, is_custom) VALUES (?, ?, ?, ?, ?, 0)`,
        [id, name, category, muscle, equipment],
      );
    }

    const typeIds: string[] = [];
    for (const t of SESSION_TYPES) {
      const id = newId();
      typeIds.push(id);
      await db.execute(
        `INSERT INTO session_types (id, name, duration_minutes, capacity, credit_cost, colour) VALUES (?, ?, ?, ?, 1, ?)`,
        [id, t.name, t.minutes, t.capacity, t.colour],
      );
    }

    // Programmes, copied from the gallery the way a trainer would.
    const programIds = new Map<string, { id: string; dayIds: string[] }>();
    for (const key of new Set(PEOPLE.map((p) => p.template).filter(Boolean) as string[])) {
      const t = TEMPLATES.find((x) => x.key === key)!;
      const pid = newId();
      const dayIds: string[] = [];
      const structure = {
        blocks: [{
          id: newId(), name: `${t.weeks}-Week Block`, weeks: t.weeks, sort_order: 0, notes: '',
          days: t.days.map((d, di) => {
            const dayId = newId();
            dayIds.push(dayId);
            return {
              id: dayId, name: d.name, sort_order: di, notes: '',
              prescriptions: d.exercises.filter((e) => exercise.has(e.name)).map((e, pi) => ({
                id: newId(), exercise_id: exercise.get(e.name)!, sort_order: pi, target_sets: e.sets,
                target_reps_min: e.reps[0], target_reps_max: e.reps[1], target_load_grams: null,
                target_rpe_tenths: e.rpe ? e.rpe * 10 : null, target_rir: null, rest_seconds: e.rest ?? null,
                tempo: '', superset_group: e.superset ?? '', notes: e.notes ?? '',
              })),
            };
          }),
        }],
      };
      await db.execute(
        `INSERT INTO programs (id, name, description, is_template, structure, created_at, updated_at)
         VALUES (?, ?, ?, 0, ?, ?, ?)`,
        [pid, t.name, t.summary, JSON.stringify(structure), iso(at(-60)), iso(at(-40))],
      );
      programIds.set(key, { id: pid, dayIds });
    }

    let invoiceNumber = 1;

    for (const [index, person] of PEOPLE.entries()) {
      const clientId = newId();
      const joined = at(-150 + index * 12);
      await db.execute(
        `INSERT INTO clients (id, full_name, email, phone, status, notes, created_at, updated_at)
         VALUES (?, ?, ?, ?, 'active', ?, ?, ?)`,
        [clientId, person.name, `${person.name.split(' ')[0]!.toLowerCase()}@example.com`, person.phone, person.notes, iso(joined), iso(joined)],
      );

      // Intake: most have done it, a couple have gaps for the profile to flag.
      if (index !== 4) {
        await db.execute(
          `INSERT INTO parq_responses (id, client_id, answers, requires_clearance, cleared_at, completed_at)
           VALUES (?, ?, '{}', ?, NULL, ?)`,
          [newId(), clientId, index === 1 ? 1 : 0, iso(joined)],
        );
      }
      if (index !== 4 && index !== 5) {
        await db.execute(
          `INSERT INTO waiver_signatures (id, waiver_id, client_id, signed_name, signed_at) VALUES (?, NULL, ?, ?, ?)`,
          [newId(), clientId, person.name, iso(joined)],
        );
      }

      // Payment history: one earlier pack, paid, then the current one.
      const price = person.packSize >= 20 ? 4500 : person.packSize >= 10 ? 5000 : 5500;
      const packs = [
        { size: person.packSize, left: 0, bought: at(-120 + index * 7), paid: true },
        { size: person.packSize, left: person.creditsLeft, bought: at(-35 + index * 3), paid: !person.owes },
      ];
      for (const [pi, pack] of packs.entries()) {
        if (index >= 4 && pi === 0) continue; // the newer clients have only bought once
        const invoiceId = newId();
        await db.execute(
          `INSERT INTO packages (id, client_id, invoice_id, name, credits_total, credits_remaining, unit_price_minor,
             currency, purchased_on, expires_at, status)
           VALUES (?, ?, ?, ?, ?, ?, ?, 'EUR', ?, ?, ?)`,
          [newId(), clientId, invoiceId, `${pack.size}-session personal training package`, pack.size, pack.left, price,
            ymd(pack.bought), ymd(new Date(pack.bought.getTime() + 180 * 86_400_000)), pack.left > 0 ? 'active' : 'exhausted'],
        );
        await db.execute(
          `INSERT INTO invoices (id, client_id, number, status, currency, total_minor, issue_date, due_date, notes, created_at, updated_at)
           VALUES (?, ?, ?, ?, 'EUR', ?, ?, ?, '', ?, ?)`,
          [invoiceId, clientId, `INV-2026-${String(invoiceNumber++).padStart(4, '0')}`, pack.paid ? 'settled' : 'issued',
            pack.size * price, ymd(pack.bought), ymd(new Date(pack.bought.getTime() + (person.name === 'Priya Raman' ? 14 : 45) * 86_400_000)),
            iso(pack.bought), iso(pack.bought)],
        );
        if (pack.paid) {
          await db.execute(
            `INSERT INTO payments (id, invoice_id, client_id, amount_minor, currency, instrument, received_on, reference, notes, created_at)
             VALUES (?, ?, ?, ?, 'EUR', ?, ?, ?, '', ?)`,
            [newId(), invoiceId, clientId, pack.size * price, index % 3 === 0 ? 'cash' : 'bank_transfer',
              ymd(new Date(pack.bought.getTime() + 3 * 86_400_000)), index % 3 === 0 ? 'Envelope' : 'SEPA transfer', iso(pack.bought)],
          );
        }
      }

      // The regular slot: last week (marked) and this week and next (booked).
      for (let offset = -7; offset <= 13; offset++) {
        const d = at(offset, person.hour);
        const iso8601 = (d.getDay() + 6) % 7 + 1;
        if (!person.days.includes(iso8601)) continue;
        const sessionId = newId();
        const past = d.getTime() + 3600_000 < now.getTime();
        await db.execute(
          `INSERT INTO sessions (id, session_type_id, starts_at, ends_at, status, location, notes, created_at, updated_at)
           VALUES (?, ?, ?, ?, 'scheduled', 'Studio', '', ?, ?)`,
          [sessionId, typeIds[0], iso(d), iso(new Date(d.getTime() + 3600_000)), iso(now), iso(now)],
        );
        const status = !past ? 'scheduled' : offset === -3 && index === 2 ? 'no_show' : offset === 0 ? 'scheduled' : 'completed';
        await db.execute(
          `INSERT INTO session_attendees (id, session_id, client_id, status, credits_charged, notes, created_at, updated_at)
           VALUES (?, ?, ?, ?, ?, '', ?, ?)`,
          [newId(), sessionId, clientId, status, status === 'completed' ? 1 : 0, iso(now), iso(now)],
        );
      }

      // Six weeks of training that trends upward, ending `lastSeen` days ago.
      const plan = person.template ? programIds.get(person.template) : undefined;
      let assignmentId: string | null = null;
      if (plan) {
        assignmentId = newId();
        await db.execute(
          `INSERT INTO program_assignments (id, program_id, client_id, starts_on, status, notes, created_at, updated_at)
           VALUES (?, ?, ?, ?, 'active', '', ?, ?)`,
          [assignmentId, plan.id, clientId, ymd(at(-person.lastSeen - 35)), iso(now), iso(now)],
        );
      }
      const sessions = 12;
      for (let k = 0; k < sessions; k++) {
        const day = at(-person.lastSeen - (sessions - 1 - k) * 3);
        const workoutId = newId();
        await db.execute(
          `INSERT INTO workout_sessions (id, client_id, assignment_id, day_id, week_number, performed_on, status, notes, created_at, updated_at)
           VALUES (?, ?, ?, ?, ?, ?, 'completed', '', ?, ?)`,
          [workoutId, clientId, assignmentId, plan ? plan.dayIds[k % plan.dayIds.length] : null, Math.floor(k / 3) + 1, ymd(day), iso(day), iso(day)],
        );
        const progress = 1 + k * 0.012;
        const lifts: [string, number, number][] = [
          ['Back Squat', person.squat, k % 4 === 3 ? 6 : 5],
          ['Bench Press', person.bench, 8],
          ['Romanian Deadlift', person.squat * 0.8, 8],
        ];
        for (const [name, base, reps] of lifts) {
          const load = Math.round((base * progress) / 2.5) * 2.5 * 1000;
          await db.execute(
            `INSERT INTO set_logs (id, workout_session_id, exercise_id, set_index, reps, load_grams, rpe_tenths, is_warmup, completed, notes)
             VALUES (?, ?, ?, 1, 8, ?, NULL, 1, 1, '')`,
            [newId(), workoutId, exercise.get(name), Math.round(load * 0.5 / 2500) * 2500],
          );
          for (let set = 2; set <= 4; set++) {
            await db.execute(
              `INSERT INTO set_logs (id, workout_session_id, exercise_id, set_index, reps, load_grams, rpe_tenths, is_warmup, completed, notes)
               VALUES (?, ?, ?, ?, ?, ?, ?, 0, 1, '')`,
              [newId(), workoutId, exercise.get(name), set, reps, load, 70 + set * 5],
            );
          }
        }
      }

      // Body weight, every couple of weeks, drifting toward their goal.
      for (let m = 0; m < 7; m++) {
        const drift = person.name === 'Leo Martins' ? 0.35 : -0.5;
        await db.execute(
          `INSERT INTO biometric_entries (id, client_id, measured_on, weight_grams, body_fat_bp, circumferences, notes)
           VALUES (?, ?, ?, ?, ?, '{}', '')`,
          [newId(), clientId, ymd(at(-person.lastSeen - (6 - m) * 14)),
            Math.round((person.weight + drift * m + (m % 2 ? 0.3 : -0.2)) * 1000),
            Math.round((22 - m * 0.4 - index * 0.8) * 100)],
        );
      }
    }

    // A duo session later this week, so the calendar shows a shared slot.
    const duo = newId();
    const duoAt = at(2, 19);
    await db.execute(
      `INSERT INTO sessions (id, session_type_id, starts_at, ends_at, status, location, notes, created_at, updated_at)
       VALUES (?, ?, ?, ?, 'scheduled', 'Park', '', ?, ?)`,
      [duo, typeIds[1], iso(duoAt), iso(new Date(duoAt.getTime() + 3600_000)), iso(now), iso(now)],
    );
    const pair = await db.select<{ id: string }>(`SELECT id FROM clients WHERE full_name IN ('Aisha Kone', 'Sofia Lindqvist')`);
    for (const p of pair) {
      await db.execute(
        `INSERT INTO session_attendees (id, session_id, client_id, status, credits_charged, notes, created_at, updated_at)
         VALUES (?, ?, ?, 'scheduled', 0, '', ?, ?)`,
        [newId(), duo, p.id, iso(now), iso(now)],
      );
    }
  });
}
