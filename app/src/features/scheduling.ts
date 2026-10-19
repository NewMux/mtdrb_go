/**
 * The calendar: booking, rescheduling and cancelling sessions.
 *
 * Bookings are made offline like every other write. The device mints the
 * session's id and each attendee's, so a session booked in a basement can be
 * marked attended in the same basement and both operations reach the server
 * referring to the same rows.
 *
 * The device checks for overlaps before booking, as a courtesy — the real
 * refusal is the server's exclusion constraint, because two devices booking
 * the same slot offline would both pass a local check. A clash the server
 * refuses comes back as a refused operation on the Sync screen, in words.
 *
 * Repeating bookings are expanded here into one booking per occurrence rather
 * than sent as a series. A three-month series will usually hit something, and
 * this way a clash refuses that one Tuesday rather than the whole series.
 */

import type { Database } from '@/db/types';
import { newId } from '@/lib/id';
import { act } from './actions';

export interface SessionType {
  id: string;
  name: string;
  durationMinutes: number;
  capacity: number;
  creditCost: number;
  colour: string | null;
}

export async function sessionTypes(db: Database): Promise<SessionType[]> {
  return db.select<SessionType>(
    `SELECT id, name, coalesce(duration_minutes, 60) AS durationMinutes,
            coalesce(capacity, 1) AS capacity, coalesce(credit_cost, 1) AS creditCost, colour
       FROM session_types WHERE archived_at IS NULL ORDER BY capacity, name`,
  );
}

export interface CalendarAttendee {
  attendeeId: string;
  clientId: string;
  name: string;
  status: string;
}

export interface CalendarSession {
  id: string;
  startsAt: string;
  endsAt: string;
  status: string;
  location: string;
  notes: string;
  typeId: string | null;
  typeName: string;
  colour: string | null;
  attendees: CalendarAttendee[];
}

/** Sessions overlapping a window, with their rosters, in time order. */
export async function sessionsBetween(db: Database, from: Date, to: Date, includeCancelled = false): Promise<CalendarSession[]> {
  const rows = await db.select<{
    id: string; startsAt: string; endsAt: string | null; status: string; location: string | null;
    notes: string | null; typeId: string | null; typeName: string | null; colour: string | null;
    attendeeId: string | null; clientId: string | null; name: string | null; attendeeStatus: string | null;
  }>(
    `SELECT s.id, s.starts_at AS startsAt, s.ends_at AS endsAt, coalesce(s.status, 'scheduled') AS status,
            s.location, s.notes, s.session_type_id AS typeId, st.name AS typeName, st.colour,
            sa.id AS attendeeId, sa.client_id AS clientId, c.full_name AS name, sa.status AS attendeeStatus
       FROM sessions s
       LEFT JOIN session_types st ON st.id = s.session_type_id
       LEFT JOIN session_attendees sa ON sa.session_id = s.id
       LEFT JOIN clients c ON c.id = sa.client_id AND c.deleted_at IS NULL
      WHERE s.starts_at >= ? AND s.starts_at < ?
        AND (? = 1 OR coalesce(s.status, '') != 'cancelled')
      ORDER BY s.starts_at, c.full_name`,
    [from.toISOString(), to.toISOString(), includeCancelled ? 1 : 0],
  );

  const byId = new Map<string, CalendarSession>();
  for (const r of rows) {
    let s = byId.get(r.id);
    if (!s) {
      s = {
        id: r.id, startsAt: r.startsAt, endsAt: r.endsAt ?? r.startsAt, status: r.status,
        location: r.location ?? '', notes: r.notes ?? '', typeId: r.typeId, typeName: r.typeName ?? 'Session',
        colour: r.colour, attendees: [],
      };
      byId.set(r.id, s);
    }
    if (r.attendeeId && r.clientId && r.name) {
      s.attendees.push({ attendeeId: r.attendeeId, clientId: r.clientId, name: r.name, status: r.attendeeStatus ?? 'scheduled' });
    }
  }
  return [...byId.values()];
}

export async function sessionById(db: Database, id: string): Promise<CalendarSession | null> {
  const s = await db.selectOne<{ startsAt: string }>(`SELECT starts_at AS startsAt FROM sessions WHERE id = ?`, [id]);
  if (!s) return null;
  const at = new Date(s.startsAt);
  const list = await sessionsBetween(db, new Date(at.getTime() - 1000), new Date(at.getTime() + 1000), true);
  return list.find((x) => x.id === id) ?? null;
}

/** Counts of sessions per day across a window, for the week strip's dots. */
export async function busyDays(db: Database, from: Date, to: Date): Promise<Map<string, number>> {
  const rows = await db.select<{ startsAt: string }>(
    `SELECT starts_at AS startsAt FROM sessions
      WHERE starts_at >= ? AND starts_at < ? AND coalesce(status, '') != 'cancelled'`,
    [from.toISOString(), to.toISOString()],
  );
  const map = new Map<string, number>();
  for (const r of rows) {
    const key = dayKey(new Date(r.startsAt));
    map.set(key, (map.get(key) ?? 0) + 1);
  }
  return map;
}

