/**
 * Shared building blocks.
 *
 * Two rules hold the look together. Anything tappable is a pill or a circle;
 * anything that merely contains is a rounded rectangle — so shape alone says
 * what responds to a finger. And the accent is spent once per screen, on the
 * action the trainer came to take, which is why most buttons here are grey.
 *
 * Every colour comes from the theme, and every horizontal margin is written
 * start/end rather than left/right, so the same component is correct in light
 * and dark, English and Arabic.
 */

import React from 'react';
import {
  ActivityIndicator, Pressable, Text, TextInput, View,
  type KeyboardTypeOptions, type StyleProp, type TextStyle, type ViewStyle,
} from 'react-native';

import { Icon, type IconName } from './icon';
import { radius, space, TOUCH_TARGET } from './theme';
import { makeStyles, useTheme } from './theming';

export function Screen({ children, style }: { children: React.ReactNode; style?: StyleProp<ViewStyle> }) {
  const styles = useStyles();
  return <View style={[styles.screen, style]}>{children}</View>;
}

export function Card({
  children, style, tone = 'default',
}: {
  children: React.ReactNode;
  style?: StyleProp<ViewStyle>;
  /** `accent` fills the card with lime — for the one thing that matters most. */
  tone?: 'default' | 'raised' | 'accent' | 'outline';
}) {
  const styles = useStyles();
  const toneStyle =
    tone === 'accent' ? styles.cardAccent :
    tone === 'raised' ? styles.cardRaised :
    tone === 'outline' ? styles.cardOutline : styles.cardDefault;
  return <View style={[styles.card, toneStyle, style]}>{children}</View>;
}

export function Display({ children }: { children: React.ReactNode }) {
  const styles = useStyles();
  return <Text style={styles.display}>{children}</Text>;
}

export function Title({ children }: { children: React.ReactNode }) {
  const styles = useStyles();
  return <Text style={styles.title} accessibilityRole="header">{children}</Text>;
}

export function Heading({ children, onAccent }: { children: React.ReactNode; onAccent?: boolean }) {
  const styles = useStyles();
  return <Text style={[styles.heading, onAccent && styles.onAccent]}>{children}</Text>;
}

/** A section marker. Uppercase and spaced in Latin script; plain bold in Arabic. */
export function Label({ children }: { children: React.ReactNode }) {
  const styles = useStyles();
  return <Text style={styles.label}>{children}</Text>;
}

export function Body({
  children, muted, onAccent, numberOfLines, style,
}: {
  children: React.ReactNode;
  muted?: boolean;
  onAccent?: boolean;
  numberOfLines?: number;
  style?: StyleProp<TextStyle>;
}) {
  const styles = useStyles();
  return (
    <Text numberOfLines={numberOfLines} style={[styles.body, muted && styles.muted, onAccent && styles.onAccent, style]}>
      {children}
    </Text>
  );
}

export type CaptionTone = 'muted' | 'warning' | 'danger' | 'success' | 'accent' | 'onAccent' | 'ink';

export function Caption({
  children, tone, numberOfLines,
}: { children: React.ReactNode; tone?: CaptionTone; numberOfLines?: number }) {
  const styles = useStyles();
  const toneStyle =
    tone === 'warning' ? styles.warning :
    tone === 'danger' ? styles.danger :
    tone === 'success' ? styles.success :
    tone === 'accent' ? styles.accentText :
    tone === 'onAccent' ? styles.onAccentMuted :
    tone === 'ink' ? styles.ink : styles.muted;
  return <Text numberOfLines={numberOfLines} style={[styles.caption, toneStyle]}>{children}</Text>;
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
  const styles = useStyles();
  const valueStyle =
    tone === 'accent' ? styles.accentText :
    tone === 'onAccent' ? styles.onAccent : undefined;
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
  label, onPress, tone = 'default', disabled, busy, style, icon, compact,
}: {
  label: string;
  onPress: () => void;
  tone?: ButtonTone;
  disabled?: boolean;
  busy?: boolean;
  style?: StyleProp<ViewStyle>;
  icon?: IconName;
  /** Desk-height rather than gym-floor height, for toolbars and dense panes. */
  compact?: boolean;
}) {
  const styles = useStyles();
  const { colors } = useTheme();
  const inactive = disabled || busy;
  const toneStyle =
    inactive ? styles.buttonDefault :
    tone === 'primary' ? styles.buttonPrimary :
    tone === 'danger' ? styles.buttonDanger :
    tone === 'quiet' ? styles.buttonQuiet : styles.buttonDefault;

  const labelColor =
    inactive ? colors.inkMuted :
    tone === 'primary' ? colors.onAccent :
    tone === 'danger' ? colors.danger :
    tone === 'quiet' ? colors.inkMuted : colors.ink;

  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ disabled: inactive }}
      onPress={onPress}
      disabled={inactive}
      style={({ pressed }) => [
        styles.button, compact && styles.buttonCompact, toneStyle,
        pressed && styles.pressed,
        inactive && styles.disabled,
        style,
      ]}
    >
      {busy ? (
        <ActivityIndicator color={colors.ink} />
      ) : (
        <View style={styles.buttonInner}>
          {icon ? <Icon name={icon} size={compact ? 16 : 18} color={labelColor} /> : null}
          <Text style={[compact ? styles.buttonLabelCompact : styles.buttonLabel, { color: labelColor }]}>{label}</Text>
        </View>
      )}
    </Pressable>
  );
}

