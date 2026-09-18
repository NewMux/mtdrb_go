/**
 * Shared building blocks.
 *
 * Deliberately few. The gym-floor screens need large touch targets and legible
 * numbers far more than they need a component library.
 */

import React from 'react';
import {
  ActivityIndicator, Pressable, StyleSheet, Text, TextInput, View,
  type KeyboardTypeOptions, type StyleProp, type ViewStyle,
} from 'react-native';
import { colors, radius, space, TOUCH_TARGET, type as typography } from './theme';

export function Screen({ children, style }: { children: React.ReactNode; style?: StyleProp<ViewStyle> }) {
  return <View style={[styles.screen, style]}>{children}</View>;
}

export function Card({ children, style }: { children: React.ReactNode; style?: StyleProp<ViewStyle> }) {
  return <View style={[styles.card, style]}>{children}</View>;
}

export function Title({ children }: { children: React.ReactNode }) {
  return <Text style={styles.title}>{children}</Text>;
}

export function Heading({ children }: { children: React.ReactNode }) {
  return <Text style={styles.heading}>{children}</Text>;
}

export function Body({ children, muted }: { children: React.ReactNode; muted?: boolean }) {
  return <Text style={[styles.body, muted && styles.muted]}>{children}</Text>;
}

export function Caption({ children, tone }: { children: React.ReactNode; tone?: 'muted' | 'warning' | 'danger' | 'success' }) {
  const toneStyle =
    tone === 'warning' ? styles.warning :
    tone === 'danger' ? styles.danger :
    tone === 'success' ? styles.success : styles.muted;
  return <Text style={[styles.caption, toneStyle]}>{children}</Text>;
}

/** A number read at arm's length — a load, a rep count, a balance. */
export function Metric({ value, label }: { value: string; label?: string }) {
  return (
    <View>
      <Text style={styles.metric}>{value}</Text>
      {label ? <Text style={styles.caption}>{label}</Text> : null}
    </View>
  );
}

type ButtonTone = 'default' | 'primary' | 'danger' | 'quiet';

export function Button({
  label, onPress, tone = 'default', disabled, busy, style,
}: {
  label: string;
  onPress: () => void;
  tone?: ButtonTone;
  disabled?: boolean;
  busy?: boolean;
  style?: StyleProp<ViewStyle>;
}) {
  const toneStyle =
    tone === 'primary' ? styles.buttonPrimary :
    tone === 'danger' ? styles.buttonDanger :
    tone === 'quiet' ? styles.buttonQuiet : styles.buttonDefault;

  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ disabled: disabled || busy }}
      onPress={onPress}
      disabled={disabled || busy}
      style={({ pressed }) => [
        styles.button, toneStyle,
        pressed && styles.buttonPressed,
        (disabled || busy) && styles.buttonDisabled,
        style,
      ]}
    >
      {busy
        ? <ActivityIndicator color={colors.ink} />
        : <Text style={[styles.buttonLabel, tone === 'quiet' && styles.muted]}>{label}</Text>}
    </Pressable>
  );
}

/** A status pill: attendance outcome, invoice state, sync state. */
export function Pill({ label, tone = 'muted' }: { label: string; tone?: 'muted' | 'success' | 'warning' | 'danger' | 'accent' }) {
  const toneStyle =
    tone === 'success' ? styles.pillSuccess :
    tone === 'warning' ? styles.pillWarning :
    tone === 'danger' ? styles.pillDanger :
    tone === 'accent' ? styles.pillAccent : styles.pillMuted;
  return (
    <View style={[styles.pill, toneStyle]}>
      <Text style={styles.pillLabel}>{label}</Text>
    </View>
  );
}

export function Row({ children, style }: { children: React.ReactNode; style?: StyleProp<ViewStyle> }) {
  return <View style={[styles.row, style]}>{children}</View>;
}

export function Spacer({ size = space.md }: { size?: number }) {
  return <View style={{ height: size }} />;
}

/** Shown where a list is legitimately empty, rather than a blank screen. */
export function Empty({ title, detail }: { title: string; detail?: string }) {
  return (
    <View style={styles.empty}>
      <Heading>{title}</Heading>
      {detail ? <><Spacer size={space.xs} /><Body muted>{detail}</Body></> : null}
    </View>
  );
}

/** A labelled text input. */
export function Field({
  label, value, onChangeText, placeholder, keyboardType, secure, autoCapitalize, hint, style,
}: {
  label: string;
  value: string;
  onChangeText: (text: string) => void;
  placeholder?: string;
  keyboardType?: KeyboardTypeOptions;
  secure?: boolean;
  autoCapitalize?: 'none' | 'words' | 'sentences';
  hint?: string;
  style?: StyleProp<ViewStyle>;
}) {
  return (
    <View style={style}>
      <Text style={styles.fieldLabel}>{label}</Text>
      <TextInput
        accessibilityLabel={label}
        style={styles.input}
        value={value}
        onChangeText={onChangeText}
        placeholder={placeholder}
        placeholderTextColor={colors.inkMuted}
        keyboardType={keyboardType}
        secureTextEntry={secure}
        autoCapitalize={autoCapitalize ?? 'sentences'}
        autoCorrect={false}
      />
      {hint ? <Text style={styles.caption}>{hint}</Text> : null}
    </View>
  );
}

/**
 * A number pad field, sized for the gym floor.
 *
 * Its own component because these are tapped mid-set with one thumb, and the
 * default input height is roughly half what that needs.
 */