/** A local calendar day as YYYY-MM-DD. */
export function dayKey(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

export interface BookingInput {
  sessionTypeId: string;
  startsAt: Date;
  durationMinutes: number;
  clientIds: string[];
  location?: string;
  notes?: string;
  /** Book the same slot this many weeks running, including the first. */
  weeks?: number;
}

/** Existing sessions that a proposed slot would overlap. */
export async function clashes(db: Database, startsAt: Date, durationMinutes: number, ignoreId?: string): Promise<CalendarSession[]> {
  const end = new Date(startsAt.getTime() + durationMinutes * 60_000);
  const sameDay = await sessionsBetween(
    db, new Date(startsAt.getTime() - 12 * 3600_000), new Date(startsAt.getTime() + 12 * 3600_000),
  );
  return sameDay.filter((s) => s.id !== ignoreId
    && new Date(s.startsAt) < end && new Date(s.endsAt) > startsAt);
}

/**
 * Books a session — or a weekly run of them — for one or more clients.
 *
 * Returns the ids of the sessions created.
 */
export async function bookSession(db: Database, input: BookingInput): Promise<string[]> {
  const created: string[] = [];
  const weeks = Math.max(1, Math.min(52, input.weeks ?? 1));

  for (let w = 0; w < weeks; w++) {
    const starts = new Date(input.startsAt.getTime());
    starts.setDate(starts.getDate() + w * 7);
    const ends = new Date(starts.getTime() + input.durationMinutes * 60_000);
    const attendeeIds = input.clientIds.map(() => newId());
    const now = new Date().toISOString();

    const id = await act(
      db,
      'session.book',
      (sessionId) => ({
        id: sessionId,
        session_type_id: input.sessionTypeId,
        starts_at: starts.toISOString(),
        client_ids: input.clientIds,
        attendee_ids: attendeeIds,
        location: input.location ?? '',
        notes: input.notes ?? '',
      }),
      async (sessionId) => {
        await db.execute(
          `INSERT INTO sessions (id, session_type_id, starts_at, ends_at, status, location, notes, created_at, updated_at)
           VALUES (?, ?, ?, ?, 'scheduled', ?, ?, ?, ?)`,
          [sessionId, input.sessionTypeId, starts.toISOString(), ends.toISOString(), input.location ?? '', input.notes ?? '', now, now],
        );
        for (let i = 0; i < input.clientIds.length; i++) {
          await db.execute(
            `INSERT INTO session_attendees (id, session_id, client_id, status, credits_charged, notes, created_at, updated_at)
             VALUES (?, ?, ?, 'scheduled', 0, '', ?, ?)`,
            [attendeeIds[i], sessionId, input.clientIds[i], now, now],
          );
        }
      },
    );
    created.push(id);
  }
  return created;
}

export async function rescheduleSession(db: Database, sessionId: string, startsAt: Date): Promise<void> {
  const current = await db.selectOne<{ starts_at: string; ends_at: string | null }>(
    `SELECT starts_at, ends_at FROM sessions WHERE id = ?`, [sessionId],
  );
  if (!current) return;
  const duration = current.ends_at
    ? new Date(current.ends_at).getTime() - new Date(current.starts_at).getTime()
    : 3600_000;
  const ends = new Date(startsAt.getTime() + duration);
  await act(
    db,
    'session.reschedule',
    { session_id: sessionId, starts_at: startsAt.toISOString() },
    async () => {
      await db.execute(
        `UPDATE sessions SET starts_at = ?, ends_at = ?, updated_at = ? WHERE id = ?`,
        [startsAt.toISOString(), ends.toISOString(), new Date().toISOString(), sessionId],
      );
    },
  );
}

/**
 * Cancels a session.
 *
 * This is the trainer cancelling — nobody is charged. A client's late cancel
 * is an attendance outcome, marked on the roster, and burns a credit.
 */
export async function cancelSession(db: Database, sessionId: string): Promise<void> {
  await act(
    db,
    'session.cancel',
    { session_id: sessionId },
    async () => {
      await db.execute(
        `UPDATE sessions SET status = 'cancelled', updated_at = ? WHERE id = ?`,
        [new Date().toISOString(), sessionId],
      );
    },
  );
}

/** A client's upcoming sessions, soonest first. */
export async function upcomingFor(db: Database, clientId: string, limit = 5) {
  return db.select<{ sessionId: string; startsAt: string; typeName: string; attendeeId: string }>(
    `SELECT s.id AS sessionId, s.starts_at AS startsAt, coalesce(st.name, 'Session') AS typeName, sa.id AS attendeeId
       FROM session_attendees sa
       JOIN sessions s ON s.id = sa.session_id
       LEFT JOIN session_types st ON st.id = s.session_type_id
      WHERE sa.client_id = ? AND s.starts_at >= ? AND coalesce(s.status, '') != 'cancelled'
      ORDER BY s.starts_at LIMIT ?`,
    [clientId, new Date().toISOString(), limit],
  );
}
