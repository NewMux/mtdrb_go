/**
 * Calendar arithmetic: month grids, weeks and date ranges.
 *
 * Dates here are calendar days in the device's local time, written as
 * YYYY-MM-DD — the form the API speaks — so nothing is ever off by a day for
 * a trainer in Dubai reading a session booked at 23:30.
 */

/** 0 is Sunday. The UAE starts its week on Monday, Saudi Arabia on Sunday. */
export type Weekday = 0 | 1 | 2 | 3 | 4 | 5 | 6;

export function isoDay(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

export function parseDay(day: string): Date {
  const [y, m, d] = day.split('-').map(Number);
  return new Date(y ?? 1970, (m ?? 1) - 1, d ?? 1);
}

export function addDays(d: Date, days: number): Date {
  const next = new Date(d);
  next.setDate(next.getDate() + days);
  return next;
}

export function startOfWeek(d: Date, weekStartsOn: Weekday): Date {
  const start = new Date(d.getFullYear(), d.getMonth(), d.getDate());
  const offset = (start.getDay() - weekStartsOn + 7) % 7;
  return addDays(start, -offset);
}

/** The seven days of the week containing `d`. */
export function weekDays(d: Date, weekStartsOn: Weekday): Date[] {
  const start = startOfWeek(d, weekStartsOn);
  return Array.from({ length: 7 }, (_, i) => addDays(start, i));
}

/**
 * A month as whole weeks, padded with the neighbouring months' days so every
 * row has seven cells — how a month view is always drawn.
 */
export function monthGrid(year: number, month: number, weekStartsOn: Weekday): Date[][] {
  const first = new Date(year, month, 1);
  const start = startOfWeek(first, weekStartsOn);
  const weeks: Date[][] = [];
  let cursor = start;
  do {
    weeks.push(Array.from({ length: 7 }, (_, i) => addDays(cursor, i)));
    cursor = addDays(cursor, 7);
  } while (cursor.getMonth() === month && cursor.getFullYear() === year);
  return weeks;
}

export type RangePreset = 'today' | 'last7' | 'last30' | 'last90' | 'monthToDate' | 'lastMonth' | 'quarterToDate' | 'yearToDate';

export interface DayRange { from: string; to: string }

/** The preset date ranges every analytics filter offers, inclusive of both ends. */
export function presetRange(preset: RangePreset, today = new Date()): DayRange {
  const t = new Date(today.getFullYear(), today.getMonth(), today.getDate());
  const to = isoDay(t);
  switch (preset) {
    case 'today': return { from: to, to };
    case 'last7': return { from: isoDay(addDays(t, -6)), to };
    case 'last30': return { from: isoDay(addDays(t, -29)), to };
    case 'last90': return { from: isoDay(addDays(t, -89)), to };
    case 'monthToDate': return { from: isoDay(new Date(t.getFullYear(), t.getMonth(), 1)), to };
    case 'lastMonth': {
      const first = new Date(t.getFullYear(), t.getMonth() - 1, 1);
      const last = new Date(t.getFullYear(), t.getMonth(), 0);
      return { from: isoDay(first), to: isoDay(last) };
    }
    case 'quarterToDate': {
      const q = Math.floor(t.getMonth() / 3) * 3;
      return { from: isoDay(new Date(t.getFullYear(), q, 1)), to };
    }
    case 'yearToDate': return { from: isoDay(new Date(t.getFullYear(), 0, 1)), to };
  }
}
