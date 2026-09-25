/**
 * Billing's own stack: its list, and the screens that open from it, push
 * inside the tab so the tab bar and the sidebar stay put.
 */

import React from 'react';
import { Stack } from 'expo-router';

import { useTheme } from '@/ui/theming';

export default function BillingLayout() {
  const { colors } = useTheme();
  return (
    <Stack
      screenOptions={{
        headerStyle: { backgroundColor: colors.surface },
        headerTintColor: colors.ink,
        headerTitleStyle: { fontWeight: '600' },
        contentStyle: { backgroundColor: colors.bg },
      }}
    >
      <Stack.Screen name="index" options={{ headerShown: false }} />
    </Stack>
  );
}
