/**
 * Choosing from a list, a date, or a date range.
 *
 * Built from the app's own sheet and month grid rather than the platform
 * pickers: those differ on iOS, Android and the web in look, in first
 * weekday and in how they handle Arabic, and a trainer moving between phone
 * and laptop should meet one control.
 */

import React, { useMemo, useState } from 'react';
import { Pressable, Text, View } from 'react-native';

import { useT, type TKey } from '@/i18n';
import {
  addDays, isoDay, monthGrid, parseDay, presetRange, type DayRange, type RangePreset, type Weekday,
} from './calendar-logic';
import { Button, IconButton, Row, SearchField, Spacer } from './components';
import { Icon } from './icon';
import { Sheet } from './overlay';
import { radius, space, TOUCH_TARGET } from './theme';
import { makeStyles, useTheme } from './theming';

export interface Option<T extends string> {
  value: T;
  label: string;
  detail?: string;
}

/** A field that opens a list. Searchable once the list is long enough to need it. */
export function Select<T extends string>({
  label, value, options, onChange, placeholder, compact,
}: {
  label: string;
  value: T | null;
  options: readonly Option<T>[];
  onChange: (value: T) => void;
  placeholder?: string;
  compact?: boolean;
}) {
  const styles = useStyles();
  const { colors } = useTheme();
  const { t } = useT();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const current = options.find((o) => o.value === value);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return options;
    return options.filter((o) => o.label.toLowerCase().includes(q) || o.detail?.toLowerCase().includes(q));
  }, [options, query]);

  return (
    <View>
      {compact ? null : <Text style={styles.fieldLabel}>{label}</Text>}
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={`${label}: ${current?.label ?? placeholder ?? ''}`}
        onPress={() => setOpen(true)}
        style={({ pressed }) => [styles.field, compact && styles.fieldCompact, pressed && { opacity: 0.7 }]}
      >
        <Text style={[styles.value, !current && styles.placeholder]} numberOfLines={1}>
          {current?.label ?? placeholder ?? t('picker.select')}
        </Text>
        <Icon name="expand" size={16} color={colors.inkMuted} />
      </Pressable>

      <Sheet visible={open} onClose={() => { setOpen(false); setQuery(''); }} title={label} width={440}>
        {options.length > 8 ? (
          <>
            <SearchField value={query} onChangeText={setQuery} label={t('common.search')} placeholder={t('common.search')} />
            <Spacer size={space.sm} />
          </>
        ) : null}
        {filtered.length === 0 ? <Text style={styles.detail}>{t('picker.noOptions')}</Text> : null}
        {filtered.map((option) => {
          const selected = option.value === value;
          return (
            <Pressable
              key={option.value}
              accessibilityRole="radio"
              accessibilityState={{ selected }}
              onPress={() => { onChange(option.value); setOpen(false); setQuery(''); }}
              style={({ pressed }) => [styles.option, selected && styles.optionSelected, pressed && { opacity: 0.7 }]}
            >
              <View style={{ flex: 1 }}>
                <Text style={styles.optionLabel}>{option.label}</Text>
                {option.detail ? <Text style={styles.detail}>{option.detail}</Text> : null}
              </View>
              {selected ? <Icon name="check" size={18} color={colors.accentInk} /> : null}
            </Pressable>
          );
        })}
      </Sheet>
    </View>
  );
}

