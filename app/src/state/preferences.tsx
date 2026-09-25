/**
 * Language, numerals and appearance — per device, not per account.
 *
 * A trainer who reads Arabic on their phone may still want English on the
 * shared laptop at the studio, so these live in the device's own `meta` table
 * rather than on the server. On the web they are mirrored to localStorage too,
 * which is the only store the page can read before the app starts: that is
 * what lets the first paint already be right-to-left and dark, rather than
 * flashing English and white.
 *
 * Direction is the one preference that cannot apply live on a phone. React
 * Native lays out right-to-left only after `I18nManager.forceRTL` and a
 * restart, so switching to Arabic there asks to restart; on the web the
 * document's `dir` changes in place.
 */

import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { DevSettings, I18nManager, Platform, useColorScheme } from 'react-native';
import { getLocales } from 'expo-localization';

import { getMeta, setMeta } from '@/db/types';
import { i18nFor, I18nProvider, type Digits, type Locale } from '@/i18n';
import { isRTL, LOCALES } from '@/i18n/plural';
import { themeFor, ThemeProvider } from '@/ui/theming';
import type { ColorScheme } from '@/ui/theme';
import { useApp } from './app';

export type ThemeMode = 'system' | ColorScheme;

export interface Preferences {
  /** null follows the device's language. */
  locale: Locale | null;
  digits: Digits;
  theme: ThemeMode;
}

const PREFS_KEY = 'ui.prefs';
/** Read by app/+html.tsx before the bundle runs. Keep the two in step. */
export const WEB_PREFS_KEY = 'coachpulse.prefs';

const DEFAULTS: Preferences = { locale: null, digits: 'latn', theme: 'system' };

function parse(raw: string | null): Preferences | null {
  if (!raw) return null;
  try {
    const value = JSON.parse(raw) as Partial<Preferences>;
    return {
      locale: value.locale && LOCALES.includes(value.locale) ? value.locale : null,
      digits: value.digits === 'arab' ? 'arab' : 'latn',
      theme: value.theme === 'light' || value.theme === 'dark' ? value.theme : 'system',
    };
  } catch {
    return null;
  }
}

function webStorage(): Storage | null {
  if (Platform.OS !== 'web' || typeof window === 'undefined') return null;
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

function deviceLocale(): Locale {
  try {
    const code = getLocales()[0]?.languageCode;
    return code === 'ar' ? 'ar' : 'en';
  } catch {
    return 'en';
  }
}

interface PreferencesValue {
  prefs: Preferences;
  locale: Locale;
  scheme: ColorScheme;
  setLocale: (locale: Locale) => void;
  setDigits: (digits: Digits) => void;
  setTheme: (theme: ThemeMode) => void;
  /** True on a phone whose layout direction waits on a restart. */
  restartPending: boolean;
  restart: () => void;
}

const PreferencesContext = createContext<PreferencesValue | null>(null);

export function PreferencesProvider({ children }: { children: React.ReactNode }) {
  const { db } = useApp();
  const system = useColorScheme();

  // The web can read its copy synchronously, before the database opens.
  const [prefs, setPrefs] = useState<Preferences>(() => parse(webStorage()?.getItem(WEB_PREFS_KEY) ?? null) ?? DEFAULTS);

  useEffect(() => {
    if (!db) return;
    let cancelled = false;
    void getMeta(db, PREFS_KEY).then((raw) => {
      const stored = parse(raw);
      if (!cancelled && stored) setPrefs(stored);
    });
    return () => { cancelled = true; };
  }, [db]);

  const update = useCallback((patch: Partial<Preferences>) => {
    setPrefs((current) => {
      const next = { ...current, ...patch };
      const raw = JSON.stringify(next);
      webStorage()?.setItem(WEB_PREFS_KEY, raw);
      if (db) void setMeta(db, PREFS_KEY, raw);
      return next;
    });
  }, [db]);

  const locale = prefs.locale ?? deviceLocale();
  const rtl = isRTL(locale);
  const scheme: ColorScheme = prefs.theme === 'system' ? (system === 'light' ? 'light' : 'dark') : prefs.theme;

  // The web applies direction in place, on the document the page already has.
  useEffect(() => {
    if (Platform.OS !== 'web' || typeof document === 'undefined') return;
    document.documentElement.dir = rtl ? 'rtl' : 'ltr';
    document.documentElement.lang = locale;
    document.documentElement.style.colorScheme = scheme;
  }, [rtl, locale, scheme]);

  const restartPending = Platform.OS !== 'web' && I18nManager.isRTL !== rtl;

  useEffect(() => {
    if (Platform.OS === 'web') return;
    I18nManager.allowRTL(true);
    if (I18nManager.isRTL !== rtl) I18nManager.forceRTL(rtl);
  }, [rtl]);

  const restart = useCallback(() => {
    // expo-updates can reload a release build; in development the dev menu's
    // reload does the same job. Required lazily so the web bundle, which
    // never restarts, does not carry it.
    try {
      // eslint-disable-next-line @typescript-eslint/no-var-requires
      const Updates = require('expo-updates') as typeof import('expo-updates');
      void Updates.reloadAsync().catch(() => DevSettings.reload());
    } catch {
      DevSettings.reload();
    }
  }, []);

  const value = useMemo<PreferencesValue>(() => ({
    prefs,
    locale,
    scheme,
    setLocale: (next) => update({ locale: next }),
    setDigits: (digits) => update({ digits }),
    setTheme: (theme) => update({ theme }),
    restartPending,
    restart,
  }), [prefs, locale, scheme, update, restartPending, restart]);

  // On a phone the layout direction is whatever the running bundle started
  // with until the restart, so the theme follows that rather than promising a
  // flip it cannot deliver.
  const layoutRTL = Platform.OS === 'web' ? rtl : I18nManager.isRTL;

  return (
    <PreferencesContext.Provider value={value}>
      <I18nProvider value={i18nFor(locale, prefs.digits)}>
        <ThemeProvider theme={themeFor(scheme, layoutRTL)}>{children}</ThemeProvider>
      </I18nProvider>
    </PreferencesContext.Provider>
  );
}

export function usePreferences(): PreferencesValue {
  const value = useContext(PreferencesContext);
  if (!value) throw new Error('usePreferences outside PreferencesProvider');
  return value;
}
