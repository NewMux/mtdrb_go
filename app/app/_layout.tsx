/**
 * The root layout.
 *
 * Holds the providers everything else reads from — the local database and
 * account, then the device's language and appearance, then the overlays a
 * screen can raise — and the one piece of routing logic that is not a screen:
 * whether the trainer is signed in.
 */

import React, { useEffect } from 'react';
import { ActivityIndicator, Text, View } from 'react-native';
import { Stack, useRouter, useSegments } from 'expo-router';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { StatusBar } from 'expo-status-bar';

import { isHeldByAnotherTab } from '@/db/errors';
import { useT } from '@/i18n';
import { practiceSettings } from '@/features/plan';
import { AppProvider, useApp, useQuery } from '@/state/app';
import { PreferencesProvider } from '@/state/preferences';
import { OverlayProvider } from '@/ui/overlay';
import { space } from '@/ui/theme';
import { makeStyles, useTheme } from '@/ui/theming';

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
  // The practice's mirrored row: null until the first sync brings it, which
  // is why a missing row sends nobody anywhere.
  const practice = useQuery(practiceSettings).data;
  const needsSetup = account?.role === 'owner' && practice != null && !practice.onboarded_at;

  useEffect(() => {
    if (!ready) return;
    const onSignIn = segments[0] === 'sign-in';
    // A reset link is opened by someone who cannot sign in; it is the one
    // other screen reachable without an account.
    const onReset = segments[0] === 'reset-password';
    const onSetup = segments[0] === 'onboarding';
    if (!account && !onSignIn && !onReset) router.replace('/sign-in');
    else if (account && onSignIn) router.replace(needsSetup ? '/onboarding' : '/');
    // A new practice is set up before anything else; one that is set up has
    // no business on the wizard.
    else if (needsSetup && !onSetup) router.replace('/onboarding');
    else if (account && onSetup && practice?.onboarded_at) router.replace('/');
  }, [ready, account, segments, router, needsSetup, practice?.onboarded_at]);

  return null;
}

function Shell() {
  const { ready, fatal } = useApp();
  const { t } = useT();
  const theme = useTheme();
  const styles = useStyles();
  const { colors } = theme;

  // No local database means no app: every screen reads from it. Saying so
  // beats a spinner that never stops.
  if (fatal && isHeldByAnotherTab(fatal)) {
    return (
      <View style={styles.centre}>
        <Text style={styles.fatalTitle}>{t('shell.otherTabTitle')}</Text>
        <Text style={styles.fatalBody}>{t('shell.otherTabBody')}</Text>
      </View>
    );
  }

  if (fatal) {
    return (
      <View style={styles.centre}>
        <Text style={styles.fatalTitle}>{t('shell.storageTitle')}</Text>
        <Text style={styles.fatalBody}>
          {t('shell.storageBody')}
          {'\n\n'}
          {fatal}
        </Text>
      </View>
    );
  }

  if (!ready) {
    return (
      <View style={styles.centre}>
        <ActivityIndicator color={colors.accentInk} size="large" />
      </View>
    );
  }

  return (
    <>
      <StatusBar style={theme.scheme === 'dark' ? 'light' : 'dark'} />
      <AuthGate />
      <Stack
        screenOptions={{
          headerStyle: { backgroundColor: colors.surface },
          headerTintColor: colors.ink,
          headerTitleStyle: { fontWeight: '600' },
          contentStyle: { backgroundColor: colors.bg },
        }}
      >
        <Stack.Screen name="index" options={{ headerShown: false }} />
        <Stack.Screen name="dashboard" options={{ headerShown: false }} />
        <Stack.Screen name="sign-in" options={{ headerShown: false }} />
        <Stack.Screen name="reset-password" options={{ headerShown: false }} />
        <Stack.Screen name="onboarding" options={{ headerShown: false }} />
        <Stack.Screen name="+not-found" options={{ headerShown: false }} />
        <Stack.Screen name="workout/[id]" options={{ title: t('workout.fallbackTitle') }} />
        <Stack.Screen name="sell-package" options={{ title: t('sellPackage.title'), presentation: 'modal' }} />
        <Stack.Screen name="sync" options={{ title: t('syncScreen.title'), presentation: 'modal' }} />
      </Stack>
    </>
  );
}

export default function RootLayout() {
  return (
    <SafeAreaProvider>
      <AppProvider>
        <PreferencesProvider>
          <OverlayProvider>
            <Shell />
          </OverlayProvider>
        </PreferencesProvider>
      </AppProvider>
    </SafeAreaProvider>
  );
}

const useStyles = makeStyles(({ colors, type }) => ({
  centre: {
    flex: 1,
    backgroundColor: colors.bg,
    alignItems: 'center',
    justifyContent: 'center',
    padding: space.xl,
  },
  fatalTitle: { ...type.title, color: colors.ink, marginBottom: space.md },
  fatalBody: { ...type.body, color: colors.inkMuted, textAlign: 'center' },
}));
