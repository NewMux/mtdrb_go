/**
 * Charts.
 *
 * A small set drawn with react-native-svg, because every chart in the app is
 * one series answering one question — "is the weight coming down", "is the
 * squat going up", "which months paid" — and a charting library would bring a
 * legend, a colour cycle and a dual axis that none of them want.
 *
 * Specs: 2px lines, rounded bar ends anchored to the baseline, recessive
 * grid, and one direct label on the latest value instead of a number on every
 * point. Text is in label colours, never the series colour. Press and drag
 * scrubs a line chart to read any point, standing in for hover on a phone.
 */

import React, { useMemo, useState } from 'react';
import { View, type LayoutChangeEvent, type StyleProp, type ViewStyle } from 'react-native';
import Svg, { Circle, Defs, Line, LinearGradient, Path, Rect, Stop } from 'react-native-svg';

import { T, HStack, toneColor } from './components';
import { useTheme, wash, type Hue } from './theme';

export interface Point {
  /** Shown under the chart and in the scrub readout. */
  label: string;
  value: number;
}

function useWidth(initial = 300): [number, (e: LayoutChangeEvent) => void] {
  const [w, setW] = useState(initial);
  return [w, (e) => setW(Math.max(1, Math.round(e.nativeEvent.layout.width)))];
}

function smoothPath(xy: [number, number][]): string {
  if (xy.length === 0) return '';
  if (xy.length === 1) return `M${xy[0]![0]},${xy[0]![1]}`;
  let d = `M${xy[0]![0]},${xy[0]![1]}`;
  for (let i = 1; i < xy.length; i++) {
    const [x0, y0] = xy[i - 1]!;
    const [x1, y1] = xy[i]!;
    const mx = (x0 + x1) / 2;
    d += ` C${mx},${y0} ${mx},${y1} ${x1},${y1}`;
  }
  return d;
}

/**
 * A line with a soft area under it, and a scrub readout.
 *
 * The y-range is padded around the data rather than starting at zero: a body
 * weight moving from 84 to 82 kg is the whole story, and a zero baseline
 * would flatten it into a straight line.
 */
export function LineChart({
  data, hue = 'tint', height = 140, format = (v) => String(v), style, zeroBased = false,
}: {
  data: Point[];
  hue?: Hue;
  height?: number;
  format?: (v: number) => string;
  style?: StyleProp<ViewStyle>;
  zeroBased?: boolean;
}) {
  const { c } = useTheme();
  const [width, onLayout] = useWidth();
  const [active, setActive] = useState<number | null>(null);
  const color = toneColor(c, hue);
  const id = useMemo(() => `g${Math.random().toString(36).slice(2, 8)}`, []);

  const padTop = 10;
  const padBottom = 6;
  const plotH = height - padTop - padBottom;

  const values = data.map((d) => d.value);
  let min = zeroBased ? 0 : Math.min(...values);
  let max = Math.max(...values);
  if (!Number.isFinite(min) || !Number.isFinite(max)) { min = 0; max = 1; }
  if (max === min) { max += 1; min = zeroBased ? 0 : min - 1; }
  const span = max - min;
  if (!zeroBased) { min -= span * 0.15; max += span * 0.15; }

  const x = (i: number) => (data.length <= 1 ? width / 2 : 6 + (i * (width - 12)) / (data.length - 1));
  const y = (v: number) => padTop + plotH - ((v - min) / (max - min)) * plotH;
  const xy = data.map((d, i) => [x(i), y(d.value)] as [number, number]);
  const line = smoothPath(xy);
  const area = xy.length > 1 ? `${line} L${xy[xy.length - 1]![0]},${height} L${xy[0]![0]},${height} Z` : '';

  const shown = active ?? data.length - 1;
  const point = data[shown];

  const scrub = (px: number) => {
    if (data.length === 0) return;
    const i = Math.round(((px - 6) / Math.max(1, width - 12)) * (data.length - 1));
    setActive(Math.max(0, Math.min(data.length - 1, i)));
  };

  if (data.length === 0) return null;

  return (
    <View style={style}>
      <HStack justify="space-between" align="baseline" style={{ marginBottom: 6 }}>
        <T variant="metricSmall">{point ? format(point.value) : '—'}</T>
        <T variant="footnote" tone="label2">{point?.label ?? ''}</T>
      </HStack>
      <View
        onLayout={onLayout}
        onStartShouldSetResponder={() => true}
        onMoveShouldSetResponder={() => true}
        onResponderGrant={(e) => scrub(e.nativeEvent.locationX)}
        onResponderMove={(e) => scrub(e.nativeEvent.locationX)}
        onResponderRelease={() => setActive(null)}
        onResponderTerminate={() => setActive(null)}
        accessibilityLabel={`Chart: ${data.map((d) => `${d.label} ${format(d.value)}`).join(', ')}`}
      >
        <Svg width={width} height={height}>
          <Defs>
            <LinearGradient id={id} x1="0" y1="0" x2="0" y2="1">
              <Stop offset="0" stopColor={color} stopOpacity={0.22} />
              <Stop offset="1" stopColor={color} stopOpacity={0} />
            </LinearGradient>
          </Defs>
          {[0.25, 0.5, 0.75].map((f) => (
            <Line key={f} x1={0} x2={width} y1={padTop + plotH * f} y2={padTop + plotH * f} stroke={c.separator} strokeWidth={0.5} strokeDasharray="3 4" />
          ))}
          {area ? <Path d={area} fill={`url(#${id})`} /> : null}
          <Path d={line} stroke={color} strokeWidth={2} fill="none" strokeLinecap="round" strokeLinejoin="round" />
          {active !== null ? (
            <Line x1={xy[shown]![0]} x2={xy[shown]![0]} y1={0} y2={height} stroke={c.label3} strokeWidth={1} />
          ) : null}
          {xy[shown] ? (
            <Circle cx={xy[shown]![0]} cy={xy[shown]![1]} r={5} fill={color} stroke={c.surface} strokeWidth={2} />
          ) : null}
        </Svg>
      </View>
      {data.length > 1 ? (
        <HStack justify="space-between" style={{ marginTop: 4 }}>
          <T variant="caption2" tone="label3">{data[0]!.label}</T>
          <T variant="caption2" tone="label3">{data[data.length - 1]!.label}</T>
        </HStack>
      ) : null}
    </View>
  );
}

