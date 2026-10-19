/**
 * The calendar.
 *
 * A week strip to move between days, and the chosen day laid out hour by hour
 * the way the iOS Calendar app does it — sessions as blocks sized to their
 * length, so a crowded morning looks crowded. Tapping an empty hour books a
 * session there; tapping a session opens it.
 *
 * The agenda view is the same data as a list, for scanning the fortnight
 * ahead.
 */

import React, { useCallback, useMemo, useState } from 'react';
import { Pressable, View } from 'react-native';
import { useFocusEffect, useRouter } from 'expo-router';

import { busyDays, dayKey, sessionsBetween, type CalendarSession } from '@/features/scheduling';
import { useApp, useQuery } from '@/state/app';
import {
  Avatar, Card, EmptyState, HStack, IconButton, Page, Section, Segmented, Spacer, T, Tag, tap, VStack,
} from '@/ui/components';
import { SyncBadge } from '@/ui/sync-badge';
import { clockTime } from '@/ui/format';
import { makeStyles, radius, space, useTheme, wash } from '@/ui/theme';

const HOUR_HEIGHT = 64;
const FIRST_HOUR = 6;
const LAST_HOUR = 22;
/** Room above the first hour line so its label is not clipped by the card. */
const TIMELINE_PAD = 12;

function startOfDay(d: Date) { return new Date(d.getFullYear(), d.getMonth(), d.getDate()); }
function addDays(d: Date, n: number) { const x = new Date(d); x.setDate(x.getDate() + n); return x; }
function startOfWeek(d: Date) { const s = startOfDay(d); return addDays(s, -((s.getDay() + 6) % 7)); }

