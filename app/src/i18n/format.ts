/**
 * Numbers, money and dates in the reader's language.
 *
 * `ui/format.ts` stays the locale-free layer — minor units to a plain string,
 * held to the same vectors as the server. This file is the one the screens
 * read: the same amounts through Intl, in English or Arabic, with the digits
 * the trainer chose.
 *
 * Digits default to Latin even in Arabic. Business across the Gulf mostly
 * writes 500.00 rather than ٥٠٠٫٠٠, and a trainer reading a bank reference
 * against an invoice should not have to translate numerals. Arabic-Indic is a
 * setting away for those who want it.
 *
 * Every formatter falls back to the plain layer if Intl is missing or refuses
 * a locale, because a price that throws is worse than a price in the wrong
 * script.
 */

import { currencyExponent, money as plainMoney } from '@/ui/format';
import type { Locale } from './plural';

export type Digits = 'latn' | 'arab';

export interface FormatOptions {
  locale: Locale;
  digits: Digits;
}

/**
 * The BCP 47 tag behind a locale. English is en-GB for day-first dates, which
 * is how the Gulf and most of the world write them; Arabic is the UAE's.
 */
export function localeTag({ locale, digits }: FormatOptions): string {
  const base = locale === 'ar' ? 'ar-AE' : 'en-GB';
  return `${base}-u-nu-${digits}`;
}

function safely<T>(run: () => T, fallback: () => T): T {
  try {
    return run();
  } catch {
    return fallback();
  }
}

export function formatNumber(n: number, options: FormatOptions, fractionDigits?: number): string {
  return safely(
    () => new Intl.NumberFormat(localeTag(options), {
      minimumFractionDigits: fractionDigits,
      maximumFractionDigits: fractionDigits ?? 2,
    }).format(n),
    () => (fractionDigits === undefined ? String(n) : n.toFixed(fractionDigits)),
  );
}

/**
 * "12K", "1.2M" — for a chart axis, where a full amount does not fit and the
 * tooltip carries the exact figure.
 */
export function formatCompact(n: number, options: FormatOptions): string {
  return safely(
    () => new Intl.NumberFormat(localeTag(options), { notation: 'compact', maximumFractionDigits: 1 }).format(n),
    () => {
      const abs = Math.abs(n);
      if (abs >= 1e6) return `${Math.round(n / 1e5) / 10}M`;
      if (abs >= 1e3) return `${Math.round(n / 1e2) / 10}K`;
      return String(Math.round(n * 10) / 10);
    },
  );
}

/** "AED 500.00" / "‏500.00 AED", in the currency's own precision. */
export function formatMoney(minor: number, currency: string, options: FormatOptions): string {
  const code = currency.trim().toUpperCase();
  const exponent = currencyExponent(code);
  return safely(
    () => new Intl.NumberFormat(localeTag(options), {
      style: 'currency',
      currency: code,
      currencyDisplay: 'code',
      minimumFractionDigits: exponent,
      maximumFractionDigits: exponent,
    }).format(minor / 10 ** exponent),
    () => plainMoney(minor, code),
  );
}

/** The amount alone, for a figure with the currency set beside it: "500.00". */
export function formatAmount(minor: number, currency: string, options: FormatOptions): string {
  const exponent = currencyExponent(currency);
  return formatNumber(minor / 10 ** exponent, options, exponent);
}

type DateStyle = 'short' | 'medium' | 'long' | 'weekday' | 'month' | 'time' | 'dayMonth';

const dateStyles: Record<DateStyle, Intl.DateTimeFormatOptions> = {
  short: { day: 'numeric', month: 'short' },
  medium: { weekday: 'short', day: 'numeric', month: 'short' },
  long: { weekday: 'long', day: 'numeric', month: 'long' },
  weekday: { weekday: 'short' },
  month: { month: 'long', year: 'numeric' },
  time: { hour: '2-digit', minute: '2-digit', hour12: false },
  dayMonth: { day: 'numeric', month: 'long', year: 'numeric' },
};

export function formatDate(value: string | Date, style: DateStyle, options: FormatOptions): string {
  const date = typeof value === 'string' ? new Date(value) : value;
  if (Number.isNaN(date.getTime())) return '—';
  return safely(
    () => new Intl.DateTimeFormat(localeTag(options), dateStyles[style]).format(date),
    () => (style === 'time'
      ? `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
      : date.toISOString().slice(0, 10)),
  );
}

/**
 * Whole days from today to a due date: negative when overdue. The wording is
 * the dictionary's job, so it lives there and can plural in Arabic.
 */
export function daysUntil(dueDate: string, today = new Date()): number {
  const due = new Date(dueDate);
  const start = new Date(today.getFullYear(), today.getMonth(), today.getDate());
  const dueDay = new Date(due.getFullYear(), due.getMonth(), due.getDate());
  return Math.round((dueDay.getTime() - start.getTime()) / 86_400_000);
}
