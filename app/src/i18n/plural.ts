/**
 * Plural categories, implemented here rather than read from Intl.
 *
 * Hermes — the engine on a phone — does not reliably ship Intl.PluralRules,
 * and a missing plural rule does not throw: it silently picks "other", which
 * in Arabic means "٣ جلسات" and "١١ جلسات" read identically wrong. Two
 * languages is a small enough table to own.
 *
 * Arabic has all six CLDR categories: zero, one, two, few (3–10), many
 * (11–99) and other (100, 101, 102 and fractions).
 */

export type PluralCategory = 'zero' | 'one' | 'two' | 'few' | 'many' | 'other';

export type Locale = 'en' | 'ar';

export const LOCALES: readonly Locale[] = ['en', 'ar'];

export function pluralCategory(locale: Locale, count: number): PluralCategory {
  const n = Math.abs(count);
  if (locale === 'ar') {
    if (!Number.isInteger(n)) return 'other';
    if (n === 0) return 'zero';
    if (n === 1) return 'one';
    if (n === 2) return 'two';
    const lastTwo = n % 100;
    if (lastTwo >= 3 && lastTwo <= 10) return 'few';
    if (lastTwo >= 11 && lastTwo <= 99) return 'many';
    return 'other';
  }
  return n === 1 ? 'one' : 'other';
}

/** Languages written right to left. */
export function isRTL(locale: Locale): boolean {
  return locale === 'ar';
}
