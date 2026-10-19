/**
 * Design tokens — "Studio", in the iOS 27 idiom.
 *
 * Colours are Apple's semantic system palette, light and dark, so the app sits
 * in the platform rather than on top of it: grouped backgrounds, layered
 * fills, labels at four strengths. One tint carries every interactive thing;
 * green, orange and red mean status and nothing else.
 *
 * Every screen reads colours through `useTheme()` rather than importing a
 * constant, because the palette changes with the system appearance (or the
 * trainer's override in Settings) while the app is running.
 */

import React, { createContext, useContext, useMemo } from 'react';
import { Platform, StyleSheet, useColorScheme } from 'react-native';

export type Scheme = 'light' | 'dark';
export type ThemePreference = 'system' | Scheme;

const light = {
  scheme: 'light' as Scheme,
  /** The page behind grouped content — iOS systemGroupedBackground. */
  bg: '#F2F2F7',
  /** Cards and list groups sitting on `bg`. */
  surface: '#FFFFFF',
  /** A surface inside a surface: an input inside a card. */
  surface2: '#F2F2F7',
  /** Glass fallback where the platform has no material of its own. */
  glass: 'rgba(250,250,252,0.82)',
  glassBorder: 'rgba(255,255,255,0.6)',

  label: '#000000',
  label2: 'rgba(60,60,67,0.6)',
  label3: 'rgba(60,60,67,0.3)',
  separator: 'rgba(60,60,67,0.18)',

  fill: 'rgba(120,120,128,0.2)',
  fill2: 'rgba(120,120,128,0.12)',

  tint: '#007AFF',
  onTint: '#FFFFFF',

  green: '#34C759',
  orange: '#FF9500',
  red: '#FF3B30',
  yellow: '#FFCC00',
  indigo: '#5856D6',
  purple: '#AF52DE',
  pink: '#FF2D55',
  teal: '#30B0C7',
  mint: '#00C7BE',
  cyan: '#32ADE6',
  gray: '#8E8E93',

  shadow: 'rgba(0,0,0,0.08)',
};

export type Palette = typeof light;

const dark: Palette = {
  scheme: 'dark',
  bg: '#000000',
  surface: '#1C1C1E',
  surface2: '#2C2C2E',
  glass: 'rgba(36,36,38,0.78)',
  glassBorder: 'rgba(255,255,255,0.10)',

  label: '#FFFFFF',
  label2: 'rgba(235,235,245,0.6)',
  label3: 'rgba(235,235,245,0.3)',
  separator: 'rgba(84,84,88,0.55)',

  fill: 'rgba(120,120,128,0.36)',
  fill2: 'rgba(118,118,128,0.24)',

  tint: '#0A84FF',
  onTint: '#FFFFFF',

  green: '#30D158',
  orange: '#FF9F0A',
  red: '#FF453A',
  yellow: '#FFD60A',
  indigo: '#5E5CE6',
  purple: '#BF5AF2',
  pink: '#FF375F',
  teal: '#40C8E0',
  mint: '#63E6E2',
  cyan: '#64D2FF',
  gray: '#8E8E93',

  shadow: 'rgba(0,0,0,0.5)',
};

export const palettes = { light, dark } as const;

/** The status and category hues, for icon tiles and chart series. */
export type Hue =
  | 'tint' | 'green' | 'orange' | 'red' | 'yellow' | 'indigo'
  | 'purple' | 'pink' | 'teal' | 'mint' | 'cyan' | 'gray';

/** A hue at low opacity, for tinted backgrounds behind a coloured glyph. */
export function wash(hex: string, alpha = 0.15): string {
  const h = hex.replace('#', '');
  const n = parseInt(h.length === 3 ? h.split('').map((c) => c + c).join('') : h, 16);
  return `rgba(${(n >> 16) & 255},${(n >> 8) & 255},${n & 255},${alpha})`;
}

/** 8-point grid, with a 4 for tight internal gaps. */
export const space = { xs: 4, sm: 8, md: 12, lg: 16, xl: 20, xxl: 32 } as const;

/** Continuous-corner radii in the iOS 26+ proportions: big, soft containers. */
export const radius = { sm: 10, md: 14, lg: 22, xl: 28, pill: 999 } as const;

/**
 * Text styles, named after Apple's Dynamic Type styles.
 *
 * The font is the system font — SF Pro on Apple platforms, which is the whole
 * point — with a web stack that resolves to it on a Mac and to something
 * neutral elsewhere.
 */
