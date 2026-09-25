/**
 * The theme a screen renders with, and styles built from it.
 *
 * `makeStyles` replaces the static `StyleSheet.create(... colors.x ...)` every
 * screen used to have. It builds each stylesheet once per theme, so switching
 * light and dark costs one StyleSheet.create per screen, not one per render.
 */

import React, { createContext, useContext } from 'react';
import { Platform, StyleSheet, type TextStyle } from 'react-native';

import { palettes, type as baseType, type ColorScheme, type Palette } from './theme';

export interface Theme {
  scheme: ColorScheme;
  colors: Palette;
  isRTL: boolean;
  /** Type ramp, with the label style adjusted for the script in use. */
  type: Omit<typeof baseType, 'label'> & { label: TextStyle };
}

const themes = new Map<string, Theme>();

/**
 * One object per (scheme, direction), reused, so it can key the stylesheet
 * cache and be compared by identity.
 */
export function themeFor(scheme: ColorScheme, isRTL: boolean): Theme {
  const key = `${scheme}:${isRTL ? 'rtl' : 'ltr'}`;
  const cached = themes.get(key);
  if (cached) return cached;
  // Text aligns to the start edge whatever script it is written in: a Latin
  // client name in an Arabic list belongs on the right. A phone swaps left
  // and right itself under forceRTL; the web does not, so it is told.
  const start: TextStyle = { textAlign: Platform.OS === 'web' && isRTL ? 'right' : 'left' };
  const ramp = Object.fromEntries(
    Object.entries(baseType).map(([name, style]) => [name, { ...style, ...start }]),
  ) as unknown as typeof baseType;
  const theme: Theme = {
    scheme,
    colors: palettes[scheme],
    isRTL,
    type: {
      ...ramp,
      // Arabic letters join and have no case: tracking breaks words apart
      // and uppercasing does nothing, so an Arabic label is just bold.
      label: isRTL
        ? { fontSize: 12, fontWeight: '700', letterSpacing: 0, ...start }
        : { ...baseType.label, textTransform: 'uppercase', ...start },
    },
  };
  themes.set(key, theme);
  return theme;
}

export const ThemeContext = createContext<Theme>(themeFor('dark', false));

export function useTheme(): Theme {
  return useContext(ThemeContext);
}

export function useColors(): Palette {
  return useContext(ThemeContext).colors;
}

// The same constraint StyleSheet.create uses, so an entry's literal values
// ('row', 'absolute') keep their narrow types.
type NamedStyles<T> = StyleSheet.NamedStyles<T> | StyleSheet.NamedStyles<any>; // eslint-disable-line @typescript-eslint/no-explicit-any

export function makeStyles<T extends NamedStyles<T>>(factory: (theme: Theme) => T & NamedStyles<T>): () => T {
  const cache = new WeakMap<Theme, T>();
  return function useStyles(): T {
    const theme = useTheme();
    let styles = cache.get(theme);
    if (!styles) {
      styles = StyleSheet.create(factory(theme));
      cache.set(theme, styles);
    }
    return styles;
  };
}

export function ThemeProvider({ theme, children }: { theme: Theme; children: React.ReactNode }) {
  return <ThemeContext.Provider value={theme}>{children}</ThemeContext.Provider>;
}
