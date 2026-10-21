/**
 * An action sheet: a short list of choices rising from the bottom on glass.
 *
 * Used where iOS would show a confirmation dialog or a menu. Built by hand
 * because the platform sheet has no web equivalent, and the web demo should
 * behave the same as the phone.
 *
 * It moves the way a system sheet does: the scrim fades while the panel rises
 * on a spring, and on the way out it drops away quickly without bouncing.
 */

import React, { useEffect, useRef, useState } from 'react';
import { Animated, Modal, Pressable, StyleSheet, useWindowDimensions, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { Glass } from './glass';
import { Icon, type IconName } from './icon';
import { T, tap, toneColor } from './components';
import { radius, space, useTheme, type Hue } from './theme';

export interface SheetAction {
  label: string;
  detail?: string;
  icon?: IconName;
  hue?: Hue;
  destructive?: boolean;
  onPress: () => void;
}

export function ActionSheet({
  visible, title, message, actions, onClose,
}: {
  visible: boolean;
  title?: string;
  message?: string;
  actions: SheetAction[];
  onClose: () => void;
}) {
  const { c } = useTheme();
  const insets = useSafeAreaInsets();
  const { height } = useWindowDimensions();

  // The modal stays mounted while the panel leaves, so the exit is seen.
  const [mounted, setMounted] = useState(visible);
  const progress = useRef(new Animated.Value(0)).current;

  useEffect(() => {
    if (visible) {
      setMounted(true);
      Animated.spring(progress, { toValue: 1, damping: 24, stiffness: 260, mass: 0.9, useNativeDriver: true }).start();
    } else {
      Animated.timing(progress, { toValue: 0, duration: 180, useNativeDriver: true }).start(({ finished }) => {
        if (finished) setMounted(false);
      });
    }
  }, [visible, progress]);

  const rise = progress.interpolate({ inputRange: [0, 1], outputRange: [height * 0.5, 0] });

  return (
    <Modal visible={mounted} transparent animationType="none" onRequestClose={onClose} statusBarTranslucent>
      <Animated.View style={[StyleSheet.absoluteFill, { opacity: progress }]}>
        <Pressable accessibilityLabel="Dismiss" style={[StyleSheet.absoluteFill, { backgroundColor: 'rgba(0,0,0,0.35)' }]} onPress={onClose} />
      </Animated.View>
      <View pointerEvents="box-none" style={[styles.wrap, { paddingBottom: insets.bottom + space.sm }]}>
        <Animated.View style={[styles.stack, { transform: [{ translateY: rise }] }]}>
          <Glass style={styles.panel} intensity={80}>
            {title || message ? (
              <View style={styles.head}>
                {title ? <T variant="footnote" tone="label2" weight="600" align="center">{title}</T> : null}
                {message ? <T variant="footnote" tone="label2" align="center">{message}</T> : null}
              </View>
            ) : null}
            {actions.map((a, i) => {
              const color = a.destructive ? c.red : a.hue ? toneColor(c, a.hue) : c.tint;
              return (
                <Pressable
                  key={a.label}
                  accessibilityRole="button"
                  onPress={() => { tap('select'); onClose(); a.onPress(); }}
                  style={({ pressed }) => [styles.row, (i > 0 || title || message) && { borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: c.separator }, pressed && { backgroundColor: c.fill2 }]}
                >
                  {a.icon ? <Icon name={a.icon} size={20} color={color} /> : null}
                  <View style={{ flex: 1 }}>
                    <T variant="body" style={{ color: a.destructive ? c.red : c.label }}>{a.label}</T>
                    {a.detail ? <T variant="footnote" tone="label2" lines={2}>{a.detail}</T> : null}
                  </View>
                </Pressable>
              );
            })}
          </Glass>
          <Pressable accessibilityRole="button" onPress={onClose} style={({ pressed }) => [styles.cancelWrap, pressed && { opacity: 0.7 }]}>
            <Glass style={[styles.panel, styles.cancel]} intensity={80}>
              <T variant="headline" style={{ color: c.tint }}>Cancel</T>
            </Glass>
          </Pressable>
        </Animated.View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  wrap: { flex: 1, justifyContent: 'flex-end', paddingHorizontal: space.sm, alignItems: 'center' },
  stack: { width: '100%', maxWidth: 520, alignItems: 'center' },
  panel: { borderRadius: radius.xl, width: '100%' },
  head: { padding: space.lg, gap: 2 },
  row: { flexDirection: 'row', alignItems: 'center', gap: space.md, paddingHorizontal: space.xl, paddingVertical: 14, minHeight: 56 },
  cancel: { alignItems: 'center', justifyContent: 'center', height: 56 },
  cancelWrap: { marginTop: space.sm, width: '100%' },
});
