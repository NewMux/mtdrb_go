/**
 * The three places a trainer goes, and the one thing they create.
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
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { Tabs, useRouter } from 'expo-router';

import { colors, FAB_SIZE, radius, space, type as typography } from '@/ui/theme';

function AddClientButton() {
  const router = useRouter();
  return (
    <View pointerEvents="box-none" style={styles.fabWrap}>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel="Add client"
        onPress={() => router.push('/client/new')}
        style={({ pressed }) => [styles.fab, pressed && { opacity: 0.8 }]}
      >
        <Text style={styles.fabGlyph}>+</Text>
      </Pressable>
    </View>
  );
}

export default function TabsLayout() {
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
            paddingTop: space.md,
          },
          tabBarActiveTintColor: colors.accent,
          tabBarInactiveTintColor: colors.inkMuted,
          tabBarLabelStyle: { ...typography.caption },
          // No icon set is bundled, and a label a trainer can read in a dim
          // gym beats a glyph they have to decode.
          tabBarIconStyle: { display: 'none' },
        }}
      >
        <Tabs.Screen name="index" options={{ title: 'Today' }} />
        <Tabs.Screen name="clients" options={{ title: 'Clients' }} />
        <Tabs.Screen name="money" options={{ title: 'Money' }} />
      </Tabs>
      <AddClientButton />
    </>
  );
}

const styles = StyleSheet.create({
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
    shadowOpacity: 0.45,
    shadowRadius: 16,
    shadowOffset: { width: 0, height: 6 },
    elevation: 8,
  },
  fabGlyph: {
    fontSize: 30,
    lineHeight: 34,
    fontWeight: '600',
    color: colors.onAccent,
  },
});
