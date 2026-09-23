/**
 * The expo-sqlite driver.
 *
 * This is the only file that imports the platform database. Everything else
 * depends on the `Database` interface in ./types, so the offline logic can be
 * exercised against a real SQLite in Node without a simulator.
 */

import { Platform } from 'react-native';
import * as SQLite from 'expo-sqlite';
import { migrate } from './migrate';
import type { Database, Row } from './types';

export type { Database, Row } from './types';
export { getMeta, setMeta } from './types';

class SQLiteDatabase implements Database {
  constructor(private readonly db: SQLite.SQLiteDatabase) {}

  async execute(sql: string, params: unknown[] = []): Promise<void> {
    await this.db.runAsync(sql, params as SQLite.SQLiteBindParams);
  }

  async select<T = Row>(sql: string, params: unknown[] = []): Promise<T[]> {
    return (await this.db.getAllAsync(sql, params as SQLite.SQLiteBindParams)) as T[];
  }

  async selectOne<T = Row>(sql: string, params: unknown[] = []): Promise<T | null> {
    const row = await this.db.getFirstAsync(sql, params as SQLite.SQLiteBindParams);
    return (row as T) ?? null;
  }

  async transaction<T>(fn: () => Promise<T>): Promise<T> {
    // withTransactionAsync resolves to void, so the result is captured rather
    // than cast — a cast here would lie about what the caller gets back.
    let result!: T;
    await this.db.withTransactionAsync(async () => {
      result = await fn();
    });
    return result;
  }
}

let instance: Database | null = null;

/**
 * Opens the local database and applies the schema.
 *
 * A demo build on the web takes a different driver. expo-sqlite needs a Worker
 * and OPFS there, and a frame sandboxed without `allow-same-origin` — which is
 * how an embedded demo is hosted — denies both, so the app never started at
 * all. The condition is on the inlined build flag so this branch, and the
 * engine behind it, disappear from a normal build.
 */
export async function openDatabase(): Promise<Database> {
  if (instance) return instance;

  if (process.env.EXPO_PUBLIC_DEMO === '1' && Platform.OS === 'web') {
    // require, not a dynamic import: an import() becomes a separate chunk the
    // page has to fetch at runtime, and a demo embedded in a frame may have no
    // reachable origin to fetch it from. This keeps it in the one bundle,
    // still dropped from a build where the flag is not set.
    // eslint-disable-next-line @typescript-eslint/no-var-requires
    const { openInPageDatabase } = require('@/demo/sqljs') as typeof import('@/demo/sqljs');
    instance = await openInPageDatabase();
    return instance;
  }

  const db = await SQLite.openDatabaseAsync('coachpulse.db');
  // WAL keeps a background sync write from blocking a read the trainer is
  // waiting on mid-set.
  await db.execAsync('PRAGMA journal_mode = WAL');
  await db.execAsync('PRAGMA foreign_keys = OFF');

  const wrapped = new SQLiteDatabase(db);
  await migrate(wrapped);
  instance = wrapped;
  return instance;
}

/** Replaces the database, for tests. */
export function setDatabase(db: Database | null): void {
  instance = db;
}
