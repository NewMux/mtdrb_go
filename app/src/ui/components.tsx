/**
 * Shared building blocks.
 *
 * Deliberately few. The gym-floor screens need large touch targets and legible
 * numbers far more than they need a component library.
 *
 * Two rules hold the look together. Anything tappable is a pill or a circle;
 * anything that merely contains is a rounded rectangle — so shape alone says
 * what responds to a finger. And the accent is spent once per screen, on the
 * action the trainer came to take, which is why most buttons here are grey.
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

export function Card({
  children, style, tone = 'default',
}: {
  children: React.ReactNode;
  style?: StyleProp<ViewStyle>;
  /** `accent` fills the card with lime — for the one thing that matters most. */
  tone?: 'default' | 'raised' | 'accent';
}) {
  const toneStyle =
    tone === 'accent' ? styles.cardAccent :
    tone === 'raised' ? styles.cardRaised : styles.cardDefault;
  return <View style={[styles.card, toneStyle, style]}>{children}</View>;
}

export function Display({ children }: { children: React.ReactNode }) {
  return <Text style={styles.display}>{children}</Text>;
}

export function Title({ children }: { children: React.ReactNode }) {
  return <Text style={styles.title}>{children}</Text>;
}

export function Heading({ children, onAccent }: { children: React.ReactNode; onAccent?: boolean }) {
  return <Text style={[styles.heading, onAccent && styles.onAccent]}>{children}</Text>;
}

/** An uppercase section marker. Spaced out so it reads as a label, not prose. */
export function Label({ children }: { children: React.ReactNode }) {
  return <Text style={styles.label}>{children}</Text>;
}

export function Body({ children, muted, onAccent }: { children: React.ReactNode; muted?: boolean; onAccent?: boolean }) {
  return <Text style={[styles.body, muted && styles.muted, onAccent && styles.onAccent]}>{children}</Text>;
}

export function Caption({
  children, tone,
}: { children: React.ReactNode; tone?: 'muted' | 'warning' | 'danger' | 'success' | 'accent' | 'onAccent' }) {
  const toneStyle =
    tone === 'warning' ? styles.warning :
    tone === 'danger' ? styles.danger :
    tone === 'success' ? styles.success :
    tone === 'accent' ? styles.accentText :
    tone === 'onAccent' ? styles.onAccentMuted : styles.muted;
  return <Text style={[styles.caption, toneStyle]}>{children}</Text>;
}

/**
 * A number read at arm's length — a load, a rep count, a balance.
 *
 * The unit sits beside the figure at a much smaller size rather than inside
 * it, so the number itself is what the eye catches from a metre away.
 */
export function Metric({
  value, unit, label, tone = 'default',
}: {
  value: string;
  unit?: string;
  label?: string;
  tone?: 'default' | 'accent' | 'onAccent';
}) {
  const valueStyle =
    tone === 'accent' ? styles.metricAccent :
    tone === 'onAccent' ? styles.metricOnAccent : undefined;
  return (
    <View>
      <View style={styles.metricRow}>
        <Text style={[styles.metric, valueStyle]}>{value}</Text>
        {unit ? (
          <Text style={[styles.metricUnit, tone === 'onAccent' ? styles.onAccentMuted : styles.muted]}>
            {unit}
          </Text>
        ) : null}
      </View>
      {label ? (
        <Text style={[styles.label, tone === 'onAccent' && styles.onAccentMuted]}>{label}</Text>
      ) : null}
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
  const inactive = disabled || busy;
  const toneStyle =
    inactive ? styles.buttonDefault :
    tone === 'primary' ? styles.buttonPrimary :
    tone === 'danger' ? styles.buttonDanger :
    tone === 'quiet' ? styles.buttonQuiet : styles.buttonDefault;

  const labelStyle =
    inactive ? styles.muted :
    tone === 'primary' ? styles.buttonLabelPrimary :
    tone === 'danger' ? styles.danger :
    tone === 'quiet' ? styles.muted : undefined;

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
        : <Text style={[styles.buttonLabel, labelStyle]}>{label}</Text>}
    </Pressable>
  );
}

