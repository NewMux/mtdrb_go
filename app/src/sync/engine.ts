/**
 * The sync engine.
 *
 * Pull writes server rows into the local mirror; push drains the outbox. They
 * run in that order on purpose — sending first would have the device pull back
 * its own writes a moment later and briefly show stale values over fresh ones.
 *
 * Nothing here decides *what* an operation means. The server does that, and
 * the engine's job is only to get the queue there and the answers back.
 */

import type { Database } from '@/db/types';
import { getMeta, setMeta } from '@/db/types';
import { SYNC_TABLES, type SyncTable } from '@/db/schema';
import { ApiClient, NetworkError } from '@/api/client';
import type { PushOperation } from '@/api/types';
import * as outbox from './outbox';

const CURSOR_KEY = 'sync.cursor';

export interface SyncReport {
  pulled: number;
  pushed: number;
  conflicts: number;
  rejected: number;
  /** True when the device is offline; not an error, just a fact. */
  offline: boolean;
  error?: string;
}

const syncTableSet = new Set<string>(SYNC_TABLES);

/**
 * Writes a pulled row into the local mirror.
 *
 * The column list comes from the row itself rather than from a local schema
 * constant, so a field the server adds arrives without a client release —
 * provided the column exists locally. Unknown columns are dropped rather than
 * throwing, because a device that refuses to sync after a server deploy is
 * worse than one that is briefly missing a field.
 */
async function upsertRow(
  db: Database,
  table: SyncTable,
  row: Record<string, unknown>,
  knownColumns: Set<string>,
): Promise<void> {
  const columns = Object.keys(row).filter((c) => knownColumns.has(c));
  if (columns.length === 0 || !columns.includes('id')) return;

  const values = columns.map((c) => {
    const value = row[c];
    if (value === null || value === undefined) return null;
    // SQLite has no boolean or object types.
    if (typeof value === 'boolean') return value ? 1 : 0;
    if (typeof value === 'object') return JSON.stringify(value);
    return value;
  });

  const placeholders = columns.map(() => '?').join(', ');
  const updates = columns
    .filter((c) => c !== 'id')
    .map((c) => `${c} = excluded.${c}`)
    .join(', ');

  await db.execute(
    `INSERT INTO ${table} (${columns.join(', ')}) VALUES (${placeholders})
     ON CONFLICT (id) DO UPDATE SET ${updates}`,
    values,
  );
}

/** Reads a local table's columns, so a pulled row can be matched to them. */
async function columnsOf(db: Database, table: string): Promise<Set<string>> {
  const rows = await db.select<{ name: string }>(`PRAGMA table_info(${table})`);
  return new Set(rows.map((r) => r.name));
}

/**
 * Pulls changes and writes them locally.
 *
 * Loops while the server says there is more, so a first sync on a busy account
 * completes rather than arriving one page per interval.
 */
export async function pull(db: Database, api: ApiClient): Promise<number> {
  const columnCache = new Map<string, Set<string>>();
  let pulled = 0;
  let cursor = (await getMeta(db, CURSOR_KEY)) ?? '';

  for (let page = 0; page < 100; page++) {
    const result = await api.pull(cursor);

    for (const change of result.changes) {
      if (!syncTableSet.has(change.collection)) {
        // A collection this client does not know about. Ignored rather than
        // fatal: an older app talking to a newer server should keep working.
        continue;
      }
      const table = change.collection as SyncTable;
      let columns = columnCache.get(table);
      if (!columns) {
        columns = await columnsOf(db, table);
        columnCache.set(table, columns);
      }
      for (const row of change.rows) {
        await upsertRow(db, table, row, columns);
        pulled++;
      }
    }

    cursor = result.cursor;
    // Persisted per page, so an interrupted first sync resumes rather than
    // starting over.
    await setMeta(db, CURSOR_KEY, cursor);

    if (!result.has_more) break;
  }
  return pulled;
}

/**
 * Drains the outbox.
 *
 * Operations refused by the server are parked with their reason rather than
 * retried, so the queue cannot jam behind one that will never succeed — and
 * the trainer gets told.
 */
export async function push(
  db: Database,
  api: ApiClient,
): Promise<{ pushed: number; conflicts: number; rejected: number }> {
  const entries = await outbox.pending(db);
  if (entries.length === 0) return { pushed: 0, conflicts: 0, rejected: 0 };

  const operations: PushOperation[] = entries.map((e) => ({
    id: e.id,
    type: e.type,
    queued_at: e.queued_at,
    data: e.data,
  }));

  let result;
  try {
    result = await api.push(operations);
  } catch (error) {
    if (error instanceof NetworkError) {
      // Still pending; the next run tries again.
      await outbox.recordAttempt(db, entries.map((e) => e.id));
      throw error;
    }
    throw error;
  }

  const applied: string[] = [];
  let conflicts = 0;
  let rejected = 0;

  for (const outcome of result.results) {
    if (outcome.status === 'applied') {
      applied.push(outcome.id);
      continue;
    }
    if (outcome.status === 'conflict') conflicts++;
    else rejected++;

    await outbox.markFailed(
      db,
      outcome.id,
      outcome.status,
      outcome.code ?? 'unknown',
      outcome.message ?? 'the server refused this operation',
    );
  }

  await outbox.markSent(db, applied);
  return { pushed: applied.length, conflicts, rejected };
}

/**
 * One full cycle.
 *
 * Pull first, then push: sending first would have the device immediately pull
 * back its own writes and briefly render stale values over fresh ones.
 */
export async function synchronise(db: Database, api: ApiClient): Promise<SyncReport> {
  const report: SyncReport = { pulled: 0, pushed: 0, conflicts: 0, rejected: 0, offline: false };

  try {
    report.pulled = await pull(db, api);
    const pushed = await push(db, api);
    report.pushed = pushed.pushed;
    report.conflicts = pushed.conflicts;
    report.rejected = pushed.rejected;
  } catch (error) {
    if (error instanceof NetworkError) {
      // Being offline is the normal case this app is built for, not a failure
      // worth showing anyone.
      report.offline = true;
      return report;
    }
    report.error = error instanceof Error ? error.message : String(error);
  }
  return report;
}

/** Forgets the cursor, so the next pull re-mirrors everything. */
export async function resetCursor(db: Database): Promise<void> {
  await setMeta(db, CURSOR_KEY, '');
}
