/**
 * Brings a device's local schema up to date.
 *
 * Shared by every driver — expo-sqlite on a phone, sql.js in the demo, and
 * node:sqlite in the tests — so the upgrade a trainer's phone runs is the one
 * the tests ran.
 *
 * The version lives in SQLite's own `PRAGMA user_version`, which is 0 on a
 * database that has never been stamped. A device from before versioning holds
 * exactly the base schema, so 0 is read as 1.
 */

import { BASE_SCHEMA, UPGRADES, type SchemaUpgrade, type SyncTable } from './schema';
import { getMeta, setMeta, type Database } from './types';

/** Collections waiting to be pulled again from the start, comma-separated. */
export const RESYNC_KEY = 'sync.resync';

export async function migrate(db: Database, upgrades: SchemaUpgrade[] = UPGRADES): Promise<number> {
  for (const statement of BASE_SCHEMA) await db.execute(statement);

  const row = await db.selectOne<{ user_version: number }>('PRAGMA user_version');
  let current = Math.max(1, Number(row?.user_version ?? 0));

  const pending = [...upgrades].sort((a, b) => a.version - b.version).filter((u) => u.version > current);
  for (const upgrade of pending) {
    // Each step and its version stamp commit together, so a crash mid-way
    // leaves the device at a version whose statements all ran.
    await db.transaction(async () => {
      for (const statement of upgrade.statements) await db.execute(statement);
      if (upgrade.resync?.length) await requestResync(db, upgrade.resync);
    });
    // PRAGMA takes no bound parameters; the version is an integer we own.
    await db.execute(`PRAGMA user_version = ${Math.trunc(upgrade.version)}`);
    current = upgrade.version;
  }
  if (Number(row?.user_version ?? 0) === 0 && pending.length === 0) {
    await db.execute(`PRAGMA user_version = ${current}`);
  }
  return current;
}

/** Asks the next pull to restart these collections from the beginning. */
export async function requestResync(db: Database, collections: readonly SyncTable[]): Promise<void> {
  const existing = await pendingResync(db);
  const merged = [...new Set([...existing, ...collections])];
  await setMeta(db, RESYNC_KEY, merged.join(','));
}

export async function pendingResync(db: Database): Promise<string[]> {
  const raw = await getMeta(db, RESYNC_KEY);
  return raw ? raw.split(',').filter(Boolean) : [];
}

export async function clearResync(db: Database): Promise<void> {
  await db.execute('DELETE FROM meta WHERE key = ?', [RESYNC_KEY]);
}
