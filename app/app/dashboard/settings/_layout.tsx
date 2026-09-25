/**
 * Settings' own stack: the list of sections, and each section pushed inside
 * the tab so the sidebar and tab bar stay put.
 */

import React from 'react';
import { Stack } from 'expo-router';

import { useT } from '@/i18n';
import { useTheme } from '@/ui/theming';

export default function SettingsLayout() {
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
      <Stack.Screen name="general" options={{ title: t('settings.general') }} />
      <Stack.Screen name="profile" options={{ title: t('settings.profile') }} />
      <Stack.Screen name="security" options={{ title: t('settings.security') }} />
      <Stack.Screen name="business" options={{ title: t('settings.business') }} />
      <Stack.Screen name="subscription" options={{ title: t('settings.subscription') }} />
    </Stack>
  );
}
