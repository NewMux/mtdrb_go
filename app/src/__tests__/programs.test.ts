/**
 * Programmes and the calendar, against real SQLite.
 *
 * The rules worth pinning: a programme saves whole and folds repeated saves
 * into one queued operation that keeps its place ahead of anything depending
 * on it; a client's position in a programme comes from workouts completed,
 * not from the calendar; and a booking carries the ids the device minted for
 * the session and every attendee.
 */

import { MemoryDatabase } from './support';
import { completeWorkout } from '@/features/actions';
import { TEMPLATES } from '@/features/library';
import {
  assignProgram, copyTemplate, emptyProgram, getProgram, listPrograms, newDay, newPrescription,
  nextSession, saveProgram,
} from '@/features/programs';
import { bookSession, cancelSession, clashes, rescheduleSession, sessionsBetween } from '@/features/scheduling';
import { openOrStartWorkout } from '@/features/workflow';
import * as outbox from '@/sync/outbox';

describe('programmes', () => {
  let db: MemoryDatabase;

  beforeEach(async () => {
    db = new MemoryDatabase();
    await db.execute(`INSERT INTO clients (id, full_name, status, notes) VALUES ('c1','Dana Rivers','active','')`);
    for (const [id, name] of [['e1', 'Back Squat'], ['e2', 'Bench Press'], ['e3', 'Barbell Row'], ['e4', 'Overhead Press'], ['e5', 'Conventional Deadlift']]) {
      await db.execute(`INSERT INTO exercises (id, name, category) VALUES (?, ?, 'squat')`, [id, name]);
    }
  });
  afterEach(() => { db.close(); });

  it('saves a programme locally with its structure, and queues it', async () => {
    const doc = emptyProgram('Strength');
    doc.blocks[0]!.days.push({ ...newDay('Day A', 0), prescriptions: [newPrescription('e1', 'Back Squat', 0)] });
    await saveProgram(db, doc);

    const back = await getProgram(db, doc.id);
    expect(back?.name).toBe('Strength');
    expect(back?.blocks[0]?.days[0]?.prescriptions[0]?.exercise_name).toBe('Back Squat');

    const queued = await outbox.pending(db);
    expect(queued).toHaveLength(1);
    expect(queued[0]?.type).toBe('program.save');
    // The wire form carries no display-only names.
    const rx = (queued[0]?.data.blocks as { days: { prescriptions: Record<string, unknown>[] }[] }[])[0]!.days[0]!.prescriptions[0]!;
    expect(rx.exercise_name).toBeUndefined();
    expect(rx.exercise_id).toBe('e1');
  });

  it('folds repeated saves into one queued operation that keeps its place', async () => {
    const doc = emptyProgram('Draft');
    await saveProgram(db, doc);
    await assignProgram(db, doc.id, 'c1');
    await saveProgram(db, { ...doc, name: 'Final' });

    const queued = await outbox.pending(db);
    expect(queued.map((q) => q.type)).toEqual(['program.save', 'program.assign']);
    expect(queued[0]?.data.name).toBe('Final');
  });

  it('copies a template, resolving exercises by name and reporting the rest', async () => {
    const t = TEMPLATES.find((x) => x.key === 'strength-5x5')!;
    const { id, skipped } = await copyTemplate(db, t);
    const doc = await getProgram(db, id);
    expect(doc?.blocks[0]?.weeks).toBe(t.weeks);
    expect(doc?.blocks[0]?.days.map((d) => d.name)).toEqual(['Workout A', 'Workout B']);
    expect(doc?.blocks[0]?.days[0]?.prescriptions.map((p) => p.exercise_name)).toEqual(['Back Squat', 'Bench Press', 'Barbell Row']);
    expect(skipped).toEqual([]);
    expect((await listPrograms(db))[0]).toMatchObject({ weeks: 8, days: 2, exercises: 6 });
  });

  it('works out the next session from workouts completed, and opens the workout on it', async () => {
    const { id } = await copyTemplate(db, TEMPLATES.find((x) => x.key === 'strength-5x5')!);
    const assignment = await assignProgram(db, id, 'c1');

    let next = await nextSession(db, 'c1');
    expect(next).toMatchObject({ assignmentId: assignment, week: 1, completed: 0, planned: 16 });
    expect(next?.day.name).toBe('Workout A');

    const first = await openOrStartWorkout(db, 'c1');
    expect(first.created).toBe(true);
    // Asking again resumes rather than opening a second workout.
    expect((await openOrStartWorkout(db, 'c1')).id).toBe(first.id);
    await completeWorkout(db, first.id);

    next = await nextSession(db, 'c1');
    expect(next?.day.name).toBe('Workout B');
    expect(next?.completed).toBe(1);

    const row = await db.selectOne<{ assignment_id: string; day_id: string }>(
      `SELECT assignment_id, day_id FROM workout_sessions WHERE id = ?`, [first.id]);
    expect(row?.assignment_id).toBe(assignment);
  });

  it('replaces the active assignment rather than stacking a second', async () => {
    const a = emptyProgram('A');
    const b = emptyProgram('B');
    await saveProgram(db, a);
    await saveProgram(db, b);
    await assignProgram(db, a.id, 'c1');
    await assignProgram(db, b.id, 'c1');
    const active = await db.select<{ program_id: string }>(
      `SELECT program_id FROM program_assignments WHERE client_id = 'c1' AND status = 'active'`);
    expect(active).toEqual([{ program_id: b.id }]);
  });
});

