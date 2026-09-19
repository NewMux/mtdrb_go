/**
 * The root layout.
 *
 * Holds the provider everything else reads from, and the one piece of routing
 * logic that is not a screen: whether the trainer is signed in.
 */

import React, { useEffect } from 'react';
import { ActivityIndicator, StyleSheet, Text, View } from 'react-native';
import { Stack, useRouter, useSegments } from 'expo-router';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { StatusBar } from 'expo-status-bar';

import { AppProvider, useApp } from '@/state/app';
import { colors, space, type as typography } from '@/ui/theme';

/**
 * Sends the trainer to the right place.
 *
 * Deliberately waits for `ready`: redirecting before the stored account is
 * restored would flash a login screen at someone who is signed in, every
 * single launch.
 */
function AuthGate() {
  const { ready, account } = useApp();
  const segments = useSegments();
  const router = useRouter();

  useEffect(() => {
    if (!ready) return;
    const onSignIn = segments[0] === 'sign-in';
    if (!account && !onSignIn) router.replace('/sign-in');
    else if (account && onSignIn) router.replace('/');
  }, [ready, account, segments, router]);

  return null;
}

function Shell() {
  const { ready, fatal } = useApp();

  // No local database means no app: every screen reads from it. Saying so
  // beats a spinner that never stops.
  if (fatal) {
    return (
      <View style={styles.centre}>
        <Text style={styles.fatalTitle}>Can&apos;t open storage</Text>
        <Text style={styles.fatalBody}>
          CoachPulse keeps everything on the device, and this browser will not let it.
          {'\n\n'}
          {fatal}
        </Text>
      </View>
    );
  }

  if (!ready) {
    return (
      <View style={styles.centre}>
        <ActivityIndicator color={colors.accent} size="large" />
      </View>
    );
  }

  return (
    <>
      <AuthGate />
      <Stack
        screenOptions={{
          headerStyle: { backgroundColor: colors.surface },
          headerTintColor: colors.ink,
          headerTitleStyle: { fontWeight: '600' },
          contentStyle: { backgroundColor: colors.bg },
        }}
      >
        <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
        <Stack.Screen name="sign-in" options={{ headerShown: false }} />
        <Stack.Screen name="client/[id]" options={{ title: 'Client' }} />
        <Stack.Screen name="client/new" options={{ title: 'New client', presentation: 'modal' }} />
        <Stack.Screen name="workout/[id]" options={{ title: 'Workout' }} />
        <Stack.Screen name="sell-package" options={{ title: 'Sell a package', presentation: 'modal' }} />
        <Stack.Screen name="sync" options={{ title: 'Sync', presentation: 'modal' }} />
      </Stack>
    </>
  );
}

export default function RootLayout() {
  return (
    <SafeAreaProvider>
      <StatusBar style="light" />
      <AppProvider>
        <Shell />
      </AppProvider>
    </SafeAreaProvider>
  );
}

const styles = StyleSheet.create({
  centre: {
    flex: 1,
    backgroundColor: colors.bg,
    alignItems: 'center',
    justifyContent: 'center',
    padding: space.xl,
  },
  fatalTitle: { ...typography.title, color: colors.ink, marginBottom: space.md },
  fatalBody: { ...typography.body, color: colors.inkMuted, textAlign: 'center' },
});
