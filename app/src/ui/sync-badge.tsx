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
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { useRouter } from 'expo-router';

import { DEMO, useApp } from '@/state/app';
import { colors, radius, space, type as typography } from './theme';

export function SyncBadge() {
  const { sync } = useApp();
  const router = useRouter();

  const { label, tone } = DEMO
    ? { label: sync.pending > 0 ? `Demo · ${sync.pending} queued` : 'Demo · no server', tone: colors.warning }
    : describe(sync);

  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`Sync status: ${label}`}
      onPress={() => router.push('/sync')}
      style={({ pressed }) => [styles.badge, pressed && { opacity: 0.7 }]}
    >
      <View style={[styles.dot, { backgroundColor: tone }]} />
      <Text style={styles.label}>{label}</Text>
    </Pressable>
  );
}

function describe(sync: {
  running: boolean; offline: boolean; pending: number; failed: number; error: string | null;
}): { label: string; tone: string } {
  if (sync.failed > 0) {
    return { label: `${sync.failed} need${sync.failed === 1 ? 's' : ''} attention`, tone: colors.danger };
  }
  if (sync.running) return { label: 'Syncing', tone: colors.accent };
  if (sync.offline) {
    return sync.pending > 0
      ? { label: `Offline · ${sync.pending} waiting`, tone: colors.warning }
      : { label: 'Offline', tone: colors.inkMuted };
  }
  if (sync.error) return { label: 'Sync error', tone: colors.danger };
  if (sync.pending > 0) return { label: `${sync.pending} waiting`, tone: colors.warning };
  return { label: 'All synced', tone: colors.success };
}

const styles = StyleSheet.create({
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
  label: { ...typography.caption, color: colors.inkMuted },
});
