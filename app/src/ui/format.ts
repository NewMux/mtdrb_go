/**
 * Display formatting.
 *
 * Everything crossing the wire is an integer in its smallest unit — minor
 * currency units, grams, tenths of an RPE point. These turn those into
 * something a person reads, and nothing else in the app should be doing this
 * arithmetic inline.
 */

/** Renders minor units, e.g. 50000 EUR as "500.00 EUR". */
export function money(minor: number, currency: string): string {
  const negative = minor < 0;
  const abs = Math.abs(minor);
  const whole = Math.floor(abs / 100);
  const cents = abs % 100;
  return `${negative ? '-' : ''}${whole}.${String(cents).padStart(2, '0')} ${currency}`;
}

/**
 * Renders a load in the trainer's preferred unit.
 *
 * Stored in grams so it never rounds; converted only here, at the edge.
 */
export function load(grams: number | null | undefined, unit: 'kg' | 'lb' = 'kg'): string {
  if (grams === null || grams === undefined) return '—';
  if (unit === 'lb') {
    const pounds = grams / 453.59237;
    return `${trimZeros(pounds.toFixed(1))} lb`;
  }
  return `${trimZeros((grams / 1000).toFixed(2))} kg`;
}

/** Renders RPE from tenths: 85 becomes "8.5". */
export function rpe(tenths: number | null | undefined): string {
  if (tenths === null || tenths === undefined) return '—';
  return trimZeros((tenths / 10).toFixed(1));
}

/** Renders body fat from basis points: 1550 becomes "15.5%". */
export function bodyFat(basisPoints: number | null | undefined): string {
  if (basisPoints === null || basisPoints === undefined) return '—';
  return `${trimZeros((basisPoints / 100).toFixed(1))}%`;
}

/** Renders a rest interval as "2:30" or "90s". */
export function rest(seconds: number | null | undefined): string {
  if (seconds === null || seconds === undefined) return '—';
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remainder = seconds % 60;
  return remainder === 0 ? `${minutes}m` : `${minutes}:${String(remainder).padStart(2, '0')}`;
}

/** A prescribed rep range: "6–8", or "8" when both bounds match. */
export function repRange(min?: number | null, max?: number | null): string {
  if (min === null || min === undefined) return max ? `${max}` : '—';
  if (max === null || max === undefined || max === min) return `${min}`;
  return `${min}–${max}`;
}

/** A time of day, as "09:00". */
export function clockTime(iso: string): string {
  const d = new Date(iso);
  return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
}

/** A date a person reads: "Fri 1 May". */
export function shortDate(iso: string): string {
  const d = new Date(iso);
  return d.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' });
}

/** "3 days overdue", "due today", "due in 5 days". */
export function dueLabel(dueDate: string | null | undefined, today = new Date()): string {
  if (!dueDate) return '';
  const due = new Date(dueDate);
  const days = Math.round((due.getTime() - startOfDay(today).getTime()) / 86_400_000);
  if (days < 0) return `${Math.abs(days)} day${Math.abs(days) === 1 ? '' : 's'} overdue`;
  if (days === 0) return 'due today';
  return `due in ${days} day${days === 1 ? '' : 's'}`;
}

/**
 * The inverse direction: what a trainer types, turned back into integers.
 *
 * These live beside the formatters on purpose. Unit conversion happens in
 * exactly one file, in both directions, so a screen can never invent its own
 * rounding — and every one of them returns null rather than NaN, because a
 * half-typed "1." must leave the field empty, not write garbage to the ledger.
 */

/** "72.5" kg becomes 72500 grams. */
export function parseLoad(text: string, unit: 'kg' | 'lb' = 'kg'): number | null {
  const value = parseNumber(text);
  if (value === null) return null;
  return Math.round(unit === 'lb' ? value * 453.59237 : value * 1000);
}

/** "8.5" becomes 85 tenths. Out-of-range values are rejected, not clamped. */
export function parseRpe(text: string): number | null {
  const value = parseNumber(text);
  if (value === null) return null;
  const tenths = Math.round(value * 10);
  return tenths < 0 || tenths > 100 ? null : tenths;
}

/** "12" becomes 12. Reps are whole; "12.5" is a typo, not half a rep. */
export function parseReps(text: string): number | null {
  const value = parseNumber(text);
  if (value === null || value < 0 || !Number.isInteger(value)) return null;
  return value;
}

/** "120.50" becomes 12050 minor units. */
export function parseMoney(text: string): number | null {
  const value = parseNumber(text);
  if (value === null) return null;
  return Math.round(value * 100);
}

/** "82.4" kg becomes 82400 grams. */
export function parseWeight(text: string): number | null {
  return parseLoad(text, 'kg');
}

/** "15.5" becomes 1550 basis points. */
export function parseBodyFat(text: string): number | null {
  const value = parseNumber(text);
  if (value === null) return null;
  const bp = Math.round(value * 100);
  return bp < 0 || bp > 10000 ? null : bp;
}

/**
 * A finite number, or null.
 *
 * Accepts a comma as the decimal separator, because a phone keyboard in most
 * of Europe offers one and a trainer typing "72,5" means seventy-two and a
 * half — not nothing.
 */
function parseNumber(text: string): number | null {
  const trimmed = text.trim().replace(',', '.');
  if (trimmed === '') return null;
  if (!/^-?\d*\.?\d+$/.test(trimmed)) return null;
  const value = Number(trimmed);
  return Number.isFinite(value) ? value : null;
}

function startOfDay(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), d.getDate());
}

function trimZeros(s: string): string {
  return s.includes('.') ? s.replace(/\.?0+$/, '') : s;
}
