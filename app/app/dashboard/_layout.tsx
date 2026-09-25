/**
 * The shell every module lives in.
 *
 * One navigator for both shapes of the app. On a phone it is a tab bar with
 * the five places a trainer goes on a gym floor; on a desk the bar is hidden
 * and the sidebar beside it carries every module at once. The same screens
 * render in both, so a module built once works in both.
 *
 * The lime circle is the phone's single create action. A trainer signs someone
 * up standing next to them, often mid-conversation, and that is the one thing
 * worth reaching for without first navigating somewhere. It floats *above* the
 * bar rather than straddling it: with five tabs the straddle would land on the
 * middle destination and eat its taps. On a desk, adding a client is a button
 * on the Clients screen, where a trainer at a laptop looks for it.
 */

import React from 'react';
import { type ColorValue, Pressable, View } from 'react-native';
import { Tabs, usePathname, useRouter } from 'expo-router';

import { useT } from '@/i18n';
import { Icon, type IconName } from '@/ui/icon';
import { useLayout } from '@/ui/layout';
import { NAV_MODULES, PHONE_TABS } from '@/ui/nav';
import { Sidebar } from '@/ui/sidebar';
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
        onPress={() => router.push('/dashboard/clients/new')}
        style={({ pressed }) => [styles.fab, pressed && { opacity: 0.8 }]}
      >
        <Icon name="add" size={28} color={colors.onAccent} strokeWidth={2.5} />
      </Pressable>
    </View>
  );
}

// The navigator types the tint as ColorValue since SDK 57, but it only ever
// hands back the plain strings set as tabBarActive/InactiveTintColor below.
const tabIcon = (name: IconName) => ({ color, size }: { color: ColorValue; size: number }) => (
  <Icon name={name} color={color as string} size={size - 4} />
);

/** Screens where the add button would sit over something the trainer needs. */
const NO_FAB = ['/dashboard/clients/new', '/dashboard/more', '/dashboard/settings'];

export default function DashboardLayout() {
  const { colors, type } = useTheme();
  const styles = useStyles();
  const { t } = useT();
  const { wide } = useLayout();
  const pathname = usePathname();

  const phoneTab = (key: string) => PHONE_TABS.includes(key as never);
  // A tab bar draws screens in declaration order, so the phone's four come
  // first, in the order a trainer reaches for them.
  const ordered = [
    ...PHONE_TABS.map((key) => NAV_MODULES.find((m) => m.key === key)!),
    ...NAV_MODULES.filter((m) => !phoneTab(m.key)),
  ];

  const tabs = (
    <Tabs
      // The desk hides the bar entirely; the sidebar does its job.
      tabBar={wide ? () => null : undefined}
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
      {ordered.map((module) => (
        <Tabs.Screen
          key={module.key}
          name={module.screen}
          options={{
            title: t(module.label),
            tabBarIcon: tabIcon(module.icon),
            // Off the phone's bar, but still a destination: reached from More.
            href: phoneTab(module.key) ? undefined : null,
          }}
        />
      ))}
      <Tabs.Screen name="more" options={{ title: t('nav.more'), tabBarIcon: tabIcon('more') }} />
    </Tabs>
  );

  if (wide) {
    return (
      <View style={styles.desk}>
        <Sidebar />
        <View style={{ flex: 1 }}>{tabs}</View>
      </View>
    );
  }

  return (
    <>
      {tabs}
      {NO_FAB.some((p) => pathname.startsWith(p)) ? null : <AddClientButton />}
    </>
  );
}

const useStyles = makeStyles(({ colors }) => ({
  desk: { flex: 1, flexDirection: 'row', backgroundColor: colors.bg },
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
