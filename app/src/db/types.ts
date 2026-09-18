/**
 * The database interface and the helpers that only need SQL.
 *
 * Kept apart from the expo-sqlite driver on purpose: everything that speaks to
 * the database — the sync engine, the outbox, every action — depends on this
 * file and not on the platform. That is what lets the offline logic be tested
 * against a real SQLite in Node rather than mocked, which is the difference
 * between testing the queries and testing a fake.
 */

export type Row = Record<string, unknown>;

export interface Database {
  execute(sql: string, params?: unknown[]): Promise<void>;
  select<T = Row>(sql: string, params?: unknown[]): Promise<T[]>;
  selectOne<T = Row>(sql: string, params?: unknown[]): Promise<T | null>;
  transaction<T>(fn: () => Promise<T>): Promise<T>;
}

/** Reads a device-local setting. */
export async function getMeta(db: Database, key: string): Promise<string | null> {
  const row = await db.selectOne<{ value: string }>(
    'SELECT value FROM meta WHERE key = ?',
    [key],
  );
  return row?.value ?? null;
}

/** Writes a device-local setting. */
export async function setMeta(db: Database, key: string, value: string): Promise<void> {
  await db.execute(
    'INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value',
    [key, value],
  );
}
