/**
 * The sync indicator.
 *
 * Present on every screen because the trainer's real question is never "is it
 * syncing" — it is "did what I just did survive". Three states answer that:
 * everything sent, N waiting, or N refused and needing a look.
 *
 * Offline is shown as a fact, not an error. This app is built to be used in a
 * basement.
 */

import React from 'react';
import { Pressable, Text, View } from 'react-native';
import { useRouter } from 'expo-router';

import { useT, type I18n } from '@/i18n';
import { DEMO, useApp, type SyncState } from '@/state/app';
import type { Palette } from './theme';
import { radius, space } from './theme';
import { makeStyles, useTheme } from './theming';

export function SyncBadge() {
  const { sync } = useApp();
  const router = useRouter();
  const i18n = useT();
  const { colors } = useTheme();
  const styles = useStyles();

  const { label, tone } = DEMO
    ? {
        label: sync.pending > 0 ? i18n.t('sync.demoQueued', { count: sync.pending }) : i18n.t('sync.demoNoServer'),
        tone: colors.warning,
      }
    : describe(sync, i18n, colors);

  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={i18n.t('sync.status', { label })}
      onPress={() => router.push('/sync')}
      style={({ pressed }) => [styles.badge, pressed && { opacity: 0.7 }]}
    >
      <View style={[styles.dot, { backgroundColor: tone }]} />
      <Text style={styles.label}>{label}</Text>
    </Pressable>
  );
}

function describe(sync: SyncState, { t }: I18n, colors: Palette): { label: string; tone: string } {
  // Before anything else: nothing will send until the plan is renewed, and
  // "3 waiting" would read as a network problem.
  if (sync.inactive) return { label: t('sync.readOnly'), tone: colors.danger };
  if (sync.failed > 0) return { label: t('sync.needsAttention', { count: sync.failed }), tone: colors.danger };
  if (sync.running) return { label: t('sync.syncing'), tone: colors.accentInk };
  if (sync.offline) {
    return sync.pending > 0
      ? { label: t('sync.offlineWaiting', { count: sync.pending }), tone: colors.warning }
      : { label: t('sync.offline'), tone: colors.inkMuted };
  }
  if (sync.error) return { label: t('sync.error'), tone: colors.danger };
  if (sync.pending > 0) return { label: t('sync.waiting', { count: sync.pending }), tone: colors.warning };
  return { label: t('sync.allSynced'), tone: colors.success };
}

const useStyles = makeStyles(({ colors, type }) => ({
  badge: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    paddingHorizontal: space.md,
    paddingVertical: space.sm,
    borderRadius: radius.pill,
    backgroundColor: colors.surfaceRaised,
  },
  dot: { width: 7, height: 7, borderRadius: radius.pill },
  label: { ...type.caption, color: colors.inkMuted },
}));
