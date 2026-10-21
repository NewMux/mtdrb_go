/**
 * What the numbers say: nudges for today, the week at a glance, a client's
 * progress, and the business over time.
 *
 * All of it is read from the device. Figures that only the ledger can answer
 * honestly — revenue *recognised* — still come from the server when it is
 * reachable (see the Business tab); what is here is either a count of things
 * the device holds in full (sessions, sets, measurements) or money the trainer
 * has recorded receiving, which is labelled as collected, never as earned.
 */

import type { Database } from '@/db/types';
import { money } from '@/ui/format';
import { lowBalanceClients, type LowBalanceClient } from './queries';

/* ─────────────────────────────── today ─────────────────────────────── */

export interface Nudge {
  key: string;
  kind: 'renew' | 'overdue' | 'inactive' | 'unmarked' | 'clearance';
  title: string;
  detail: string;
  clientId?: string;
  count?: number;
}

/**
 * The short list of things worth doing something about today.
 *
 * Each nudge is one tap from its fix. Ordered by money first — an unmarked
 * session is revenue not yet recognised, an overdue invoice is money not yet
 * collected — then by relationships.
 */
export async function nudges(db: Database, threshold = 2, now = new Date()): Promise<Nudge[]> {
  const out: Nudge[] = [];

  const unmarked = await db.selectOne<{ n: number }>(
    `SELECT count(*) AS n FROM session_attendees sa JOIN sessions s ON s.id = sa.session_id
      WHERE sa.status = 'scheduled' AND coalesce(s.status, '') != 'cancelled'
        AND s.ends_at < ? AND s.starts_at >= ?`,
    [now.toISOString(), new Date(now.getTime() - 7 * 86_400_000).toISOString()],
  );
  if ((unmarked?.n ?? 0) > 0) {
    out.push({
      key: 'unmarked', kind: 'unmarked', count: unmarked!.n,
      title: `${unmarked!.n} session${unmarked!.n === 1 ? '' : 's'} to mark`,
      detail: 'Finished but not marked — the credit and the revenue are waiting on you.',
    });
  }

  const overdue = await db.selectOne<{ n: number; total: number; currency: string | null }>(
    `SELECT count(*) AS n,
            coalesce(sum(i.total_minor - coalesce((SELECT sum(p.amount_minor) FROM payments p WHERE p.invoice_id = i.id), 0)), 0) AS total,
            max(i.currency) AS currency
       FROM invoices i
      WHERE i.deleted_at IS NULL AND i.status IN ('issued','partially_paid') AND i.due_date < ?`,
    [now.toISOString().slice(0, 10)],
  );
  if ((overdue?.n ?? 0) > 0) {
    out.push({
      key: 'overdue', kind: 'overdue', count: overdue!.n,
      title: `${overdue!.n} overdue invoice${overdue!.n === 1 ? '' : 's'}`,
      detail: `${money(overdue!.total, overdue!.currency ?? '')} past its due date.`,
    });
  }

  const low: LowBalanceClient[] = await lowBalanceClients(db, threshold);
  for (const c of low.slice(0, 3)) {
    out.push({
      key: `renew-${c.id}`, kind: 'renew', clientId: c.id,
      title: c.creditsRemaining <= 0 ? `${c.fullName} is out of sessions` : `${c.fullName} has ${c.creditsRemaining} left`,
      detail: c.creditsRemaining < 0 ? 'Overdrawn — sell a renewal before the next session.' : 'A good moment to offer the next pack.',
    });
  }
  if (low.length > 3) {
    out.push({
      key: 'renew-more', kind: 'renew', count: low.length - 3,
      title: `${low.length - 3} more to renew`, detail: 'See everyone running low.',
    });
  }

  const inactive = await inactiveClients(db, 14, now);
  for (const c of inactive.slice(0, 2)) {
    out.push({
      key: `inactive-${c.id}`, kind: 'inactive', clientId: c.id,
      title: `${c.fullName} hasn't trained in ${c.days} days`,
      detail: 'Nothing booked. A quick message keeps them coming back.',
    });
  }

  const clearance = await db.select<{ id: string; fullName: string }>(
    `SELECT c.id, c.full_name AS fullName FROM clients c
      WHERE c.deleted_at IS NULL AND c.status = 'active'
        AND EXISTS (SELECT 1 FROM parq_responses q WHERE q.client_id = c.id
                     AND q.requires_clearance = 1 AND q.cleared_at IS NULL)`,
  );
  for (const c of clearance.slice(0, 2)) {
    out.push({
      key: `clear-${c.id}`, kind: 'clearance', clientId: c.id,
      title: `${c.fullName} needs medical clearance`,
      detail: 'Their PAR-Q flagged something. Get sign-off before training hard.',
    });
  }

  return out;
}

