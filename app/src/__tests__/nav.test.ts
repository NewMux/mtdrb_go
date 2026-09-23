import { isHeldByAnotherTab } from '@/db/errors';
import { moduleForPath, NAV_MODULES, PHONE_TABS } from '@/ui/nav';

describe('navigation', () => {
  it('finds the module a path belongs to, however deep', () => {
    expect(moduleForPath('/dashboard')?.key).toBe('dashboard');
    expect(moduleForPath('/dashboard/')?.key).toBe('dashboard');
    expect(moduleForPath('/dashboard/today')?.key).toBe('today');
    expect(moduleForPath('/dashboard/clients/0190a1b2')?.key).toBe('clients');
    expect(moduleForPath('/sync')).toBeUndefined();
  });

  it('keeps every phone tab in the module list, and every href unique', () => {
    for (const key of PHONE_TABS) expect(NAV_MODULES.some((m) => m.key === key)).toBe(true);
    expect(new Set(NAV_MODULES.map((m) => m.href)).size).toBe(NAV_MODULES.length);
  });
});

describe('storage errors', () => {
  it('tells a second tab apart from a broken browser', () => {
    expect(isHeldByAnotherTab("NoModificationAllowedError: Failed to execute 'createSyncAccessHandle'")).toBe(true);
    expect(isHeldByAnotherTab('OPFS: Storage directory access is denied')).toBe(false);
  });
});
