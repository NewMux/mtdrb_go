/**
 * The five places a trainer goes.
 *
 * Today is first because it answers the only question asked on a gym floor:
 * who is in front of me, and what happens next. Then the week, the people,
 * the training, and the business.
 *
 * On iOS this is the system tab bar — on iOS 26 and later, the floating
 * Liquid Glass capsule that minimises as content scrolls. Elsewhere a
 * JavaScript tab bar draws the same shape, so the web demo and Android build
 * look like the same product.
 *
 * Creating things lives behind the + in each tab's header rather than a
 * floating button: a button over the tab bar fights the glass capsule for the
 * same space.
 */

import React from 'react';
import { Platform, Pressable, StyleSheet, Text, View } from 'react-native';
import { Tabs } from 'expo-router';
import { NativeTabs, Icon as NativeIcon, Label } from 'expo-router/unstable-native-tabs';
import type { BottomTabBarProps } from '@react-navigation/bottom-tabs';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { Glass } from '@/ui/glass';
import { Icon, type IconName } from '@/ui/icon';
import { tap } from '@/ui/components';
import { radius, type as typography, useTheme } from '@/ui/theme';

const TABS: { name: string; title: string; icon: IconName; sf: { default: string; selected: string } }[] = [
  { name: 'index', title: 'Today', icon: 'today', sf: { default: 'sun.max', selected: 'sun.max.fill' } },
  { name: 'calendar', title: 'Calendar', icon: 'calendar', sf: { default: 'calendar', selected: 'calendar' } },
  { name: 'clients', title: 'Clients', icon: 'clients', sf: { default: 'person.2', selected: 'person.2.fill' } },
  { name: 'programs', title: 'Programs', icon: 'programs', sf: { default: 'dumbbell', selected: 'dumbbell.fill' } },
  { name: 'business', title: 'Business', icon: 'business', sf: { default: 'chart.bar', selected: 'chart.bar.fill' } },
];

export default function TabsLayout() {
  const { c } = useTheme();

  if (Platform.OS === 'ios') {
    return (
      <NativeTabs tintColor={c.tint} minimizeBehavior="onScrollDown">
        {TABS.map((t) => (
          <NativeTabs.Trigger key={t.name} name={t.name}>
            {/* The names are checked against the SF Symbols set by expo-symbols' types. */}
            <NativeIcon sf={t.sf as never} />
            <Label>{t.title}</Label>
          </NativeTabs.Trigger>
        ))}
      </NativeTabs>
    );
  }

  return (
    <Tabs
      tabBar={(props) => <GlassTabBar {...props} />}
      screenOptions={{ headerShown: false, sceneStyle: { backgroundColor: c.bg } }}
    >
      {TABS.map((t) => <Tabs.Screen key={t.name} name={t.name} options={{ title: t.title }} />)}
    </Tabs>
  );
}

/**
 * The floating capsule, drawn by hand.
 *
 * The selected tab sits in a soft lozenge rather than merely changing colour,
 * which is how the iOS 26 bar shows where you are without relying on hue.
 */
function GlassTabBar({ state, navigation }: BottomTabBarProps) {
  const { c } = useTheme();
  const insets = useSafeAreaInsets();

  return (
    <View pointerEvents="box-none" style={[styles.wrap, { paddingBottom: Math.max(insets.bottom, 12) }]}>
      <Glass style={styles.bar} intensity={70}>
        {state.routes.map((route, index) => {
          const tab = TABS.find((t) => t.name === route.name);
          if (!tab) return null;
          const focused = state.index === index;
          const color = focused ? c.tint : c.label;
          return (
            <Pressable
              key={route.key}
              accessibilityRole="tab"
              accessibilityState={{ selected: focused }}
              accessibilityLabel={tab.title}
              onPress={() => {
                const event = navigation.emit({ type: 'tabPress', target: route.key, canPreventDefault: true });
                if (!focused && !event.defaultPrevented) {
                  tap('select');
                  navigation.navigate(route.name, route.params);
                }
              }}
              style={styles.item}
            >
              <View style={[styles.lozenge, focused && { backgroundColor: c.fill2 }]}>
                <Icon name={tab.icon} size={22} color={color} />
                <Text style={[typography.caption2, styles.label, { color, fontWeight: focused ? '700' : '500' }]} numberOfLines={1}>
                  {tab.title}
                </Text>
              </View>
            </Pressable>
          );
        })}
      </Glass>
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: { position: 'absolute', left: 0, right: 0, bottom: 0, alignItems: 'center', paddingHorizontal: 16 },
  bar: {
    flexDirection: 'row', width: '100%', maxWidth: 520, height: 64, borderRadius: radius.pill,
    paddingHorizontal: 6, alignItems: 'center',
    shadowColor: '#000', shadowOpacity: 0.15, shadowRadius: 20, shadowOffset: { width: 0, height: 8 },
  },
  item: { flex: 1, height: '100%', alignItems: 'center', justifyContent: 'center' },
  lozenge: { alignItems: 'center', justifyContent: 'center', height: 52, minWidth: 64, borderRadius: 26, paddingHorizontal: 8, gap: 2 },
  label: { marginTop: 1 },
});
