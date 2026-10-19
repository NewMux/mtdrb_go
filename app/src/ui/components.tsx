/**
 * The component library.
 *
 * Built to read as iOS 27 on an iPhone and to carry the same feel to the web
 * and Android builds: large titles over grouped content, inset rounded list
 * groups, capsule buttons, glass only where something floats.
 *
 * Two rules hold the look together. Anything tappable is a capsule, a circle,
 * or a full list row with a chevron; anything that merely contains is a
 * rounded rectangle. And the tint is spent on the action the trainer came to
 * take — most controls are grey.
 */

import React, { useRef } from 'react';
import {
  ActivityIndicator, Animated, Platform, Pressable, RefreshControl, StyleSheet,
  Switch, Text, TextInput, View,
  type KeyboardTypeOptions, type ScrollView, type StyleProp, type TextStyle, type ViewStyle,
} from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import * as Haptics from 'expo-haptics';

import { Glass } from './glass';
import { Icon, type IconName } from './icon';
import {
  makeStyles, radius, space, TAB_BAR_CLEARANCE, TOUCH, TOUCH_FLOOR, type as typography,
  useTheme, wash, type Hue, type Palette, type TypeStyle,
} from './theme';

/* ───────────────────────────── feedback ───────────────────────────── */

/** A light tap of haptic feedback. Silent where the platform has none. */
export function tap(kind: 'light' | 'medium' | 'success' | 'warning' | 'select' = 'light') {
  if (Platform.OS === 'web') return;
  if (kind === 'success') void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
  else if (kind === 'warning') void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Warning);
  else if (kind === 'select') void Haptics.selectionAsync();
  else void Haptics.impactAsync(kind === 'medium' ? Haptics.ImpactFeedbackStyle.Medium : Haptics.ImpactFeedbackStyle.Light);
}

/* ─────────────────────────────── text ─────────────────────────────── */

export type Tone = 'label' | 'label2' | 'label3' | 'onTint' | Hue;

export function toneColor(c: Palette, tone: Tone): string {
  return (c as unknown as Record<string, string>)[tone] ?? c.label;
}

/**
 * Text in one of the Dynamic Type styles.
 *
 * Colour is named by role (`label2` for secondary) or by hue, so a screen
 * never hard-codes a value that is wrong in the other appearance.
 */
export function T({
  children, variant = 'body', tone = 'label', weight, align, lines, style, selectable,
}: {
  children: React.ReactNode;
  variant?: TypeStyle;
  tone?: Tone;
  weight?: TextStyle['fontWeight'];
  align?: TextStyle['textAlign'];
  lines?: number;
  style?: StyleProp<TextStyle>;
  selectable?: boolean;
}) {
  const { c } = useTheme();
  return (
    <Text
      numberOfLines={lines}
      selectable={selectable}
      style={[
        typography[variant],
        { color: toneColor(c, tone) },
        weight ? { fontWeight: weight } : null,
        align ? { textAlign: align } : null,
        style,
      ]}
    >
      {children}
    </Text>
  );
}

/* ────────────────────────────── layout ────────────────────────────── */

export function HStack({
  children, gap = space.sm, align = 'center', justify, style, wrap,
}: {
  children: React.ReactNode;
  gap?: number;
  align?: ViewStyle['alignItems'];
  justify?: ViewStyle['justifyContent'];
  style?: StyleProp<ViewStyle>;
  wrap?: boolean;
}) {
  return (
    <View style={[{ flexDirection: 'row', alignItems: align, justifyContent: justify, gap }, wrap && { flexWrap: 'wrap' }, style]}>
      {children}
    </View>
  );
}

export function VStack({
  children, gap = space.sm, align, style,
}: { children: React.ReactNode; gap?: number; align?: ViewStyle['alignItems']; style?: StyleProp<ViewStyle> }) {
  return <View style={[{ gap, alignItems: align }, style]}>{children}</View>;
}

export function Spacer({ size = space.md, flex }: { size?: number; flex?: boolean }) {
  return <View style={flex ? { flex: 1 } : { height: size, width: size }} />;
}

/**
 * A screen with an iOS large title.
 *
 * The big title scrolls away with the content, and a compact title on a glass
 * bar fades in as it goes — the same choreography as a native navigation bar,
 * done in JavaScript so the web build gets it too.
 */
