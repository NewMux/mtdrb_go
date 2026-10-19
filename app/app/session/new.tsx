/**
 * Booking a session.
 *
 * Built for thumbs rather than date pickers: tap who, tap a day, tap a time.
 * Times that would clash with something already booked are struck through as
 * you look, so a double-booking is caught before it is made — the server's
 * constraint is the real guard, but finding out at sync time is too late to
 * fix in front of the client.
 *
 * "Repeat weekly" books the same slot for a run of weeks in one go, which is
 * how most personal training is actually sold.
 */

import React, { useMemo, useState } from 'react';
import { Pressable, View } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';

import { listClients } from '@/features/queries';
import { bookSession, clashes, sessionsBetween, sessionTypes } from '@/features/scheduling';
import { useApp, useQuery } from '@/state/app';
import {
  Avatar, Banner, Body, Button, FieldRow, HStack, Section, SearchField, Segmented, T, tap, VStack,
} from '@/ui/components';
import { Icon } from '@/ui/icon';
import { makeStyles, radius, space, useTheme } from '@/ui/theme';
import { timeLabel, WhenPicker } from '@/ui/when-picker';

const REPEATS = [
  { value: '1', label: 'Once' },
  { value: '4', label: '4 weeks' },
  { value: '8', label: '8 weeks' },
  { value: '12', label: '12 weeks' },
] as const;

function addDays(d: Date, n: number) { const x = new Date(d); x.setDate(x.getDate() + n); return x; }
function startOfDay(d: Date) { return new Date(d.getFullYear(), d.getMonth(), d.getDate()); }

