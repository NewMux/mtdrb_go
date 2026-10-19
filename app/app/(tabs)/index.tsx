/**
 * Today.
 *
 * The first screen, and the one the product is really about: who is in front
 * of me, what are we doing, and what happens when this session ends.
 *
 * Three layers, top to bottom. The day at a glance (a ring that fills as the
 * roster is marked). The nudges — the few things worth acting on today, each
 * one tap from its fix. And the schedule, where tapping a session opens its
 * outcome buttons in place: marking attendance happens between sets, and a
 * navigation animation for a two-second decision is time the trainer stands
 * still for.
 */

import React, { useCallback, useState } from 'react';
import { Pressable, View } from 'react-native';
import { useFocusEffect, useRouter } from 'expo-router';

import { markAttendance } from '@/features/actions';
import { nudges, weekGlance, type Nudge } from '@/features/insights';
import { nextSession } from '@/features/programs';
import { todaysRoster, type RosterEntry } from '@/features/queries';
import { openOrStartWorkout } from '@/features/workflow';
import type { AttendanceStatus } from '@/api/types';
import { useApp, useQuery } from '@/state/app';
import { Ring } from '@/ui/charts';
import {
  Avatar, Button, Card, EmptyState, HStack, IconButton, IconTile, ListRow, Page, Section,
  Spacer, Stat, StatGrid, T, Tag, tap, VStack,
} from '@/ui/components';
import { ActionSheet } from '@/ui/sheet';
import { SyncBadge } from '@/ui/sync-badge';
import { clockTime } from '@/ui/format';
import type { IconName } from '@/ui/icon';
import { makeStyles, radius, space, useTheme, type Hue } from '@/ui/theme';

const OUTCOMES: { status: AttendanceStatus; label: string; icon: IconName; hue: Hue }[] = [
  { status: 'completed', label: 'Attended', icon: 'checkCircle', hue: 'green' },
  { status: 'no_show', label: 'No-show', icon: 'closeCircle', hue: 'red' },
  { status: 'late_cancel', label: 'Late cancel', icon: 'clock', hue: 'orange' },
  { status: 'early_cancel', label: 'Cancelled', icon: 'calendar', hue: 'gray' },
];

interface Entry extends RosterEntry {
  plan: string | null;
}

