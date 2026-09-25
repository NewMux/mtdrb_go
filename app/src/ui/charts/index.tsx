/**
 * The chart kit.
 *
 * Small and in-house on react-native-svg, because it has to draw the same on
 * a phone and in a browser, survive the web export's static pre-render, and
 * follow one set of rules the charting libraries would each break
 * differently:
 *
 *   - colour follows the series, in the palette's fixed order, never its rank;
 *   - thin marks: 2px lines, bars rounded only at the value end, a 2px gap of
 *     surface between neighbouring fills;
 *   - a legend whenever there are two or more series, and a tooltip on hover
 *     or tap, so identity never rests on colour alone;
 *   - one y axis, always from zero for bars and areas.
 *
 * Plots stay left to right in Arabic too, which is how Gulf business charts
 * are read; the labels around them are translated and the legend follows the
 * page's direction.
 */

import React, { useMemo, useState } from 'react';
import { Pressable, Text, View, type LayoutChangeEvent } from 'react-native';
import Svg, { Circle, G, Line, Path, Rect, Text as SvgText } from 'react-native-svg';

import { useT } from '@/i18n';
import { radius, space } from '../theme';
import { makeStyles, useTheme } from '../theming';
import { arcPath, areaPath, bands, barPath, linearScale, linePath, niceTicks, thinLabels } from './geometry';

export interface Series {
  key: string;
  label: string;
  values: (number | null)[];
}

type Format = (value: number) => string;

const AXIS_WIDTH = 52;
const X_AXIS_HEIGHT = 24;
const TOP_PAD = 12;

function useWidth(): [number, (e: LayoutChangeEvent) => void] {
  const [width, setWidth] = useState(0);
  return [width, (e) => setWidth(Math.round(e.nativeEvent.layout.width))];
}

export function Legend({ items }: { items: { label: string; color: string; value?: string }[] }) {
  const styles = useStyles();
  const { t } = useT();
  return (
    <View style={styles.legend} accessibilityLabel={t('chart.legend')}>
      {items.map((item) => (
        <View key={item.label} style={styles.legendItem}>
          <View style={[styles.swatch, { backgroundColor: item.color }]} />
          <Text style={styles.legendLabel}>{item.label}</Text>
          {item.value ? <Text style={styles.legendValue}>{item.value}</Text> : null}
        </View>
      ))}
    </View>
  );
}

/** The card that follows the pointer: the hovered category and each series' value. */
function Tooltip({
  x, width, title, rows,
}: { x: number; width: number; title: string; rows: { label: string; color: string; value: string }[] }) {
  const styles = useStyles();
  const boxWidth = 168;
  const left = Math.max(0, Math.min(width - boxWidth, x - boxWidth / 2));
  return (
    <View pointerEvents="none" style={[styles.tooltip, { left, width: boxWidth }]}>
      <Text style={styles.tooltipTitle}>{title}</Text>
      {rows.map((row) => (
        <View key={row.label} style={styles.tooltipRow}>
          <View style={[styles.swatch, { backgroundColor: row.color }]} />
          <Text style={styles.tooltipLabel} numberOfLines={1}>{row.label}</Text>
          <Text style={styles.tooltipValue}>{row.value}</Text>
        </View>
      ))}
    </View>
  );
}

/** Invisible columns over the plot that pick the category under a finger or pointer. */
function HitBands({
  count, left, width, height, onActive,
}: { count: number; left: number; width: number; height: number; onActive: (i: number | null) => void }) {
  const step = count > 0 ? width / count : width;
  return (
    <View style={{ position: 'absolute', left, top: 0, width, height, flexDirection: 'row' }}>
      {Array.from({ length: count }, (_, i) => (
        <Pressable
          key={i}
          style={{ width: step, height }}
          onPressIn={() => onActive(i)}
          onHoverIn={() => onActive(i)}
          onHoverOut={() => onActive(null)}
          accessibilityElementsHidden
          importantForAccessibility="no"
        />
      ))}
    </View>
  );
}

/** System sans for text inside the SVG, which otherwise falls back to a serif. */
const FONT = 'system-ui, -apple-system, "Segoe UI", Roboto, sans-serif';