export function Page({
  title, eyebrow, trailing, children, refreshing, onRefresh, tabBar = true, scrollRef,
  contentStyle, keyboard,
}: {
  title: string;
  /** A small line above the title: today's date, a count. */
  eyebrow?: string;
  /** Header buttons, right-aligned beside the title. */
  trailing?: React.ReactNode;
  children: React.ReactNode;
  refreshing?: boolean;
  onRefresh?: () => void;
  /** Leaves room for the floating tab bar. False for pushed screens. */
  tabBar?: boolean;
  scrollRef?: React.RefObject<ScrollView | null>;
  contentStyle?: StyleProp<ViewStyle>;
  keyboard?: boolean;
}) {
  const { c } = useTheme();
  const s = usePageStyles();
  const insets = useSafeAreaInsets();
  const y = useRef(new Animated.Value(0)).current;

  const barOpacity = y.interpolate({ inputRange: [24, 56], outputRange: [0, 1], extrapolate: 'clamp' });
  const top = insets.top;

  return (
    <View style={s.root}>
      <Animated.ScrollView
        ref={scrollRef as React.RefObject<ScrollView>}
        onScroll={Animated.event([{ nativeEvent: { contentOffset: { y } } }], { useNativeDriver: true })}
        scrollEventThrottle={16}
        keyboardShouldPersistTaps={keyboard ? 'handled' : undefined}
        keyboardDismissMode="on-drag"
        contentContainerStyle={[
          { paddingTop: top + space.sm, paddingBottom: tabBar ? TAB_BAR_CLEARANCE + insets.bottom : space.xxl + insets.bottom },
          contentStyle,
        ]}
        refreshControl={onRefresh ? (
          <RefreshControl refreshing={!!refreshing} onRefresh={onRefresh} tintColor={c.label2} progressViewOffset={top} />
        ) : undefined}
      >
        <View style={s.titleRow}>
          <View style={{ flex: 1 }}>
            {eyebrow ? <T variant="footnote" tone="label2" weight="600" style={s.eyebrow}>{eyebrow.toUpperCase()}</T> : null}
            <T variant="largeTitle" lines={1}>{title}</T>
          </View>
          {trailing ? <HStack gap={space.sm}>{trailing}</HStack> : null}
        </View>
        {children}
      </Animated.ScrollView>

      <Animated.View pointerEvents="none" style={[s.bar, { height: top + 44, opacity: barOpacity }]}>
        <Glass style={StyleSheet.absoluteFill} intensity={60} />
        <View style={[s.barInner, { paddingTop: top }]}>
          <T variant="headline" lines={1}>{title}</T>
        </View>
      </Animated.View>
    </View>
  );
}

const usePageStyles = makeStyles((c) => ({
  root: { flex: 1, backgroundColor: c.bg },
  titleRow: {
    flexDirection: 'row', alignItems: 'flex-end', justifyContent: 'space-between',
    paddingHorizontal: space.xl, paddingBottom: space.md, gap: space.md,
  },
  eyebrow: { marginBottom: 2, letterSpacing: 0.4 },
  bar: { position: 'absolute', top: 0, left: 0, right: 0 },
  barInner: { flex: 1, alignItems: 'center', justifyContent: 'center' },
}));

/**
 * A plain scrolling body for pushed screens, under a native header.
 */
export function Body({
  children, keyboard, contentStyle, refreshing, onRefresh,
}: {
  children: React.ReactNode;
  keyboard?: boolean;
  contentStyle?: StyleProp<ViewStyle>;
  refreshing?: boolean;
  onRefresh?: () => void;
}) {
  const { c } = useTheme();
  const insets = useSafeAreaInsets();
  return (
    <Animated.ScrollView
      style={{ flex: 1, backgroundColor: c.bg }}
      contentInsetAdjustmentBehavior="automatic"
      keyboardShouldPersistTaps={keyboard ? 'handled' : undefined}
      keyboardDismissMode="on-drag"
      contentContainerStyle={[{ paddingTop: space.lg, paddingBottom: space.xxl + insets.bottom }, contentStyle]}
      refreshControl={onRefresh ? (
        <RefreshControl refreshing={!!refreshing} onRefresh={onRefresh} tintColor={c.label2} />
      ) : undefined}
    >
      {children}
    </Animated.ScrollView>
  );
}

/* ───────────────────────────── surfaces ───────────────────────────── */

export function Card({
  children, style, padded = true, onPress, accessibilityLabel,
}: {
  children: React.ReactNode;
  style?: StyleProp<ViewStyle>;
  padded?: boolean;
  onPress?: () => void;
  accessibilityLabel?: string;
}) {
  const s = useSurfaceStyles();
  if (onPress) {
    return (
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={accessibilityLabel}
        onPress={() => { tap(); onPress(); }}
        style={({ pressed }) => [s.card, padded && s.padded, pressed && s.pressed, style]}
      >
        {children}
      </Pressable>
    );
  }
  return <View style={[s.card, padded && s.padded, style]}>{children}</View>;
}

const useSurfaceStyles = makeStyles((c) => ({
  card: {
    backgroundColor: c.surface,
    borderRadius: radius.lg,
    ...(c.scheme === 'light' ? {
      shadowColor: '#000', shadowOpacity: 0.04, shadowRadius: 12, shadowOffset: { width: 0, height: 2 },
    } : {}),
  },
  padded: { padding: space.lg },
  pressed: { opacity: 0.75, transform: [{ scale: 0.985 }] },
}));

