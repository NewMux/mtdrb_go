/**
 * Why the local database would not open.
 *
 * On the web the database lives in the browser's private file system, and a
 * file there can be held by one tab at a time. A second tab gets an access
 * handle error that reads like a broken browser, when the fix is to close the
 * other tab — so that case is told apart and said plainly.
 */
export function isHeldByAnotherTab(message: string): boolean {
  return /SyncAccessHandle|NoModificationAllowed|Access Handles? cannot|already (open|locked)|database is locked/i.test(message);
}
