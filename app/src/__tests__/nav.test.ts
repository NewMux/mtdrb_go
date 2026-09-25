import { isHeldByAnotherTab } from '@/db/errors';
import { ALL_NAV_MODULES, HIDDEN_MODULES, moduleForPath, NAV_GROUPS, NAV_MODULES, PHONE_TABS } from '@/ui/nav';

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

describe('unbuilt modules', () => {
  it('stay out of every menu but keep their screens registered', () => {
    const hidden = HIDDEN_MODULES.map((m) => m.key).sort();
    expect(hidden).toEqual(['analytics', 'calendar', 'insights', 'programs', 'shop', 'tasks']);
    for (const m of NAV_MODULES) expect(m.unbuilt).toBeFalsy();
    for (const key of PHONE_TABS) expect(HIDDEN_MODULES.some((m) => m.key === key)).toBe(false);
    for (const g of NAV_GROUPS) expect(g.modules.length).toBeGreaterThan(0);
    expect(NAV_MODULES.length + HIDDEN_MODULES.length).toBe(ALL_NAV_MODULES.length);
  });
});

describe('storage errors', () => {
  it('tells a second tab apart from a broken browser', () => {
    expect(isHeldByAnotherTab("NoModificationAllowedError: Failed to execute 'createSyncAccessHandle'")).toBe(true);
    expect(isHeldByAnotherTab('OPFS: Storage directory access is denied')).toBe(false);
  });
});