/** A status pill: attendance outcome, invoice state, sync state. */
export function Pill({
  label, tone = 'muted',
}: { label: string; tone?: 'muted' | 'success' | 'warning' | 'danger' | 'accent' | 'solid' }) {
  const toneStyle =
    tone === 'success' ? styles.pillSuccess :
    tone === 'warning' ? styles.pillWarning :
    tone === 'danger' ? styles.pillDanger :
    tone === 'accent' ? styles.pillAccent :
    tone === 'solid' ? styles.pillSolid : styles.pillMuted;
  const textStyle =
    tone === 'success' ? styles.success :
    tone === 'warning' ? styles.warning :
    tone === 'danger' ? styles.danger :
    tone === 'accent' ? styles.accentText :
    tone === 'solid' ? styles.onAccent : styles.muted;
  return (
    <View style={[styles.pill, toneStyle]}>
      <Text style={[styles.pillLabel, textStyle]}>{label}</Text>
    </View>
  );
}

/**
 * A small lime badge carrying a figure — a credit count, a rating.
 *
 * Reserved for numbers that decide something, so it keeps its weight.
 */
export function Chip({ label, tone = 'accent' }: { label: string; tone?: 'accent' | 'muted' | 'danger' }) {
  const toneStyle =
    tone === 'muted' ? styles.chipMuted :
    tone === 'danger' ? styles.chipDanger : styles.chipAccent;
  const textStyle =
    tone === 'muted' ? styles.muted :
    tone === 'danger' ? styles.onDanger : styles.onAccent;
  return (
    <View style={[styles.chip, toneStyle]}>
      <Text style={[styles.chipLabel, textStyle]}>{label}</Text>
    </View>
  );
}

export function Row({ children, style }: { children: React.ReactNode; style?: StyleProp<ViewStyle> }) {
  return <View style={[styles.row, style]}>{children}</View>;
}

export function Spacer({ size = space.md }: { size?: number }) {
  return <View style={{ height: size }} />;
}

/** A hairline between list rows, softer than a card edge. */
export function Divider() {
  return <View style={styles.divider} />;
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
      {hint ? <><Spacer size={space.xs} /><Text style={styles.caption}>{hint}</Text></> : null}
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

/** A choice among a handful of options, laid out as pills rather than a picker. */
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

/**
 * A small inline action — the kind that sits beside a section label.
 *
 * Its own component because a full Button next to a label reads as a second
 * heading and pulls rank over the section it belongs to.
 */
export function TextButton({ label, onPress }: { label: string; onPress: () => void }) {
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      onPress={onPress}
      hitSlop={12}
      style={({ pressed }) => [pressed && styles.buttonPressed]}
    >
      <Text style={styles.textButton}>{label}</Text>
    </Pressable>
  );
}

