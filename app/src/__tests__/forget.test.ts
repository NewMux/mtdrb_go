import { MemoryDatabase } from './support';
import { forgetPractice } from '@/db/forget';
import { SYNC_TABLES } from '@/db/schema';
import { getMeta, setMeta } from '@/db/types';
import * as outbox from '@/sync/outbox';
import { newId } from '@/lib/id';

describe('forgetting a deleted practice', () => {
  it('leaves no record of it on the device, but keeps the device preferences', async () => {
    const db = new MemoryDatabase();
    await db.execute('INSERT INTO clients (id, full_name) VALUES (?, ?)', [newId(), 'Dana']);
    await outbox.enqueue(db, newId(), 'attendance.mark', {});
    await setMeta(db, 'sync.cursor', 'abc');
    await setMeta(db, 'auth.account', '{"email":"sam@x"}');
    await setMeta(db, 'ui.prefs', '{"locale":"ar"}');

    await forgetPractice(db);

    for (const table of [...SYNC_TABLES, 'outbox']) {
      const row = await db.selectOne<{ n: number }>(`SELECT count(*) AS n FROM ${table}`);
      expect([table, row?.n]).toEqual([table, 0]);
    }
    expect(await getMeta(db, 'sync.cursor')).toBeNull();
    expect(await getMeta(db, 'auth.account')).toBeNull();
    expect(await getMeta(db, 'ui.prefs')).toBe('{"locale":"ar"}');
    db.close();
  });
});
