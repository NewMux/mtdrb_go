/**
 * The create sheet, behind the + in every header.
 *
 * Six things a trainer starts from nowhere. Big tiles rather than a menu,
 * because this sheet is opened one-handed between clients.
 */

import React from 'react';
import { Pressable, View } from 'react-native';
import { useRouter } from 'expo-router';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { emptyProgram, saveProgram } from '@/features/programs';
import { useApp } from '@/state/app';
import { HStack, IconButton, IconTile, T, tap } from '@/ui/components';
import type { IconName } from '@/ui/icon';
import { makeStyles, radius, space, type Hue } from '@/ui/theme';

interface Action { key: string; label: string; detail: string; icon: IconName; hue: Hue }

const ACTIONS: Action[] = [
  { key: 'book', label: 'Book Session', detail: 'One-off or weekly', icon: 'calendar', hue: 'tint' },
  { key: 'client', label: 'New Client', detail: 'Works offline', icon: 'personAdd', hue: 'green' },
  { key: 'workout', label: 'Start Workout', detail: 'Log sets live', icon: 'play', hue: 'orange' },
  { key: 'sell', label: 'Sell Package', detail: 'Invoice + link', icon: 'cart', hue: 'purple' },
  { key: 'payment', label: 'Record Payment', detail: 'Cash, bank, wallet', icon: 'money', hue: 'teal' },
  { key: 'program', label: 'New Program', detail: 'Blank or template', icon: 'programs', hue: 'indigo' },
];

export default function QuickScreen() {
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const { db, touch } = useApp();
  const s = useStyles();

  const go = async (key: string) => {
    tap();
    router.back();
    switch (key) {
      case 'book': router.push('/session/new'); break;
      case 'client': router.push('/client/new'); break;
      case 'workout': router.push({ pathname: '/client/pick', params: { next: 'workout' } }); break;
      case 'sell': router.push({ pathname: '/client/pick', params: { next: 'sell' } }); break;
      case 'payment': router.push('/business'); break;
      case 'program': {
        if (!db) return;
        const doc = emptyProgram();
        await saveProgram(db, doc);
        touch();
        router.push({ pathname: '/program/[id]', params: { id: doc.id, fresh: '1' } });
        break;
      }
    }
  };

  return (
    <View style={[s.root, { paddingBottom: insets.bottom + space.lg }]}>
      <HStack justify="space-between" style={s.header}>
        <T variant="title2">Create</T>
        <IconButton icon="close" label="Close" variant="gray" size={32} onPress={() => router.back()} />
      </HStack>
      <View style={s.grid}>
        {ACTIONS.map((a) => (
          <Pressable
            key={a.key}
            accessibilityRole="button"
            accessibilityLabel={a.label}
            onPress={() => { void go(a.key); }}
            style={({ pressed }) => [s.tile, pressed && { opacity: 0.7, transform: [{ scale: 0.97 }] }]}
          >
            <IconTile icon={a.icon} hue={a.hue} size={40} />
            <View>
              <T variant="headline">{a.label}</T>
              <T variant="footnote" tone="label2">{a.detail}</T>
            </View>
          </Pressable>
        ))}
      </View>
    </View>
  );
}

const useStyles = makeStyles((c) => ({
  root: { flex: 1, backgroundColor: c.bg, paddingHorizontal: space.lg },
  header: { paddingTop: space.xl, paddingBottom: space.lg, paddingHorizontal: space.xs },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: space.md },
  tile: {
    flexBasis: '47%', flexGrow: 1, backgroundColor: c.surface, borderRadius: radius.lg,
    padding: space.lg, gap: space.md, minHeight: 124, justifyContent: 'space-between',
  },
}));