/**
 * The value axis. Ticks are compact ("12K") whatever the series format — a
 * full "AED 15,000.00" does not fit beside a plot, and the tooltip carries
 * the exact figure.
 */
function YAxis({ ticks, y, width }: { ticks: number[]; y: (v: number) => number; width: number }) {
  const { colors } = useTheme();
  const { compact } = useT();
  return (
    <G>
      {ticks.map((tick) => (
        <G key={tick}>
          <Line x1={AXIS_WIDTH} x2={width} y1={y(tick)} y2={y(tick)} stroke={tick === 0 ? colors.baseline : colors.grid} strokeWidth={1} />
          <SvgText x={AXIS_WIDTH - 8} y={y(tick) + 4} fontSize={11} fontFamily={FONT} fill={colors.inkMuted} textAnchor="end">
            {compact(tick)}
          </SvgText>
        </G>
      ))}
    </G>
  );
}

function XLabels({ labels, centre, plotWidth, y }: { labels: string[]; centre: (i: number) => number; plotWidth: number; y: number }) {
  const { colors } = useTheme();
  const keep = thinLabels(labels.length, plotWidth);
  return (
    <G>
      {keep.map((i) => (
        <SvgText key={i} x={AXIS_WIDTH + centre(i)} y={y} fontSize={11} fontFamily={FONT} fill={colors.inkMuted} textAnchor="middle">
          {labels[i]}
        </SvgText>
      ))}
    </G>
  );
}

function summarise(series: Series[], labels: string[], format: Format): string {
  return series
    .map((s) => {
      const last = [...s.values].reverse().find((v) => v !== null);
      return `${s.label}: ${labels.at(-1) ?? ''} ${last === undefined || last === null ? '—' : format(last)}`;
    })
    .join('; ');
}

/**
 * Change over time. With one series and `area`, the region under the line is
 * tinted — "how much" as well as "which way".
 */
export function LineChart({
  labels, series, format, height = 220, area,
}: {
  labels: string[];
  series: Series[];
  format: Format;
  height?: number;
  area?: boolean;
}) {
  const styles = useStyles();
  const { colors } = useTheme();
  const { t } = useT();
  const [width, onLayout] = useWidth();
  const [active, setActive] = useState<number | null>(null);

  const values = series.flatMap((s) => s.values.filter((v): v is number => v !== null));
  const ticks = useMemo(() => niceTicks(Math.min(0, ...values), Math.max(0, ...values)), [values.join(',')]);
  const plotWidth = Math.max(0, width - AXIS_WIDTH);
  const plotBottom = height - X_AXIS_HEIGHT;
  const y = linearScale([ticks[0]!, ticks.at(-1)!], [plotBottom, TOP_PAD]);
  const b = bands(labels.length, plotWidth, 0);

  const empty = values.length === 0;

  return (
    <View accessible accessibilityLabel={empty ? t('chart.noData') : summarise(series, labels, format)}>
      {series.length > 1 ? <Legend items={series.map((s, i) => ({ label: s.label, color: colors.series[i % 8]! }))} /> : null}
      <View onLayout={onLayout} style={[styles.plot, { height }]}>
        {width > 0 && !empty ? (
          <>
            <Svg width={width} height={height}>
              <YAxis ticks={ticks} y={y} width={width} />
              {active !== null ? (
                <Line x1={AXIS_WIDTH + b.centre(active)} x2={AXIS_WIDTH + b.centre(active)} y1={TOP_PAD} y2={plotBottom} stroke={colors.baseline} strokeWidth={1} />
              ) : null}
              {series.map((s, si) => {
                const color = colors.series[si % 8]!;
                const points = s.values.map((v, i) => (v === null ? null : { x: AXIS_WIDTH + b.centre(i), y: y(v) }));
                return (
                  <G key={s.key}>
                    {area && series.length === 1 ? <Path d={areaPath(points, y(0))} fill={color} opacity={0.14} /> : null}
                    <Path d={linePath(points)} stroke={color} strokeWidth={2} fill="none" strokeLinejoin="round" strokeLinecap="round" />
                    {active !== null && points[active] ? (
                      <Circle cx={points[active]!.x} cy={points[active]!.y} r={5} fill={color} stroke={colors.surface} strokeWidth={2} />
                    ) : null}
                  </G>
                );
              })}
              <XLabels labels={labels} centre={b.centre} plotWidth={plotWidth} y={height - 6} />
            </Svg>
            <HitBands count={labels.length} left={AXIS_WIDTH} width={plotWidth} height={plotBottom} onActive={setActive} />
            {active !== null ? (
              <Tooltip
                x={AXIS_WIDTH + b.centre(active)}
                width={width}
                title={labels[active] ?? ''}
                rows={series.map((s, si) => ({
                  label: s.label,
                  color: colors.series[si % 8]!,
                  value: s.values[active] === null || s.values[active] === undefined ? '—' : format(s.values[active]!),
                }))}
              />
            ) : null}
          </>
        ) : null}
        {width > 0 && empty ? <View style={styles.empty}><Text style={styles.emptyLabel}>{t('chart.noData')}</Text></View> : null}
      </View>
    </View>
  );
}