export default function TodayScreen() {
  const { db, account, touch, syncNow, sync } = useApp();
  const router = useRouter();
  const s = useStyles();
  const [open, setOpen] = useState<string | null>(null);
  const [allTips, setAllTips] = useState(false);
  /** The client being marked attended with nothing left on their pack. */
  const [noCredit, setNoCredit] = useState<Entry | null>(null);

  const roster = useQuery(async (database): Promise<Entry[]> => {
    const rows = await todaysRoster(database);
    return Promise.all(rows.map(async (r) => {
      const next = await nextSession(database, r.clientId);
      return { ...r, plan: next ? `${next.day.name} · Week ${next.week}` : null };
    }));
  }, []);
  const tips = useQuery((database) => nudges(database), []);
  const week = useQuery((database) => weekGlance(database), []);

  useFocusEffect(useCallback(() => { roster.reload(); tips.reload(); week.reload(); }, [roster.reload, tips.reload, week.reload]));

  const mark = async (entry: Entry, status: AttendanceStatus, allowOverdraft = false) => {
    if (!db) return;
    tap(status === 'completed' ? 'success' : 'medium');
    await markAttendance(db, entry.attendeeId, status, { allowOverdraft });
    setOpen(null);
    touch();
    void syncNow();
  };

  /**
   * Journey A's hinge.
   *
   * The local balance is a mirror and may be stale, so this is a warning and
   * not a refusal — the server holds the real answer and will refuse if it has
   * to. What the trainer gets is the choice: sell a renewal, or let this one
   * run into overdraft, decided before the client leaves.
   */
  const attended = (entry: Entry) => {
    if (entry.creditsRemaining > 0) { void mark(entry, 'completed'); return; }
    setNoCredit(entry);
  };

  const train = async (entry: Entry) => {
    if (!db) return;
    const { id, created } = await openOrStartWorkout(db, entry.clientId, { sessionId: entry.sessionId });
    if (created) touch();
    router.push({ pathname: '/workout/[id]', params: { id, client: entry.clientId, name: entry.clientName } });
  };

  const act = (n: Nudge) => {
    switch (n.kind) {
      case 'overdue': router.push('/business'); break;
      case 'unmarked': router.push('/calendar'); break;
      case 'renew':
        if (n.clientId) router.push({ pathname: '/client/[id]', params: { id: n.clientId } });
        else router.push({ pathname: '/clients', params: { filter: 'low' } });
        break;
      default:
        if (n.clientId) router.push({ pathname: '/client/[id]', params: { id: n.clientId } });
    }
  };

  const entries = roster.data ?? [];
  const marked = entries.filter((e) => e.status !== 'scheduled').length;
  const next = entries.find((e) => e.status === 'scheduled' && new Date(e.endsAt) > new Date());
  const firstName = (account?.display_name ?? '').split(' ')[0] || 'Coach';
  const today = new Date().toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' });

  return (
    <Page
      title="Today"
      eyebrow={today}
      refreshing={sync.running}
      onRefresh={() => { void syncNow(); }}
      trailing={
        <>
          <SyncBadge />
          <IconButton icon="plus" label="Create" onPress={() => router.push('/quick')} />
          <Pressable accessibilityRole="button" accessibilityLabel="Settings" onPress={() => { tap(); router.push('/settings'); }}>
            <Avatar name={account?.display_name ?? 'Coach'} size={40} />
          </Pressable>
        </>
      }
    >
      {/* The day at a glance. */}
      <Card style={s.hero}>
        <HStack gap={space.lg}>
          <Ring value={entries.length ? marked / entries.length : 0} hue="green" size={76} stroke={10}>
            <T variant="headline">{entries.length ? `${marked}/${entries.length}` : '0'}</T>
          </Ring>
          <VStack gap={2} style={{ flex: 1 }}>
            <T variant="title3" weight="700">
              {entries.length === 0 ? `A clear day, ${firstName}` :
                marked === entries.length ? 'All sessions marked' : `${entries.length - marked} to go`}
            </T>
            <T variant="subhead" tone="label2">
              {next
                ? `Next: ${next.clientName} at ${clockTime(next.startsAt)}`
                : entries.length === 0 ? 'Nothing booked today.' : 'Nothing left on the schedule.'}
            </T>
          </VStack>
        </HStack>
      </Card>

      {(tips.data ?? []).length > 0 ? (
        <Section
          title="For You"
          action={(tips.data ?? []).length > TIP_LIMIT ? (allTips ? 'Show Less' : `Show All ${(tips.data ?? []).length}`) : undefined}
          onAction={() => setAllTips(!allTips)}
        >
          {(tips.data ?? []).slice(0, allTips ? undefined : TIP_LIMIT).map((n, i) => (
            <ListRow
              key={n.key}
              first={i === 0}
              leading={<IconTile icon={NUDGE_ICON[n.kind]} hue={NUDGE_HUE[n.kind]} />}
              title={n.title}
              subtitle={n.detail}
              lines={2}
              onPress={() => act(n)}
            />
          ))}
        </Section>
      ) : null}

      <Section title="Schedule" action="Calendar" onAction={() => router.push('/calendar')} plain>
        {entries.length === 0 ? (
          <Card>
            <EmptyState
              icon="calendar"
              title="Nothing booked"
              message={roster.loading ? 'Loading…' : 'Book a session and it shows up here.'}
              action="Book a Session"
              onAction={() => router.push('/session/new')}
            />
          </Card>
        ) : (
          <VStack gap={space.md}>
            {entries.map((entry) => {
              const expanded = open === entry.attendeeId;
              const done = entry.status !== 'scheduled';
              return (
                <Card key={entry.attendeeId} padded={false}>
                  <Pressable
                    accessibilityRole="button"
                    accessibilityLabel={`${entry.clientName} at ${clockTime(entry.startsAt)}`}
                    accessibilityState={{ expanded }}
                    onPress={() => { tap('select'); setOpen(expanded ? null : entry.attendeeId); }}
                    style={({ pressed }) => [s.session, pressed && { opacity: 0.7 }]}
                  >
                    <View style={s.time}>
                      <T variant="headline" tone={done ? 'label3' : 'label'}>{clockTime(entry.startsAt)}</T>
                      <T variant="caption" tone="label3">{minutes(entry.startsAt, entry.endsAt)}</T>
                    </View>
                    <View style={s.rail} />
                    <Avatar name={entry.clientName} size={42} />
                    <VStack gap={3} style={{ flex: 1 }}>
                      <T variant="headline" lines={1} tone={done ? 'label2' : 'label'}>{entry.clientName}</T>
                      <T variant="footnote" tone="label2" lines={1}>
                        {entry.plan ?? ([entry.sessionTypeName, entry.location].filter(Boolean).join(' · ') || 'Session')}
                      </T>
                      <HStack gap={6}>
                        <StatusTag status={entry.status} />
                        <Tag
                          label={entry.creditsRemaining <= 0 ? (entry.creditsRemaining < 0 ? `${entry.creditsRemaining} owed` : 'No sessions left') : `${entry.creditsRemaining} left`}
                          hue={entry.creditsRemaining <= 0 ? 'red' : entry.creditsRemaining <= 2 ? 'orange' : 'gray'}
                        />
                      </HStack>
                    </VStack>
                  </Pressable>

                  {expanded ? (
                    <View style={s.drawer}>
                      <View style={s.outcomes}>
                        {OUTCOMES.map((o) => (
                          <Outcome
                            key={o.status}
                            {...o}
                            active={entry.status === o.status}
                            onPress={() => {
                              if (o.status === 'completed') attended(entry);
                              else void mark(entry, o.status);
                            }}
                          />
                        ))}
                      </View>
                      <HStack gap={space.sm}>
                        <Button label="Start Workout" icon="play" variant="filled" style={{ flex: 1 }} full onPress={() => { void train(entry); }} />
                        <Button
                          label="Profile"
                          icon="person"
                          variant="gray"
                          onPress={() => router.push({ pathname: '/client/[id]', params: { id: entry.clientId } })}
                        />
                      </HStack>
                    </View>
                  ) : null}
                </Card>
              );
            })}
          </VStack>
        )}
      </Section>

      <Section title="This Week" plain>
        <StatGrid>
          <Stat icon="calendar" hue="tint" label="Booked" value={String(week.data?.booked ?? 0)} footnote={`${week.data?.remaining ?? 0} still to come`} />
          <Stat icon="checkCircle" hue="green" label="Attended" value={String(week.data?.completed ?? 0)} />
          <Stat
            icon="heartPulse" hue="pink" label="Attendance"
            value={week.data?.attendanceRate == null ? '—' : `${Math.round(week.data.attendanceRate * 100)}`}
            unit={week.data?.attendanceRate == null ? undefined : '%'}
            footnote="last 30 days"
          />
          <Stat icon="strength" hue="orange" label="Workouts" value={String(week.data?.workoutsLogged ?? 0)} footnote="logged this week" />
        </StatGrid>
      </Section>

      {sync.pending > 0 ? (
        <T variant="footnote" tone="label2" align="center" style={{ marginTop: space.xl, paddingHorizontal: space.xxl }}>
          {sync.pending} change{sync.pending === 1 ? '' : 's'} saved on this device, waiting to send.
        </T>
      ) : null}
      <Spacer size={space.lg} />

      <ActionSheet
        visible={!!noCredit}
        title={noCredit ? `${noCredit.clientName} has no sessions left` : undefined}
        message="Renew their package now, or record this session as an overdraft to settle later."
        onClose={() => setNoCredit(null)}
        actions={noCredit ? [
          {
            label: 'Renew Package', icon: 'cart', hue: 'tint',
            onPress: () => router.push({ pathname: '/sell-package', params: { client: noCredit.clientId, name: noCredit.clientName } }),
          },
          { label: 'Record as Overdraft', icon: 'invoice', hue: 'orange', onPress: () => { void mark(noCredit, 'completed', true); } },
        ] : []}
      />
    </Page>
  );
}

