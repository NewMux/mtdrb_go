/**
 * An action sheet: a short list of choices rising from the bottom on glass.
 *
 * Used where iOS would show a confirmation dialog or a menu. Built by hand
 * because the platform sheet has no web equivalent, and the web demo should
 * behave the same as the phone.
 */

import React from 'react';
import { Modal, Pressable, StyleSheet, View } from 'react-native';
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
  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onClose}>
      <Pressable accessibilityLabel="Dismiss" style={[StyleSheet.absoluteFill, { backgroundColor: 'rgba(0,0,0,0.35)' }]} onPress={onClose} />
      <View pointerEvents="box-none" style={[styles.wrap, { paddingBottom: insets.bottom + space.sm }]}>
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
        <Pressable accessibilityRole="button" onPress={onClose} style={({ pressed }) => [{ marginTop: space.sm }, pressed && { opacity: 0.7 }]}>
          <Glass style={[styles.panel, styles.cancel]} intensity={80}>
            <T variant="headline" style={{ color: c.tint }}>Cancel</T>
          </Glass>
        </Pressable>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  wrap: { flex: 1, justifyContent: 'flex-end', paddingHorizontal: space.sm, alignItems: 'center' },
  panel: { borderRadius: radius.xl, width: '100%', maxWidth: 520 },
  head: { padding: space.lg, gap: 2 },
  row: { flexDirection: 'row', alignItems: 'center', gap: space.md, paddingHorizontal: space.xl, paddingVertical: 14, minHeight: 56 },
  cancel: { alignItems: 'center', justifyContent: 'center', height: 56 },
});