/**
 * An inset grouped section — the iOS Settings shape.
 *
 * A bold header with an optional trailing action, rows in one rounded group,
 * and a footnote underneath for whatever needs explaining.
 */
export function Section({
  title, action, onAction, footer, children, style, plain,
}: {
  title?: string;
  action?: string;
  onAction?: () => void;
  footer?: string;
  children: React.ReactNode;
  style?: StyleProp<ViewStyle>;
  /** Content without the rounded group: cards, tiles, a chart. */
  plain?: boolean;
}) {
  const s = useSectionStyles();
  const { c } = useTheme();
  return (
    <View style={[s.section, style]}>
      {title ? (
        <HStack justify="space-between" style={s.header}>
          <T variant="title3" weight="700">{title}</T>
          {action && onAction ? (
            <Pressable accessibilityRole="button" hitSlop={10} onPress={() => { tap('select'); onAction(); }}>
              {({ pressed }) => (
                <T variant="subhead" style={{ color: c.tint, opacity: pressed ? 0.5 : 1 }}>{action}</T>
              )}
            </Pressable>
          ) : null}
        </HStack>
      ) : null}
      {plain ? children : <View style={s.group}>{children}</View>}
      {footer ? <T variant="footnote" tone="label2" style={s.footer}>{footer}</T> : null}
    </View>
  );
}

const useSectionStyles = makeStyles((c) => ({
  section: { marginHorizontal: space.lg, marginTop: space.xl },
  header: { paddingHorizontal: space.xs, marginBottom: space.sm },
  group: { backgroundColor: c.surface, borderRadius: radius.lg, overflow: 'hidden' },
  footer: { paddingHorizontal: space.lg, marginTop: space.sm },
}));

/** A rounded coloured square holding a white glyph — the Settings icon tile. */
export function IconTile({ icon, hue = 'tint', size = 30 }: { icon: IconName; hue?: Hue; size?: number }) {
  const { c } = useTheme();
  return (
    <View style={{
      width: size, height: size, borderRadius: size * 0.27, backgroundColor: toneColor(c, hue),
      alignItems: 'center', justifyContent: 'center',
    }}>
      <Icon name={icon} size={size * 0.58} color="#FFFFFF" />
    </View>
  );
}

/** A coloured glyph on a soft wash of the same colour — quieter than a tile. */
export function IconBubble({ icon, hue = 'tint', size = 36 }: { icon: IconName; hue?: Hue; size?: number }) {
  const { c } = useTheme();
  const color = toneColor(c, hue);
  return (
    <View style={{
      width: size, height: size, borderRadius: size / 2, backgroundColor: wash(color, 0.16),
      alignItems: 'center', justifyContent: 'center',
    }}>
      <Icon name={icon} size={size * 0.5} color={color} />
    </View>
  );
}

/**
 * One row in a grouped list.
 *
 * Leading icon tile or avatar, a title with an optional subtitle, a trailing
 * value, and an accessory. Separators are drawn inset from the leading
 * element, as on iOS, so the rows read as one group.
 */
export function ListRow({
  title, subtitle, value, valueTone = 'label2', icon, hue, leading, trailing, accessory,
  onPress, first, destructive, titleTone, lines = 1, accessibilityLabel,
}: {
  title: string;
  subtitle?: string;
  value?: string;
  valueTone?: Tone;
  icon?: IconName;
  hue?: Hue;
  leading?: React.ReactNode;
  trailing?: React.ReactNode;
  accessory?: 'chevron' | 'check' | 'none';
  onPress?: () => void;
  /** Suppresses the separator above — pass for the first row in a group. */
  first?: boolean;
  destructive?: boolean;
  titleTone?: Tone;
  lines?: number;
  accessibilityLabel?: string;
}) {
  const s = useRowStyles();
  const { c } = useTheme();
  const lead = leading ?? (icon ? <IconTile icon={icon} hue={hue} /> : null);
  const showChevron = accessory === 'chevron' || (accessory === undefined && !!onPress && !trailing);

  const content = (pressed: boolean) => (
    <View style={[s.row, pressed && { backgroundColor: c.fill2 }]}>
      {lead ? <View style={s.lead}>{lead}</View> : null}
      <View style={[s.main, !first && s.separator, !first && { marginLeft: 0 }]}>
        <View style={{ flex: 1, paddingVertical: subtitle ? 10 : 0 }}>
          <T variant="body" tone={destructive ? 'red' : titleTone ?? 'label'} lines={lines}>{title}</T>
          {subtitle ? <T variant="subhead" tone="label2" lines={2}>{subtitle}</T> : null}
        </View>
        {value ? <T variant="body" tone={valueTone} lines={1} style={s.value}>{value}</T> : null}
        {trailing}
        {accessory === 'check' ? <Icon name="check" size={17} color={c.tint} /> : null}
        {showChevron ? <Icon name="chevronRight" size={14} color={c.label3} weight="bold" /> : null}
      </View>
    </View>
  );

  if (!onPress) return content(false);
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel ?? title}
      onPress={() => { tap('select'); onPress(); }}
    >
      {({ pressed }) => content(pressed)}
    </Pressable>
  );
}