/** Active clients with history but nothing done or booked for `days` days. */
export async function inactiveClients(db: Database, days: number, now = new Date()) {
  const rows = await db.select<{ id: string; fullName: string; last: string | null }>(
    `SELECT c.id, c.full_name AS fullName,
            max(coalesce((SELECT max(s.starts_at) FROM session_attendees sa JOIN sessions s ON s.id = sa.session_id
                           WHERE sa.client_id = c.id AND sa.status = 'completed'), ''),
                coalesce((SELECT max(w.performed_on) FROM workout_sessions w
                           WHERE w.client_id = c.id AND w.deleted_at IS NULL), '')) AS last
       FROM clients c
      WHERE c.deleted_at IS NULL AND c.status = 'active'
        AND NOT EXISTS (SELECT 1 FROM session_attendees sa JOIN sessions s ON s.id = sa.session_id
                         WHERE sa.client_id = c.id AND s.starts_at >= ? AND coalesce(s.status,'') != 'cancelled')`,
    [now.toISOString()],
  );
  return rows
    .filter((r) => r.last)
    .map((r) => ({ ...r, days: Math.floor((now.getTime() - new Date(r.last!).getTime()) / 86_400_000) }))
    .filter((r) => r.days >= days)
    .sort((a, b) => b.days - a.days);
}

export interface WeekGlance {
  booked: number;
  completed: number;
  remaining: number;
  /** Completed ÷ (completed + no-shows + late cancels), last 30 days. */
  attendanceRate: number | null;
  workoutsLogged: number;
}

function startOfWeek(d: Date): Date {
  const s = new Date(d.getFullYear(), d.getMonth(), d.getDate());
  const dow = (s.getDay() + 6) % 7; // Monday = 0
  s.setDate(s.getDate() - dow);
  return s;
}

export async function weekGlance(db: Database, now = new Date()): Promise<WeekGlance> {
  const from = startOfWeek(now);
  const to = new Date(from.getTime() + 7 * 86_400_000);
  const counts = await db.selectOne<{ booked: number; completed: number; remaining: number }>(
    `SELECT count(*) AS booked,
            coalesce(sum(CASE WHEN sa.status = 'completed' THEN 1 ELSE 0 END), 0) AS completed,
            coalesce(sum(CASE WHEN sa.status = 'scheduled' AND s.starts_at >= ? THEN 1 ELSE 0 END), 0) AS remaining
       FROM session_attendees sa JOIN sessions s ON s.id = sa.session_id
      WHERE s.starts_at >= ? AND s.starts_at < ? AND coalesce(s.status, '') != 'cancelled'`,
    [now.toISOString(), from.toISOString(), to.toISOString()],
  );
  const rate = await db.selectOne<{ done: number; missed: number }>(
    `SELECT coalesce(sum(CASE WHEN sa.status = 'completed' THEN 1 ELSE 0 END), 0) AS done,
            coalesce(sum(CASE WHEN sa.status IN ('no_show','late_cancel') THEN 1 ELSE 0 END), 0) AS missed
       FROM session_attendees sa JOIN sessions s ON s.id = sa.session_id
      WHERE s.starts_at >= ? AND s.starts_at < ?`,
    [new Date(now.getTime() - 30 * 86_400_000).toISOString(), now.toISOString()],
  );
  const workouts = await db.selectOne<{ n: number }>(
    `SELECT count(*) AS n FROM workout_sessions WHERE performed_on >= ? AND deleted_at IS NULL`,
    [from.toISOString().slice(0, 10)],
  );
  const total = (rate?.done ?? 0) + (rate?.missed ?? 0);
  return {
    booked: counts?.booked ?? 0,
    completed: counts?.completed ?? 0,
    remaining: counts?.remaining ?? 0,
    attendanceRate: total > 0 ? (rate!.done / total) : null,
    workoutsLogged: workouts?.n ?? 0,
  };
}