/**
 * Magnitude by category. Grouped puts series side by side; stacked puts them
 * on top of one another, for parts of a whole.
 */
export function BarChart({
  labels, series, format, height = 220, stacked,
}: {
  labels: string[];
  series: Series[];
  format: Format;
  height?: number;
  stacked?: boolean;
}) {
  const styles = useStyles();
  const { colors } = useTheme();
  const { t } = useT();
  const [width, onLayout] = useWidth();
  const [active, setActive] = useState<number | null>(null);

  const totals = labels.map((_, i) => series.reduce((sum, s) => sum + (s.values[i] ?? 0), 0));
  const values = stacked ? totals : series.flatMap((s) => s.values.filter((v): v is number => v !== null));
  const ticks = niceTicks(Math.min(0, ...values), Math.max(0, ...values));
  const plotWidth = Math.max(0, width - AXIS_WIDTH);
  const plotBottom = height - X_AXIS_HEIGHT;
  const y = linearScale([ticks[0]!, ticks.at(-1)!], [plotBottom, TOP_PAD]);
  const b = bands(labels.length, plotWidth, 0.3);
  const gap = 2;
  const groupWidth = stacked ? b.inner : (b.inner - gap * (series.length - 1)) / Math.max(1, series.length);
  const empty = values.every((v) => v === 0);

  return (
    <View accessible accessibilityLabel={empty ? t('chart.noData') : summarise(series, labels, format)}>
      {series.length > 1 ? <Legend items={series.map((s, i) => ({ label: s.label, color: colors.series[i % 8]! }))} /> : null}
      <View onLayout={onLayout} style={[styles.plot, { height }]}>
        {width > 0 && !empty ? (
          <>
            <Svg width={width} height={height}>
              <YAxis ticks={ticks} y={y} width={width} />
              {active !== null ? (
                <Rect x={AXIS_WIDTH + active * b.step} y={TOP_PAD} width={b.step} height={plotBottom - TOP_PAD} fill={colors.surfaceRaised} opacity={0.6} />
              ) : null}
              {labels.map((_, i) => {
                let stackBase = 0;
                return (
                  <G key={i}>
                    {series.map((s, si) => {
                      const v = s.values[i] ?? 0;
                      const color = colors.series[si % 8]!;
                      if (stacked) {
                        const from = y(stackBase);
                        const to = y(stackBase + v);
                        stackBase += v;
                        const isTop = si === series.length - 1 || series.slice(si + 1).every((n) => (n.values[i] ?? 0) === 0);
                        // A 2px gap of surface between stacked segments, so
                        // adjacent fills never touch.
                        const segTop = to;
                        const segBottom = si === 0 ? from : from - gap;
                        return isTop
                          ? <Path key={s.key} d={barPath(AXIS_WIDTH + b.start(i), b.inner, segBottom, segTop)} fill={color} />
                          : <Rect key={s.key} x={AXIS_WIDTH + b.start(i)} y={segTop} width={b.inner} height={Math.max(0, segBottom - segTop)} fill={color} />;
                      }
                      const x = AXIS_WIDTH + b.start(i) + si * (groupWidth + gap);
                      return <Path key={s.key} d={barPath(x, groupWidth, y(0), y(v))} fill={color} />;
                    })}
                  </G>
                );
              })}
              <XLabels labels={labels} centre={b.centre} plotWidth={plotWidth} y={height - 6} />
            </Svg>
            <HitBands count={labels.length} left={AXIS_WIDTH} width={plotWidth} height={plotBottom} onActive={setActive} />
            {active !== null ? (
              <Tooltip
                x={AXIS_WIDTH + b.centre(active)}
                width={width}
                title={labels[active] ?? ''}
                rows={series.map((s, si) => ({
                  label: s.label,
                  color: colors.series[si % 8]!,
                  value: format(s.values[active] ?? 0),
                }))}
              />
            ) : null}
          </>
        ) : null}
        {width > 0 && empty ? <View style={styles.empty}><Text style={styles.emptyLabel}>{t('chart.noData')}</Text></View> : null}
      </View>
    </View>
  );
}

