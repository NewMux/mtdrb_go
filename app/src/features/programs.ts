/**
 * Programmes: building them, copying templates, putting clients on them, and
 * working out what a client should do today.
 *
 * A programme is edited as one document — blocks of weeks, training days that
 * repeat each week, and prescriptions on each day — and saved whole. The
 * document's shape is the server's own `Program` JSON, so what the builder
 * writes and what a pull brings back are the same thing.
 *
 * Saving is optimistic and offline like everything else. Repeated saves of the
 * same programme while it is still waiting to send are folded into the one
 * queued operation rather than stacked, so an afternoon of editing on a train
 * sends one programme, not two hundred versions of it — and the operation keeps
 * its place in the queue, ahead of any assignment that depends on it.
 */

import type { Database } from '@/db/types';
import { newId } from '@/lib/id';
import { act } from './actions';
import { TEMPLATES, type ProgramTemplate } from './library';

export interface Prescription {
  id: string;
  exercise_id: string;
  exercise_name?: string;
  sort_order: number;
  target_sets: number;
  target_reps_min?: number | null;
  target_reps_max?: number | null;
  target_load_grams?: number | null;
  target_rpe_tenths?: number | null;
  target_rir?: number | null;
  rest_seconds?: number | null;
  tempo: string;
  superset_group: string;
  notes: string;
}

export interface ProgramDay {
  id: string;
  name: string;
  sort_order: number;
  notes: string;
  prescriptions: Prescription[];
}

export interface ProgramBlock {
  id: string;
  name: string;
  weeks: number;
  sort_order: number;
  notes: string;
  days: ProgramDay[];
}

export interface ProgramDoc {
  id: string;
  name: string;
  description: string;
  is_template: boolean;
  archived?: boolean;
  blocks: ProgramBlock[];
}

export interface ProgramSummary {
  id: string;
  name: string;
  description: string;
  weeks: number;
  days: number;
  exercises: number;
  clients: number;
  updatedAt: string | null;
}

/* ───────────────────────────── reading ───────────────────────────── */

function parse(row: { id: string; name: string; description: string | null; is_template: number | null; structure: string | null }): ProgramDoc {
  let blocks: ProgramBlock[] = [];
  if (row.structure) {
    try {
      const parsed = JSON.parse(row.structure) as { blocks?: ProgramBlock[] } | ProgramBlock[];
      blocks = Array.isArray(parsed) ? parsed : parsed.blocks ?? [];
    } catch {
      blocks = [];
    }
  }
  return {
    id: row.id,
    name: row.name,
    description: row.description ?? '',
    is_template: row.is_template === 1,
    blocks: blocks.map((b, bi) => ({
      ...b,
      sort_order: b.sort_order ?? bi,
      notes: b.notes ?? '',
      days: (b.days ?? []).map((d, di) => ({
        ...d,
        sort_order: d.sort_order ?? di,
        notes: d.notes ?? '',
        prescriptions: (d.prescriptions ?? []).map((p, pi) => ({
          ...p, sort_order: p.sort_order ?? pi, tempo: p.tempo ?? '',
          superset_group: p.superset_group ?? '', notes: p.notes ?? '',
        })),
      })),
    })),
  };
}

export function totalWeeks(doc: ProgramDoc): number {
  return doc.blocks.reduce((n, b) => n + Math.max(1, b.weeks), 0);
}

export async function listPrograms(db: Database): Promise<ProgramSummary[]> {
  const rows = await db.select<{
    id: string; name: string; description: string | null; is_template: number | null;
    structure: string | null; updated_at: string | null; clients: number;
  }>(
    `SELECT p.id, p.name, p.description, p.is_template, p.structure, p.updated_at,
            (SELECT count(*) FROM program_assignments a
              WHERE a.program_id = p.id AND a.status = 'active') AS clients
       FROM programs p
      WHERE p.archived_at IS NULL
      ORDER BY p.updated_at DESC, p.name`,
  );
  return rows.map((row) => {
    const doc = parse(row);
    const days = doc.blocks.reduce((n, b) => n + b.days.length, 0);
    const exercises = doc.blocks.reduce((n, b) => n + b.days.reduce((m, d) => m + d.prescriptions.length, 0), 0);
    return {
      id: row.id, name: row.name, description: row.description ?? '',
      weeks: totalWeeks(doc), days, exercises, clients: row.clients, updatedAt: row.updated_at,
    };
  });
}