/* ───────────────────────────── a client ───────────────────────────── */

/**
 * Estimated one-rep max, by Epley.
 *
 * Only meaningful for sets of roughly ten or fewer, so higher-rep sets are
 * left out of PR detection rather than inflating it.
 */
export function e1rm(loadGrams: number, reps: number): number {
  if (reps <= 0 || loadGrams <= 0) return 0;
  if (reps === 1) return loadGrams;
  return loadGrams * (1 + reps / 30);
}

export interface LiftRecord {
  exerciseId: string;
  name: string;
  bestE1rmGrams: number;
  bestLoadGrams: number;
  bestReps: number;
  on: string;
  sessions: number;
  /** Best e1RM per session, oldest first — the progression line. */
  history: { on: string; e1rm: number }[];
}

/** A client's main lifts and how they have moved. */
export async function liftRecords(db: Database, clientId: string, limit = 6): Promise<LiftRecord[]> {
  const rows = await db.select<{
    exerciseId: string; name: string; on: string; reps: number; load: number;
  }>(
    `SELECT sl.exercise_id AS exerciseId, coalesce(e.name, 'Exercise') AS name, w.performed_on AS "on",
            sl.reps, sl.load_grams AS load
       FROM set_logs sl
       JOIN workout_sessions w ON w.id = sl.workout_session_id
       LEFT JOIN exercises e ON e.id = sl.exercise_id
      WHERE w.client_id = ? AND w.deleted_at IS NULL
        AND sl.completed = 1 AND sl.is_warmup = 0
        AND sl.reps BETWEEN 1 AND 12 AND sl.load_grams > 0
      ORDER BY w.performed_on`,
    [clientId],
  );

  const byExercise = new Map<string, LiftRecord & { perDay: Map<string, number> }>();
  for (const r of rows) {
    let rec = byExercise.get(r.exerciseId);
    if (!rec) {
      rec = {
        exerciseId: r.exerciseId, name: r.name, bestE1rmGrams: 0, bestLoadGrams: 0, bestReps: 0,
        on: r.on, sessions: 0, history: [], perDay: new Map(),
      };
      byExercise.set(r.exerciseId, rec);
    }
    const est = e1rm(r.load, r.reps);
    if (est > rec.bestE1rmGrams) {
      rec.bestE1rmGrams = est; rec.bestLoadGrams = r.load; rec.bestReps = r.reps; rec.on = r.on;
    }
    rec.perDay.set(r.on, Math.max(rec.perDay.get(r.on) ?? 0, est));
  }

  return [...byExercise.values()]
    .map(({ perDay, ...rec }) => ({
      ...rec,
      sessions: perDay.size,
      history: [...perDay].map(([on, v]) => ({ on, e1rm: v })),
    }))
    .sort((a, b) => b.sessions - a.sessions || b.bestE1rmGrams - a.bestE1rmGrams)
    .slice(0, limit);
}

/**
 * Whether a just-logged set is a personal record for this client.
 *
 * Compared against every earlier completed working set on the same lift, in
 * other workouts — a PR is beating your past, not your previous set today.
 */
