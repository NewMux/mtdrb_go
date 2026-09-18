/**
 * The outbox.
 *
 * Every write the trainer makes goes here first and is applied to the local
 * tables optimistically, so the UI responds at once whether or not there is
 * signal. The engine drains the queue when there is.
 *
 * Two rules keep this honest:
 *
 * Operations carry a client-minted id, so replaying one the server already
 * applied is recognisable rather than duplicating it. The server's push
 * endpoint is built to converge on replay for exactly this reason.
 *
 * An operation the server *refused* stops being retried. Left pending it would
 * jam the queue behind it forever, and the trainer would never learn that the
 * session they marked was rejected for want of credits.
 */

import type { Database } from '@/db/types';
import type { OperationType } from '@/api/types';

export type OutboxStatus = 'pending' | 'sent' | 'conflict' | 'rejected';

export interface OutboxEntry {
  id: string;
  type: OperationType;
  data: Record<string, unknown>;
  queued_at: string;
  status: OutboxStatus;
  attempts: number;
  error_code?: string | null;
  error_message?: string | null;
}

interface OutboxRow {
  id: string;
  type: string;
  data: string;
  queued_at: string;
  status: string;
  attempts: number;
  error_code: string | null;
  error_message: string | null;
}

function toEntry(row: OutboxRow): OutboxEntry {
  return {
    id: row.id,
    type: row.type as OperationType,
    data: JSON.parse(row.data) as Record<string, unknown>,
    queued_at: row.queued_at,
    status: row.status as OutboxStatus,
    attempts: row.attempts,
    error_code: row.error_code,
    error_message: row.error_message,
  };
}

/**
 * Queues an operation.
 *
 * The id is supplied by the caller rather than generated here, so the local
 * optimistic write and the queued operation can share it — which is what lets
 * the UI reconcile the two when the server answers.
 */
export async function enqueue(
  db: Database,
  id: string,
  type: OperationType,
  data: Record<string, unknown>,
  queuedAt: Date = new Date(),
): Promise<void> {
  await db.execute(
    `INSERT INTO outbox (id, type, data, queued_at, status, attempts)
     VALUES (?, ?, ?, ?, 'pending', 0)
     ON CONFLICT (id) DO UPDATE SET data = excluded.data, queued_at = excluded.queued_at`,
    [id, type, JSON.stringify(data), queuedAt.toISOString()],
  );
}

/**
 * Returns the next operations to send, oldest first.
 *
 * Order is the queued order, not insertion order, because the server applies a
 * batch in queued order and the two must agree about what the trainer did
 * first.
 */
export async function pending(db: Database, limit = 100): Promise<OutboxEntry[]> {
  const rows = await db.select<OutboxRow>(
    `SELECT id, type, data, queued_at, status, attempts, error_code, error_message
       FROM outbox WHERE status = 'pending'
      ORDER BY queued_at, rowid LIMIT ?`,
    [limit],
  );
  return rows.map(toEntry);
}

/** Removes operations the server accepted. */
export async function markSent(db: Database, ids: string[]): Promise<void> {
  if (ids.length === 0) return;
  const placeholders = ids.map(() => '?').join(',');
  await db.execute(`DELETE FROM outbox WHERE id IN (${placeholders})`, ids);
}

/**
 * Parks an operation the server refused.
 *
 * Kept rather than deleted: the trainer needs to see that the session they
 * marked did not go through, and why. Retrying it unchanged would only be
 * refused again.
 */
export async function markFailed(
  db: Database,
  id: string,
  status: 'conflict' | 'rejected',
  code: string,
  message: string,
): Promise<void> {
  await db.execute(
    `UPDATE outbox
        SET status = ?, error_code = ?, error_message = ?,
            attempts = attempts + 1, last_attempt_at = ?
      WHERE id = ?`,
    [status, code, message, new Date().toISOString(), id],
  );
}

/**
 * Records a failed send attempt without parking the operation.
 *
 * For a network failure, which says nothing about whether the operation is
 * valid — it stays pending and is tried again.
 */
export async function recordAttempt(db: Database, ids: string[]): Promise<void> {
  if (ids.length === 0) return;
  const placeholders = ids.map(() => '?').join(',');
  await db.execute(
    `UPDATE outbox SET attempts = attempts + 1, last_attempt_at = ?
      WHERE id IN (${placeholders})`,
    [new Date().toISOString(), ...ids],
  );
}

/** Operations the trainer needs to look at. */
export async function needsAttention(db: Database): Promise<OutboxEntry[]> {
  const rows = await db.select<OutboxRow>(
    `SELECT id, type, data, queued_at, status, attempts, error_code, error_message
       FROM outbox WHERE status IN ('conflict', 'rejected')
      ORDER BY queued_at`,
  );
  return rows.map(toEntry);
}

/** Discards a failed operation the trainer has decided to abandon. */
export async function discard(db: Database, id: string): Promise<void> {
  await db.execute('DELETE FROM outbox WHERE id = ?', [id]);
}

/** Counts for the sync indicator. */
export async function counts(db: Database): Promise<{ pending: number; failed: number }> {
  const row = await db.selectOne<{ pending: number; failed: number }>(
    `SELECT
       coalesce(sum(CASE WHEN status = 'pending' THEN 1 ELSE 0 END), 0) AS pending,
       coalesce(sum(CASE WHEN status IN ('conflict','rejected') THEN 1 ELSE 0 END), 0) AS failed
     FROM outbox`,
  );
  return { pending: row?.pending ?? 0, failed: row?.failed ?? 0 };
}