const useRowStyles = makeStyles((c) => ({
  row: { flexDirection: 'row', alignItems: 'stretch', paddingLeft: space.lg, minHeight: TOUCH + 4 },
  lead: { justifyContent: 'center', marginRight: space.md },
  main: {
    flex: 1, flexDirection: 'row', alignItems: 'center', gap: space.sm,
    paddingRight: space.lg, minHeight: TOUCH + 4,
  },
  separator: { borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: c.separator },
  value: { maxWidth: '55%', textAlign: 'right' },
}));

/** A row holding a switch. */
export function ToggleRow({
  title, subtitle, value, onChange, icon, hue, first,
}: {
  title: string; subtitle?: string; value: boolean; onChange: (v: boolean) => void;
  icon?: IconName; hue?: Hue; first?: boolean;
}) {
  const { c } = useTheme();
  return (
    <ListRow
      title={title}
      subtitle={subtitle}
      icon={icon}
      hue={hue}
      first={first}
      accessory="none"
      trailing={
        <Switch
          value={value}
          onValueChange={(v) => { tap('select'); onChange(v); }}
          trackColor={{ true: c.green, false: c.fill }}
          thumbColor="#FFFFFF"
          {...(Platform.OS === 'web' ? { activeThumbColor: '#FFFFFF' } as object : {})}
        />
      }
    />
  );
}

/* ───────────────────────────── controls ───────────────────────────── */

type ButtonVariant = 'filled' | 'tinted' | 'gray' | 'plain' | 'destructive' | 'glass';
type ButtonSize = 'large' | 'medium' | 'small';

/**
 * A capsule button.
 *
 * `filled` is the one primary action on a screen; `tinted` and `gray` are the
 * supporting ones; `glass` floats over content.
 */
export function Button({
  label, onPress, variant = 'gray', size = 'large', icon, disabled, busy, style, hue, full,
}: {
  label: string;
  onPress: () => void;
  variant?: ButtonVariant;
  size?: ButtonSize;
  icon?: IconName;
  disabled?: boolean;
  busy?: boolean;
  style?: StyleProp<ViewStyle>;
  /** Overrides the tint for a filled or tinted button. */
  hue?: Hue;
  /** Stretches to the container's width. */
  full?: boolean;
}) {
  const { c } = useTheme();
  const inactive = disabled || busy;
  const accent = hue ? toneColor(c, hue) : c.tint;

  const bg =
    variant === 'filled' ? accent :
    variant === 'tinted' ? wash(accent, 0.15) :
    variant === 'gray' ? c.fill2 :
    variant === 'destructive' ? wash(c.red, 0.14) : 'transparent';
  const fg =
    variant === 'filled' ? c.onTint :
    variant === 'destructive' ? c.red :
    variant === 'gray' || variant === 'glass' ? c.label : accent;

  const height = size === 'large' ? 52 : size === 'medium' ? 40 : 32;
  const text: TypeStyle = size === 'small' ? 'subhead' : 'headline';

  const inner = busy
    ? <ActivityIndicator color={fg} />
    : (
      <HStack gap={6} justify="center">
        {icon ? <Icon name={icon} size={size === 'small' ? 14 : 17} color={fg} /> : null}
        <Text style={[typography[text], { color: fg }]} numberOfLines={1}>{label}</Text>
      </HStack>
    );

  const shape: ViewStyle = {
    height, borderRadius: radius.pill, paddingHorizontal: size === 'small' ? 14 : 22,
    alignItems: 'center', justifyContent: 'center', alignSelf: full ? 'stretch' : undefined,
  };

  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ disabled: !!inactive, busy: !!busy }}
      disabled={inactive}
      onPress={() => { tap(variant === 'filled' ? 'medium' : 'light'); onPress(); }}
      style={({ pressed }) => [
        { opacity: inactive ? 0.4 : pressed ? 0.7 : 1, transform: [{ scale: pressed ? 0.97 : 1 }] },
        full && { alignSelf: 'stretch' },
        style,
      ]}
    >
      {variant === 'glass'
        ? <Glass interactive style={shape}>{inner}</Glass>
        : <View style={[shape, { backgroundColor: bg }]}>{inner}</View>}
    </Pressable>
  );
}