/**
 * Parts of one whole. Used sparingly — four or five segments at most; beyond
 * that a bar chart reads better and the rest fold into "Other".
 */
export function DonutChart({
  segments, format, centre, size = 180,
}: {
  segments: { key: string; label: string; value: number }[];
  format: Format;
  centre?: { value: string; label: string };
  size?: number;
}) {
  const styles = useStyles();
  const { colors } = useTheme();
  const { t, number } = useT();
  const total = segments.reduce((sum, s) => sum + Math.max(0, s.value), 0);
  const outer = size / 2;
  const inner = outer * 0.64;
  let angle = 0;

  return (
    <View
      accessible
      accessibilityLabel={total === 0 ? t('chart.noData') : segments.map((s) => `${s.label} ${format(s.value)}`).join('; ')}
      style={styles.donutWrap}
    >
      <View style={{ width: size, height: size }}>
        <Svg width={size} height={size}>
          {total === 0 ? (
            <Path d={arcPath(outer, outer, outer, inner, 0, Math.PI * 2)} fill={colors.surfaceRaised} />
          ) : segments.map((s, i) => {
            const sweep = (Math.max(0, s.value) / total) * Math.PI * 2;
            const d = arcPath(outer, outer, outer, inner, angle, angle + sweep);
            angle += sweep;
            // The surface-coloured stroke is the 2px gap between segments.
            return sweep > 0 ? <Path key={s.key} d={d} fill={colors.series[i % 8]} stroke={colors.surface} strokeWidth={2} /> : null;
          })}
        </Svg>
        {centre ? (
          <View style={styles.donutCentre} pointerEvents="none">
            <Text style={styles.donutValue} numberOfLines={1} adjustsFontSizeToFit>{centre.value}</Text>
            <Text style={styles.donutLabel}>{centre.label}</Text>
          </View>
        ) : null}
      </View>
      <View style={{ flex: 1, minWidth: 160 }}>
        <Legend
          items={segments.map((s, i) => ({
            label: s.label,
            color: colors.series[i % 8]!,
            value: total > 0 ? `${format(s.value)} · ${number(Math.round((s.value / total) * 100))}%` : format(s.value),
          }))}
        />
      </View>
    </View>
  );
}

/** A trend in a stat card: no axes, no labels, just the shape. */
export function Sparkline({ values, width = 96, height = 28, color }: { values: number[]; width?: number; height?: number; color?: string }) {
  const { colors } = useTheme();
  if (values.length < 2) return null;
  const min = Math.min(...values);
  const max = Math.max(...values);
  const x = linearScale([0, values.length - 1], [2, width - 2]);
  const y = linearScale([min, max === min ? min + 1 : max], [height - 3, 3]);
  const points = values.map((v, i) => ({ x: x(i), y: y(v) }));
  return (
    <View importantForAccessibility="no-hide-descendants" accessibilityElementsHidden>
      <Svg width={width} height={height}>
        <Path d={linePath(points)} stroke={color ?? colors.series[0]} strokeWidth={2} fill="none" strokeLinecap="round" strokeLinejoin="round" />
        <Circle cx={points.at(-1)!.x} cy={points.at(-1)!.y} r={3} fill={color ?? colors.series[0]} />
      </Svg>
    </View>
  );
}

/**
 * A grid of magnitudes — sessions by weekday and hour. One hue, light to
 * dark, so the busy hours are what the eye finds first.
 */
