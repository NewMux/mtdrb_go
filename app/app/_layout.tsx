/**
 * The root layout.
 *
 * Holds the provider everything else reads from, the theme, and the one piece
 * of routing logic that is not a screen: whether the trainer is signed in.
 *
 * Pushed screens use the native stack header, which on iOS 26+ is the system
 * glass bar with its own back button. Forms open as sheets — the iOS idiom
 * for "do this, then come back".
 */

import React, { useEffect } from 'react';
import { ActivityIndicator, Platform, View } from 'react-native';
import { Stack, useRouter, useSegments } from 'expo-router';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { StatusBar } from 'expo-status-bar';

import { AppProvider, useApp } from '@/state/app';
import { EmptyState, IconButton } from '@/ui/components';
import { ThemeProvider, useTheme } from '@/ui/theme';

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

/**
 * The header buttons iOS draws itself: a glass circle holding a chevron to go
 * back, or an X to dismiss a sheet. On iOS the system supplies both, so these
 * exist only where the platform would otherwise show a bare arrow.
 */
function GlassBack({ icon, label }: { icon: 'chevronLeft' | 'close'; label: string }) {
  const router = useRouter();
  return (
    <View style={{ marginHorizontal: 8 }}>
      <IconButton icon={icon} label={label} size={36} onPress={() => router.back()} />
    </View>
  );
}
const backButton = Platform.OS === 'ios'
  ? {}
  : { headerLeft: ({ canGoBack }: { canGoBack?: boolean }) => (canGoBack ? <GlassBack icon="chevronLeft" label="Back" /> : null) };

/** A sheet: grabber, rounded top, the content behind dimmed and pushed back. */
const sheet = {
  presentation: Platform.OS === 'ios' ? 'formSheet' : 'modal',
  sheetGrabberVisible: true,
  sheetCornerRadius: 28,
  ...(Platform.OS === 'ios' ? {} : { headerLeft: () => <GlassBack icon="close" label="Close" /> }),
} as const;

function Shell() {
  const { ready, fatal } = useApp();
  const { c, scheme } = useTheme();

  // No local database means no app: every screen reads from it. Saying so
  // beats a spinner that never stops.
  if (fatal) {
    return (
      <View style={{ flex: 1, backgroundColor: c.bg, justifyContent: 'center' }}>
        <EmptyState
          icon="warning"
          hue="red"
          title="Can't open storage"
          message={`CoachPulse keeps everything on the device, and this browser will not let it.\n\n${fatal}`}
        />
      </View>
    );
  }

  if (!ready) {
    return (
      <View style={{ flex: 1, backgroundColor: c.bg, alignItems: 'center', justifyContent: 'center' }}>
        <ActivityIndicator color={c.label2} size="large" />
      </View>
    );
  }

  return (
    <>
      <StatusBar style={scheme === 'dark' ? 'light' : 'dark'} />
      <AuthGate />
      <Stack
        screenOptions={{
          headerStyle: { backgroundColor: c.bg },
          headerTintColor: c.tint,
          headerTitleStyle: { color: c.label, fontWeight: '600' },
          headerShadowVisible: false,
          headerBackButtonDisplayMode: 'minimal',
          headerTitleAlign: 'center',
          ...backButton,
          headerTransparent: Platform.OS === 'ios',
          headerBlurEffect: Platform.OS === 'ios' ? (scheme === 'dark' ? 'systemChromeMaterialDark' : 'systemChromeMaterialLight') : undefined,
          contentStyle: { backgroundColor: c.bg },
        }}
      >
        <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
        <Stack.Screen name="sign-in" options={{ headerShown: false }} />
        <Stack.Screen name="client/[id]" options={{ title: '' }} />
        <Stack.Screen name="client/new" options={{ title: 'New Client', ...sheet }} />
        <Stack.Screen name="client/pick" options={{ title: 'Choose Client', ...sheet }} />
        <Stack.Screen name="workout/[id]" options={{ title: 'Workout' }} />
        <Stack.Screen name="workout/summary" options={{ title: 'Summary', ...sheet, headerShown: false }} />
        <Stack.Screen name="sell-package" options={{ title: 'Sell a Package', ...sheet }} />
        <Stack.Screen name="payment" options={{ title: 'Record Payment', ...sheet }} />
        <Stack.Screen name="session/new" options={{ title: 'Book Session', ...sheet }} />
        <Stack.Screen name="session/[id]" options={{ title: 'Session', ...sheet }} />
        <Stack.Screen name="program/[id]" options={{ title: '' }} />
        <Stack.Screen name="program/day" options={{ title: 'Training Day' }} />
        <Stack.Screen name="program/assign" options={{ title: 'Assign Program', ...sheet }} />
        <Stack.Screen name="exercises" options={{ title: 'Exercises', ...sheet }} />
        <Stack.Screen name="quick" options={{
          title: 'Create', ...sheet, headerShown: false,
          ...(Platform.OS === 'ios' ? { sheetAllowedDetents: [0.52, 0.9] } : {}),
        }} />
        <Stack.Screen name="settings" options={{
          title: 'Settings',
          ...(Platform.OS === 'ios' ? { headerLargeTitle: true, headerLargeTitleShadowVisible: false, headerLargeStyle: { backgroundColor: c.bg } } : {}),
        }} />
        <Stack.Screen name="sync" options={{ title: 'Sync', ...sheet }} />
      </Stack>
    </>
  );
}

function Themed({ children }: { children: React.ReactNode }) {
  const { prefs } = useApp();
  return <ThemeProvider preference={prefs.theme}>{children}</ThemeProvider>;
}

export default function RootLayout() {
  return (
    <SafeAreaProvider>
      <AppProvider>
        <Themed>
          <Shell />
        </Themed>
      </AppProvider>
    </SafeAreaProvider>
  );
}