export default function CalendarScreen() {
  const router = useRouter();
  const { sync, syncNow } = useApp();
  const s = useStyles();
  const [selected, setSelected] = useState(() => startOfDay(new Date()));
  const [view, setView] = useState<'day' | 'agenda'>('day');

  const weekStart = useMemo(() => startOfWeek(selected), [selected]);
  const days = useMemo(() => Array.from({ length: 7 }, (_, i) => addDays(weekStart, i)), [weekStart]);

  const busy = useQuery((db) => busyDays(db, weekStart, addDays(weekStart, 7)), [weekStart.getTime()]);
  const daySessions = useQuery((db) => sessionsBetween(db, selected, addDays(selected, 1)), [selected.getTime()]);
  const agenda = useQuery(
    (db) => (view === 'agenda' ? sessionsBetween(db, startOfDay(new Date()), addDays(startOfDay(new Date()), 14)) : Promise.resolve([])),
    [view],
  );

  useFocusEffect(useCallback(() => { busy.reload(); daySessions.reload(); agenda.reload(); }, [busy.reload, daySessions.reload, agenda.reload]));

  const book = (hour?: number) => {
    const at = new Date(selected);
    at.setHours(hour ?? Math.max(FIRST_HOUR, new Date().getHours() + 1), 0, 0, 0);
    router.push({ pathname: '/session/new', params: { at: at.toISOString() } });
  };
  const openSession = (id: string) => router.push({ pathname: '/session/[id]', params: { id } });

  const isToday = dayKey(selected) === dayKey(new Date());
  const month = selected.toLocaleDateString(undefined, { month: 'long', year: 'numeric' });

  return (
    <Page
      title="Calendar"
      eyebrow={month}
      refreshing={sync.running}
      onRefresh={() => { void syncNow(); }}
      trailing={
        <>
          <SyncBadge />
          <IconButton icon="plus" label="Book a session" onPress={() => book()} />
        </>
      }
    >
      <View style={s.weekHeader}>
        <IconButton icon="chevronLeft" label="Previous week" size={34} variant="gray" onPress={() => setSelected(addDays(selected, -7))} />
        <View style={s.week}>
          {days.map((d) => {
            const key = dayKey(d);
            const on = key === dayKey(selected);
            const today = key === dayKey(new Date());
            const count = busy.data?.get(key) ?? 0;
            return (
              <Pressable
                key={key}
                accessibilityRole="button"
                accessibilityLabel={d.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' })}
                accessibilityState={{ selected: on }}
                onPress={() => { tap('select'); setSelected(d); }}
                style={s.day}
              >
                <T variant="caption2" tone={on ? 'tint' : 'label2'} weight="600">
                  {d.toLocaleDateString(undefined, { weekday: 'narrow' })}
                </T>
                <View style={[s.dateBubble, on && s.dateOn, !on && today && s.dateToday]}>
                  <T variant="headline" style={on ? s.dateOnText : today ? s.todayText : undefined}>{d.getDate()}</T>
                </View>
                <View style={s.dots}>
                  {Array.from({ length: Math.min(3, count) }).map((_, i) => <View key={i} style={s.dot} />)}
                </View>
              </Pressable>
            );
          })}
        </View>
        <IconButton icon="chevronRight" label="Next week" size={34} variant="gray" onPress={() => setSelected(addDays(selected, 7))} />
      </View>

      <View style={{ paddingHorizontal: space.lg, marginTop: space.md }}>
        <HStack gap={space.sm}>
          <Segmented
            style={{ flex: 1 }}
            options={[{ value: 'day', label: 'Day' }, { value: 'agenda', label: 'Next 2 Weeks' }] as const}
            value={view}
            onChange={setView}
          />
          {!isToday ? (
            <Pressable accessibilityRole="button" onPress={() => setSelected(startOfDay(new Date()))} style={s.todayBtn}>
              <T variant="subhead" tone="tint" weight="600">Today</T>
            </Pressable>
          ) : null}
        </HStack>
      </View>

      {view === 'day' ? (
        <>
          <Section plain title={selected.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' })}>
            <T variant="subhead" tone="label2" style={{ marginTop: -space.xs, marginBottom: space.md, paddingHorizontal: space.xs }}>
              {(daySessions.data ?? []).length === 0 ? 'Nothing booked — tap an hour to book.' :
                `${(daySessions.data ?? []).length} session${(daySessions.data ?? []).length === 1 ? '' : 's'} · ${(daySessions.data ?? []).reduce((n, x) => n + x.attendees.length, 0)} clients`}
            </T>
            <Card padded={false} style={{ overflow: 'hidden' }}>
              <DayTimeline sessions={daySessions.data ?? []} day={selected} onBook={book} onOpen={openSession} />
            </Card>
          </Section>
        </>
      ) : (
        <Agenda sessions={agenda.data ?? []} onOpen={openSession} onBook={() => book()} />
      )}
    </Page>
  );
}

function DayTimeline({
  sessions, day, onBook, onOpen,
}: { sessions: CalendarSession[]; day: Date; onBook: (hour: number) => void; onOpen: (id: string) => void }) {
  const { c } = useTheme();
  const s = useStyles();
  const hours = Array.from({ length: LAST_HOUR - FIRST_HOUR }, (_, i) => FIRST_HOUR + i);
  const now = new Date();
  const showNow = dayKey(now) === dayKey(day) && now.getHours() >= FIRST_HOUR && now.getHours() < LAST_HOUR;
  const nowTop = TIMELINE_PAD + (now.getHours() - FIRST_HOUR + now.getMinutes() / 60) * HOUR_HEIGHT;

  // Overlapping sessions sit side by side rather than on top of each other.
  const lanes = layoutLanes(sessions);

  return (
    <View style={{ height: hours.length * HOUR_HEIGHT + TIMELINE_PAD * 2, position: 'relative', paddingTop: TIMELINE_PAD }}>
      {hours.map((h, i) => (
        <Pressable
          key={h}
          accessibilityRole="button"
          accessibilityLabel={`Book at ${h}:00`}
          onPress={() => { tap('select'); onBook(h); }}
          style={({ pressed }) => [s.hour, { top: TIMELINE_PAD + i * HOUR_HEIGHT }, pressed && { backgroundColor: c.fill2 }]}
        >
          <T variant="caption" tone="label3" style={s.hourLabel}>{`${String(h).padStart(2, '0')}:00`}</T>
          <View style={s.hourLine} />
        </Pressable>
      ))}

      <View pointerEvents="box-none" style={s.lanes}>
      {sessions.map((x) => {
        const start = new Date(x.startsAt);
        const end = new Date(x.endsAt);
        const top = TIMELINE_PAD + Math.max(0, (start.getHours() - FIRST_HOUR + start.getMinutes() / 60) * HOUR_HEIGHT);
        const height = Math.max(36, ((end.getTime() - start.getTime()) / 3600_000) * HOUR_HEIGHT - 3);
        const lane = lanes.get(x.id) ?? { index: 0, count: 1 };
        const allDone = x.attendees.length > 0 && x.attendees.every((a) => a.status !== 'scheduled');
        const colour = allDone ? c.green : x.colour || c.tint;
        return (
          <Pressable
            key={x.id}
            accessibilityRole="button"
            accessibilityLabel={`${x.typeName} at ${clockTime(x.startsAt)} with ${x.attendees.map((a) => a.name).join(', ') || 'nobody'}`}
            onPress={() => { tap(); onOpen(x.id); }}
            style={({ pressed }) => [s.block, {
              top, height,
              left: `${(lane.index / lane.count) * 100}%`,
              width: `${100 / lane.count}%`,
              opacity: pressed ? 0.7 : 1,
            }]}
          >
            <View style={[s.blockInner, { backgroundColor: wash(colour, c.scheme === 'dark' ? 0.28 : 0.16), borderLeftColor: colour }]}>
              <T variant="footnote" weight="700" lines={1}>
                {x.attendees.map((a) => a.name.split(' ')[0]).join(', ') || x.typeName}
              </T>
              {height > 44 ? (
                <T variant="caption" tone="label2" lines={1}>
                  {clockTime(x.startsAt)}–{clockTime(x.endsAt)} · {x.typeName}
                </T>
              ) : null}
            </View>
          </Pressable>
        );
      })}
      </View>

      {showNow ? (
        <View pointerEvents="none" style={[s.now, { top: nowTop }]}>
          <View style={s.nowDot} />
          <View style={s.nowLine} />
        </View>
      ) : null}
    </View>
  );
}

/** Assigns each session a lane so overlapping ones share the width. */
function layoutLanes(sessions: CalendarSession[]): Map<string, { index: number; count: number }> {
  const out = new Map<string, { index: number; count: number }>();
  const sorted = [...sessions].sort((a, b) => a.startsAt.localeCompare(b.startsAt));
  let group: { id: string; end: number; lane: number }[] = [];
  const flush = () => {
    const count = Math.max(1, ...group.map((g) => g.lane + 1));
    for (const g of group) out.set(g.id, { index: g.lane, count });
    group = [];
  };
  for (const x of sorted) {
    const start = new Date(x.startsAt).getTime();
    const end = new Date(x.endsAt).getTime();
    if (group.length && start >= Math.max(...group.map((g) => g.end))) flush();
    const used = new Set(group.filter((g) => g.end > start).map((g) => g.lane));
    let lane = 0;
    while (used.has(lane)) lane++;
    group.push({ id: x.id, end, lane });
  }
  flush();
  return out;
}

function Agenda({ sessions, onOpen, onBook }: { sessions: CalendarSession[]; onOpen: (id: string) => void; onBook: () => void }) {
  const s = useStyles();
  if (sessions.length === 0) {
    return (
      <Section plain>
        <Card><EmptyState icon="calendar" title="Nothing in the next two weeks" message="Book a session, or a weekly slot, and it appears here." action="Book a Session" onAction={onBook} /></Card>
      </Section>
    );
  }
  const groups = new Map<string, CalendarSession[]>();
  for (const x of sessions) {
    const key = dayKey(new Date(x.startsAt));
    groups.set(key, [...(groups.get(key) ?? []), x]);
  }
  return (
    <>
      {[...groups].map(([key, list]) => {
        const d = new Date(list[0]!.startsAt);
        const label = key === dayKey(new Date()) ? 'Today'
          : key === dayKey(addDays(new Date(), 1)) ? 'Tomorrow'
          : d.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'short' });
        return (
          <Section key={key} title={label} plain>
            <Card padded={false}>
              {list.map((x, i) => (
                <Pressable
                  key={x.id}
                  accessibilityRole="button"
                  onPress={() => { tap('select'); onOpen(x.id); }}
                  style={({ pressed }) => [s.agendaRow, i > 0 && s.agendaSep, pressed && { opacity: 0.6 }]}
                >
                  <VStack gap={0} style={{ width: 56 }}>
                    <T variant="headline">{clockTime(x.startsAt)}</T>
                    <T variant="caption" tone="label3">{clockTime(x.endsAt)}</T>
                  </VStack>
                  <HStack gap={-8}>
                    {x.attendees.slice(0, 3).map((a) => <Avatar key={a.attendeeId} name={a.name} size={34} />)}
                  </HStack>
                  <VStack gap={2} style={{ flex: 1 }}>
                    <T variant="body" weight="600" lines={1}>{x.attendees.map((a) => a.name).join(', ') || 'Open slot'}</T>
                    <T variant="footnote" tone="label2" lines={1}>{[x.typeName, x.location].filter(Boolean).join(' · ')}</T>
                  </VStack>
                  {x.attendees.length > 0 && x.attendees.every((a) => a.status === 'completed') ? <Tag label="Done" hue="green" /> : null}
                </Pressable>
              ))}
            </Card>
          </Section>
        );
      })}
      <Spacer />
    </>
  );
}