/** A circular icon button — glass by default, for headers and floating controls. */
export function IconButton({
  icon, onPress, label, variant = 'glass', size = 40, hue, badge,
}: {
  icon: IconName;
  onPress: () => void;
  label: string;
  variant?: 'glass' | 'gray' | 'filled' | 'tinted';
  size?: number;
  hue?: Hue;
  badge?: number;
}) {
  const { c } = useTheme();
  const accent = hue ? toneColor(c, hue) : c.tint;
  const fg = variant === 'filled' ? c.onTint : variant === 'tinted' ? accent : c.label;
  const shape: ViewStyle = { width: size, height: size, borderRadius: size / 2, alignItems: 'center', justifyContent: 'center' };
  const glyph = <Icon name={icon} size={size * 0.45} color={fg} />;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      hitSlop={6}
      onPress={() => { tap(); onPress(); }}
      style={({ pressed }) => ({ opacity: pressed ? 0.7 : 1, transform: [{ scale: pressed ? 0.94 : 1 }] })}
    >
      {variant === 'glass'
        ? <Glass interactive style={shape}>{glyph}</Glass>
        : (
          <View style={[shape, {
            backgroundColor: variant === 'filled' ? accent : variant === 'tinted' ? wash(accent, 0.15) : c.fill2,
          }]}>{glyph}</View>
        )}
      {badge ? (
        <View style={{
          position: 'absolute', top: -2, right: -2, minWidth: 18, height: 18, borderRadius: 9,
          backgroundColor: c.red, alignItems: 'center', justifyContent: 'center', paddingHorizontal: 4,
        }}>
          <Text style={{ color: '#FFF', fontSize: 11, fontWeight: '700' }}>{badge > 99 ? '99+' : badge}</Text>
        </View>
      ) : null}
    </Pressable>
  );
}

/** A small capsule label: a status, a count, a category. */
export function Tag({ label, hue = 'gray', icon, solid }: { label: string; hue?: Hue; icon?: IconName; solid?: boolean }) {
  const { c } = useTheme();
  const color = toneColor(c, hue);
  return (
    <View style={{
      flexDirection: 'row', alignItems: 'center', gap: 4,
      paddingHorizontal: 8, paddingVertical: 3, borderRadius: radius.pill,
      backgroundColor: solid ? color : wash(color, 0.15),
    }}>
      {icon ? <Icon name={icon} size={11} color={solid ? '#FFF' : color} /> : null}
      <Text style={[typography.caption, { color: solid ? '#FFF' : color, fontWeight: '600' }]}>{label}</Text>
    </View>
  );
}

const AVATAR_HUES: Hue[] = ['tint', 'indigo', 'purple', 'pink', 'orange', 'teal', 'green', 'cyan'];

/** Initials on a colour picked from the name, so a person keeps theirs. */
export function Avatar({ name, size = 40 }: { name: string; size?: number }) {
  const { c } = useTheme();
  const initials = name.split(/\s+/).filter(Boolean).slice(0, 2).map((p) => p[0]!.toUpperCase()).join('') || '?';
  let hash = 0;
  for (let i = 0; i < name.length; i++) hash = (hash * 31 + name.charCodeAt(i)) | 0;
  const color = toneColor(c, AVATAR_HUES[Math.abs(hash) % AVATAR_HUES.length]!);
  return (
    <View style={{
      width: size, height: size, borderRadius: size / 2, backgroundColor: wash(color, c.scheme === 'dark' ? 0.3 : 0.18),
      alignItems: 'center', justifyContent: 'center',
    }}>
      <Text style={{ color, fontSize: size * 0.38, fontWeight: '700' }}>{initials}</Text>
    </View>
  );
}

/** The iOS segmented control. */
export function Segmented<V extends string>({
  options, value, onChange, style,
}: {
  options: readonly { value: V; label: string }[];
  value: V;
  onChange: (v: V) => void;
  style?: StyleProp<ViewStyle>;
}) {
  const s = useSegStyles();
  return (
    <View style={[s.track, style]} accessibilityRole="tablist">
      {options.map((o) => {
        const on = o.value === value;
        return (
          <Pressable
            key={o.value}
            accessibilityRole="tab"
            accessibilityState={{ selected: on }}
            onPress={() => { if (!on) { tap('select'); onChange(o.value); } }}
            style={[s.seg, on && s.on]}
          >
            <T variant="subhead" weight={on ? '600' : '500'} lines={1}>{o.label}</T>
          </Pressable>
        );
      })}
    </View>
  );
}

const useSegStyles = makeStyles((c) => ({
  track: { flexDirection: 'row', backgroundColor: c.fill2, borderRadius: radius.pill, padding: 3 },
  seg: { flex: 1, height: 32, alignItems: 'center', justifyContent: 'center', borderRadius: radius.pill, paddingHorizontal: 6 },
  on: {
    backgroundColor: c.scheme === 'dark' ? '#636366' : '#FFFFFF',
    shadowColor: '#000', shadowOpacity: 0.12, shadowRadius: 6, shadowOffset: { width: 0, height: 2 }, elevation: 2,
  },
}));

