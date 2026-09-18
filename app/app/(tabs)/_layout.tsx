/**
 * The three places a trainer goes.
 *
 * Today is first because it is the answer to the only question asked while
 * standing on a gym floor: who is in front of me, and what happens next.
 */

import React from 'react';
import { Tabs } from 'expo-router';
import { colors, space } from '@/ui/theme';

export default function TabsLayout() {
  return (
    <Tabs
      screenOptions={{
        headerStyle: { backgroundColor: colors.surface },
        headerTintColor: colors.ink,
        headerTitleStyle: { fontWeight: '600' },
        sceneStyle: { backgroundColor: colors.bg },
        tabBarStyle: {
          backgroundColor: colors.surface,
          borderTopColor: colors.border,
          height: 64,
          paddingBottom: space.sm,
          paddingTop: space.sm,
        },
        tabBarActiveTintColor: colors.accent,
        tabBarInactiveTintColor: colors.inkMuted,
        tabBarLabelStyle: { fontSize: 13, fontWeight: '600' },
        // No icon set is bundled, and a label a trainer can read in a dim gym
        // beats a glyph they have to decode.
        tabBarIconStyle: { display: 'none' },
      }}
    >
      <Tabs.Screen name="index" options={{ title: 'Today' }} />
      <Tabs.Screen name="clients" options={{ title: 'Clients' }} />
      <Tabs.Screen name="money" options={{ title: 'Money' }} />
    </Tabs>
  );
}