export async function getProgram(db: Database, id: string): Promise<ProgramDoc | null> {
  const row = await db.selectOne<{ id: string; name: string; description: string | null; is_template: number | null; structure: string | null }>(
    `SELECT id, name, description, is_template, structure FROM programs WHERE id = ?`,
    [id],
  );
  if (!row) return null;
  const doc = parse(row);
  await attachNames(db, doc);
  return doc;
}

/** Fills in exercise names from the local library, for display. */
async function attachNames(db: Database, doc: ProgramDoc): Promise<void> {
  const ids = new Set<string>();
  for (const b of doc.blocks) for (const d of b.days) for (const p of d.prescriptions) ids.add(p.exercise_id);
  if (ids.size === 0) return;
  const rows = await db.select<{ id: string; name: string }>(
    `SELECT id, name FROM exercises WHERE id IN (${[...ids].map(() => '?').join(',')})`,
    [...ids],
  );
  const names = new Map(rows.map((r) => [r.id, r.name]));
  for (const b of doc.blocks) for (const d of b.days) for (const p of d.prescriptions) {
    p.exercise_name = names.get(p.exercise_id) ?? p.exercise_name ?? 'Exercise';
  }
}

/* ───────────────────────────── writing ───────────────────────────── */

export function emptyProgram(name = 'New Program'): ProgramDoc {
  return {
    id: newId(), name, description: '', is_template: false,
    blocks: [{ id: newId(), name: 'Block 1', weeks: 4, sort_order: 0, notes: '', days: [] }],
  };
}

export function newDay(name: string, sortOrder: number): ProgramDay {
  return { id: newId(), name, sort_order: sortOrder, notes: '', prescriptions: [] };
}

export function newPrescription(exerciseId: string, exerciseName: string, sortOrder: number): Prescription {
  return {
    id: newId(), exercise_id: exerciseId, exercise_name: exerciseName, sort_order: sortOrder,
    target_sets: 3, target_reps_min: 8, target_reps_max: 10, target_load_grams: null,
    target_rpe_tenths: null, target_rir: null, rest_seconds: 90, tempo: '', superset_group: '', notes: '',
  };
}

/** Renumbers sort orders after an edit, so the document is always contiguous. */
function normalise(doc: ProgramDoc): ProgramDoc {
  return {
    ...doc,
    name: doc.name.trim() || 'Untitled Program',
    blocks: doc.blocks.map((b, bi) => ({
      ...b,
      sort_order: bi,
      weeks: Math.min(52, Math.max(1, Math.round(b.weeks) || 1)),
      days: b.days.map((d, di) => ({
        ...d,
        sort_order: di,
        prescriptions: d.prescriptions.map((p, pi) => ({ ...p, sort_order: pi })),
      })),
    })),
  };
}

/** The wire form: no display-only names. */
function wire(doc: ProgramDoc) {
  return {
    id: doc.id,
    name: doc.name,
    description: doc.description,
    is_template: doc.is_template,
    archived: doc.archived ?? false,
    blocks: doc.blocks.map((b) => ({
      id: b.id, name: b.name, weeks: b.weeks, sort_order: b.sort_order, notes: b.notes,
      days: b.days.map((d) => ({
        id: d.id, name: d.name, sort_order: d.sort_order, notes: d.notes,
        prescriptions: d.prescriptions.map(({ exercise_name: _name, ...p }) => p),
      })),
    })),
  };
}

/**
 * Saves a programme, locally at once and queued for the server.
 */