export default function BookSessionScreen() {
  const params = useLocalSearchParams<{ at?: string; client?: string }>();
  const { db, touch, syncNow } = useApp();
  const router = useRouter();
  const s = useStyles();
  const { c } = useTheme();

  const initial = params.at ? new Date(params.at) : (() => {
    const d = new Date(); d.setMinutes(0, 0, 0); d.setHours(d.getHours() + 1); return d;
  })();

  const [clientIds, setClientIds] = useState<string[]>(params.client ? [params.client] : []);
  const [search, setSearch] = useState('');
  const [picking, setPicking] = useState(!params.client);
  const [day, setDay] = useState(startOfDay(initial));
  const [minutes, setMinutes] = useState(initial.getHours() * 60 + initial.getMinutes());
  const [typeId, setTypeId] = useState<string | null>(null);
  const [repeat, setRepeat] = useState<(typeof REPEATS)[number]['value']>('1');
  const [location, setLocation] = useState('');
  const [busy, setBusy] = useState(false);

  const types = useQuery((d) => sessionTypes(d), []);
  const clients = useQuery((d) => listClients(d, search), [search]);
  const everyone = useQuery((d) => listClients(d, ''), []);
  const dayBookings = useQuery((d) => sessionsBetween(d, day, addDays(day, 1)), [day.getTime()]);

  const type = (types.data ?? []).find((t) => t.id === typeId) ?? (types.data ?? [])[0] ?? null;
  const duration = type?.durationMinutes ?? 60;
  const capacity = type?.capacity ?? 1;

  const startsAt = useMemo(() => {
    const d = new Date(day); d.setHours(Math.floor(minutes / 60), minutes % 60, 0, 0); return d;
  }, [day, minutes]);

  const conflicts = useQuery((d) => clashes(d, startsAt, duration), [startsAt.getTime(), duration]);

  const taken = (m: number) => (dayBookings.data ?? []).some((x) => {
    const st = new Date(day); st.setHours(Math.floor(m / 60), m % 60, 0, 0);
    const en = new Date(st.getTime() + duration * 60_000);
    return new Date(x.startsAt) < en && new Date(x.endsAt) > st;
  });

  const names = new Map((everyone.data ?? []).map((x) => [x.id, x.fullName]));
  const tooMany = clientIds.length > capacity;
  const inPast = startsAt.getTime() < Date.now() - 15 * 60_000;

  const toggle = (id: string) => {
    tap('select');
    setClientIds((ids) => ids.includes(id) ? ids.filter((x) => x !== id) : capacity === 1 ? [id] : [...ids, id]);
    if (capacity === 1) setPicking(false);
  };

  const book = async () => {
    if (!db || !type || clientIds.length === 0) return;
    setBusy(true);
    await bookSession(db, {
      sessionTypeId: type.id, startsAt, durationMinutes: duration, clientIds,
      location: location.trim(), weeks: Number(repeat),
    });
    tap('success');
    touch();
    void syncNow();
    setBusy(false);
    router.back();
  };

  const count = Number(repeat);

  return (
    <Body keyboard>
      {(types.data ?? []).length === 0 && !types.loading ? (
        <Banner style={{ marginHorizontal: space.lg }} tone="orange" message="No session types have synced to this device yet. Connect once to pull them down." />
      ) : null}

      <Section title="Who" action={picking ? 'Done' : 'Change'} onAction={() => setPicking(!picking)} plain>
        {!picking ? (
          <View style={s.card}>
            {clientIds.length === 0 ? (
              <Pressable onPress={() => setPicking(true)} style={{ padding: space.lg }}>
                <T variant="body" tone="tint">Choose a client</T>
              </Pressable>
            ) : (
              <HStack gap={space.sm} wrap style={{ padding: space.md }}>
                {clientIds.map((id) => (
                  <View key={id} style={s.pill}>
                    <Avatar name={names.get(id) ?? '?'} size={26} />
                    <T variant="subhead" weight="600">{names.get(id) ?? 'Client'}</T>
                  </View>
                ))}
              </HStack>
            )}
          </View>
        ) : (
          <View style={s.card}>
            <View style={{ padding: space.md }}>
              <SearchField value={search} onChangeText={setSearch} placeholder="Find a client" />
            </View>
            {(clients.data ?? []).slice(0, 8).map((x) => {
              const on = clientIds.includes(x.id);
              return (
                <Pressable key={x.id} accessibilityRole="checkbox" accessibilityState={{ checked: on }} onPress={() => toggle(x.id)}
                  style={({ pressed }) => [s.pickRow, pressed && { backgroundColor: c.fill2 }]}>
                  <Avatar name={x.fullName} size={32} />
                  <T variant="body" style={{ flex: 1 }}>{x.fullName}</T>
                  <Icon name={on ? 'checkCircle' : 'circle'} size={22} color={on ? c.tint : c.label3} />
                </Pressable>
              );
            })}
            {(clients.data ?? []).length === 0 ? (
              <T variant="subhead" tone="label2" style={{ padding: space.lg }}>No clients match.</T>
            ) : null}
          </View>
        )}
      </Section>

      {(types.data ?? []).length > 1 ? (
        <Section title="Type" plain>
          <Segmented
            options={(types.data ?? []).map((t) => ({ value: t.id, label: t.name }))}
            value={type?.id ?? ''}
            onChange={setTypeId}
          />
          <T variant="footnote" tone="label2" style={{ marginTop: 6, marginLeft: 4 }}>
            {duration} min · up to {capacity} {capacity === 1 ? 'client' : 'clients'} · {type?.creditCost ?? 1} credit{(type?.creditCost ?? 1) === 1 ? '' : 's'} each
          </T>
        </Section>
      ) : null}

      <Section title="When" plain>
        <WhenPicker day={day} minutes={minutes} onDay={setDay} onMinutes={setMinutes} taken={taken} />
      </Section>

      <Section title="Repeat" plain>
        <Segmented options={REPEATS} value={repeat} onChange={setRepeat} />
      </Section>

      <Section>
        <FieldRow first label="Location" value={location} onChangeText={setLocation} placeholder="Studio, park, client's home" />
      </Section>

      <View style={{ paddingHorizontal: space.lg, marginTop: space.lg, gap: space.md }}>
        {(conflicts.data ?? []).length > 0 ? (
          <Banner tone="orange" title="This clashes" message={`Overlaps ${(conflicts.data ?? []).map((x) => x.attendees.map((a) => a.name).join(', ') || x.typeName).join('; ')}. You can still book it — the server will refuse it if the slot is truly taken.`} />
        ) : null}
        {tooMany ? <Banner tone="red" message={`This session type seats ${capacity}. Choose a group type or fewer clients.`} /> : null}
        {inPast ? <Banner tone="gray" message="That time has already passed — fine for recording a session after the fact." /> : null}
        <VStack gap={4} align="center">
          <T variant="subhead" tone="label2">
            {startsAt.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' })} · {timeLabel(minutes)}–{timeLabel(minutes + duration)}
          </T>
        </VStack>
        <Button
          label={count > 1 ? `Book ${count} Sessions` : 'Book Session'}
          icon="calendar"
          variant="filled"
          full
          busy={busy}
          disabled={!type || clientIds.length === 0 || tooMany}
          onPress={() => { void book(); }}
        />
      </View>
    </Body>
  );
}

const useStyles = makeStyles((c) => ({
  card: { backgroundColor: c.surface, borderRadius: radius.lg, overflow: 'hidden' },
  pill: { flexDirection: 'row', alignItems: 'center', gap: 6, paddingRight: 12, paddingLeft: 4, height: 34, borderRadius: radius.pill, backgroundColor: c.fill2 },
  pickRow: { flexDirection: 'row', alignItems: 'center', gap: space.md, paddingHorizontal: space.lg, paddingVertical: 10, borderTopWidth: 0.5, borderTopColor: c.separator },
}));
