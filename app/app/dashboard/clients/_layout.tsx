/**
 * Clients's own stack: its list, and the screens that open from it, push
 * inside the tab so the tab bar and the sidebar stay put.
 */

import React from 'react';
import { Stack } from 'expo-router';

import { useT } from '@/i18n';
import { useTheme } from '@/ui/theming';

export default function ClientsLayout() {
  const { colors } = useTheme();
  const { t } = useT();
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
      <Stack.Screen name="[id]" options={{ title: t('client.title') }} />
      <Stack.Screen name="new" options={{ title: t('newClient.title'), presentation: 'modal' }} />
    </Stack>
  );
}