export async function saveProgram(db: Database, input: ProgramDoc): Promise<void> {
  const doc = normalise(input);
  const data = wire(doc);
  const now = new Date().toISOString();
  const structure = JSON.stringify({ blocks: data.blocks });

  const pending = await db.select<{ id: string; data: string }>(
    `SELECT id, data FROM outbox WHERE type = 'program.save' AND status = 'pending'`,
  );
  const queued = pending.find((row) => {
    try { return (JSON.parse(row.data) as { id?: string }).id === doc.id; } catch { return false; }
  });

  const writeLocal = async () => {
    await db.execute(
      `INSERT INTO programs (id, name, description, is_template, archived_at, structure, created_at, updated_at)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?)
       ON CONFLICT (id) DO UPDATE SET name = excluded.name, description = excluded.description,
         is_template = excluded.is_template, archived_at = excluded.archived_at,
         structure = excluded.structure, updated_at = excluded.updated_at`,
      [doc.id, doc.name, doc.description, doc.is_template ? 1 : 0, doc.archived ? now : null, structure, now, now],
    );
  };

  if (queued) {
    await db.transaction(async () => {
      await writeLocal();
      await db.execute(`UPDATE outbox SET data = ? WHERE id = ?`, [JSON.stringify(data), queued.id]);
    });
    return;
  }
  await act(db, 'program.save', data, writeLocal);
}

export async function archiveProgram(db: Database, id: string): Promise<void> {
  const doc = await getProgram(db, id);
  if (!doc) return;
  await saveProgram(db, { ...doc, archived: true });
}

export async function duplicateProgram(db: Database, id: string): Promise<string | null> {
  const doc = await getProgram(db, id);
  if (!doc) return null;
  const copy: ProgramDoc = {
    ...doc,
    id: newId(),
    name: `${doc.name} (Copy)`,
    blocks: doc.blocks.map((b) => ({
      ...b, id: newId(),
      days: b.days.map((d) => ({ ...d, id: newId(), prescriptions: d.prescriptions.map((p) => ({ ...p, id: newId() })) })),
    })),
  };
  await saveProgram(db, copy);
  return copy.id;
}

/**
 * Turns a gallery template into one of the trainer's own programmes.
 *
 * Exercises are matched by name against this practice's library. One that
 * the trainer has renamed or archived is left out rather than failing the
 * copy; the count of what was skipped comes back so the screen can say so.
 */
export async function copyTemplate(
  db: Database, template: ProgramTemplate,
): Promise<{ id: string; skipped: string[] }> {
  const library = await db.select<{ id: string; name: string }>(
    `SELECT id, name FROM exercises WHERE archived_at IS NULL`,
  );
  const byName = new Map(library.map((e) => [e.name.toLowerCase(), e]));
  const skipped: string[] = [];

  const doc: ProgramDoc = {
    id: newId(),
    name: template.name,
    description: template.summary,
    is_template: false,
    blocks: [{
      id: newId(), name: `${template.weeks}-Week Block`, weeks: template.weeks, sort_order: 0, notes: '',
      days: template.days.map((day, di) => ({
        id: newId(), name: day.name, sort_order: di, notes: '',
        prescriptions: day.exercises.flatMap((e, pi) => {
          const match = byName.get(e.name.toLowerCase());
          if (!match) { skipped.push(e.name); return []; }
          return [{
            id: newId(), exercise_id: match.id, exercise_name: match.name, sort_order: pi,
            target_sets: e.sets, target_reps_min: e.reps[0], target_reps_max: e.reps[1],
            target_load_grams: null, target_rpe_tenths: e.rpe ? e.rpe * 10 : null, target_rir: null,
            rest_seconds: e.rest ?? null, tempo: '', superset_group: e.superset ?? '', notes: e.notes ?? '',
          }];
        }),
      })),
    }],
  };
  await saveProgram(db, doc);
  return { id: doc.id, skipped };
}

export function templateByKey(key: string): ProgramTemplate | undefined {
  return TEMPLATES.find((t) => t.key === key);
}

/* ─────────────────────────── assignments ─────────────────────────── */

/**
 * Puts a client on a programme, replacing whatever they were following.
 *
 * The server closes the previous assignment in the same transaction; the
 * device does the same locally so the profile shows the new plan at once.
 */
export async function assignProgram(
  db: Database, programId: string, clientId: string, startsOn: Date = new Date(),
): Promise<string> {
  const day = startsOn.toISOString().slice(0, 10);
  return act(
    db,
    'program.assign',
    (id) => ({ id, program_id: programId, client_id: clientId, starts_on: day }),
    async (id) => {
      await db.execute(
        `UPDATE program_assignments SET status = 'completed' WHERE client_id = ? AND status = 'active'`,
        [clientId],
      );
      await db.execute(
        `INSERT INTO program_assignments (id, program_id, client_id, starts_on, status, notes, created_at, updated_at)
         VALUES (?, ?, ?, ?, 'active', '', ?, ?)`,
        [id, programId, clientId, day, new Date().toISOString(), new Date().toISOString()],
      );
    },
  );
}