describe('calendar', () => {
  let db: MemoryDatabase;
  const nine = new Date('2026-11-02T09:00:00Z');

  beforeEach(async () => {
    db = new MemoryDatabase();
    await db.execute(`INSERT INTO clients (id, full_name, status, notes) VALUES ('c1','Dana Rivers','active',''), ('c2','Leo Martins','active','')`);
    await db.execute(`INSERT INTO session_types (id, name, duration_minutes, capacity, credit_cost) VALUES ('t1','1-on-1',60,1,1)`);
  });
  afterEach(() => { db.close(); });

  it('books with device-minted ids for the session and each attendee', async () => {
    const [id] = await bookSession(db, { sessionTypeId: 't1', startsAt: nine, durationMinutes: 60, clientIds: ['c1'] });
    const [session] = await sessionsBetween(db, new Date('2026-11-02T00:00:00Z'), new Date('2026-11-03T00:00:00Z'));
    expect(session?.id).toBe(id);
    expect(session?.endsAt).toBe('2026-11-02T10:00:00.000Z');

    const [op] = await outbox.pending(db);
    expect(op?.type).toBe('session.book');
    expect(op?.data.id).toBe(id);
    expect(op?.data.attendee_ids).toEqual([session?.attendees[0]?.attendeeId]);
  });

  it('expands a weekly booking into one operation per week', async () => {
    const ids = await bookSession(db, { sessionTypeId: 't1', startsAt: nine, durationMinutes: 60, clientIds: ['c1'], weeks: 4 });
    expect(new Set(ids).size).toBe(4);
    const all = await sessionsBetween(db, nine, new Date('2026-12-31T00:00:00Z'));
    expect(all.map((s) => s.startsAt.slice(0, 10))).toEqual(['2026-11-02', '2026-11-09', '2026-11-16', '2026-11-23']);
    expect((await outbox.pending(db)).filter((o) => o.type === 'session.book')).toHaveLength(4);
  });

  it('spots a clash before booking', async () => {
    await bookSession(db, { sessionTypeId: 't1', startsAt: nine, durationMinutes: 60, clientIds: ['c1'] });
    expect(await clashes(db, new Date('2026-11-02T09:30:00Z'), 60)).toHaveLength(1);
    expect(await clashes(db, new Date('2026-11-02T10:00:00Z'), 60)).toHaveLength(0);
  });

  it('moves a session keeping its length, and cancels without deleting', async () => {
    const [id] = await bookSession(db, { sessionTypeId: 't1', startsAt: nine, durationMinutes: 45, clientIds: ['c1'] });
    await rescheduleSession(db, id!, new Date('2026-11-03T15:00:00Z'));
    let [s] = await sessionsBetween(db, new Date('2026-11-03T00:00:00Z'), new Date('2026-11-04T00:00:00Z'));
    expect(s?.endsAt).toBe('2026-11-03T15:45:00.000Z');

    await cancelSession(db, id!);
    expect(await sessionsBetween(db, new Date('2026-11-03T00:00:00Z'), new Date('2026-11-04T00:00:00Z'))).toHaveLength(0);
    [s] = await sessionsBetween(db, new Date('2026-11-03T00:00:00Z'), new Date('2026-11-04T00:00:00Z'), true);
    expect(s?.status).toBe('cancelled');
    expect((await outbox.pending(db)).map((o) => o.type)).toEqual(['session.book', 'session.reschedule', 'session.cancel']);
  });
});