/** Enough to act on between clients; the rest wait behind "Show All". */
const TIP_LIMIT = 4;

const NUDGE_ICON: Record<Nudge['kind'], IconName> = {
  renew: 'cart', overdue: 'invoice', inactive: 'message', unmarked: 'checkCircle', clearance: 'shield',
};
const NUDGE_HUE: Record<Nudge['kind'], Hue> = {
  renew: 'orange', overdue: 'red', inactive: 'indigo', unmarked: 'green', clearance: 'pink',
};

function minutes(start: string, end: string): string {
  const m = Math.round((new Date(end).getTime() - new Date(start).getTime()) / 60_000);
  return Number.isFinite(m) && m > 0 ? `${m} min` : '';
}

function StatusTag({ status }: { status: AttendanceStatus }) {
  switch (status) {
    case 'completed': return <Tag label="Attended" hue="green" icon="check" />;
    case 'no_show': return <Tag label="No-show" hue="red" />;
    case 'late_cancel': return <Tag label="Late cancel" hue="orange" />;
    case 'early_cancel': return <Tag label="Cancelled" hue="gray" />;
    default: return <Tag label="Booked" hue="tint" />;
  }
}

function Outcome({
  label, icon, hue, active, onPress,
}: { label: string; icon: IconName; hue: Hue; active: boolean; onPress: () => void }) {
  const { c } = useTheme();
  const s = useStyles();
  const color = (c as unknown as Record<string, string>)[hue]!;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ selected: active }}
      onPress={onPress}
      style={({ pressed }) => [s.outcome, active && { backgroundColor: color }, pressed && { opacity: 0.7, transform: [{ scale: 0.96 }] }]}
    >
      <IconTile icon={icon} hue={active ? 'gray' : hue} size={30} />
      <T variant="caption" weight="600" style={active ? { color: '#FFF' } : undefined} align="center">{label}</T>
    </Pressable>
  );
}

const useStyles = makeStyles((c) => ({
  hero: { marginHorizontal: space.lg },
  session: { flexDirection: 'row', alignItems: 'center', gap: space.md, padding: space.lg },
  time: { width: 52, alignItems: 'flex-start' },
  rail: { width: 3, alignSelf: 'stretch', borderRadius: 2, backgroundColor: c.tint, opacity: 0.8 },
  drawer: { paddingHorizontal: space.lg, paddingBottom: space.lg, gap: space.md },
  outcomes: { flexDirection: 'row', gap: space.sm },
  outcome: {
    flex: 1, alignItems: 'center', gap: 6, paddingVertical: space.md, borderRadius: radius.md,
    backgroundColor: c.surface2,
  },
}));
