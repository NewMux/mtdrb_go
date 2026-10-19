/**
 * The sync indicator.
 *
 * Present in every tab header because the trainer's real question is never
 * "is it syncing" — it is "did what I just did survive". Three states answer
 * that: everything sent, N waiting, or N refused and needing a look.
 *
 * Offline is shown as a fact, not an error. This app is built to be used in a
 * basement. When all is well the badge shrinks to a single glyph so it does
 * not compete with the title.
 */

import React from 'react';
import { Pressable, Text, View } from 'react-native';
import { useRouter } from 'expo-router';

import { DEMO, useApp, type SyncState } from '@/state/app';
import { Glass } from './glass';
import { Icon, type IconName } from './icon';
import { tap, toneColor } from './components';
import { type as typography, useTheme, type Hue } from './theme';

export function SyncBadge() {
  const { sync } = useApp();
  const { c } = useTheme();
  const router = useRouter();

  const { label, hue, icon, compact } = DEMO
    ? { label: sync.pending > 0 ? `${sync.pending} queued` : 'Demo', hue: 'orange' as Hue, icon: 'offline' as IconName, compact: false }
    : describe(sync);
  const color = toneColor(c, hue);

  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`Sync status: ${label}`}
      onPress={() => { tap(); router.push('/sync'); }}
      style={({ pressed }) => ({ opacity: pressed ? 0.7 : 1 })}
    >
      <Glass interactive style={{ height: 40, minWidth: 40, borderRadius: 20, paddingHorizontal: compact ? 0 : 12, alignItems: 'center', justifyContent: 'center' }}>
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 6 }}>
          <Icon name={icon} size={16} color={color} />
          {compact ? null : <Text style={[typography.footnote, { color: c.label, fontWeight: '600' }]}>{label}</Text>}
        </View>
      </Glass>
    </Pressable>
  );
}

export function describe(sync: Pick<SyncState, 'running' | 'offline' | 'pending' | 'failed' | 'error'>): {
  label: string; hue: Hue; icon: IconName; compact: boolean;
} {
  if (sync.failed > 0) {
    return { label: `${sync.failed} need${sync.failed === 1 ? 's' : ''} a look`, hue: 'red', icon: 'warning', compact: false };
  }
  if (sync.running) return { label: 'Syncing', hue: 'tint', icon: 'sync', compact: true };
  if (sync.offline) {
    return sync.pending > 0
      ? { label: `${sync.pending} waiting`, hue: 'orange', icon: 'offline', compact: false }
      : { label: 'Offline', hue: 'gray', icon: 'offline', compact: true };
  }
  if (sync.error) return { label: 'Sync error', hue: 'red', icon: 'warning', compact: false };
  if (sync.pending > 0) return { label: `${sync.pending} waiting`, hue: 'orange', icon: 'sync', compact: false };
  return { label: 'All synced', hue: 'green', icon: 'synced', compact: true };
}