/** A round button carrying only an icon. Always labelled for screen readers. */
export function IconButton({
  icon, label, onPress, tone = 'default', size = 40,
}: {
  icon: IconName;
  label: string;
  onPress: () => void;
  tone?: 'default' | 'primary' | 'quiet';
  size?: number;
}) {
  const styles = useStyles();
  const { colors } = useTheme();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      onPress={onPress}
      hitSlop={8}
      style={({ pressed }) => [
        styles.iconButton,
        { width: size, height: size },
        tone === 'primary' ? styles.buttonPrimary : tone === 'quiet' ? styles.buttonQuiet : styles.buttonDefault,
        pressed && styles.pressed,
      ]}
    >
      <Icon name={icon} size={Math.round(size * 0.45)} color={tone === 'primary' ? colors.onAccent : colors.ink} />
    </Pressable>
  );
}

export type PillTone = 'muted' | 'success' | 'warning' | 'danger' | 'accent' | 'solid';

/** A status pill: attendance outcome, invoice state, sync state. */
export function Pill({ label, tone = 'muted' }: { label: string; tone?: PillTone }) {
  const styles = useStyles();
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
  const styles = useStyles();
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

/** A count on a nav item or a tab: small, round, and only when non-zero. */
export function Badge({ count, tone = 'accent' }: { count: number; tone?: 'accent' | 'danger' }) {
  const styles = useStyles();
  if (count <= 0) return null;
  return (
    <View style={[styles.badge, tone === 'danger' ? styles.chipDanger : styles.chipAccent]}>
      <Text style={[styles.badgeLabel, tone === 'danger' ? styles.onDanger : styles.onAccent]}>
        {count > 99 ? '99+' : String(count)}
      </Text>
    </View>
  );
}

/** Initials in a circle. The accent is kept for the trainer's own. */
export function Avatar({ name, size = 40, self }: { name: string; size?: number; self?: boolean }) {
  const styles = useStyles();
  const initials = name.trim().split(/\s+/).slice(0, 2).map((p) => p.slice(0, 1)).join('').toUpperCase() || '·';
  return (
    <View
      style={[styles.avatar, self ? styles.avatarSelf : styles.avatarOther, { width: size, height: size }]}
      accessibilityElementsHidden
      importantForAccessibility="no"
    >
      <Text style={[styles.avatarLabel, self ? styles.onAccent : styles.ink, { fontSize: size * 0.38 }]}>{initials}</Text>
    </View>
  );
}

export function Row({ children, style }: { children: React.ReactNode; style?: StyleProp<ViewStyle> }) {
  const styles = useStyles();
  return <View style={[styles.row, style]}>{children}</View>;
}

export function Spacer({ size = space.md }: { size?: number }) {
  return <View style={{ height: size }} />;
}

/** A hairline between list rows, softer than a card edge. */
export function Divider() {
  const styles = useStyles();
  return <View style={styles.divider} />;
}

/** Shown where a list is legitimately empty, rather than a blank screen. */
export function Empty({
  title, detail, icon, action,
}: {
  title: string;
  detail?: string;
  icon?: IconName;
  action?: { label: string; onPress: () => void };
}) {
  const styles = useStyles();
  const { colors } = useTheme();
  return (
    <View style={styles.empty}>
      {icon ? (
        <>
          <View style={styles.emptyIcon}><Icon name={icon} size={22} color={colors.inkMuted} /></View>
          <Spacer size={space.md} />
        </>
      ) : null}
      <Heading>{title}</Heading>
      {detail ? <><Spacer size={space.xs} /><Body muted style={styles.center}>{detail}</Body></> : null}
      {action ? <><Spacer /><Button label={action.label} onPress={action.onPress} compact /></> : null}
    </View>
  );
}

/** A labelled text input. */
export function Field({
  label, value, onChangeText, placeholder, keyboardType, secure, autoCapitalize, hint, style, multiline, error,
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
  multiline?: boolean;
  error?: string | null;
}) {
  const styles = useStyles();
  const { colors } = useTheme();
  return (
    <View style={style}>
      <Text style={styles.fieldLabel}>{label}</Text>
      <TextInput
        accessibilityLabel={label}
        style={[styles.input, multiline && styles.inputMultiline, error ? styles.inputError : null]}
        value={value}
        onChangeText={onChangeText}
        placeholder={placeholder}
        placeholderTextColor={colors.inkMuted}
        keyboardType={keyboardType}
        secureTextEntry={secure}
        autoCapitalize={autoCapitalize ?? 'sentences'}
        autoCorrect={false}
        multiline={multiline}
      />
      {error ? <><Spacer size={space.xs} /><Caption tone="danger">{error}</Caption></> : null}
      {hint && !error ? <><Spacer size={space.xs} /><Caption>{hint}</Caption></> : null}
    </View>
  );
}

/** A search box with its glyph, for filtering a list in place. */
export function SearchField({
  value, onChangeText, placeholder, label,
}: { value: string; onChangeText: (text: string) => void; placeholder?: string; label: string }) {
  const styles = useStyles();
  const { colors } = useTheme();
  return (
    <View style={styles.search}>
      <Icon name="search" size={18} color={colors.inkMuted} />
      <TextInput
        accessibilityLabel={label}
        style={styles.searchInput}
        value={value}
        onChangeText={onChangeText}
        placeholder={placeholder}
        placeholderTextColor={colors.inkMuted}
        autoCapitalize="none"
        autoCorrect={false}
      />
      {value ? (
        <Pressable accessibilityRole="button" accessibilityLabel={label} onPress={() => onChangeText('')} hitSlop={10}>
          <Icon name="close" size={16} color={colors.inkMuted} />
        </Pressable>
      ) : null}
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
  const styles = useStyles();
  const { colors } = useTheme();
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
  message, tone = 'warning', action,
}: {
  message: string;
  tone?: 'warning' | 'danger' | 'success' | 'muted';
  action?: { label: string; onPress: () => void };
}) {
  const styles = useStyles();
  const { colors } = useTheme();
  const toneStyle =
    tone === 'danger' ? styles.bannerDanger :
    tone === 'success' ? styles.bannerSuccess :
    tone === 'muted' ? styles.bannerMuted : styles.bannerWarning;
  const icon: IconName = tone === 'danger' ? 'error' : tone === 'success' ? 'success' : 'warning';
  const iconColor = tone === 'danger' ? colors.danger : tone === 'success' ? colors.success : tone === 'muted' ? colors.inkMuted : colors.warning;
  return (
    <View style={[styles.banner, toneStyle]} accessibilityRole="alert">
      <Icon name={icon} size={18} color={iconColor} />
      <View style={{ flex: 1 }}>
        <Text style={styles.body}>{message}</Text>
        {action ? (
          <>
            <Spacer size={space.sm} />
            <TextButton label={action.label} onPress={action.onPress} />
          </>
        ) : null}
      </View>
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
  const styles = useStyles();
  return (
    <View style={styles.segmented} accessibilityRole="radiogroup">
      {options.map((option) => (
        <Pressable
          key={option.value}
          accessibilityRole="radio"
          accessibilityState={{ selected: option.value === value }}
          onPress={() => onChange(option.value)}
          style={({ pressed }) => [
            styles.segment,
            option.value === value && styles.segmentActive,
            pressed && styles.pressed,
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
 * A module's sub-sections — Overview, List, Analytics.
 *
 * Underlined rather than filled: these switch views inside one screen, and a
 * row of lime pills at the top would spend the accent before the trainer has
 * done anything.
 */
export function Tabs<T extends string>({
  tabs, value, onChange,
}: {
  tabs: readonly { value: T; label: string; count?: number }[];
  value: T;
  onChange: (value: T) => void;
}) {
  const styles = useStyles();
  return (
    <View style={styles.tabs} accessibilityRole="tablist">
      {tabs.map((tab) => {
        const active = tab.value === value;
        return (
          <Pressable
            key={tab.value}
            accessibilityRole="tab"
            accessibilityState={{ selected: active }}
            onPress={() => onChange(tab.value)}
            style={({ pressed }) => [styles.tab, active && styles.tabActive, pressed && styles.pressed]}
          >
            <Text style={[styles.tabLabel, active && styles.tabLabelActive]}>{tab.label}</Text>
            {tab.count !== undefined && tab.count > 0 ? <Badge count={tab.count} tone="accent" /> : null}
          </Pressable>
        );
      })}
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
  const styles = useStyles();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      onPress={onPress}
      hitSlop={12}
      style={({ pressed }) => [pressed && styles.pressed]}
    >
      <Text style={styles.textButton}>{label}</Text>
    </Pressable>
  );
}

/** A thin lime track showing how far through something is. */
export function Progress({ value, onAccent }: { value: number; onAccent?: boolean }) {
  const styles = useStyles();
  const clamped = Math.max(0, Math.min(1, Number.isFinite(value) ? value : 0));
  return (
    <View
      accessibilityRole="progressbar"
      accessibilityValue={{ min: 0, max: 100, now: Math.round(clamped * 100) }}
      style={[styles.progressTrack, onAccent && styles.progressTrackOnAccent]}
    >
      <View style={[styles.progressFill, onAccent && styles.progressFillOnAccent, { width: `${clamped * 100}%` }]} />
    </View>
  );
}

/**
 * One headline number with what it means and where it is heading.
 *
 * The dashboard's unit. The label says what the number is; the trend line,
 * when present, says which way it moved and against what — never colour
 * alone, so the arrow carries direction for a reader who cannot tell green
 * from red.
 */
export function StatCard({
  label, value, unit, detail, trend, tone = 'default', onPress, icon,
}: {
  label: string;
  value: string;
  unit?: string;
  detail?: string;
  trend?: { direction: 'up' | 'down' | 'flat'; text: string; good?: boolean };
  tone?: 'default' | 'accent';
  onPress?: () => void;
  icon?: IconName;
}) {
  const styles = useStyles();
  const { colors } = useTheme();
  const onAccent = tone === 'accent';
  const trendColor = !trend || trend.good === undefined
    ? (onAccent ? colors.onAccent : colors.inkMuted)
    : trend.good ? colors.success : colors.danger;
  const body = (
    <Card tone={onAccent ? 'accent' : 'default'} style={styles.stat}>
      <Row style={{ justifyContent: 'space-between', alignItems: 'flex-start' }}>
        <Text style={[styles.label, onAccent && styles.onAccentMuted]} numberOfLines={1}>{label}</Text>
        {icon ? <Icon name={icon} size={16} color={onAccent ? colors.onAccent : colors.inkMuted} /> : null}
      </Row>
      <Spacer size={space.sm} />
      <View style={styles.metricRow}>
        <Text style={[styles.statValue, onAccent && styles.onAccent]} numberOfLines={1} adjustsFontSizeToFit>{value}</Text>
        {unit ? <Text style={[styles.metricUnit, onAccent ? styles.onAccentMuted : styles.muted]}>{unit}</Text> : null}
      </View>
      {trend ? (
        <View style={[styles.row, { gap: space.xs, marginTop: space.xs }]}>
          {trend.direction !== 'flat' ? (
            <Icon name={trend.direction === 'up' ? 'up' : 'down'} size={14} color={onAccent ? colors.onAccent : trendColor} />
          ) : null}
          <Text style={[styles.caption, { color: onAccent ? colors.onAccent : trendColor }]}>{trend.text}</Text>
        </View>
      ) : null}
      {detail ? <Text style={[styles.caption, onAccent ? styles.onAccentMuted : styles.muted, { marginTop: space.xs }]}>{detail}</Text> : null}
    </Card>
  );
  if (!onPress) return body;
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={`${label}: ${value}${unit ? ` ${unit}` : ''}`} onPress={onPress} style={({ pressed }) => [{ flex: 1 }, pressed && styles.pressed]}>
      {body}
    </Pressable>
  );
}

/**
 * A pro feature, shown locked rather than hidden.
 *
 * Hiding it would leave a Starter trainer not knowing the feature exists;
 * showing it greyed with one sentence of why is how a plan sells itself.
 */
export function UpgradePrompt({
  title, detail, actionLabel, onPress,
}: { title: string; detail: string; actionLabel: string; onPress?: () => void }) {
  const styles = useStyles();
  const { colors } = useTheme();
  return (
    <Card tone="outline">
      <Row>
        <View style={styles.proMark}><Icon name="pro" size={16} color={colors.onAccent} /></View>
        <View style={{ flex: 1 }}>
          <Heading>{title}</Heading>
          <Spacer size={space.xs} />
          <Body muted>{detail}</Body>
        </View>
      </Row>
      {onPress ? <><Spacer /><Button label={actionLabel} tone="primary" onPress={onPress} compact /></> : null}
    </Card>
  );
}

const useStyles = makeStyles(({ colors, type }) => ({
  screen: { flex: 1, backgroundColor: colors.bg },
  card: { borderRadius: radius.lg, padding: space.lg },
  cardDefault: { backgroundColor: colors.surface },
  cardRaised: { backgroundColor: colors.surfaceRaised },
  cardAccent: { backgroundColor: colors.accent },
  cardOutline: { backgroundColor: colors.surface, borderWidth: 1, borderColor: colors.border },

  display: { ...type.display, color: colors.ink },
  title: { ...type.title, color: colors.ink },
  heading: { ...type.heading, color: colors.ink },
  label: { ...type.label, color: colors.inkMuted },
  body: { ...type.body, color: colors.ink },
  caption: { ...type.caption, color: colors.inkMuted },
  center: { textAlign: 'center' },
  ink: { color: colors.ink },
  muted: { color: colors.inkMuted },
  warning: { color: colors.warning },
  danger: { color: colors.danger },
  success: { color: colors.success },
  accentText: { color: colors.accentInk },
  onAccent: { color: colors.onAccent },
  onAccentMuted: { color: 'rgba(18, 18, 18, 0.65)' },
  onDanger: { color: '#ffffff' },

  metricRow: { flexDirection: 'row', alignItems: 'baseline', gap: space.xs },
  metric: { ...type.metric, color: colors.ink },
  metricUnit: { ...type.metricUnit },
  statValue: { fontSize: 28, fontWeight: '800', letterSpacing: -0.8, color: colors.ink, flexShrink: 1 },
  stat: { flex: 1, minHeight: 112 },

  button: {
    minHeight: TOUCH_TARGET,
    borderRadius: radius.pill,
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: space.xl,
  },
  buttonCompact: { minHeight: 40, paddingHorizontal: space.lg },
  buttonInner: { flexDirection: 'row', alignItems: 'center', gap: space.sm },
  buttonDefault: { backgroundColor: colors.surfaceRaised },
  buttonPrimary: { backgroundColor: colors.accent },
  buttonDanger: { backgroundColor: 'transparent', borderWidth: 1, borderColor: colors.danger },
  buttonQuiet: { backgroundColor: 'transparent' },
  pressed: { opacity: 0.7 },
  disabled: { opacity: 0.5 },
  buttonLabel: { ...type.heading },
  buttonLabelCompact: { ...type.small, fontWeight: '700' },
  iconButton: { borderRadius: radius.pill, alignItems: 'center', justifyContent: 'center' },

  pill: {
    paddingHorizontal: space.md,
    paddingVertical: 6,
    borderRadius: radius.pill,
    borderWidth: 1,
    alignSelf: 'flex-start',
  },
  pillMuted: { borderColor: colors.border, backgroundColor: colors.surfaceRaised },
  pillSuccess: { borderColor: 'transparent', backgroundColor: colors.successSoft },
  pillWarning: { borderColor: 'transparent', backgroundColor: colors.warningSoft },
  pillDanger: { borderColor: 'transparent', backgroundColor: colors.dangerSoft },
  pillAccent: { borderColor: 'transparent', backgroundColor: colors.accentSoft },
  pillSolid: { borderColor: 'transparent', backgroundColor: colors.accent },
  pillLabel: { ...type.caption },

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
  chipLabel: { ...type.caption },
  badge: { minWidth: 18, height: 18, paddingHorizontal: 5, borderRadius: radius.pill, alignItems: 'center', justifyContent: 'center' },
  badgeLabel: { fontSize: 10, fontWeight: '800' },

  avatar: { borderRadius: radius.pill, alignItems: 'center', justifyContent: 'center' },
  avatarSelf: { backgroundColor: colors.accent },
  avatarOther: { backgroundColor: colors.surfaceRaised },
  avatarLabel: { fontWeight: '700' },

  row: { flexDirection: 'row', alignItems: 'center', gap: space.md },
  divider: { height: 1, backgroundColor: colors.border },
  empty: { padding: space.xl, alignItems: 'center' },
  emptyIcon: {
    width: 48, height: 48, borderRadius: radius.pill,
    backgroundColor: colors.surfaceRaised, alignItems: 'center', justifyContent: 'center',
  },

  fieldLabel: { ...type.label, color: colors.inkMuted, marginBottom: space.sm },
  input: {
    minHeight: TOUCH_TARGET,
    borderRadius: radius.md,
    backgroundColor: colors.surfaceRaised,
    color: colors.ink,
    paddingHorizontal: space.lg,
    textAlign: 'auto',
    ...type.body,
  },
  inputMultiline: { minHeight: 96, paddingTop: space.md, textAlignVertical: 'top' },
  inputError: { borderWidth: 1, borderColor: colors.danger },
  numberInput: { ...type.metric, textAlign: 'center', minHeight: TOUCH_TARGET + 12 },
  search: {
    flexDirection: 'row', alignItems: 'center', gap: space.sm,
    minHeight: 44, borderRadius: radius.pill, backgroundColor: colors.surfaceRaised,
    paddingHorizontal: space.lg,
  },
  searchInput: { flex: 1, color: colors.ink, ...type.body, minHeight: 44, textAlign: 'auto' },

  banner: { borderRadius: radius.md, padding: space.lg, flexDirection: 'row', gap: space.md, alignItems: 'flex-start' },
  bannerWarning: { backgroundColor: colors.warningSoft },
  bannerDanger: { backgroundColor: colors.dangerSoft },
  bannerSuccess: { backgroundColor: colors.successSoft },
  bannerMuted: { backgroundColor: colors.surfaceRaised },

  segmented: { flexDirection: 'row', gap: space.sm, flexWrap: 'wrap' },
  segment: {
    flexGrow: 1,
    minHeight: TOUCH_TARGET - 8,
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: space.md,
    borderRadius: radius.pill,
    backgroundColor: colors.surfaceRaised,
  },
  segmentActive: { backgroundColor: colors.accent },
  segmentLabel: { ...type.caption, color: colors.inkMuted },
  segmentLabelActive: { color: colors.onAccent },

  tabs: { flexDirection: 'row', gap: space.xl, borderBottomWidth: 1, borderBottomColor: colors.border, flexWrap: 'wrap' },
  tab: {
    flexDirection: 'row', alignItems: 'center', gap: space.sm,
    paddingVertical: space.md, borderBottomWidth: 2, borderBottomColor: 'transparent', marginBottom: -1,
  },
  tabActive: { borderBottomColor: colors.accentInk },
  tabLabel: { ...type.small, fontWeight: '600', color: colors.inkMuted },
  tabLabelActive: { color: colors.ink, fontWeight: '700' },

  textButton: { ...type.caption, color: colors.accentInk, letterSpacing: type.label.letterSpacing ?? 0, textTransform: type.label.textTransform },

  progressTrack: { height: 8, borderRadius: radius.pill, backgroundColor: colors.surfaceRaised, overflow: 'hidden' },
  progressTrackOnAccent: { backgroundColor: 'rgba(18,18,18,0.18)' },
  progressFill: { height: '100%', borderRadius: radius.pill, backgroundColor: colors.accent },
  progressFillOnAccent: { backgroundColor: colors.onAccent },

  proMark: {
    width: 32, height: 32, borderRadius: radius.pill, backgroundColor: colors.accent,
    alignItems: 'center', justifyContent: 'center',
  },
}));
