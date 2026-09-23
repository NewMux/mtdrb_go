/**
 * Display formatting.
 *
 * Everything crossing the wire is an integer in its smallest unit — minor
 * currency units, grams, tenths of an RPE point. These turn those into
 * something a person reads, and nothing else in the app should be doing this
 * arithmetic inline.
 */

/**
 * ISO-4217 currencies whose minor unit is not a hundredth.
 *
 * Most of the Gulf counts in hundredths, but Kuwait, Bahrain and Oman count in
 * fils — thousandths — so a dinar rendered with two places would be off by a
 * factor of ten. Mirrors the table in internal/platform/money, and both are
 * held to the same vectors file.
 */
const EXPONENTS: Record<string, number> = {
  BHD: 3, IQD: 3, JOD: 3, KWD: 3, LYD: 3, OMR: 3, TND: 3,
  BIF: 0, CLP: 0, DJF: 0, GNF: 0, ISK: 0, JPY: 0, KMF: 0, KRW: 0, PYG: 0,
  RWF: 0, UGX: 0, VND: 0, VUV: 0, XAF: 0, XOF: 0, XPF: 0,
};

/** Decimal places in the currency's minor unit. */
export function currencyExponent(currency: string): number {
  return EXPONENTS[currency.trim().toUpperCase()] ?? 2;
}

/** Minor units as an editable amount with no currency code: "500.00", "12.500". */
export function amountText(minor: number, currency: string): string {
  const rendered = money(minor, currency);
  return rendered.slice(0, rendered.lastIndexOf(' '));
}

/** Renders minor units in the currency's own precision: "500.00 AED", "12.500 KWD". */
export function money(minor: number, currency: string): string {
  const code = currency.trim().toUpperCase();
  const exponent = currencyExponent(code);
  const negative = minor < 0;
  const abs = Math.abs(minor);
  if (exponent === 0) return `${negative ? '-' : ''}${abs} ${code}`;
  const scale = 10 ** exponent;
  const whole = Math.floor(abs / scale);
  const fraction = abs % scale;
  return `${negative ? '-' : ''}${whole}.${String(fraction).padStart(exponent, '0')} ${code}`;
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
export function parseMoney(text: string, currency = 'EUR'): number | null {
  // Parsed as digits rather than through a float, so "0.29" is 29 and never
  // 28.999… rounded, and so a fraction finer than the currency allows is
  // refused rather than silently rounded away.
  const trimmed = text.trim().replace(',', '.');
  if (!/^-?\d*\.?\d+$/.test(trimmed)) return null;
  const negative = trimmed.startsWith('-');
  const [whole = '', fraction = ''] = trimmed.replace('-', '').split('.');
  const exponent = currencyExponent(currency);
  if (fraction.length > exponent) return null;
  const minor = Number((whole || '0') + fraction.padEnd(exponent, '0'));
  if (!Number.isSafeInteger(minor)) return null;
  return negative ? -minor : minor;
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

function trimZeros(s: string): string {
  return s.includes('.') ? s.replace(/\.?0+$/, '') : s;
}