export function NumberField({
  label, value, onChangeText, placeholder, style,
}: {
  label: string;
  value: string;
  onChangeText: (text: string) => void;
  placeholder?: string;
  style?: StyleProp<ViewStyle>;
}) {
  return (
    <View style={[{ flex: 1 }, style]}>
      <Text style={styles.fieldLabel}>{label}</Text>
      <TextInput
        accessibilityLabel={label}
        style={[styles.input, styles.numberInput]}
        value={value}
        onChangeText={onChangeText}
        placeholder={placeholder}
        placeholderTextColor={colors.inkMuted}
        keyboardType="decimal-pad"
        selectTextOnFocus
      />
    </View>
  );
}

/** Something the trainer needs told, in place rather than in an alert. */
export function Banner({
  message, tone = 'warning',
}: { message: string; tone?: 'warning' | 'danger' | 'success' | 'muted' }) {
  const toneStyle =
    tone === 'danger' ? styles.bannerDanger :
    tone === 'success' ? styles.bannerSuccess :
    tone === 'muted' ? styles.bannerMuted : styles.bannerWarning;
  return (
    <View style={[styles.banner, toneStyle]}>
      <Text style={styles.body}>{message}</Text>
    </View>
  );
}

/** A choice among a handful of options, laid out as buttons rather than a picker. */
export function SegmentedChoice<T extends string>({
  options, value, onChange,
}: {
  options: readonly { value: T; label: string }[];
  value: T;
  onChange: (value: T) => void;
}) {
  return (
    <View style={styles.segmented}>
      {options.map((option) => (
        <Pressable
          key={option.value}
          accessibilityRole="radio"
          accessibilityState={{ selected: option.value === value }}
          onPress={() => onChange(option.value)}
          style={({ pressed }) => [
            styles.segment,
            option.value === value && styles.segmentActive,
            pressed && styles.buttonPressed,
          ]}
        >
          <Text style={[styles.segmentLabel, option.value === value && styles.segmentLabelActive]}>
            {option.label}
          </Text>
        </Pressable>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.bg },
  card: {
    backgroundColor: colors.surface,
    borderRadius: radius.lg,
    borderWidth: 1,
    borderColor: colors.border,
    padding: space.lg,
  },
  title: { ...typography.title, color: colors.ink },
  heading: { ...typography.heading, color: colors.ink },
  body: { ...typography.body, color: colors.ink },
  caption: { ...typography.caption, color: colors.inkMuted },
  metric: { ...typography.metric, color: colors.ink },
  muted: { color: colors.inkMuted },
  warning: { color: colors.warning },
  danger: { color: colors.danger },
  success: { color: colors.success },

  button: {
    minHeight: TOUCH_TARGET,
    borderRadius: radius.md,
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: space.lg,
    borderWidth: 1,
  },
  buttonDefault: { backgroundColor: colors.surfaceRaised, borderColor: colors.border },
  buttonPrimary: { backgroundColor: colors.accent, borderColor: colors.accent },
  buttonDanger: { backgroundColor: 'transparent', borderColor: colors.danger },
  buttonQuiet: { backgroundColor: 'transparent', borderColor: 'transparent' },
  buttonPressed: { opacity: 0.72 },
  buttonDisabled: { opacity: 0.4 },
  buttonLabel: { ...typography.heading, color: colors.ink },

  pill: {
    paddingHorizontal: space.md,
    paddingVertical: space.xs,
    borderRadius: radius.pill,
    borderWidth: 1,
    alignSelf: 'flex-start',
  },
  pillMuted: { borderColor: colors.border, backgroundColor: colors.surfaceRaised },
  pillSuccess: { borderColor: colors.success, backgroundColor: 'transparent' },
  pillWarning: { borderColor: colors.warning, backgroundColor: 'transparent' },
  pillDanger: { borderColor: colors.danger, backgroundColor: 'transparent' },
  pillAccent: { borderColor: colors.accent, backgroundColor: 'transparent' },
  pillLabel: { ...typography.caption, color: colors.ink },

  row: { flexDirection: 'row', alignItems: 'center', gap: space.md },
  empty: { padding: space.xl, alignItems: 'center' },

  fieldLabel: { ...typography.caption, color: colors.inkMuted, marginBottom: space.xs },
  input: {
    minHeight: TOUCH_TARGET,
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.border,
    backgroundColor: colors.surfaceRaised,
    color: colors.ink,
    paddingHorizontal: space.md,
    ...typography.body,
  },
  numberInput: { ...typography.metric, textAlign: 'center', minHeight: TOUCH_TARGET + 8 },

  banner: {
    borderRadius: radius.md,
    borderWidth: 1,
    padding: space.md,
  },
  bannerWarning: { borderColor: colors.warning, backgroundColor: 'rgba(251,191,36,0.08)' },
  bannerDanger: { borderColor: colors.danger, backgroundColor: 'rgba(248,113,113,0.08)' },
  bannerSuccess: { borderColor: colors.success, backgroundColor: 'rgba(52,211,153,0.08)' },
  bannerMuted: { borderColor: colors.border, backgroundColor: colors.surfaceRaised },

  segmented: {
    flexDirection: 'row',
    borderRadius: radius.md,
    borderWidth: 1,
    borderColor: colors.border,
    overflow: 'hidden',
  },
  segment: {
    flex: 1,
    minHeight: TOUCH_TARGET,
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: space.sm,
    backgroundColor: colors.surface,
  },
  segmentActive: { backgroundColor: colors.accent },
  segmentLabel: { ...typography.caption, color: colors.inkMuted },
  segmentLabelActive: { color: colors.bg },
});