const family = Platform.select({
  web: '-apple-system, BlinkMacSystemFont, "SF Pro Text", "Inter", "Segoe UI", Roboto, sans-serif',
  default: undefined,
});
const rounded = Platform.select({
  ios: 'ui-rounded',
  web: 'ui-rounded, -apple-system, "SF Pro Rounded", "Inter", sans-serif',
  default: undefined,
});

export const type = {
  largeTitle: { fontFamily: family, fontSize: 34, lineHeight: 41, fontWeight: '700' as const, letterSpacing: 0.37 },
  title1: { fontFamily: family, fontSize: 28, lineHeight: 34, fontWeight: '700' as const, letterSpacing: 0.36 },
  title2: { fontFamily: family, fontSize: 22, lineHeight: 28, fontWeight: '700' as const, letterSpacing: 0.35 },
  title3: { fontFamily: family, fontSize: 20, lineHeight: 25, fontWeight: '600' as const, letterSpacing: 0.38 },
  headline: { fontFamily: family, fontSize: 17, lineHeight: 22, fontWeight: '600' as const, letterSpacing: -0.43 },
  body: { fontFamily: family, fontSize: 17, lineHeight: 22, fontWeight: '400' as const, letterSpacing: -0.43 },
  callout: { fontFamily: family, fontSize: 16, lineHeight: 21, fontWeight: '400' as const, letterSpacing: -0.31 },
  subhead: { fontFamily: family, fontSize: 15, lineHeight: 20, fontWeight: '400' as const, letterSpacing: -0.23 },
  footnote: { fontFamily: family, fontSize: 13, lineHeight: 18, fontWeight: '400' as const, letterSpacing: -0.08 },
  caption: { fontFamily: family, fontSize: 12, lineHeight: 16, fontWeight: '400' as const, letterSpacing: 0 },
  caption2: { fontFamily: family, fontSize: 11, lineHeight: 13, fontWeight: '500' as const, letterSpacing: 0.06 },
  /** Big figures — loads, balances, counts — in the rounded face with even digits. */
  metric: {
    fontFamily: rounded, fontSize: 34, lineHeight: 40, fontWeight: '700' as const,
    letterSpacing: -0.5, fontVariant: ['tabular-nums'] as ('tabular-nums')[],
  },
  metricSmall: {
    fontFamily: rounded, fontSize: 22, lineHeight: 28, fontWeight: '700' as const,
    letterSpacing: -0.3, fontVariant: ['tabular-nums'] as ('tabular-nums')[],
  },
} as const;

export type TypeStyle = keyof typeof type;

/** The platform minimum, and the larger one for mid-set thumbs in the logger. */
export const TOUCH = 44;
export const TOUCH_FLOOR = 56;

/** Height reserved at the bottom of tab screens for the floating tab bar. */
export const TAB_BAR_CLEARANCE = 110;

export const mono = Platform.select({
  ios: 'Menlo',
  android: 'monospace',
  default: 'ui-monospace, SFMono-Regular, monospace',
});

interface ThemeValue {
  c: Palette;
  scheme: Scheme;
}

const ThemeContext = createContext<ThemeValue>({ c: light, scheme: 'light' });

/**
 * Resolves the palette from the system appearance and the trainer's override.
 *
 * Mounted once, high in the tree; everything below reads it with `useTheme`.
 */
export function ThemeProvider({
  preference = 'system', children,
}: { preference?: ThemePreference; children: React.ReactNode }) {
  const system = useColorScheme();
  const scheme: Scheme = preference === 'system' ? (system === 'dark' ? 'dark' : 'light') : preference;
  const value = useMemo(() => ({ c: palettes[scheme], scheme }), [scheme]);
  return React.createElement(ThemeContext.Provider, { value }, children);
}

export function useTheme(): ThemeValue {
  return useContext(ThemeContext);
}

/**
 * Styles that depend on the palette, built once per scheme.
 *
 * `StyleSheet.create` at module level cannot see the theme, so a screen
 * declares its styles as a function of the palette and this hook memoises the
 * result for the current appearance.
 */
export function makeStyles<T extends StyleSheet.NamedStyles<T>>(
  build: (c: Palette) => T,
): () => T {
  const cache: Partial<Record<Scheme, T>> = {};
  return function useStyles() {
    const { c, scheme } = useTheme();
    if (!cache[scheme]) cache[scheme] = StyleSheet.create(build(c));
    return cache[scheme] as T;
  };
}
