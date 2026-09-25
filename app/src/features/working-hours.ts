/**
 * Working hours, as the business settings edit them.
 *
 * The server stores a map of weekday to intervals and refuses anything
 * malformed; these helpers keep the form in the same shape and catch a typo
 * before it is sent, so "9:00" becomes "09:00" rather than a refusal.
 */

import type { WorkingHours } from '@/api/account';

export type Interval = [string, string];

/** Normalises a typed time to HH:MM, or null if it is not one. */
export function normaliseTime(text: string): string | null {
  const match = /^\s*(\d{1,2})(?::?(\d{2}))?\s*$/.exec(text);
  if (!match) return null;
  const hours = Number(match[1]);
  const minutes = Number(match[2] ?? '0');
  if (hours > 23 || minutes > 59) return null;
  return `${String(hours).padStart(2, '0')}:${String(minutes).padStart(2, '0')}`;
}

/** The days of the week in the order the trainer's week runs. */
export function weekOrder(weekStart: number): string[] {
  return Array.from({ length: 7 }, (_, i) => String((weekStart + i) % 7));
}

/**
 * Checks an edited week and returns it ready to send, or the first problem.
 * Empty days are dropped: a day with no hours is a day off.
 */
export function cleanHours(hours: Record<string, Interval[]>):
  { ok: true; hours: WorkingHours } | { ok: false; day: string } {
  const out: WorkingHours = {};
  for (const [day, intervals] of Object.entries(hours)) {
    const cleaned: Interval[] = [];
    for (const [from, to] of intervals) {
      const start = normaliseTime(from);
      const end = normaliseTime(to);
      if (!start || !end || start >= end) return { ok: false, day };
      cleaned.push([start, end]);
    }
    cleaned.sort((a, b) => a[0].localeCompare(b[0]));
    for (let i = 1; i < cleaned.length; i++) {
      if (cleaned[i]![0] < cleaned[i - 1]![1]) return { ok: false, day };
    }
    if (cleaned.length > 0) out[day] = cleaned;
  }
  return { ok: true, hours: out };
}