/** A month you can page through, with one day chosen. */
export function MonthPicker({
  value, onChange, weekStartsOn = 1, min, max,
}: {
  value: string | null;
  onChange: (day: string) => void;
  weekStartsOn?: Weekday;
  min?: string;
  max?: string;
}) {
  const styles = useStyles();
  const { t, date } = useT();
  const initial = value ? parseDay(value) : new Date();
  const [cursor, setCursor] = useState({ year: initial.getFullYear(), month: initial.getMonth() });
  const weeks = monthGrid(cursor.year, cursor.month, weekStartsOn);
  const today = isoDay(new Date());

  const shift = (delta: number) => {
    const d = new Date(cursor.year, cursor.month + delta, 1);
    setCursor({ year: d.getFullYear(), month: d.getMonth() });
  };

  return (
    <View>
      <Row style={{ justifyContent: 'space-between' }}>
        <IconButton icon="previous" label={t('picker.prevMonth')} onPress={() => shift(-1)} tone="quiet" size={36} />
        <Text style={styles.monthTitle}>{date(new Date(cursor.year, cursor.month, 1), 'month')}</Text>
        <IconButton icon="next" label={t('picker.nextMonth')} onPress={() => shift(1)} tone="quiet" size={36} />
      </Row>
      <View style={styles.weekRow}>
        {weeks[0]!.map((d) => (
          <Text key={d.getDay()} style={styles.weekday}>{date(d, 'weekday')}</Text>
        ))}
      </View>
      {weeks.map((week) => (
        <View key={isoDay(week[0]!)} style={styles.weekRow}>
          {week.map((d) => {
            const day = isoDay(d);
            const outside = d.getMonth() !== cursor.month;
            const disabled = (min !== undefined && day < min) || (max !== undefined && day > max);
            const selected = day === value;
            return (
              <Pressable
                key={day}
                disabled={disabled}
                accessibilityRole="button"
                accessibilityState={{ selected, disabled }}
                accessibilityLabel={date(d, 'dayMonth')}
                onPress={() => onChange(day)}
                style={[styles.day, selected && styles.daySelected, day === today && !selected && styles.dayToday]}
              >
                <Text style={[styles.dayLabel, outside && styles.dayOutside, selected && styles.dayLabelSelected, disabled && { opacity: 0.3 }]}>
                  {d.getDate()}
                </Text>
              </Pressable>
            );
          })}
        </View>
      ))}
    </View>
  );
}

/** A date field: tap, pick a day from a month. */
export function DateField({
  label, value, onChange, weekStartsOn, min, max, clearable,
}: {
  label: string;
  value: string | null;
  onChange: (day: string | null) => void;
  weekStartsOn?: Weekday;
  min?: string;
  max?: string;
  clearable?: boolean;
}) {
  const styles = useStyles();
  const { colors } = useTheme();
  const { t, date } = useT();
  const [open, setOpen] = useState(false);

  return (
    <View>
      <Text style={styles.fieldLabel}>{label}</Text>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={`${label}: ${value ? date(parseDay(value), 'dayMonth') : t('picker.select')}`}
        onPress={() => setOpen(true)}
        style={({ pressed }) => [styles.field, pressed && { opacity: 0.7 }]}
      >
        <Text style={[styles.value, !value && styles.placeholder]}>
          {value ? date(parseDay(value), 'dayMonth') : t('picker.select')}
        </Text>
        <Icon name="calendar" size={16} color={colors.inkMuted} />
      </Pressable>
      <Sheet visible={open} onClose={() => setOpen(false)} title={label} width={400}>
        <MonthPicker
          value={value}
          weekStartsOn={weekStartsOn}
          min={min}
          max={max}
          onChange={(day) => { onChange(day); setOpen(false); }}
        />
        <Spacer />
        <Row>
          <Button label={t('picker.today')} compact style={{ flex: 1 }} onPress={() => { onChange(isoDay(new Date())); setOpen(false); }} />
          {clearable ? <Button label={t('picker.clear')} compact tone="quiet" onPress={() => { onChange(null); setOpen(false); }} /> : null}
        </Row>
      </Sheet>
    </View>
  );
}

const PRESETS: { value: RangePreset; key: TKey }[] = [
  { value: 'last7', key: 'range.last7' },
  { value: 'last30', key: 'range.last30' },
  { value: 'last90', key: 'range.last90' },
  { value: 'monthToDate', key: 'range.monthToDate' },
  { value: 'lastMonth', key: 'range.lastMonth' },
  { value: 'quarterToDate', key: 'range.quarterToDate' },
  { value: 'yearToDate', key: 'range.yearToDate' },
];

/**
 * The date filter every analytics view shares: preset rows first, because
 * "last 30 days" is what a trainer means nearly every time.
 */
