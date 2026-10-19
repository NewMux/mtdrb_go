/**
 * A day row and a grid of half-hour times — picking a slot with a thumb
 * rather than a date wheel. Times the caller marks as taken are struck
 * through, so a clash is visible before it is chosen.
 */

import React, { useMemo } from 'react';
import { Pressable, ScrollView, View } from 'react-native';

import { T, tap } from './components';
import { makeStyles, radius, space } from './theme';

function addDays(d: Date, n: number) { const x = new Date(d); x.setDate(x.getDate() + n); return x; }
function startOfDay(d: Date) { return new Date(d.getFullYear(), d.getMonth(), d.getDate()); }
function key(d: Date) { return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`; }

export function timeLabel(m: number) {
  const h = Math.floor(m / 60) % 24;
  return `${String(h).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`;
}

export function WhenPicker({
  day, minutes, onDay, onMinutes, taken, firstHour = 6, lastHour = 21, days = 21,
}: {
  day: Date;
  minutes: number;
  onDay: (d: Date) => void;
  onMinutes: (m: number) => void;
  taken?: (m: number) => boolean;
  firstHour?: number;
  lastHour?: number;
  days?: number;
}) {
  const s = useStyles();
  const dayList = useMemo(() => Array.from({ length: days }, (_, i) => addDays(startOfDay(new Date()), i)), [days]);
  const slots = useMemo(
    () => Array.from({ length: (lastHour - firstHour) * 2 + 1 }, (_, i) => firstHour * 60 + i * 30),
    [firstHour, lastHour],
  );
  const today = key(new Date());

  return (
    <View>
      <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: space.sm, paddingHorizontal: space.lg }} style={{ marginHorizontal: -space.lg }}>
        {dayList.map((d) => {
          const on = key(d) === key(day);
          return (
            <Pressable key={key(d)} accessibilityRole="button" accessibilityState={{ selected: on }}
              accessibilityLabel={d.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' })}
              onPress={() => { tap('select'); onDay(d); }} style={[s.dayChip, on && s.on]}>
              <T variant="caption2" weight="600" tone={on ? undefined : 'label2'} style={on ? s.onText : undefined}>
                {key(d) === today ? 'TODAY' : d.toLocaleDateString(undefined, { weekday: 'short' }).toUpperCase()}
              </T>
              <T variant="title3" weight="700" style={on ? s.onText : undefined}>{d.getDate()}</T>
            </Pressable>
          );
        })}
      </ScrollView>

      <View style={s.times}>
        {slots.map((m) => {
          const on = m === minutes;
          const clash = !on && !!taken?.(m);
          return (
            <Pressable key={m} accessibilityRole="button" accessibilityState={{ selected: on }}
              accessibilityLabel={`${timeLabel(m)}${clash ? ', clashes with a booking' : ''}`}
              onPress={() => { tap('select'); onMinutes(m); }}
              style={[s.time, on && s.on, clash && { opacity: 0.4 }]}>
              <T variant="subhead" weight="600" style={[on ? s.onText : undefined, clash ? { textDecorationLine: 'line-through' } : undefined]}>
                {timeLabel(m)}
              </T>
            </Pressable>
          );
        })}
      </View>
    </View>
  );
}

const useStyles = makeStyles((c) => ({
  dayChip: { width: 58, height: 66, borderRadius: radius.md, backgroundColor: c.surface, alignItems: 'center', justifyContent: 'center', gap: 2 },
  on: { backgroundColor: c.tint },
  onText: { color: '#FFFFFF' },
  times: { flexDirection: 'row', flexWrap: 'wrap', gap: space.sm, marginTop: space.md },
  time: { width: '22.5%', flexGrow: 1, height: 40, borderRadius: radius.pill, backgroundColor: c.surface, alignItems: 'center', justifyContent: 'center' },
}));
