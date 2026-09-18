/**
 * An in-memory stand-in for the device database.
 *
 * Backed by node:sqlite, which ships with Node 22, so the offline logic can be
 * tested without a simulator. The SQL is the same SQL the device runs — a
 * mock object would let a broken query pass.
 */

import { DatabaseSync } from 'node:sqlite';
import type { Database, Row } from '@/db/types';
import { MIGRATIONS } from '@/db/schema';

export class MemoryDatabase implements Database {
  private readonly db = new DatabaseSync(':memory:');

  constructor() {
    for (const statement of MIGRATIONS) this.db.exec(statement);
  }

  async execute(sql: string, params: unknown[] = []): Promise<void> {
    this.db.prepare(sql).run(...(params as never[]));
  }

  async select<T = Row>(sql: string, params: unknown[] = []): Promise<T[]> {
    return this.db.prepare(sql).all(...(params as never[])) as T[];
  }

  async selectOne<T = Row>(sql: string, params: unknown[] = []): Promise<T | null> {
    const row = this.db.prepare(sql).get(...(params as never[]));
    return (row as T) ?? null;
  }

  async transaction<T>(fn: () => Promise<T>): Promise<T> {
    this.db.exec('BEGIN');
    try {
      const result = await fn();
      this.db.exec('COMMIT');
      return result;
    } catch (error) {
      this.db.exec('ROLLBACK');
      throw error;
    }
  }

  close(): void {
    this.db.close();
  }
}

/** A fetch that answers from a scripted queue, for driving the client. */
export function stubFetch(
  handler: (url: string, init: RequestInit | undefined) => { status: number; body: unknown },
): { fetch: typeof fetch; calls: { url: string; init: RequestInit | undefined }[] } {
  const calls: { url: string; init: RequestInit | undefined }[] = [];

  const impl = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString();
    calls.push({ url, init });
    const { status, body } = handler(url, init);
    return {
      ok: status >= 200 && status < 300,
      status,
      text: async () => (body === null ? '' : JSON.stringify(body)),
      json: async () => body,
    } as Response;
  }) as unknown as typeof fetch;

  return { fetch: impl, calls };
}

/** A token store held in memory. */
export function memoryTokens(access = 'access-token', refresh = 'refresh-token') {
  let a: string | null = access;
  let r: string | null = refresh;
  return {
    accessToken: async () => a,
    refreshToken: async () => r,
    save: async (newAccess: string, newRefresh: string) => {
      a = newAccess;
      r = newRefresh;
    },
    clear: async () => {
      a = null;
      r = null;
    },
    current: () => ({ access: a, refresh: r }),
  };
}