export interface PlannedSession {
  assignmentId: string;
  program: ProgramDoc;
  day: ProgramDay;
  block: ProgramBlock;
  /** 1-based week within the whole programme. */
  week: number;
  totalWeeks: number;
  /** Workouts completed under this assignment so far. */
  completed: number;
  /** Planned sessions in the whole programme. */
  planned: number;
}

/**
 * What a client should do next.
 *
 * The programme is laid out as a sequence — each block's days, repeated for
 * each of its weeks — and the client's position is the number of workouts
 * they have completed under this assignment. Counting completed workouts
 * rather than calendar weeks means a missed week does not skip a session:
 * the client picks up where they left off, which is what a coach would do.
 */
export async function nextSession(db: Database, clientId: string): Promise<PlannedSession | null> {
  const assignment = await db.selectOne<{ id: string; program_id: string }>(
    `SELECT id, program_id FROM program_assignments
      WHERE client_id = ? AND status = 'active'
      ORDER BY starts_on DESC LIMIT 1`,
    [clientId],
  );
  if (!assignment) return null;
  const program = await getProgram(db, assignment.program_id);
  if (!program) return null;

  const sequence: { block: ProgramBlock; day: ProgramDay; week: number }[] = [];
  let weekOffset = 0;
  for (const block of program.blocks) {
    for (let w = 1; w <= Math.max(1, block.weeks); w++) {
      for (const day of block.days) sequence.push({ block, day, week: weekOffset + w });
    }
    weekOffset += Math.max(1, block.weeks);
  }
  if (sequence.length === 0) return null;

  const done = await db.selectOne<{ n: number }>(
    `SELECT count(*) AS n FROM workout_sessions
      WHERE assignment_id = ? AND status = 'completed' AND deleted_at IS NULL`,
    [assignment.id],
  );
  const completed = done?.n ?? 0;
  // Past the end, the programme repeats from its last block rather than
  // stopping: a client between programmes still has something to do.
  const lastWeek = Math.max(1, program.blocks.at(-1)?.days.length ?? 1);
  const position = completed < sequence.length
    ? completed
    : sequence.length - lastWeek + ((completed - sequence.length) % lastWeek);
  const entry = sequence[Math.max(0, Math.min(sequence.length - 1, position))]!;

  return {
    assignmentId: assignment.id, program, day: entry.day, block: entry.block,
    week: entry.week, totalWeeks: totalWeeks(program), completed, planned: sequence.length,
  };
}

/** Finds a training day anywhere in a programme. */
export function findDay(doc: ProgramDoc, dayId: string): ProgramDay | null {
  for (const b of doc.blocks) for (const d of b.days) if (d.id === dayId) return d;
  return null;
}

/** The programme and day a workout was started against, if any. */
export async function workoutPlan(
  db: Database, workoutId: string,
): Promise<{ program: ProgramDoc; day: ProgramDay; week: number } | null> {
  const w = await db.selectOne<{ assignment_id: string | null; day_id: string | null; week_number: number | null }>(
    `SELECT assignment_id, day_id, week_number FROM workout_sessions WHERE id = ?`,
    [workoutId],
  );
  if (!w?.assignment_id || !w.day_id) return null;
  const a = await db.selectOne<{ program_id: string }>(
    `SELECT program_id FROM program_assignments WHERE id = ?`, [w.assignment_id],
  );
  if (!a) return null;
  const program = await getProgram(db, a.program_id);
  if (!program) return null;
  const day = findDay(program, w.day_id);
  return day ? { program, day, week: w.week_number ?? 1 } : null;
}

/** Clients on a programme, for its detail screen. */
export async function programClients(db: Database, programId: string) {
  return db.select<{ id: string; fullName: string; startsOn: string }>(
    `SELECT c.id, c.full_name AS fullName, a.starts_on AS startsOn
       FROM program_assignments a JOIN clients c ON c.id = a.client_id
      WHERE a.program_id = ? AND a.status = 'active' AND c.deleted_at IS NULL
      ORDER BY c.full_name`,
    [programId],
  );
}
