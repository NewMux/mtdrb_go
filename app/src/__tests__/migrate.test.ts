import { MemoryDatabase, stubFetch, memoryTokens } from './support';
import { ApiClient } from '@/api/client';
import { migrate, pendingResync } from '@/db/migrate';
import { SCHEMA_VERSION, type SchemaUpgrade } from '@/db/schema';
import { pull } from '@/sync/engine';
import type { PullResult } from '@/api/types';

/**
 * A device keeps its database across app releases, so every schema change has
 * to reach one that already synced. Pull drops columns the device does not
 * know and the device's cursor is past its old rows, so without a resync a new
 * column would fill in only as each row next changed — possibly never.
 */
describe('local schema upgrades', () => {
  const addGoals: SchemaUpgrade = {
    version: 2,
    statements: [`ALTER TABLE clients ADD COLUMN goals TEXT`],
    resync: ['clients'],
  };

  it('stamps a fresh device at the current version', async () => {
    const db = new MemoryDatabase({ baseOnly: true });
    expect(await migrate(db)).toBe(SCHEMA_VERSION);
    const row = await db.selectOne<{ user_version: number }>('PRAGMA user_version');
    expect(row?.user_version).toBe(SCHEMA_VERSION);
    db.close();
  });

  it('runs an upgrade once, and asks for its collections again', async () => {
    const db = new MemoryDatabase({ baseOnly: true });
    await db.execute(`INSERT INTO clients (id, full_name) VALUES ('c1', 'Dana Rivers')`);

    expect(await migrate(db, [addGoals])).toBe(2);
    expect(await pendingResync(db)).toEqual(['clients']);

    // Running again is a no-op rather than a duplicate-column error.
    expect(await migrate(db, [addGoals])).toBe(2);

    const seen: string[] = [];
    const page: PullResult = {
      cursor: 'cursor-2', has_more: false, server_time: '',
      changes: [{ collection: 'clients', rows: [{ id: 'c1', full_name: 'Dana Rivers', goals: 'Deadlift 140' }] }],
    };
    const { fetch } = stubFetch((url) => { seen.push(url); return { status: 200, body: page }; });
    const api = new ApiClient({ baseUrl: 'https://api.test', tokens: memoryTokens(), fetchImpl: fetch });

    await pull(db, api);
    expect(seen[0]).toContain('reset=clients');
    expect(await pendingResync(db)).toEqual([]);
    const dana = await db.selectOne<{ goals: string }>(`SELECT goals FROM clients WHERE id = 'c1'`);
    expect(dana?.goals).toBe('Deadlift 140');

    // The reset is spent: the next pull resumes from the cursor.
    await pull(db, api);
    expect(seen[1]).not.toContain('reset=');
    db.close();
  });
});
