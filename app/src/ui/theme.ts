/**
 * Design tokens.
 *
 * Sized for the actual context: a phone held in one hand, often sweaty, in a
 * gym with variable light. Touch targets are generous, contrast is high, and
 * the numbers a trainer reads mid-set are large.
 */

import { Platform } from 'react-native';

export const colors = {
  bg: '#0f1115',
  surface: '#171a21',
  surfaceRaised: '#1f232c',
  border: '#272c36',
  ink: '#e9ecf1',
  inkMuted: '#9aa3b2',
  accent: '#60a5fa',
  success: '#34d399',
  warning: '#fbbf24',
  danger: '#f87171',
} as const;

export const space = { xs: 4, sm: 8, md: 12, lg: 16, xl: 24, xxl: 32 } as const;

export const radius = { sm: 8, md: 12, lg: 16, pill: 999 } as const;

export const type = {
  display: { fontSize: 34, fontWeight: '700' as const },
  title: { fontSize: 22, fontWeight: '600' as const },
  heading: { fontSize: 17, fontWeight: '600' as const },
  body: { fontSize: 16, fontWeight: '400' as const },
  small: { fontSize: 14, fontWeight: '400' as const },
  caption: { fontSize: 12, fontWeight: '500' as const },
  /** For loads and reps, which are read at arm's length between sets. */
  metric: { fontSize: 28, fontWeight: '700' as const },
} as const;

/**
 * The minimum comfortable touch target.
 *
 * Above the platform minimums on purpose: the person tapping this is often
 * mid-set, one-handed, and should not have to aim.
 */
export const TOUCH_TARGET = 52;

export const mono = Platform.select({
  ios: 'Menlo',
  android: 'monospace',
  default: 'ui-monospace, monospace',
});
