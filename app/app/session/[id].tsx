/**
 * One session: who is in it, what happened, and moving or cancelling it.
 *
 * Outcomes are marked per attendee, because in a semi-private slot one client
 * can attend while another no-shows — each with their own credit and revenue
 * consequence on the server.
 */

import React, { useCallback, useEffect, useState } from 'react';
import { View } from 'react-native';
import { useFocusEffect, useLocalSearchParams, useRouter } from 'expo-router';

import { markAttendance } from '@/features/actions';
import { listClients } from '@/features/queries';
import { cancelSession, clashes, rescheduleSession, sessionById } from '@/features/scheduling';
import { openOrStartWorkout } from '@/features/workflow';
import type { AttendanceStatus } from '@/api/types';
import { useApp, useQuery } from '@/state/app';
import {
  Avatar, Banner, Body, Button, Card, HStack, IconTile, ListRow, Section, Segmented, Spacer, T, Tag, tap, VStack,
} from '@/ui/components';
import { ActionSheet } from '@/ui/sheet';
import { clockTime } from '@/ui/format';
import { space } from '@/ui/theme';
import { timeLabel, WhenPicker } from '@/ui/when-picker';

const OUTCOMES = [
  { value: 'completed', label: 'Attended' },
  { value: 'no_show', label: 'No-show' },
  { value: 'late_cancel', label: 'Late' },
  { value: 'early_cancel', label: 'Cancelled' },
] as const;

