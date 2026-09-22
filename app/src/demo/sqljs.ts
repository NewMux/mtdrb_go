/**
 * Native stub for the in-page demo driver.
 *
 * Metro picks this file (over ./sqljs.web.ts) for iOS and Android bundles.
 * The demo flag is web-only — see openDatabase() in ../db/index.ts — so this
 * is never actually called on a device; it exists so requiring '@/demo/sqljs'
 * resolves on native without pulling sql.js, and its `fs` dependency, into a
 * bundle that has no `fs`.
 */

import type { Database } from '@/db/types';

export async function openInPageDatabase(): Promise<Database> {
  throw new Error('openInPageDatabase is web-only');
}