export async function isPersonalRecord(
  db: Database, clientId: string, exerciseId: string, workoutId: string, loadGrams: number, reps: number,
): Promise<{ e1rm: boolean; load: boolean }> {
  if (reps < 1 || reps > 12 || loadGrams <= 0) return { e1rm: false, load: false };
  const best = await db.selectOne<{ maxLoad: number | null; maxE1: number | null; n: number }>(
    `SELECT max(sl.load_grams) AS maxLoad, max(sl.load_grams * (1 + sl.reps / 30.0)) AS maxE1, count(*) AS n
       FROM set_logs sl JOIN workout_sessions w ON w.id = sl.workout_session_id
      WHERE w.client_id = ? AND sl.exercise_id = ? AND w.id != ?
        AND sl.completed = 1 AND sl.is_warmup = 0 AND sl.reps BETWEEN 1 AND 12`,
    [clientId, exerciseId, workoutId],
  );
  // A first-ever set is not a record worth celebrating; there is nothing to beat.
  if (!best || best.n === 0) return { e1rm: false, load: false };
  return {
    e1rm: e1rm(loadGrams, reps) > (best.maxE1 ?? 0) + 1,
    load: loadGrams > (best.maxLoad ?? 0),
  };
}

/** Sessions attended ÷ sessions booked over the last `days`, for one client. */
export async function adherence(db: Database, clientId: string, days = 30, now = new Date()) {
  const r = await db.selectOne<{ done: number; total: number }>(
    `SELECT coalesce(sum(CASE WHEN sa.status = 'completed' THEN 1 ELSE 0 END), 0) AS done,
            coalesce(sum(CASE WHEN sa.status IN ('completed','no_show','late_cancel') THEN 1 ELSE 0 END), 0) AS total
       FROM session_attendees sa JOIN sessions s ON s.id = sa.session_id
      WHERE sa.client_id = ? AND s.starts_at >= ? AND s.starts_at < ?`,
    [clientId, new Date(now.getTime() - days * 86_400_000).toISOString(), now.toISOString()],
  );
  return { done: r?.done ?? 0, total: r?.total ?? 0, rate: r && r.total > 0 ? r.done / r.total : null };
}

export interface ClientFlag {
  key: string;
  label: string;
  hue: 'red' | 'orange' | 'gray';
}

/** Intake and compliance gaps worth seeing before a session. */
export async function clientFlags(db: Database, clientId: string): Promise<ClientFlag[]> {
  const flags: ClientFlag[] = [];
  const parq = await db.selectOne<{ requires_clearance: number | null; cleared_at: string | null }>(
    `SELECT requires_clearance, cleared_at FROM parq_responses WHERE client_id = ?
      ORDER BY completed_at DESC, id DESC LIMIT 1`,
    [clientId],
  );
  if (!parq) flags.push({ key: 'parq', label: 'No PAR-Q', hue: 'orange' });
  else if (parq.requires_clearance === 1 && !parq.cleared_at) flags.push({ key: 'clearance', label: 'Needs clearance', hue: 'red' });
  const waiver = await db.selectOne<{ n: number }>(
    `SELECT count(*) AS n FROM waiver_signatures WHERE client_id = ?`, [clientId],
  );
  if ((waiver?.n ?? 0) === 0) flags.push({ key: 'waiver', label: 'Waiver unsigned', hue: 'orange' });
  return flags;
}

/* ───────────────────────────── business ───────────────────────────── */

export interface MonthTotal {
  month: string; // YYYY-MM
  label: string;
  minor: number;
}

