import { nextSort, sortRows } from '@/ui/table-logic';
import { isoDay, monthGrid, presetRange, weekDays } from '@/ui/calendar-logic';

describe('table sorting', () => {
  const rows = [
    { name: 'Morgan', credits: 3, email: 'm@x' },
    { name: 'dana', credits: 7, email: null },
    { name: 'Priya', credits: 0, email: 'p@x' },
  ];

  it('sorts text case-insensitively and numbers numerically', () => {
    expect(sortRows(rows, (r) => r.name, 'asc').map((r) => r.name)).toEqual(['dana', 'Morgan', 'Priya']);
    expect(sortRows(rows, (r) => r.credits, 'desc').map((r) => r.credits)).toEqual([7, 3, 0]);
  });

  it('keeps empty values last whichever way the order runs', () => {
    expect(sortRows(rows, (r) => r.email, 'asc').at(-1)?.name).toBe('dana');
    expect(sortRows(rows, (r) => r.email, 'desc').at(-1)?.name).toBe('dana');
  });

  it('cycles a header through ascending, descending and off', () => {
    const a = nextSort(null, 'name');
    const b = nextSort(a, 'name');
    expect(a).toEqual({ key: 'name', direction: 'asc' });
    expect(b).toEqual({ key: 'name', direction: 'desc' });
    expect(nextSort(b, 'name')).toBeNull();
    expect(nextSort(b, 'credits')).toEqual({ key: 'credits', direction: 'asc' });
  });
});

describe('calendar arithmetic', () => {
  it('draws a month as whole weeks from the chosen first weekday', () => {
    // September 2026 starts on a Tuesday.
    const monday = monthGrid(2026, 8, 1);
    expect(isoDay(monday[0]![0]!)).toBe('2026-08-31');
    expect(monday.every((w) => w.length === 7)).toBe(true);
    const sunday = monthGrid(2026, 8, 0);
    expect(isoDay(sunday[0]![0]!)).toBe('2026-08-30');
    expect(isoDay(sunday.at(-1)!.at(-1)!)).toBe('2026-10-03');
  });

  it('starts a week on Monday in the UAE and Sunday in Saudi Arabia', () => {
    const wed = new Date(2026, 8, 23);
    expect(isoDay(weekDays(wed, 1)[0]!)).toBe('2026-09-21');
    expect(isoDay(weekDays(wed, 0)[0]!)).toBe('2026-09-20');
  });

  it('builds preset ranges inclusive of today', () => {
    const today = new Date(2026, 8, 23);
    expect(presetRange('last7', today)).toEqual({ from: '2026-09-17', to: '2026-09-23' });
    expect(presetRange('monthToDate', today)).toEqual({ from: '2026-09-01', to: '2026-09-23' });
    expect(presetRange('lastMonth', today)).toEqual({ from: '2026-08-01', to: '2026-08-31' });
    expect(presetRange('quarterToDate', today)).toEqual({ from: '2026-07-01', to: '2026-09-23' });
  });
});
