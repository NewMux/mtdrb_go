/**
 * The language a screen speaks in.
 *
 * `useT()` gives a screen its translator and the formatters that go with it,
 * so a screen never reaches for Intl or a locale itself.
 */

import React, { createContext, useContext } from 'react';

import { ar } from './ar';
import { en, type EnglishMessages } from './en';
import {
  daysUntil, formatAmount, formatCompact, formatDate, formatMoney, formatNumber, type Digits, type FormatOptions,
} from './format';
import { isRTL, type Locale } from './plural';
import { createTranslator, type MessageKey, type Params } from './translate';

export type { Locale } from './plural';
export type { Digits } from './format';
export type TKey = MessageKey<EnglishMessages>;

const dictionaries: Record<Locale, unknown> = { en, ar };

export interface I18n {
  locale: Locale;
  digits: Digits;
  isRTL: boolean;
  dir: 'ltr' | 'rtl';
  t: (key: TKey, params?: Params) => string;
  number: (n: number, fractionDigits?: number) => string;
  /** "12K" — for axes and tight spaces. */
  compact: (n: number) => string;
  money: (minor: number, currency: string) => string;
  amount: (minor: number, currency: string) => string;
  date: (value: string | Date, style?: Parameters<typeof formatDate>[1]) => string;
  time: (value: string | Date) => string;
  /** "3 days overdue", "due today", "due in 5 days" — or '' with no due date. */
  due: (dueDate: string | null | undefined, today?: Date) => string;
}

const cache = new Map<string, I18n>();

export function i18nFor(locale: Locale, digits: Digits): I18n {
  const key = `${locale}:${digits}`;
  const hit = cache.get(key);
  if (hit) return hit;

  const options: FormatOptions = { locale, digits };
  const number = (n: number, fractionDigits?: number) => formatNumber(n, options, fractionDigits);
  const t = createTranslator<TKey>(locale, dictionaries[locale], en, (n) => number(n));

  const i18n: I18n = {
    locale,
    digits,
    isRTL: isRTL(locale),
    dir: isRTL(locale) ? 'rtl' : 'ltr',
    t,
    number,
    compact: (n) => formatCompact(n, options),
    money: (minor, currency) => formatMoney(minor, currency, options),
    amount: (minor, currency) => formatAmount(minor, currency, options),
    date: (value, style = 'medium') => formatDate(value, style, options),
    time: (value) => formatDate(value, 'time', options),
    due: (dueDate, today) => {
      if (!dueDate) return '';
      const days = daysUntil(dueDate, today);
      if (days < 0) return t('due.overdue', { count: Math.abs(days) });
      if (days === 0) return t('due.today');
      return t('due.inDays', { count: days });
    },
  };
  cache.set(key, i18n);
  return i18n;
}

export const I18nContext = createContext<I18n>(i18nFor('en', 'latn'));

export function useT(): I18n {
  return useContext(I18nContext);
}

export function I18nProvider({ value, children }: { value: I18n; children: React.ReactNode }) {
  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

/** Days overdue, for tone decisions ("is this late?") that do not need words. */
export { daysUntil };