/** Horizontally scrolling filter chips. */
export function ChipBar<V extends string>({
  options, value, onChange,
}: {
  options: readonly { value: V; label: string; count?: number }[];
  value: V;
  onChange: (v: V) => void;
}) {
  const { c } = useTheme();
  return (
    <Animated.ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ paddingHorizontal: space.lg, gap: space.sm }}>
      {options.map((o) => {
        const on = o.value === value;
        return (
          <Pressable
            key={o.value}
            accessibilityRole="tab"
            accessibilityState={{ selected: on }}
            onPress={() => { tap('select'); onChange(o.value); }}
            style={({ pressed }) => ({
              flexDirection: 'row', alignItems: 'center', gap: 6,
              height: 34, paddingHorizontal: 14, borderRadius: radius.pill,
              backgroundColor: on ? c.label : c.surface, opacity: pressed ? 0.7 : 1,
            })}
          >
            <Text style={[typography.subhead, { color: on ? c.bg : c.label, fontWeight: '600' }]}>{o.label}</Text>
            {o.count !== undefined ? (
              <Text style={[typography.footnote, { color: on ? c.bg : c.label2, opacity: 0.8 }]}>{o.count}</Text>
            ) : null}
          </Pressable>
        );
      })}
    </Animated.ScrollView>
  );
}

/* ────────────────────────────── inputs ────────────────────────────── */

/** A labelled text field. */
export function Field({
  label, value, onChangeText, placeholder, keyboardType, secure, autoCapitalize, hint,
  style, multiline, autoFocus,
}: {
  label?: string;
  value: string;
  onChangeText: (t: string) => void;
  placeholder?: string;
  keyboardType?: KeyboardTypeOptions;
  secure?: boolean;
  autoCapitalize?: 'none' | 'words' | 'sentences';
  hint?: string;
  style?: StyleProp<ViewStyle>;
  multiline?: boolean;
  autoFocus?: boolean;
}) {
  const s = useFieldStyles();
  const { c } = useTheme();
  return (
    <View style={style}>
      {label ? <T variant="footnote" tone="label2" weight="500" style={s.label}>{label}</T> : null}
      <TextInput
        accessibilityLabel={label ?? placeholder}
        style={[s.input, multiline && s.multiline]}
        value={value}
        onChangeText={onChangeText}
        placeholder={placeholder}
        placeholderTextColor={c.label3}
        keyboardType={keyboardType}
        secureTextEntry={secure}
        autoCapitalize={autoCapitalize ?? 'sentences'}
        autoCorrect={false}
        multiline={multiline}
        autoFocus={autoFocus}
      />
      {hint ? <T variant="footnote" tone="label2" style={s.hint}>{hint}</T> : null}
    </View>
  );
}

/** A field inside a grouped list: label on the left, input filling the row. */
export function FieldRow({
  label, value, onChangeText, placeholder, keyboardType, autoCapitalize, first, secure, suffix,
}: {
  label: string;
  value: string;
  onChangeText: (t: string) => void;
  placeholder?: string;
  keyboardType?: KeyboardTypeOptions;
  autoCapitalize?: 'none' | 'words' | 'sentences';
  first?: boolean;
  secure?: boolean;
  suffix?: string;
}) {
  const s = useRowStyles();
  const f = useFieldStyles();
  const { c } = useTheme();
  return (
    <View style={s.row}>
      <View style={[s.main, !first && s.separator]}>
        <T variant="body" style={{ width: 110 }}>{label}</T>
        <TextInput
          accessibilityLabel={label}
          style={f.inline}
          value={value}
          onChangeText={onChangeText}
          placeholder={placeholder}
          placeholderTextColor={c.label3}
          keyboardType={keyboardType}
          autoCapitalize={autoCapitalize ?? 'sentences'}
          autoCorrect={false}
          secureTextEntry={secure}
        />
        {suffix ? <T variant="body" tone="label2">{suffix}</T> : null}
      </View>
    </View>
  );
}

/** A big numeric field for loads and reps, read at arm's length. */
export function NumberField({
  label, value, onChangeText, placeholder, style, unit,
}: {
  label: string;
  value: string;
  onChangeText: (t: string) => void;
  placeholder?: string;
  style?: StyleProp<ViewStyle>;
  unit?: string;
}) {
  const s = useFieldStyles();
  const { c } = useTheme();
  return (
    <View style={[{ flex: 1 }, style]}>
      <T variant="caption2" tone="label2" align="center" style={{ marginBottom: 4, letterSpacing: 0.5 }}>
        {(unit ? `${label} · ${unit}` : label).toUpperCase()}
      </T>
      <TextInput
        accessibilityLabel={label}
        style={s.number}
        value={value}
        onChangeText={onChangeText}
        placeholder={placeholder}
        placeholderTextColor={c.label3}
        keyboardType="decimal-pad"
        selectTextOnFocus
      />
    </View>
  );
}