export default function SessionScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const { db, touch, syncNow } = useApp();
  const router = useRouter();
  const [moving, setMoving] = useState(false);
  const [confirmCancel, setConfirmCancel] = useState(false);
  const [overdraft, setOverdraft] = useState<{ name: string; go: () => void } | null>(null);

  const session = useQuery((d) => sessionById(d, id), [id]);
  const credits = useQuery((d) => listClients(d, ''), []);
  useFocusEffect(useCallback(() => { session.reload(); credits.reload(); }, [session.reload, credits.reload]));

  const x = session.data;
  const start = x ? new Date(x.startsAt) : new Date();
  const [day, setDay] = useState(new Date(start.getFullYear(), start.getMonth(), start.getDate()));
  const [minutes, setMinutes] = useState(start.getHours() * 60 + start.getMinutes());
  const duration = x ? Math.round((new Date(x.endsAt).getTime() - new Date(x.startsAt).getTime()) / 60_000) || 60 : 60;
  const target = new Date(day); target.setHours(Math.floor(minutes / 60), minutes % 60, 0, 0);
  // The picker starts on the session's own slot once it has loaded.
  useEffect(() => {
    if (!x) return;
    const at = new Date(x.startsAt);
    setDay(new Date(at.getFullYear(), at.getMonth(), at.getDate()));
    setMinutes(at.getHours() * 60 + at.getMinutes());
  }, [x?.startsAt]); // eslint-disable-line react-hooks/exhaustive-deps

  const conflicts = useQuery((d) => (moving ? clashes(d, target, duration, id) : Promise.resolve([])), [moving, target.getTime(), duration, id]);

  if (!x) return <Body><T variant="body" tone="label2" align="center">{session.loading ? 'Loading…' : 'This session is not on this device.'}</T></Body>;

  const balance = new Map((credits.data ?? []).map((c) => [c.id, c.creditsRemaining]));
  const cancelled = x.status === 'cancelled';

  const mark = async (attendeeId: string, clientId: string, name: string, status: AttendanceStatus) => {
    if (!db) return;
    const go = async (allowOverdraft: boolean) => {
      tap(status === 'completed' ? 'success' : 'medium');
      await markAttendance(db, attendeeId, status, { allowOverdraft });
      touch(); session.reload(); void syncNow();
    };
    if (status !== 'completed' || (balance.get(clientId) ?? 0) > 0) { await go(false); return; }
    setOverdraft({ name, go: () => { void go(true); } });
  };

  const move = async () => {
    if (!db) return;
    await rescheduleSession(db, x.id, target);
    tap('success');
    setMoving(false);
    touch(); session.reload(); void syncNow();
  };

  const cancel = async () => {
    if (!db) return;
    await cancelSession(db, x.id);
    tap('warning');
    touch(); void syncNow();
    router.back();
  };

  return (
    <Body keyboard>
      <View style={{ paddingHorizontal: space.lg }}>
        <Card>
          <HStack gap={space.md}>
            <IconTile icon="calendar" hue={cancelled ? 'gray' : 'tint'} size={44} />
            <VStack gap={2} style={{ flex: 1 }}>
              <T variant="title3" weight="700">{x.typeName}</T>
              <T variant="subhead" tone="label2">
                {start.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' })}
              </T>
              <T variant="subhead" tone="label2">{clockTime(x.startsAt)}–{clockTime(x.endsAt)}{x.location ? ` · ${x.location}` : ''}</T>
            </VStack>
            {cancelled ? <Tag label="Cancelled" hue="gray" /> : null}
          </HStack>
        </Card>
      </View>

      <Section title={x.attendees.length === 1 ? 'Client' : `Clients · ${x.attendees.length}`} plain>
        <VStack gap={space.md}>
          {x.attendees.map((a) => (
            <Card key={a.attendeeId}>
              <HStack gap={space.md}>
                <Avatar name={a.name} size={40} />
                <VStack gap={0} style={{ flex: 1 }}>
                  <T variant="headline">{a.name}</T>
                  <T variant="footnote" tone={(balance.get(a.clientId) ?? 0) <= 0 ? 'red' : 'label2'}>
                    {balance.get(a.clientId) ?? 0} session{(balance.get(a.clientId) ?? 0) === 1 ? '' : 's'} left
                  </T>
                </VStack>
                <Button label="Train" icon="play" size="small" variant="tinted" onPress={async () => {
                  if (!db) return;
                  const w = await openOrStartWorkout(db, a.clientId, { sessionId: x.id });
                  if (w.created) touch();
                  router.push({ pathname: '/workout/[id]', params: { id: w.id, client: a.clientId, name: a.name } });
                }} />
              </HStack>
              {!cancelled ? (
                <>
                  <Spacer size={space.md} />
                  <Segmented
                    options={OUTCOMES}
                    value={(OUTCOMES.some((o) => o.value === a.status) ? a.status : '') as (typeof OUTCOMES)[number]['value']}
                    onChange={(v) => { void mark(a.attendeeId, a.clientId, a.name, v); }}
                  />
                </>
              ) : null}
            </Card>
          ))}
          {x.attendees.length === 0 ? <Card><T variant="body" tone="label2">Nobody is booked into this slot.</T></Card> : null}
        </VStack>
      </Section>

      {!cancelled ? (
        <>
          <Section title="Move" action={moving ? 'Close' : 'Change Time'} onAction={() => setMoving(!moving)} plain>
            {moving ? (
              <VStack gap={space.md}>
                <WhenPicker day={day} minutes={minutes} onDay={setDay} onMinutes={setMinutes} />
                {(conflicts.data ?? []).length > 0 ? <Banner tone="orange" message="That overlaps another session." /> : null}
                <Button label={`Move to ${target.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })}, ${timeLabel(minutes)}`}
                  variant="filled" full onPress={() => { void move(); }} />
              </VStack>
            ) : (
              <Card><T variant="subhead" tone="label2">Clients keep their place — only the time changes.</T></Card>
            )}
          </Section>

          <Section footer="Cancelling here means you called it off — nobody is charged. A client cancelling late is an outcome above, and does use a credit.">
            <ListRow first title="Cancel Session" destructive accessory="none" onPress={() => setConfirmCancel(true)} />
          </Section>
        </>
      ) : null}

      <ActionSheet
        visible={confirmCancel}
        title="Cancel this session?"
        message="Everyone on it is taken off the schedule. No credits are used."
        onClose={() => setConfirmCancel(false)}
        actions={[{ label: 'Cancel Session', destructive: true, onPress: () => { void cancel(); } }]}
      />
      <ActionSheet
        visible={!!overdraft}
        title={overdraft ? `${overdraft.name} has no sessions left` : undefined}
        message="Record this session as an overdraft to settle when they renew?"
        onClose={() => setOverdraft(null)}
        actions={overdraft ? [{ label: 'Record as Overdraft', icon: 'invoice', hue: 'orange', onPress: overdraft.go }] : []}
      />
    </Body>
  );
}