const useStyles = makeStyles((c) => ({
  weekHeader: { flexDirection: 'row', alignItems: 'center', paddingHorizontal: space.md, gap: 4 },
  week: { flex: 1, flexDirection: 'row', justifyContent: 'space-between' },
  day: { alignItems: 'center', gap: 4, flex: 1, paddingVertical: 4 },
  dateBubble: { width: 38, height: 38, borderRadius: 19, alignItems: 'center', justifyContent: 'center' },
  dateOn: { backgroundColor: c.tint },
  dateOnText: { color: '#FFFFFF' },
  dateToday: { backgroundColor: wash(c.tint, 0.14) },
  todayText: { color: c.tint },
  dots: { flexDirection: 'row', gap: 3, height: 5 },
  dot: { width: 5, height: 5, borderRadius: 3, backgroundColor: c.label3 },
  todayBtn: { height: 38, paddingHorizontal: 14, borderRadius: radius.pill, backgroundColor: c.fill2, justifyContent: 'center' },
  hour: { position: 'absolute', left: 0, right: 0, height: HOUR_HEIGHT, flexDirection: 'row' },
  hourLabel: { width: 52, textAlign: 'right', paddingRight: 8, marginTop: -7 },
  hourLine: { flex: 1, height: 0.5, backgroundColor: c.separator },
  block: { position: 'absolute', paddingRight: 6 },
  blockInner: { flex: 1, borderRadius: 8, borderLeftWidth: 3, paddingHorizontal: 8, paddingVertical: 5, overflow: 'hidden' },
  lanes: { position: 'absolute', top: 0, bottom: 0, left: 56, right: 0 },
  now: { position: 'absolute', left: 46, right: 0, flexDirection: 'row', alignItems: 'center' },
  nowDot: { width: 9, height: 9, borderRadius: 5, backgroundColor: c.red },
  nowLine: { flex: 1, height: 1.5, backgroundColor: c.red },
  agendaRow: { flexDirection: 'row', alignItems: 'center', gap: space.md, padding: space.lg },
  agendaSep: { borderTopWidth: 0.5, borderTopColor: c.separator },
}));