/** A search field in the iOS shape. */
export function SearchField({
  value, onChangeText, placeholder = 'Search', style,
}: { value: string; onChangeText: (t: string) => void; placeholder?: string; style?: StyleProp<ViewStyle> }) {
  const { c } = useTheme();
  const s = useFieldStyles();
  return (
    <View style={[s.search, style]}>
      <Icon name="search" size={16} color={c.label2} />
      <TextInput
        accessibilityLabel={placeholder}
        style={s.searchInput}
        value={value}
        onChangeText={onChangeText}
        placeholder={placeholder}
        placeholderTextColor={c.label2}
        autoCapitalize="none"
        autoCorrect={false}
        returnKeyType="search"
        clearButtonMode="while-editing"
      />
      {value && Platform.OS !== 'ios' ? (
        <Pressable accessibilityLabel="Clear search" onPress={() => onChangeText('')} hitSlop={8}>
          <Icon name="closeCircle" size={16} color={c.label3} />
        </Pressable>
      ) : null}
    </View>
  );
}

/**
 * A stepper: a figure between minus and plus.
 *
 * Mid-set, a trainer nudges a load by a plate rather than typing it, so these
 * are big and the step is the smallest real increment.
 */
export function Stepper({
  label, value, onChange, step = 1, min = 0, format, unit,
}: {
  label: string;
  value: number;
  onChange: (v: number) => void;
  step?: number;
  min?: number;
  format?: (v: number) => string;
  unit?: string;
}) {
  const { c } = useTheme();
  const s = useStepperStyles();
  const set = (v: number) => { tap('select'); onChange(Math.max(min, Math.round(v * 1000) / 1000)); };
  return (
    <View style={s.wrap}>
      <T variant="caption2" tone="label2" align="center" style={{ letterSpacing: 0.5 }}>
        {(unit ? `${label} · ${unit}` : label).toUpperCase()}
      </T>
      <HStack gap={0} justify="space-between" style={{ marginTop: 6 }}>
        <Pressable accessibilityRole="button" accessibilityLabel={`Decrease ${label}`} onPress={() => set(value - step)} style={({ pressed }) => [s.btn, pressed && { opacity: 0.6 }]}>
          <Icon name="minus" size={18} color={c.label} />
        </Pressable>
        <T variant="metric" align="center" style={{ flex: 1 }} lines={1}>{format ? format(value) : String(value)}</T>
        <Pressable accessibilityRole="button" accessibilityLabel={`Increase ${label}`} onPress={() => set(value + step)} style={({ pressed }) => [s.btn, pressed && { opacity: 0.6 }]}>
          <Icon name="plus" size={18} color={c.label} />
        </Pressable>
      </HStack>
    </View>
  );
}

const useStepperStyles = makeStyles((c) => ({
  wrap: { flex: 1, backgroundColor: c.surface2, borderRadius: radius.lg, padding: space.md },
  btn: { width: TOUCH, height: TOUCH, borderRadius: TOUCH / 2, backgroundColor: c.fill2, alignItems: 'center', justifyContent: 'center' },
}));

const useFieldStyles = makeStyles((c) => ({
  label: { marginBottom: 6, marginLeft: 4 },
  hint: { marginTop: 6, marginLeft: 4 },
  input: {
    minHeight: 50, borderRadius: radius.md, backgroundColor: c.surface,
    color: c.label, paddingHorizontal: space.lg, ...typography.body,
  },
  multiline: { minHeight: 100, paddingTop: 14, textAlignVertical: 'top' },
  inline: { flex: 1, color: c.label, ...typography.body, minHeight: TOUCH, textAlign: 'right' },
  number: {
    minHeight: TOUCH_FLOOR + 8, borderRadius: radius.md, backgroundColor: c.surface2,
    color: c.label, textAlign: 'center', ...typography.metric,
  },
  search: {
    flexDirection: 'row', alignItems: 'center', gap: 6, height: 38,
    borderRadius: radius.pill, backgroundColor: c.fill2, paddingHorizontal: 12,
  },
  searchInput: { flex: 1, color: c.label, ...typography.body, height: 38, padding: 0 },
}));

/* ───────────────────────────── feedback ───────────────────────────── */