/** Money recorded as received, per calendar month, oldest first. */
export async function collectedByMonth(db: Database, months = 6, now = new Date()): Promise<MonthTotal[]> {
  const start = new Date(now.getFullYear(), now.getMonth() - (months - 1), 1);
  const rows = await db.select<{ month: string; minor: number }>(
    `SELECT substr(received_on, 1, 7) AS month, sum(amount_minor) AS minor
       FROM payments
      WHERE received_on >= ? AND reversed_by IS NULL
      GROUP BY substr(received_on, 1, 7)`,
    [start.toISOString().slice(0, 10)],
  );
  const map = new Map(rows.map((r) => [r.month, r.minor]));
  const out: MonthTotal[] = [];
  for (let i = 0; i < months; i++) {
    const d = new Date(start.getFullYear(), start.getMonth() + i, 1);
    const key = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`;
    out.push({ month: key, label: d.toLocaleDateString(undefined, { month: 'short' }), minor: map.get(key) ?? 0 });
  }
  return out;
}

export interface PracticeStats {
  activeClients: number;
  newThisMonth: number;
  sessionsThisMonth: number;
  noShowRate: number | null;
  prepaidCredits: number;
  averagePackMinor: number | null;
}

export async function practiceStats(db: Database, now = new Date()): Promise<PracticeStats> {
  const monthStart = new Date(now.getFullYear(), now.getMonth(), 1).toISOString();
  const clients = await db.selectOne<{ active: number; fresh: number }>(
    `SELECT coalesce(sum(CASE WHEN status = 'active' THEN 1 ELSE 0 END), 0) AS active,
            coalesce(sum(CASE WHEN created_at >= ? THEN 1 ELSE 0 END), 0) AS fresh
       FROM clients WHERE deleted_at IS NULL`,
    [monthStart],
  );
  const sessions = await db.selectOne<{ done: number; noShow: number; marked: number }>(
    `SELECT coalesce(sum(CASE WHEN sa.status = 'completed' THEN 1 ELSE 0 END), 0) AS done,
            coalesce(sum(CASE WHEN sa.status = 'no_show' THEN 1 ELSE 0 END), 0) AS noShow,
            coalesce(sum(CASE WHEN sa.status != 'scheduled' THEN 1 ELSE 0 END), 0) AS marked
       FROM session_attendees sa JOIN sessions s ON s.id = sa.session_id
      WHERE s.starts_at >= ?`,
    [monthStart],
  );
  const credits = await db.selectOne<{ n: number }>(
    `SELECT coalesce(sum(credits_remaining), 0) AS n FROM packages WHERE status = 'active' AND credits_remaining > 0`,
  );
  const packs = await db.selectOne<{ avg: number | null }>(
    `SELECT avg(credits_total * unit_price_minor) AS avg FROM packages WHERE credits_total > 0`,
  );
  return {
    activeClients: clients?.active ?? 0,
    newThisMonth: clients?.fresh ?? 0,
    sessionsThisMonth: sessions?.done ?? 0,
    noShowRate: sessions && sessions.marked > 0 ? sessions.noShow / sessions.marked : null,
    prepaidCredits: credits?.n ?? 0,
    averagePackMinor: packs?.avg ? Math.round(packs.avg) : null,
  };
}

export interface PaymentRow {
  id: string;
  clientName: string;
  amountMinor: number;
  currency: string;
  instrument: string;
  receivedOn: string;
  reference: string;
  invoiceNumber: string | null;
}

export async function recentPayments(db: Database, limit = 20): Promise<PaymentRow[]> {
  return db.select<PaymentRow>(
    `SELECT p.id, coalesce(c.full_name, '') AS clientName, p.amount_minor AS amountMinor,
            coalesce(p.currency, '') AS currency, coalesce(p.instrument, '') AS instrument,
            p.received_on AS receivedOn, coalesce(p.reference, '') AS reference, i.number AS invoiceNumber
       FROM payments p
       LEFT JOIN invoices i ON i.id = p.invoice_id
       LEFT JOIN clients c ON c.id = coalesce(p.client_id, i.client_id)
      WHERE p.reversed_by IS NULL
      ORDER BY p.received_on DESC, p.created_at DESC LIMIT ?`,
    [limit],
  );
}