/** A tiny trend line with no axes, for list rows and stat tiles. */
export function Sparkline({
  values, hue = 'tint', width = 64, height = 24,
}: { values: number[]; hue?: Hue; width?: number; height?: number }) {
  const { c } = useTheme();
  if (values.length < 2) return null;
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = max - min || 1;
  const xy = values.map((v, i) => [
    2 + (i * (width - 4)) / (values.length - 1),
    2 + (height - 4) - ((v - min) / span) * (height - 4),
  ] as [number, number]);
  const last = xy[xy.length - 1]!;
  return (
    <Svg width={width} height={height}>
      <Path d={smoothPath(xy)} stroke={toneColor(c, hue)} strokeWidth={2} fill="none" strokeLinecap="round" />
      <Circle cx={last[0]} cy={last[1]} r={2.5} fill={toneColor(c, hue)} />
    </Svg>
  );
}

/**
 * Columns from a zero baseline, with the latest one emphasised.
 *
 * Bars encode magnitude, so the baseline is always zero. Tapping a bar reads
 * its value.
 */
export function BarChart({
  data, hue = 'tint', height = 120, format = (v) => String(v), highlightLast = true,
}: {
  data: Point[];
  hue?: Hue;
  height?: number;
  format?: (v: number) => string;
  highlightLast?: boolean;
}) {
  const { c } = useTheme();
  const [width, onLayout] = useWidth();
  const [active, setActive] = useState<number | null>(null);
  const color = toneColor(c, hue);
  const max = Math.max(1, ...data.map((d) => d.value));
  const gap = 8;
  const barW = data.length > 0 ? Math.max(6, (width - gap * (data.length - 1)) / data.length) : 0;
  const shown = active ?? data.length - 1;

  if (data.length === 0) return null;

  return (
    <View>
      <HStack justify="space-between" align="baseline" style={{ marginBottom: 6 }}>
        <T variant="metricSmall">{format(data[shown]?.value ?? 0)}</T>
        <T variant="footnote" tone="label2">{data[shown]?.label ?? ''}</T>
      </HStack>
      <View onLayout={onLayout}>
        <Svg width={width} height={height}>
          <Line x1={0} x2={width} y1={height - 0.5} y2={height - 0.5} stroke={c.separator} strokeWidth={1} />
          {data.map((d, i) => {
            const h = Math.max(d.value > 0 ? 4 : 0, (d.value / max) * (height - 4));
            const on = i === shown;
            const fill = on || (!highlightLast && active === null) ? color : wash(color, c.scheme === 'dark' ? 0.4 : 0.3);
            return (
              <Rect
                key={i}
                x={i * (barW + gap)}
                y={height - h}
                width={barW}
                height={h}
                rx={Math.min(4, barW / 2)}
                fill={fill}
                onPress={() => setActive(i === active ? null : i)}
              />
            );
          })}
        </Svg>
        <HStack gap={gap} style={{ marginTop: 4 }}>
          {data.map((d, i) => (
            <T key={i} variant="caption2" tone={i === shown ? 'label' : 'label3'} align="center" style={{ width: barW }} lines={1}>
              {d.label}
            </T>
          ))}
        </HStack>
      </View>
    </View>
  );
}

/**
 * A progress ring, in the Activity-ring shape.
 *
 * Used for a single proportion — a day's sessions marked, a pack's credits
 * used, a week's adherence — never to compare categories.
 */
export function Ring({
  value, hue = 'tint', size = 64, stroke = 9, children,
}: {
  value: number;
  hue?: Hue;
  size?: number;
  stroke?: number;
  children?: React.ReactNode;
}) {
  const { c } = useTheme();
  const color = toneColor(c, hue);
  const v = Math.max(0, Math.min(1, Number.isFinite(value) ? value : 0));
  const r = (size - stroke) / 2;
  const circumference = 2 * Math.PI * r;
  return (
    <View style={{ width: size, height: size, alignItems: 'center', justifyContent: 'center' }}
      accessibilityRole="progressbar" accessibilityValue={{ min: 0, max: 100, now: Math.round(v * 100) }}>
      <Svg width={size} height={size} style={{ position: 'absolute', transform: [{ rotate: '-90deg' }] }}>
        <Circle cx={size / 2} cy={size / 2} r={r} stroke={wash(color, 0.2)} strokeWidth={stroke} fill="none" />
        {v > 0 ? (
          <Circle
            cx={size / 2} cy={size / 2} r={r}
            stroke={color} strokeWidth={stroke} fill="none" strokeLinecap="round"
            strokeDasharray={`${circumference * v} ${circumference}`}
          />
        ) : null}
      </Svg>
      {children}
    </View>
  );
}