/** A thin lime track showing how far through something is. */
export function Progress({ value }: { value: number }) {
  const clamped = Math.max(0, Math.min(1, Number.isFinite(value) ? value : 0));
  return (
    <View
      accessibilityRole="progressbar"
      accessibilityValue={{ min: 0, max: 100, now: Math.round(clamped * 100) }}
      style={styles.progressTrack}
    >
      <View style={[styles.progressFill, { width: `${clamped * 100}%` }]} />
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.bg },
  card: { borderRadius: radius.lg, padding: space.lg },
  cardDefault: { backgroundColor: colors.surface },
  cardRaised: { backgroundColor: colors.surfaceRaised },
  cardAccent: { backgroundColor: colors.accent },

  display: { ...typography.display, color: colors.ink },
  title: { ...typography.title, color: colors.ink },
  heading: { ...typography.heading, color: colors.ink },
  label: { ...typography.label, color: colors.inkMuted, textTransform: 'uppercase' },
  body: { ...typography.body, color: colors.ink },
  caption: { ...typography.caption, color: colors.inkMuted },
  muted: { color: colors.inkMuted },
  warning: { color: colors.warning },
  danger: { color: colors.danger },
  success: { color: colors.success },
  accentText: { color: colors.accent },
  onAccent: { color: colors.onAccent },
  onAccentMuted: { color: 'rgba(18, 18, 18, 0.65)' },
  onDanger: { color: colors.ink },

  metricRow: { flexDirection: 'row', alignItems: 'baseline', gap: space.xs },
  metric: { ...typography.metric, color: colors.ink },
  metricAccent: { color: colors.accent },
  metricOnAccent: { color: colors.onAccent },
  metricUnit: { ...typography.metricUnit },

  button: {
    minHeight: TOUCH_TARGET,
    borderRadius: radius.pill,
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: space.xl,
  },
  buttonDefault: { backgroundColor: colors.surfaceRaised },
  buttonPrimary: { backgroundColor: colors.accent },
  buttonDanger: { backgroundColor: 'transparent', borderWidth: 1, borderColor: colors.danger },
  buttonQuiet: { backgroundColor: 'transparent' },
  buttonPressed: { opacity: 0.7 },
  buttonDisabled: { opacity: 0.5 },
  buttonLabel: { ...typography.heading, color: colors.ink },
  buttonLabelPrimary: { color: colors.onAccent },

  pill: {
    paddingHorizontal: space.md,
    paddingVertical: 6,
    borderRadius: radius.pill,
    borderWidth: 1,
    alignSelf: 'flex-start',
  },
  pillMuted: { borderColor: colors.border, backgroundColor: colors.surfaceRaised },
  pillSuccess: { borderColor: 'transparent', backgroundColor: 'rgba(74, 222, 128, 0.14)' },
  pillWarning: { borderColor: 'transparent', backgroundColor: 'rgba(251, 191, 36, 0.14)' },
  pillDanger: { borderColor: 'transparent', backgroundColor: 'rgba(251, 113, 133, 0.14)' },
  pillAccent: { borderColor: 'transparent', backgroundColor: colors.accentSoft },
  pillSolid: { borderColor: 'transparent', backgroundColor: colors.accent },
  pillLabel: { ...typography.caption },

  chip: {
    minWidth: 30,
    paddingHorizontal: space.sm,
    paddingVertical: 3,
    borderRadius: radius.sm,
    alignItems: 'center',
  },
  chipAccent: { backgroundColor: colors.accent },
  chipMuted: { backgroundColor: colors.surfaceRaised },
  chipDanger: { backgroundColor: colors.danger },
  chipLabel: { ...typography.caption },

  row: { flexDirection: 'row', alignItems: 'center', gap: space.md },
  divider: { height: 1, backgroundColor: colors.border },
  empty: { padding: space.xl, alignItems: 'center' },

  fieldLabel: { ...typography.label, color: colors.inkMuted, marginBottom: space.sm, textTransform: 'uppercase' },
  input: {
    minHeight: TOUCH_TARGET,
    borderRadius: radius.md,
    backgroundColor: colors.surfaceRaised,
    color: colors.ink,
    paddingHorizontal: space.lg,
    ...typography.body,
  },
  numberInput: { ...typography.metric, textAlign: 'center', minHeight: TOUCH_TARGET + 12 },

  banner: { borderRadius: radius.md, padding: space.lg },
  bannerWarning: { backgroundColor: 'rgba(251,191,36,0.12)' },
  bannerDanger: { backgroundColor: 'rgba(251,113,133,0.12)' },
  bannerSuccess: { backgroundColor: 'rgba(74,222,128,0.12)' },
  bannerMuted: { backgroundColor: colors.surfaceRaised },

  segmented: { flexDirection: 'row', gap: space.sm },
  segment: {
    flex: 1,
    minHeight: TOUCH_TARGET - 8,
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: space.sm,
    borderRadius: radius.pill,
    backgroundColor: colors.surfaceRaised,
  },
  segmentActive: { backgroundColor: colors.accent },
  segmentLabel: { ...typography.caption, color: colors.inkMuted },
  segmentLabelActive: { color: colors.onAccent },

  textButton: { ...typography.caption, color: colors.accent, textTransform: 'uppercase', letterSpacing: 1 },

  progressTrack: {
    height: 8,
    borderRadius: radius.pill,
    backgroundColor: colors.surfaceRaised,
    overflow: 'hidden',
  },
  progressFill: { height: '100%', borderRadius: radius.pill, backgroundColor: colors.accent },
});