/** Shown where a list is legitimately empty, rather than a blank screen. */
export function EmptyState({
  icon = 'sparkles', title, message, action, onAction, hue = 'tint',
}: {
  icon?: IconName;
  title: string;
  message?: string;
  action?: string;
  onAction?: () => void;
  hue?: Hue;
}) {
  return (
    <VStack align="center" gap={space.sm} style={{ paddingVertical: space.xxl, paddingHorizontal: space.xl }}>
      <IconBubble icon={icon} hue={hue} size={56} />
      <Spacer size={space.xs} />
      <T variant="headline" align="center">{title}</T>
      {message ? <T variant="subhead" tone="label2" align="center">{message}</T> : null}
      {action && onAction ? (
        <>
          <Spacer size={space.xs} />
          <Button label={action} onPress={onAction} variant="tinted" size="medium" />
        </>
      ) : null}
    </VStack>
  );
}

/** Something the trainer needs told, in place rather than in an alert. */
export function Banner({
  message, title, tone = 'orange', icon, action, onAction, style,
}: {
  message: string;
  title?: string;
  tone?: Hue;
  icon?: IconName;
  action?: string;
  onAction?: () => void;
  style?: StyleProp<ViewStyle>;
}) {
  const { c } = useTheme();
  const color = toneColor(c, tone);
  return (
    <View style={[{
      flexDirection: 'row', gap: space.md, padding: space.lg, borderRadius: radius.lg,
      backgroundColor: wash(color, c.scheme === 'dark' ? 0.18 : 0.12),
    }, style]}>
      <Icon name={icon ?? (tone === 'red' ? 'warning' : tone === 'green' ? 'checkCircle' : 'info')} size={20} color={color} />
      <View style={{ flex: 1, gap: 2 }}>
        {title ? <T variant="headline">{title}</T> : null}
        <T variant="subhead" tone={title ? 'label2' : 'label'}>{message}</T>
        {action && onAction ? (
          <Pressable accessibilityRole="button" onPress={onAction} hitSlop={8} style={{ marginTop: 6 }}>
            <T variant="subhead" weight="600" style={{ color }}>{action}</T>
          </Pressable>
        ) : null}
      </View>
    </View>
  );
}

/** A thin track showing how far through something is. */
export function ProgressBar({ value, hue = 'tint', height = 6 }: { value: number; hue?: Hue; height?: number }) {
  const { c } = useTheme();
  const v = Math.max(0, Math.min(1, Number.isFinite(value) ? value : 0));
  return (
    <View
      accessibilityRole="progressbar"
      accessibilityValue={{ min: 0, max: 100, now: Math.round(v * 100) }}
      style={{ height, borderRadius: height, backgroundColor: c.fill2, overflow: 'hidden' }}
    >
      <View style={{ width: `${v * 100}%`, height: '100%', borderRadius: height, backgroundColor: toneColor(c, hue) }} />
    </View>
  );
}

/** A hairline, inset like a list separator. */
export function Divider({ inset = space.lg }: { inset?: number }) {
  const { c } = useTheme();
  return <View style={{ height: StyleSheet.hairlineWidth, backgroundColor: c.separator, marginLeft: inset }} />;
}

/**
 * A figure with its label — one cell of a dashboard row.
 */
export function Stat({
  label, value, unit, icon, hue = 'tint', delta, deltaGood = true, onPress, style, footnote,
}: {
  label: string;
  value: string;
  unit?: string;
  icon?: IconName;
  hue?: Hue;
  /** e.g. "+12%" — coloured green or red by `deltaGood`. */
  delta?: string;
  deltaGood?: boolean;
  onPress?: () => void;
  style?: StyleProp<ViewStyle>;
  footnote?: string;
}) {
  const { c } = useTheme();
  return (
    <Card onPress={onPress} style={[{ flex: 1, minWidth: 140 }, style]} accessibilityLabel={`${label}: ${value}${unit ? ` ${unit}` : ''}`}>
      <HStack gap={6}>
        {icon ? <Icon name={icon} size={14} color={toneColor(c, hue)} /> : null}
        <T variant="footnote" weight="600" style={{ color: toneColor(c, hue) }} lines={1}>{label}</T>
      </HStack>
      <Spacer size={6} />
      <HStack gap={4} align="baseline">
        <T variant="metricSmall" lines={1}>{value}</T>
        {unit ? <T variant="footnote" tone="label2">{unit}</T> : null}
      </HStack>
      {delta || footnote ? (
        <HStack gap={6} style={{ marginTop: 2 }}>
          {delta ? <T variant="footnote" weight="600" tone={deltaGood ? 'green' : 'red'}>{delta}</T> : null}
          {footnote ? <T variant="footnote" tone="label2" lines={1}>{footnote}</T> : null}
        </HStack>
      ) : null}
    </Card>
  );
}

/** Wraps a set of Stats into rows of two. */
export function StatGrid({ children }: { children: React.ReactNode }) {
  return <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: space.md, marginHorizontal: space.lg }}>{children}</View>;
}
