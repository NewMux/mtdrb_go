/**
 * Removes a deleted practice from this device.
 *
 * Signing out deliberately keeps the local mirror, because unsent work in the
 * outbox is real work. Deleting the account is the opposite case: the server
 * has stopped accepting anything from it and will erase it, and a phone that
 * went on holding its clients' names, health answers and measurements would
 * make "delete" a lie. Device preferences — language, theme — are not the
 * practice's and stay.
 */

import { SYNC_TABLES } from './schema';
import type { Database } from './types';

export async function forgetPractice(db: Database): Promise<void> {
  await db.transaction(async () => {
    for (const table of SYNC_TABLES) await db.execute(`DELETE FROM ${table}`);
    await db.execute('DELETE FROM outbox');
    // Cursors, the signed-in account and anything else keyed to the practice.
    await db.execute("DELETE FROM meta WHERE key LIKE 'sync.%' OR key LIKE 'auth.%'");
  });
}