export function Heatmap({
  rows, columns, values, format,
}: {
  rows: string[];
  columns: string[];
  values: number[][];
  format: Format;
}) {
  const styles = useStyles();
  const { colors } = useTheme();
  const { t } = useT();
  const [width, onLayout] = useWidth();
  const [active, setActive] = useState<{ r: number; c: number } | null>(null);
  const max = Math.max(0, ...values.flat());
  const labelWidth = 44;
  const cell = columns.length > 0 ? Math.max(8, (width - labelWidth) / columns.length) : 0;
  const cellHeight = 26;
  const shade = (v: number) => {
    if (max === 0 || v === 0) return colors.surfaceRaised;
    const ramp = colors.sequential;
    return ramp[Math.min(ramp.length - 1, Math.floor((v / max) * (ramp.length - 1) + 0.5))]!;
  };
  const keepCols = thinLabels(columns.length, width - labelWidth, 32);

  return (
    <View accessible accessibilityLabel={max === 0 ? t('chart.noData') : undefined}>
      <View onLayout={onLayout} style={{ direction: 'ltr' }}>
        {width > 0 ? rows.map((row, r) => (
          <View key={row} style={{ flexDirection: 'row', alignItems: 'center', height: cellHeight }}>
            <Text style={[styles.axisLabel, { width: labelWidth }]}>{row}</Text>
            {columns.map((col, c) => (
              <Pressable
                key={col}
                onHoverIn={() => setActive({ r, c })}
                onHoverOut={() => setActive(null)}
                onPressIn={() => setActive({ r, c })}
                accessibilityLabel={`${row} ${col}: ${format(values[r]?.[c] ?? 0)}`}
                style={{ width: cell, height: cellHeight, padding: 1 }}
              >
                <View style={{ flex: 1, borderRadius: 4, backgroundColor: shade(values[r]?.[c] ?? 0) }} />
              </Pressable>
            ))}
          </View>
        )) : null}
        {width > 0 ? (
          <View style={{ flexDirection: 'row', marginStart: labelWidth, height: 18 }}>
            {columns.map((col, c) => (
              <Text key={col} style={[styles.axisLabel, { width: cell, textAlign: 'center' }]}>
                {keepCols.includes(c) ? col : ''}
              </Text>
            ))}
          </View>
        ) : null}
      </View>
      {active ? (
        <Text style={styles.heatmapReadout}>
          {rows[active.r]} · {columns[active.c]} — {format(values[active.r]?.[active.c] ?? 0)}
        </Text>
      ) : null}
    </View>
  );
}

const useStyles = makeStyles(({ colors, type }) => ({
  // Plots are drawn left to right in every language (see the file comment).
  plot: { direction: 'ltr', position: 'relative' },
  legend: { flexDirection: 'row', flexWrap: 'wrap', gap: space.md, marginBottom: space.sm },
  legendItem: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  swatch: { width: 10, height: 10, borderRadius: 3 },
  legendLabel: { ...type.caption, color: colors.ink },
  legendValue: { ...type.caption, color: colors.inkMuted },
  tooltip: {
    position: 'absolute', top: 0, padding: space.sm, borderRadius: radius.sm,
    backgroundColor: colors.surfaceRaised, borderWidth: 1, borderColor: colors.border, gap: 4,
  },
  tooltipTitle: { ...type.caption, color: colors.ink, fontWeight: '800' },
  tooltipRow: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  tooltipLabel: { ...type.caption, color: colors.inkMuted, flex: 1 },
  tooltipValue: { ...type.caption, color: colors.ink, fontVariant: ['tabular-nums'] },
  empty: { position: 'absolute', top: 0, bottom: 0, left: 0, right: 0, alignItems: 'center', justifyContent: 'center' },
  emptyLabel: { ...type.small, color: colors.inkMuted },
  donutWrap: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: space.xl },
  donutCentre: { position: 'absolute', top: 0, bottom: 0, left: 0, right: 0, alignItems: 'center', justifyContent: 'center' },
  donutValue: { fontSize: 22, fontWeight: '800', color: colors.ink, maxWidth: '60%' },
  donutLabel: { ...type.caption, color: colors.inkMuted },
  axisLabel: { ...type.caption, color: colors.inkMuted, fontSize: 10 },
  heatmapReadout: { ...type.caption, color: colors.ink, marginTop: space.xs },
}));
