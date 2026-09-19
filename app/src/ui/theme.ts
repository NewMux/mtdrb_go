/**
 * Design tokens.
 *
 * Acid lime on charcoal. The accent is loud on purpose and used sparingly:
 * one thing per screen is lime, and it is always the thing the trainer came to
 * the screen to do. Everything else is greyscale, so the eye lands in the
 * right place in a gym with bad light and no patience.
 *
 * The neutrals are warm-neutral rather than blue: against a yellow-green
 * accent a blue-tinted grey reads as two competing hues instead of a ground
 * and a mark on it.
 */

import { Platform } from 'react-native';

export const colors = {
  /** Near-black, a hair warm so the lime does not vibrate against it. */
  bg: '#121212',
  surface: '#1c1c1e',
  surfaceRaised: '#262628',
  border: '#323235',

  ink: '#f5f5f4',
  inkMuted: '#a1a1a6',
  /** For text sitting on the accent — the accent is far too bright for white. */
  onAccent: '#121212',

  accent: '#ccff00',
  /** The accent dimmed for large fills, where full strength is glare. */
  accentSoft: 'rgba(204, 255, 0, 0.12)',

  success: '#4ade80',
  warning: '#fbbf24',
  danger: '#fb7185',
} as const;

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
  /** Uppercase section labels, spaced out so they read as labels not text. */
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

/** The floating action button that straddles the tab bar. */
export const FAB_SIZE = 58;

export const mono = Platform.select({
  ios: 'Menlo',
  android: 'monospace',
  default: 'ui-monospace, monospace',
});
