/**
 * A list that is a table on a desk and a stack of cards on a phone.
 *
 * The same rows and the same actions either way — the columns a trainer
 * scans across on a laptop become the lines of a card they scroll through
 * with a thumb. A screen supplies both renderings once; the width decides
 * which one draws.
 *
 * Sorting is by header tap: ascending, descending, off. Long lists page with
 * "Show more" rather than virtualising — the rosters here are hundreds of
 * rows, not millions, and a plain list keeps find-in-page working on the web.
 */

import React, { useMemo, useState } from 'react';
import { Pressable, ScrollView, Text, View } from 'react-native';

import { useT } from '@/i18n';
import { Button, Empty, Spacer } from './components';
import { Icon, type IconName } from './icon';
import { useLayout } from './layout';
import { nextSort, sortRows, type SortDirection, type SortValue } from './table-logic';
import { radius, space } from './theme';
import { makeStyles, useTheme } from './theming';

export interface Column<T> {
  key: string;
  label: string;
  /** Relative width; columns share the row by flex. */
  flex?: number;
  /** Fixed width in points, for a status pill or an action. */
  width?: number;
  /** Numbers align to the end so their digits line up. */
  align?: 'start' | 'end' | 'center';
  render: (row: T) => React.ReactNode;
  sortValue?: (row: T) => SortValue;
}

const PAGE = 50;

export function DataTable<T>({
  rows, columns, keyOf, onRowPress, renderCard, empty, initialSort, loading,
}: {
  rows: readonly T[];
  columns: Column<T>[];
  keyOf: (row: T) => string;
  onRowPress?: (row: T) => void;
  /** The phone rendering of one row. */
  renderCard: (row: T) => React.ReactNode;
  empty: { title: string; detail?: string; icon?: IconName };
  initialSort?: { key: string; direction: SortDirection };
  loading?: boolean;
}) {
  const styles = useStyles();
  const { colors } = useTheme();
  const { wide } = useLayout();
  const { t, locale } = useT();
  const [sort, setSort] = useState<{ key: string; direction: SortDirection } | null>(initialSort ?? null);
  const [shown, setShown] = useState(PAGE);

  const sorted = useMemo(() => {
    if (!sort) return rows;
    const column = columns.find((c) => c.key === sort.key);
    if (!column?.sortValue) return rows;
    let collator: Intl.Collator | undefined;
    try {
      collator = new Intl.Collator(locale, { sensitivity: 'base', numeric: true });
    } catch {
      collator = undefined;
    }
    return sortRows(rows, column.sortValue, sort.direction, collator);
  }, [rows, columns, sort, locale]);

  if (rows.length === 0) {
    return <Empty title={loading ? t('common.loading') : empty.title} detail={loading ? undefined : empty.detail} icon={empty.icon} />;
  }

  const visible = sorted.slice(0, shown);
  const more = sorted.length - visible.length;

  if (!wide) {
    return (
      <View>
        {visible.map((row) => (
          <Pressable
            key={keyOf(row)}
            disabled={!onRowPress}
            onPress={onRowPress ? () => onRowPress(row) : undefined}
            accessibilityRole={onRowPress ? 'button' : undefined}
            style={({ pressed }) => [{ marginBottom: space.sm }, pressed && styles.pressed]}
          >
            {renderCard(row)}
          </Pressable>
        ))}
        {more > 0 ? <Button label={t('table.showMore', { count: Math.min(more, PAGE) })} onPress={() => setShown(shown + PAGE)} compact /> : null}
      </View>
    );
  }

  const cell = (column: Column<T>) => [
    styles.cell,
    column.width ? { width: column.width } : { flex: column.flex ?? 1 },
    column.align === 'end' ? styles.alignEnd : column.align === 'center' ? styles.alignCenter : null,
  ];

  return (
    <View style={styles.table}>
      <ScrollView horizontal contentContainerStyle={{ flexGrow: 1 }} showsHorizontalScrollIndicator={false}>
        <View style={{ flex: 1, minWidth: '100%' }}>
          <View style={[styles.row, styles.headerRow]} accessibilityRole="header">
            {columns.map((column) => {
              const active = sort?.key === column.key;
              const sortable = Boolean(column.sortValue);
              return (
                <Pressable
                  key={column.key}
                  disabled={!sortable}
                  onPress={() => setSort(nextSort(sort, column.key))}
                  accessibilityRole={sortable ? 'button' : undefined}
                  accessibilityLabel={column.label}
                  style={cell(column)}
                >
                  <View style={[styles.headerInner, column.align === 'end' && { justifyContent: 'flex-end' }]}>
                    <Text style={[styles.headerLabel, active && styles.headerLabelActive]} numberOfLines={1}>{column.label}</Text>
                    {active ? (
                      <Icon name={sort?.direction === 'asc' ? 'up' : 'down'} size={12} color={colors.ink} />
                    ) : null}
                  </View>
                </Pressable>
              );
            })}
          </View>
          {visible.map((row) => (
            <Pressable
              key={keyOf(row)}
              disabled={!onRowPress}
              onPress={onRowPress ? () => onRowPress(row) : undefined}
              accessibilityRole={onRowPress ? 'button' : undefined}
              style={({ pressed, hovered }: { pressed: boolean; hovered?: boolean }) => [
                styles.row, styles.bodyRow, (pressed || hovered) && onRowPress ? styles.rowHover : null,
              ]}
            >
              {columns.map((column) => (
                <View key={column.key} style={cell(column)}>
                  {(() => {
                    const content = column.render(row);
                    return typeof content === 'string' || typeof content === 'number'
                      ? <Text style={[styles.cellText, column.align === 'end' && styles.numeric]} numberOfLines={1}>{content}</Text>
                      : content;
                  })()}
                </View>
              ))}
            </Pressable>
          ))}
        </View>
      </ScrollView>
      {more > 0 ? (
        <View style={{ padding: space.md }}>
          <Button label={t('table.showMore', { count: Math.min(more, PAGE) })} onPress={() => setShown(shown + PAGE)} compact />
        </View>
      ) : null}
      <Spacer size={space.xs} />
    </View>
  );
}

const useStyles = makeStyles(({ colors, type }) => ({
  table: { backgroundColor: colors.surface, borderRadius: radius.lg, overflow: 'hidden', borderWidth: 1, borderColor: colors.border },
  row: { flexDirection: 'row', alignItems: 'center', paddingHorizontal: space.md },
  headerRow: { borderBottomWidth: 1, borderBottomColor: colors.border, minHeight: 44 },
  bodyRow: { minHeight: 52, borderBottomWidth: 1, borderBottomColor: colors.grid },
  rowHover: { backgroundColor: colors.surfaceRaised },
  cell: { paddingHorizontal: space.sm, paddingVertical: space.sm, justifyContent: 'center' },
  alignEnd: { alignItems: 'flex-end' },
  alignCenter: { alignItems: 'center' },
  headerInner: { flexDirection: 'row', alignItems: 'center', gap: 4 },
  headerLabel: { ...type.caption, color: colors.inkMuted },
  headerLabelActive: { color: colors.ink },
  cellText: { ...type.small, color: colors.ink },
  numeric: { fontVariant: ['tabular-nums'] },
  pressed: { opacity: 0.7 },
}));
