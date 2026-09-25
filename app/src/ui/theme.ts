/**
 * Design tokens.
 *
 * Acid lime on charcoal is the product's face, and dark is the default: it is
 * what a trainer holds on a gym floor with bad light and no patience. The
 * accent is loud on purpose and used sparingly — one thing per screen is lime,
 * and it is always the thing the trainer came to the screen to do.
 *
 * Light exists for the desk: a trainer building a programme or checking a VAT
 * return at a laptop in daylight. It keeps lime as a *fill* — a lime button
 * with dark ink reads on white — but lime as *text* on white is illegible, so
 * accent text becomes a deep olive of the same hue. That is why the palettes
 * carry `accent` and `accentInk` separately rather than one colour.
 *
 * The neutrals are warm-neutral rather than blue in both modes: against a
 * yellow-green accent a blue-tinted grey reads as two competing hues instead
 * of a ground and a mark on it.
 */

import { Platform } from 'react-native';

export type ColorScheme = 'light' | 'dark';

export interface Palette {
  bg: string;
  surface: string;
  surfaceRaised: string;
  /** The sidebar on wide screens: a step off the page so the nav reads as chrome. */
  chrome: string;
  border: string;
  ink: string;
  inkMuted: string;
  /** Text sitting on the accent fill — the accent is far too bright for white. */
  onAccent: string;
  /** The lime fill. */
  accent: string;
  /** The accent used as text or an outline, where the fill colour would not read. */
  accentInk: string;
  /** The accent dimmed for large fills, where full strength is glare. */
  accentSoft: string;
  success: string;
  successSoft: string;
  warning: string;
  warningSoft: string;
  danger: string;
  dangerSoft: string;
  /** Behind a sheet or drawer. */
  scrim: string;
  /** Chart chrome: gridlines recede, the baseline anchors. */
  grid: string;
  baseline: string;
  /**
   * Categorical series, in fixed order — assigned by entity, never cycled or
   * re-ranked. The reference data-viz palette, validated as a set against
   * this app's own surfaces in both modes (adjacent CVD ΔE ≥ 8.4, normal-vision
   * ΔE ≥ 19.3). Three light steps sit under 3:1 on white, so charts always
   * carry a legend and labels rather than relying on colour.
   */
  series: readonly string[];
  /**
   * One hue, low to high, for magnitude — a peak-hours heatmap. Dark steps
   * run from deep to bright so a quiet hour recedes into the dark surface
   * rather than glowing.
   */
  sequential: readonly string[];
}

const dark: Palette = {
  bg: '#121212',
  surface: '#1c1c1e',
  surfaceRaised: '#262628',
  chrome: '#161617',
  border: '#323235',
  ink: '#f5f5f4',
  inkMuted: '#a1a1a6',
  onAccent: '#121212',
  accent: '#ccff00',
  accentInk: '#ccff00',
  accentSoft: 'rgba(204, 255, 0, 0.12)',
  success: '#4ade80',
  successSoft: 'rgba(74, 222, 128, 0.14)',
  warning: '#fbbf24',
  warningSoft: 'rgba(251, 191, 36, 0.14)',
  danger: '#fb7185',
  dangerSoft: 'rgba(251, 113, 133, 0.14)',
  scrim: 'rgba(0, 0, 0, 0.6)',
  grid: '#2c2c2a',
  baseline: '#3f3f3c',
  series: ['#3987e5', '#d95926', '#199e70', '#c98500', '#d55181', '#008300', '#9085e9', '#e66767'],
  sequential: ['#0d366b', '#104281', '#1c5cab', '#2a78d6', '#5598e7', '#86b6ef'],
};

const light: Palette = {
  bg: '#f6f6f3',
  surface: '#ffffff',
  surfaceRaised: '#efefea',
  chrome: '#fbfbf9',
  border: '#e2e1db',
  ink: '#161615',
  inkMuted: '#64635e',
  onAccent: '#121212',
  accent: '#ccff00',
  accentInk: '#4d6b00',
  accentSoft: 'rgba(132, 180, 0, 0.14)',
  success: '#15803d',
  successSoft: 'rgba(21, 128, 61, 0.10)',
  warning: '#a15c07',
  warningSoft: 'rgba(217, 119, 6, 0.12)',
  danger: '#be123c',
  dangerSoft: 'rgba(190, 18, 60, 0.09)',
  scrim: 'rgba(22, 22, 21, 0.4)',
  grid: '#ecebe6',
  baseline: '#c3c2b7',
  series: ['#2a78d6', '#eb6834', '#1baf7a', '#eda100', '#e87ba4', '#008300', '#4a3aa7', '#e34948'],
  sequential: ['#cde2fb', '#9ec5f4', '#6da7ec', '#3987e5', '#256abf', '#184f95'],
};

export const palettes: Record<ColorScheme, Palette> = { dark, light };

export const space = { xs: 4, sm: 8, md: 12, lg: 16, xl: 24, xxl: 32 } as const;

/**
 * Generous corners.
 *
 * Cards are strongly rounded and controls are fully pill-shaped, which is what
 * separates a tappable thing from a container at a glance without spending a
 * border on it.
 */
export const radius = { sm: 10, md: 14, lg: 20, xl: 28, pill: 999 } as const;

export const type = {
  display: { fontSize: 40, fontWeight: '800' as const, letterSpacing: -1 },
  title: { fontSize: 26, fontWeight: '700' as const, letterSpacing: -0.5 },
  heading: { fontSize: 17, fontWeight: '700' as const, letterSpacing: -0.2 },
  body: { fontSize: 16, fontWeight: '500' as const },
  small: { fontSize: 14, fontWeight: '500' as const },
  caption: { fontSize: 12, fontWeight: '600' as const },
  /**
   * Uppercase section labels, spaced out so they read as labels not text.
   * Arabic has no case and its letters join, so tracking would break the words
   * apart — the theme drops both for Arabic (see ui/theming.tsx).
   */
  label: { fontSize: 11, fontWeight: '700' as const, letterSpacing: 1.2 },
  /** For loads and reps, read at arm's length between sets. */
  metric: { fontSize: 34, fontWeight: '800' as const, letterSpacing: -1 },
  /** The unit trailing a metric — deliberately quiet beside it. */
  metricUnit: { fontSize: 14, fontWeight: '600' as const },
} as const;

/**
 * The minimum comfortable touch target.
 *
 * Above the platform minimums on purpose: the person tapping this is often
 * mid-set, one-handed, and should not have to aim.
 */
export const TOUCH_TARGET = 56;

/** The floating action button above the tab bar. */
export const FAB_SIZE = 58;

/** At or above this width the app lays out for a desk: sidebar, tables, two panes. */
export const WIDE_BREAKPOINT = 1024;

export const mono = Platform.select({
  ios: 'Menlo',
  android: 'monospace',
  default: 'ui-monospace, monospace',
});
