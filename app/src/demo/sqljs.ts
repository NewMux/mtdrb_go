/**
 * SQLite that runs in the page, for demo builds hosted in a sandboxed frame.
 *
 * `expo-sqlite` on web spawns a Worker and stores its file in OPFS. Both are
 * denied to a frame sandboxed without `allow-same-origin`: the Worker script
 * "cannot be accessed from origin 'null'", and OPFS refuses outright. That is
 * how an embedded demo is hosted, so the app simply never started.
 *
 * sql.js's asm.js build has neither dependency — no worker, no WebAssembly to
 * fetch, no storage handle. It is slower and it lives only in memory, which
 * for a seeded demo costs nothing: there is no server to sync with and the
 * recording is replayed again on reload.
 *
 * Only a demo build carries it: metro.config.js resolves it to nothing when
 * `EXPO_PUBLIC_DEMO` is not set. The real app keeps expo-sqlite, which is the
 * right driver on a device.
 */

import type { Database, Row } from '@/db/types';
import { migrate } from '@/db/migrate';

/** The slice of sql.js this needs, rather than pulling in its whole surface. */
interface SqlJsStatement {
  bind(params: unknown[]): void;
  step(): boolean;
  getAsObject(): Row;
  free(): void;
  run(params: unknown[]): void;
}
interface SqlJsDatabase {
  run(sql: string, params?: unknown[]): void;
  exec(sql: string): unknown;
  prepare(sql: string): SqlJsStatement;
}

class InPageDatabase implements Database {
  constructor(private readonly db: SqlJsDatabase) {}

  async execute(sql: string, params: unknown[] = []): Promise<void> {
    const statement = this.db.prepare(sql);
    try {
      statement.run(params);
    } finally {
      statement.free();
    }
  }

  async select<T = Row>(sql: string, params: unknown[] = []): Promise<T[]> {
    const statement = this.db.prepare(sql);
    try {
      statement.bind(params);
      const rows: T[] = [];
      while (statement.step()) rows.push(statement.getAsObject() as T);
      return rows;
    } finally {
      statement.free();
    }
  }

  async selectOne<T = Row>(sql: string, params: unknown[] = []): Promise<T | null> {
    const rows = await this.select<T>(sql, params);
    return rows[0] ?? null;
  }

  async transaction<T>(fn: () => Promise<T>): Promise<T> {
    this.db.run('BEGIN');
    try {
      const result = await fn();
      this.db.run('COMMIT');
      return result;
    } catch (error) {
      this.db.run('ROLLBACK');
      throw error;
    }
  }
}

/** Opens an in-memory database with the local schema applied. */
export async function openInPageDatabase(): Promise<Database> {
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  const initSqlJs = require('sql.js/dist/sql-asm.js') as
    (config?: Record<string, unknown>) => Promise<{ Database: new () => SqlJsDatabase }>;

  const SQL = await initSqlJs();
  const db = new SQL.Database();
  const wrapped = new InPageDatabase(db);
  await migrate(wrapped);
  return wrapped;
}
