/**
 * Liquid Glass.
 *
 * On iOS 26 and later this is the system material itself — refractive,
 * reacting to what scrolls beneath it. Elsewhere it is the closest honest
 * approximation: a blurred, translucent layer with a hairline edge. On web
 * that is CSS `backdrop-filter`, which every current browser supports.
 *
 * Glass is for things that float over content — the tab bar, header buttons,
 * the rest timer — never for the content itself. Cards are solid.
 */

import React from 'react';
import { Platform, StyleSheet, View, type StyleProp, type ViewStyle } from 'react-native';
import { BlurView } from 'expo-blur';
import { GlassView, isLiquidGlassAvailable } from 'expo-glass-effect';

import { useTheme } from './theme';

const nativeGlass = Platform.OS === 'ios' && safeLiquidGlass();

function safeLiquidGlass(): boolean {
  try {
    return isLiquidGlassAvailable();
  } catch {
    return false;
  }
}

export function Glass({
  children, style, tint, interactive = false, intensity = 40,
}: {
  children?: React.ReactNode;
  style?: StyleProp<ViewStyle>;
  /** A colour washed through the glass — for a primary glass button. */
  tint?: string;
  /** Lets the native material respond to touch (iOS 26+). */
  interactive?: boolean;
  intensity?: number;
}) {
  const { c, scheme } = useTheme();

  if (nativeGlass) {
    return (
      <GlassView
        glassEffectStyle="regular"
        isInteractive={interactive}
        tintColor={tint}
        style={[styles.clip, style]}
      >
        {children}
      </GlassView>
    );
  }

  if (Platform.OS === 'web' || Platform.OS === 'ios') {
    return (
      <BlurView
        intensity={intensity}
        tint={scheme === 'dark' ? 'systemChromeMaterialDark' : 'systemChromeMaterialLight'}
        style={[
          styles.clip,
          { backgroundColor: tint ?? c.glass, borderColor: c.glassBorder },
          styles.edge,
          style,
        ]}
      >
        {children}
      </BlurView>
    );
  }

  // Android: blur is costly and inconsistent across devices, so a solid
  // translucent layer stands in.
  return (
    <View style={[styles.clip, { backgroundColor: tint ?? c.surface, borderColor: c.separator }, styles.edge, style]}>
      {children}
    </View>
  );
}

const styles = StyleSheet.create({
  clip: { overflow: 'hidden' },
  edge: { borderWidth: StyleSheet.hairlineWidth },
});
