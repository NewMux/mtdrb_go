/**
 * The four places a trainer goes, and the one thing they create.
 *
 * Today is first because it is the answer to the only question asked while
 * standing on a gym floor: who is in front of me, and what happens next.
 *
 * The lime circle is the single create action. A trainer signs someone up
 * standing next to them, often mid-conversation, and that is the one thing
 * worth reaching for without first navigating somewhere.
 *
 * It floats *above* the bar rather than straddling it. The straddle is the
 * nicer shape, but it only works with an even number of tabs — with three
 * destinations the circle lands on top of the middle one and eats its taps.
 * A button that covers a destination is worse than a button sitting slightly
 * higher.
 */

import React from 'react';
import { Pressable, View } from 'react-native';
import { Tabs, useRouter } from 'expo-router';

import { useT } from '@/i18n';
import { Icon, type IconName } from '@/ui/icon';
import { FAB_SIZE, radius, space } from '@/ui/theme';
import { makeStyles, useTheme } from '@/ui/theming';

function AddClientButton() {
  const router = useRouter();
  const styles = useStyles();
  const { colors } = useTheme();
  const { t } = useT();
  return (
    <View pointerEvents="box-none" style={styles.fabWrap}>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={t('nav.addClient')}
        onPress={() => router.push('/client/new')}
        style={({ pressed }) => [styles.fab, pressed && { opacity: 0.8 }]}
      >
        <Icon name="add" size={28} color={colors.onAccent} strokeWidth={2.5} />
      </Pressable>
    </View>
  );
}

const tabIcon = (name: IconName) => ({ color, size }: { color: string; size: number }) => (
  <Icon name={name} color={color} size={size - 4} />
);

export default function TabsLayout() {
  const { colors, type } = useTheme();
  const { t } = useT();
  return (
    <>
      <Tabs
        screenOptions={{
          headerShown: false,
          sceneStyle: { backgroundColor: colors.bg },
          tabBarStyle: {
            backgroundColor: colors.surface,
            borderTopWidth: 0,
            height: 78,
            paddingBottom: space.lg,
            paddingTop: space.sm,
          },
          tabBarActiveTintColor: colors.accentInk,
          tabBarInactiveTintColor: colors.inkMuted,
          tabBarLabelStyle: { ...type.caption },
        }}
      >
        <Tabs.Screen name="index" options={{ title: t('nav.today'), tabBarIcon: tabIcon('today') }} />
        <Tabs.Screen name="dashboard" options={{ title: t('nav.practice'), tabBarIcon: tabIcon('dashboard') }} />
        <Tabs.Screen name="clients" options={{ title: t('nav.clients'), tabBarIcon: tabIcon('clients') }} />
        <Tabs.Screen name="money" options={{ title: t('nav.money'), tabBarIcon: tabIcon('money') }} />
      </Tabs>
      <AddClientButton />
    </>
  );
}

const useStyles = makeStyles(({ colors }) => ({
  // Anchored to the bar rather than placed in it: a tab bar item cannot break
  // out of its own bounds, and the overhang is the whole point of the shape.
  fabWrap: {
    position: 'absolute',
    left: 0,
    right: 0,
    // Clear of the 78pt bar, so every tab keeps its full target.
    bottom: 94,
    alignItems: 'center',
  },
  fab: {
    width: FAB_SIZE,
    height: FAB_SIZE,
    borderRadius: radius.pill,
    backgroundColor: colors.accent,
    alignItems: 'center',
    justifyContent: 'center',
    // Lifted off the page rather than outlined against it.
    shadowColor: '#000',
    shadowOpacity: 0.35,
    shadowRadius: 16,
    shadowOffset: { width: 0, height: 6 },
    elevation: 8,
  },
}));