export function DateRangeField({
  value, onChange, label,
}: { value: DayRange & { preset?: RangePreset }; onChange: (range: DayRange & { preset?: RangePreset }) => void; label: string }) {
  const styles = useStyles();
  const { colors } = useTheme();
  const { t, date } = useT();
  const [open, setOpen] = useState(false);
  const [custom, setCustom] = useState<{ from: string | null; to: string | null }>({ from: null, to: null });

  const summary = value.preset
    ? t(PRESETS.find((p) => p.value === value.preset)?.key ?? 'range.custom')
    : `${date(parseDay(value.from), 'short')} – ${date(parseDay(value.to), 'short')}`;

  return (
    <View>
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={`${label}: ${summary}`}
        onPress={() => setOpen(true)}
        style={({ pressed }) => [styles.field, styles.fieldCompact, pressed && { opacity: 0.7 }]}
      >
        <Icon name="calendar" size={16} color={colors.inkMuted} />
        <Text style={styles.value} numberOfLines={1}>{summary}</Text>
        <Icon name="expand" size={16} color={colors.inkMuted} />
      </Pressable>
      <Sheet visible={open} onClose={() => setOpen(false)} title={label} width={420}>
        {PRESETS.map((preset) => {
          const selected = value.preset === preset.value;
          return (
            <Pressable
              key={preset.value}
              accessibilityRole="radio"
              accessibilityState={{ selected }}
              onPress={() => { onChange({ ...presetRange(preset.value), preset: preset.value }); setOpen(false); }}
              style={({ pressed }) => [styles.option, pressed && { opacity: 0.7 }]}
            >
              <Text style={[styles.optionLabel, selected && { fontWeight: '800' }]}>{t(preset.key)}</Text>
              {selected ? <Icon name="check" size={16} color={colors.ink} strokeWidth={3} /> : null}
            </Pressable>
          );
        })}
        <Spacer />
        <Text style={styles.fieldLabel}>{t('range.custom')}</Text>
        <MonthPicker
          value={custom.to ?? custom.from}
          onChange={(day) => {
            if (!custom.from || custom.to) setCustom({ from: day, to: null });
            else {
              const [from, to] = day < custom.from ? [day, custom.from] : [custom.from, day];
              setCustom({ from, to });
              onChange({ from, to });
              setOpen(false);
            }
          }}
        />
        <Text style={styles.detail}>
          {custom.from && !custom.to ? t('range.pickEnd', { date: date(parseDay(custom.from), 'short') }) : t('range.pickStart')}
        </Text>
      </Sheet>
    </View>
  );
}

/** The range before this one, of the same length — the comparison a trend line quotes. */
export function previousRange(range: DayRange): DayRange {
  const from = parseDay(range.from);
  const to = parseDay(range.to);
  const days = Math.round((to.getTime() - from.getTime()) / 86_400_000) + 1;
  return { from: isoDay(addDays(from, -days)), to: isoDay(addDays(from, -1)) };
}

const useStyles = makeStyles(({ colors, type }) => ({
  fieldLabel: { ...type.label, color: colors.inkMuted, marginBottom: space.sm },
  field: {
    minHeight: TOUCH_TARGET, borderRadius: radius.md, backgroundColor: colors.surfaceRaised,
    paddingHorizontal: space.lg, flexDirection: 'row', alignItems: 'center', gap: space.sm,
  },
  fieldCompact: { minHeight: 40, borderRadius: radius.pill },
  value: { ...type.body, color: colors.ink, flex: 1 },
  placeholder: { color: colors.inkMuted },
  option: {
    flexDirection: 'row', alignItems: 'center', gap: space.md, minHeight: 48,
    paddingHorizontal: space.md, borderRadius: radius.md,
  },
  optionSelected: { backgroundColor: colors.accentSoft },
  optionLabel: { ...type.body, color: colors.ink },
  detail: { ...type.caption, color: colors.inkMuted, marginTop: space.xs },
  monthTitle: { ...type.heading, color: colors.ink },
  weekRow: { flexDirection: 'row', justifyContent: 'space-between', marginTop: space.xs },
  weekday: { ...type.caption, color: colors.inkMuted, width: 40, textAlign: 'center' },
  day: { width: 40, height: 40, borderRadius: radius.pill, alignItems: 'center', justifyContent: 'center' },
  daySelected: { backgroundColor: colors.accent },
  dayToday: { borderWidth: 1, borderColor: colors.accentInk },
  dayLabel: { ...type.small, color: colors.ink },
  dayOutside: { color: colors.inkMuted, opacity: 0.5 },
  dayLabelSelected: { color: colors.onAccent, fontWeight: '800' },
}));
