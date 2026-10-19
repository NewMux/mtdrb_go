/**
 * The rest timer: a glass capsule floating over the logger.
 *
 * It starts by itself when a working set is ticked off, so nobody has to
 * remember to start it, and it buzzes when rest is over. Glass, because it
 * floats over the set list rather than pushing it around.
 */

import React, { useEffect, useRef, useState } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { Glass } from './glass';
import { Icon } from './icon';
import { ProgressBar, T, tap } from './components';
import { radius, space, useTheme } from './theme';

export function RestTimer({
  endsAt, total, onAdjust, onSkip,
}: {
  endsAt: number;
  total: number;
  onAdjust: (seconds: number) => void;
  onSkip: () => void;
}) {
  const { c } = useTheme();
  const insets = useSafeAreaInsets();
  const [now, setNow] = useState(Date.now());
  const buzzed = useRef(false);

  useEffect(() => {
    buzzed.current = false;
    const t = setInterval(() => setNow(Date.now()), 250);
    return () => clearInterval(t);
  }, [endsAt]);

  const left = Math.max(0, Math.ceil((endsAt - now) / 1000));
  useEffect(() => {
    if (left === 0 && !buzzed.current) {
      buzzed.current = true;
      tap('success');
    }
  }, [left]);

  const done = left === 0;
  const mm = Math.floor(left / 60);
  const ss = String(left % 60).padStart(2, '0');

  return (
    <View pointerEvents="box-none" style={[styles.wrap, { bottom: insets.bottom + space.md }]}>
      <Glass style={styles.capsule} intensity={80} tint={done ? c.green : undefined}>
        <View style={styles.row}>
          <Icon name="timer" size={20} color={done ? '#FFF' : c.tint} />
          <View style={{ flex: 1 }}>
            <T variant="metricSmall" style={done ? { color: '#FFF' } : undefined}>{done ? 'Go' : `${mm}:${ss}`}</T>
            {!done ? <ProgressBar value={1 - left / Math.max(1, total)} height={4} /> : (
              <T variant="caption" style={{ color: '#FFF' }}>Rest over — next set</T>
            )}
          </View>
          {!done ? (
            <>
              <Chip label="−15" onPress={() => onAdjust(-15)} />
              <Chip label="+15" onPress={() => onAdjust(15)} />
            </>
          ) : null}
          <Pressable accessibilityRole="button" accessibilityLabel={done ? 'Dismiss timer' : 'Skip rest'} onPress={() => { tap(); onSkip(); }} hitSlop={8}
            style={[styles.skip, { backgroundColor: done ? 'rgba(255,255,255,0.25)' : c.fill2 }]}>
            <Icon name={done ? 'close' : 'chevronRight'} size={16} color={done ? '#FFF' : c.label} />
          </Pressable>
        </View>
      </Glass>
    </View>
  );
}

function Chip({ label, onPress }: { label: string; onPress: () => void }) {
  const { c } = useTheme();
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={`${label} seconds`} onPress={() => { tap('select'); onPress(); }}
      style={({ pressed }) => [styles.chip, { backgroundColor: c.fill2, opacity: pressed ? 0.6 : 1 }]}>
      <T variant="subhead" weight="600">{label}</T>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  wrap: { position: 'absolute', left: space.lg, right: space.lg, alignItems: 'center' },
  capsule: {
    width: '100%', maxWidth: 520, borderRadius: radius.pill, paddingVertical: 10, paddingHorizontal: 18,
    shadowColor: '#000', shadowOpacity: 0.2, shadowRadius: 20, shadowOffset: { width: 0, height: 8 },
  },
  row: { flexDirection: 'row', alignItems: 'center', gap: space.md },
  chip: { height: 34, paddingHorizontal: 12, borderRadius: 17, alignItems: 'center', justifyContent: 'center' },
  skip: { width: 34, height: 34, borderRadius: 17, alignItems: 'center', justifyContent: 'center' },
});
