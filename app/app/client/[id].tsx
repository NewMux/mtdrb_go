/**
 * A client.
 *
 * Laid out like an iOS contact card: who they are and the four things a
 * trainer does with someone — message, book, train, sell — then four views.
 *
 * Overview is what a trainer checks before the client walks in: sessions
 * left, where they are in their programme, anything flagged, what is booked.
 * Training is the lifting record; Progress is the body; Billing is the money.
 */

import React, { useCallback, useMemo, useState } from 'react';
import { Linking, Pressable, View } from 'react-native';
import { Stack, useFocusEffect, useLocalSearchParams, useRouter } from 'expo-router';

import { recordBiometrics, updateClient } from '@/features/actions';
import { adherence, clientFlags, liftRecords, type LiftRecord } from '@/features/insights';
import { MESSAGE_TEMPLATES, sendMessage } from '@/features/messages';
import { nextSession } from '@/features/programs';
import {
  biometricHistory, clientDetail, clientPackages, clientPayments, outstandingInvoices, recentWorkouts,
} from '@/features/queries';
import { upcomingFor } from '@/features/scheduling';
import { openOrStartWorkout } from '@/features/workflow';
import { useApp, useQuery } from '@/state/app';
import { LineChart, Ring, Sparkline } from '@/ui/charts';
import {
  Avatar, Banner, Body, Button, Card, EmptyState, Field, HStack, IconTile, ListRow, NumberField,
  ProgressBar, Section, Segmented, Spacer, T, Tag, tap, VStack,
} from '@/ui/components';
import { Icon, type IconName } from '@/ui/icon';
import { ActionSheet } from '@/ui/sheet';
import {
  bodyFat as formatBodyFat, clockTime, dueLabel, load as formatLoad, money, parseBodyFat, parseLoad, shortDate,
} from '@/ui/format';
import { makeStyles, radius, space, useTheme, wash, type Hue } from '@/ui/theme';

type Tab = 'overview' | 'training' | 'progress' | 'billing';

export default function ClientScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const { db, account, touch, syncNow, prefs } = useApp();
  const router = useRouter();
  const s = useStyles();
  const [tab, setTab] = useState<Tab>('overview');
  const [messaging, setMessaging] = useState<'pick' | 'whatsapp' | 'sms' | null>(null);

  const client = useQuery((d) => clientDetail(d, id), [id]);
  const plan = useQuery((d) => nextSession(d, id), [id]);
  const upcoming = useQuery((d) => upcomingFor(d, id, 4), [id]);
  const flags = useQuery((d) => clientFlags(d, id), [id]);
  const stick = useQuery((d) => adherence(d, id), [id]);
  const packs = useQuery((d) => clientPackages(d, id), [id]);
  const invoices = useQuery((d) => outstandingInvoices(d, id), [id]);

  const reload = useCallback(() => {
    client.reload(); plan.reload(); upcoming.reload(); flags.reload(); stick.reload(); packs.reload(); invoices.reload();
  }, [client.reload, plan.reload, upcoming.reload, flags.reload, stick.reload, packs.reload, invoices.reload]);
  useFocusEffect(useCallback(() => { reload(); }, [reload]));

  const profile = client.data;
  const currency = account?.currency ?? 'EUR';

  const train = async () => {
    if (!db || !profile) return;
    const { id: workoutId, created } = await openOrStartWorkout(db, profile.id);
    if (created) touch();
    router.push({ pathname: '/workout/[id]', params: { id: workoutId, client: profile.id, name: profile.fullName } });
  };

  if (!profile) {
    return (
      <Body>
        <EmptyState icon="person" title={client.loading ? 'Loading…' : 'Not on this device'} message={client.loading ? undefined : 'This client has not synced here yet.'} />
      </Body>
    );
  }

  const owed = (invoices.data ?? []).reduce((n, i) => n + i.totalMinor - i.paidMinor, 0);
  const activePack = (packs.data ?? []).find((p) => p.status === 'active' || p.status === 'exhausted');
  const firstName = profile.fullName.split(' ')[0] ?? profile.fullName;

  const messageCtx = {
    firstName,
    trainerName: (account?.display_name ?? '').split(' ')[0] || 'Your coach',
    creditsRemaining: profile.creditsRemaining,
    nextSession: upcoming.data?.[0] ? new Date(upcoming.data[0].startsAt).toLocaleString(undefined, { weekday: 'long', hour: '2-digit', minute: '2-digit' }) : null,
    owed: owed > 0 ? money(owed, currency) : null,
  };

  return (
    <>
      <Stack.Screen options={{ title: '' }} />
      <Body>
        {/* Contact card */}
        <VStack align="center" gap={space.xs} style={{ paddingHorizontal: space.lg }}>
          <Avatar name={profile.fullName} size={88} />
          <Spacer size={space.sm} />
          <T variant="title1" align="center">{profile.fullName}</T>
          <T variant="subhead" tone="label2" align="center">
            {[profile.phone, profile.email].filter(Boolean).join(' · ') || 'No contact details'}
          </T>
          {(flags.data ?? []).length > 0 ? (
            <HStack gap={6} style={{ marginTop: space.sm }} wrap justify="center">
              {(flags.data ?? []).map((f) => <Tag key={f.key} label={f.label} hue={f.hue} icon="warning" />)}
            </HStack>
          ) : null}
        </VStack>

        <View style={s.actions}>
          <QuickAction icon="message" label="Message" onPress={() => setMessaging('pick')} />
          <QuickAction icon="calendar" label="Book" onPress={() => router.push({ pathname: '/session/new', params: { client: profile.id } })} />
          <QuickAction icon="play" label="Train" onPress={() => { void train(); }} />
          <QuickAction icon="cart" label="Sell" onPress={() => router.push({ pathname: '/sell-package', params: { client: profile.id, name: profile.fullName } })} />
        </View>

        <View style={{ paddingHorizontal: space.lg }}>
          <Segmented
            options={[
              { value: 'overview', label: 'Overview' },
              { value: 'training', label: 'Training' },
              { value: 'progress', label: 'Progress' },
              { value: 'billing', label: 'Billing' },
            ] as const}
            value={tab}
            onChange={setTab}
          />
        </View>

        {tab === 'overview' ? (
          <>
            <View style={s.duo}>
              <Card style={{ flex: 1 }} onPress={() => setTab('billing')} accessibilityLabel="Sessions left">
                <HStack gap={space.md}>
                  <Ring
                    value={activePack && activePack.creditsTotal > 0 ? Math.max(0, profile.creditsRemaining) / activePack.creditsTotal : 0}
                    hue={profile.creditsRemaining <= 0 ? 'red' : profile.creditsRemaining <= 2 ? 'orange' : 'green'}
                    size={54} stroke={7}
                  >
                    <T variant="headline">{profile.creditsRemaining}</T>
                  </Ring>
                  <VStack gap={0} style={{ flex: 1 }}>
                    <T variant="footnote" tone="label2">Sessions left</T>
                    <T variant="subhead" weight="600" lines={2}>
                      {profile.creditsRemaining < 0 ? 'Overdrawn' : activePack ? `of ${activePack.creditsTotal}` : 'No pack'}
                    </T>
                  </VStack>
                </HStack>
              </Card>
              <Card style={{ flex: 1 }}>
                <HStack gap={space.md}>
                  <Ring value={stick.data?.rate ?? 0} hue="pink" size={54} stroke={7}>
                    <T variant="footnote" weight="700">{stick.data?.rate == null ? '—' : `${Math.round(stick.data.rate * 100)}%`}</T>
                  </Ring>
                  <VStack gap={0} style={{ flex: 1 }}>
                    <T variant="footnote" tone="label2">Attendance</T>
                    <T variant="subhead" weight="600">{stick.data ? `${stick.data.done} of ${stick.data.total}` : '—'}</T>
                  </VStack>
                </HStack>
              </Card>
            </View>

            {profile.creditsRemaining <= 2 ? (
              <Banner
                style={{ marginHorizontal: space.lg, marginTop: space.md }}
                tone={profile.creditsRemaining <= 0 ? 'red' : 'orange'}
                icon="cart"
                title={profile.creditsRemaining <= 0 ? 'Out of sessions' : 'Running low'}
                message={profile.creditsRemaining <= 0 ? 'Sell a renewal before the next session.' : `${profile.creditsRemaining} session${profile.creditsRemaining === 1 ? '' : 's'} left — a good time to offer the next pack.`}
                action="Sell a Package"
                onAction={() => router.push({ pathname: '/sell-package', params: { client: profile.id, name: profile.fullName } })}
              />
            ) : null}

            <Section title="Program" action={plan.data ? 'Change' : undefined} onAction={() => router.push({ pathname: '/program/assign', params: { client: profile.id } })} plain>
              {plan.data ? (
                <Card>
                  <HStack gap={space.md}>
                    <IconTile icon="programs" hue="tint" size={40} />
                    <VStack gap={0} style={{ flex: 1 }}>
                      <T variant="headline" lines={1}>{plan.data.program.name}</T>
                      <T variant="subhead" tone="label2">Week {Math.min(plan.data.week, plan.data.totalWeeks)} of {plan.data.totalWeeks}</T>
                    </VStack>
                  </HStack>
                  <Spacer size={space.md} />
                  <ProgressBar value={plan.data.completed / Math.max(1, plan.data.planned)} />
                  <T variant="footnote" tone="label2" style={{ marginTop: 6 }}>
                    {plan.data.completed} of {plan.data.planned} workouts done
                  </T>
                  <Spacer size={space.md} />
                  <View style={s.nextUp}>
                    <VStack gap={2} style={{ flex: 1 }}>
                      <T variant="caption2" tone="label2" weight="700">UP NEXT</T>
                      <T variant="headline">{plan.data.day.name}</T>
                      <T variant="footnote" tone="label2" lines={1}>
                        {plan.data.day.prescriptions.map((p) => p.exercise_name).slice(0, 3).join(', ')}
                        {plan.data.day.prescriptions.length > 3 ? ` +${plan.data.day.prescriptions.length - 3}` : ''}
                      </T>
                    </VStack>
                    <Button label="Start" icon="play" variant="filled" size="medium" onPress={() => { void train(); }} />
                  </View>
                </Card>
              ) : (
                <Card>
                  <EmptyState
                    icon="programs"
                    title="No program yet"
                    message="Assign one and every workout opens on the right day with targets filled in."
                    action="Assign a Program"
                    onAction={() => router.push({ pathname: '/program/assign', params: { client: profile.id } })}
                  />
                </Card>
              )}
            </Section>

            <Section title="Upcoming" action="Book" onAction={() => router.push({ pathname: '/session/new', params: { client: profile.id } })}>
              {(upcoming.data ?? []).length === 0 ? (
                <ListRow first title="Nothing booked" titleTone="label2" accessory="none" />
              ) : (upcoming.data ?? []).map((u, i) => (
                <ListRow
                  key={u.sessionId}
                  first={i === 0}
                  icon="calendar"
                  hue="tint"
                  title={new Date(u.startsAt).toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'short' })}
                  subtitle={`${clockTime(u.startsAt)} · ${u.typeName}`}
                  onPress={() => router.push({ pathname: '/session/[id]', params: { id: u.sessionId } })}
                />
              ))}
            </Section>

            <Notes clientId={profile.id} initial={profile.notes} onSaved={() => { touch(); void syncNow(); }} />
          </>
        ) : null}

        {tab === 'training' ? <TrainingTab clientId={profile.id} units={prefs.units} /> : null}
        {tab === 'progress' ? <ProgressTab clientId={profile.id} units={prefs.units} /> : null}
        {tab === 'billing' ? (
          <BillingTab
            clientId={profile.id}
            currency={currency}
            onSell={() => router.push({ pathname: '/sell-package', params: { client: profile.id, name: profile.fullName } })}
            onPay={(invoice) => router.push({ pathname: '/payment', params: { invoice } })}
          />
        ) : null}
      </Body>

      <ActionSheet
        visible={messaging === 'pick'}
        title={`Message ${firstName}`}
        onClose={() => setMessaging(null)}
        actions={[
          { label: 'WhatsApp', icon: 'message', hue: 'green', onPress: () => setTimeout(() => setMessaging('whatsapp'), 250) },
          { label: 'Text Message', icon: 'message', hue: 'tint', onPress: () => setTimeout(() => setMessaging('sms'), 250) },
          ...(profile.phone ? [{ label: 'Call', icon: 'phone' as IconName, hue: 'green' as Hue, onPress: () => { void Linking.openURL(`tel:${profile.phone}`); } }] : []),
        ]}
      />
      <ActionSheet
        visible={messaging === 'whatsapp' || messaging === 'sms'}
        title="Choose a message"
        message="It opens ready to send — edit it first if you like."
        onClose={() => setMessaging(null)}
        actions={MESSAGE_TEMPLATES.map((t) => {
          const text = t.build(messageCtx);
          const channel = messaging === 'sms' ? 'sms' : 'whatsapp';
          return { label: t.label, detail: text, onPress: () => { void sendMessage(channel, profile.phone, text); } };
        })}
      />
    </>
  );
}

function QuickAction({ icon, label, onPress }: { icon: IconName; label: string; onPress: () => void }) {
  const { c } = useTheme();
  const s = useStyles();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      onPress={() => { tap(); onPress(); }}
      style={({ pressed }) => [s.quick, pressed && { opacity: 0.7, transform: [{ scale: 0.96 }] }]}
    >
      <Icon name={icon} size={20} color={c.tint} />
      <T variant="caption" weight="600" tone="tint">{label}</T>
    </Pressable>
  );
}

function Notes({ clientId, initial, onSaved }: { clientId: string; initial: string; onSaved: () => void }) {
  const { db } = useApp();
  const [text, setText] = useState(initial);
  const [editing, setEditing] = useState(false);
  const save = async () => {
    if (!db) return;
    await updateClient(db, clientId, { notes: text });
    setEditing(false);
    onSaved();
  };
  return (
    <Section title="Coach Notes" action={editing ? 'Save' : 'Edit'} onAction={() => { if (editing) void save(); else setEditing(true); }} plain
      footer="Goals, injuries, preferences — anything you want in front of you before a session.">
      {editing ? (
        <Field value={text} onChangeText={setText} multiline autoFocus placeholder="e.g. Left knee — avoid deep lunges. Goal: 100 kg squat by March." />
      ) : (
        <Card onPress={() => setEditing(true)}>
          <T variant="body" tone={text ? 'label' : 'label3'}>{text || 'No notes yet. Tap to add.'}</T>
        </Card>
      )}
    </Section>
  );
}

function TrainingTab({ clientId, units }: { clientId: string; units: 'kg' | 'lb' }) {
  const router = useRouter();
  const lifts = useQuery((d) => liftRecords(d, clientId, 6), [clientId]);
  const workouts = useQuery((d) => recentWorkouts(d, clientId, 8), [clientId]);
  const [open, setOpen] = useState<string | null>(null);

  return (
    <>
      <Section title="Personal Records" plain footer="Estimated one-rep max from the best working set (Epley). Tap a lift to see its trend.">
        {(lifts.data ?? []).length === 0 ? (
          <Card><EmptyState icon="trophy" hue="yellow" title="No lifts logged yet" message="Records appear after the first workout." /></Card>
        ) : (
          <Card padded={false}>
            {(lifts.data ?? []).map((l, i) => (
              <LiftRow key={l.exerciseId} lift={l} units={units} first={i === 0} open={open === l.exerciseId} onPress={() => setOpen(open === l.exerciseId ? null : l.exerciseId)} />
            ))}
          </Card>
        )}
      </Section>

      <Section title="Recent Workouts">
        {(workouts.data ?? []).length === 0 ? (
          <ListRow first title="No workouts logged yet" titleTone="label2" accessory="none" />
        ) : (workouts.data ?? []).map((w, i) => (
          <ListRow
            key={w.id}
            first={i === 0}
            icon={w.status === 'completed' ? 'checkCircle' : 'play'}
            hue={w.status === 'completed' ? 'green' : 'orange'}
            title={shortDate(w.performedOn)}
            subtitle={`${w.setCount} sets${w.volumeGrams > 0 ? ` · ${formatLoad(w.volumeGrams, units).replace(/\.\d+/, '')} moved` : ''}`}
            value={w.status === 'completed' ? undefined : 'In progress'}
            valueTone="orange"
            onPress={() => router.push({ pathname: '/workout/[id]', params: { id: w.id, client: clientId } })}
          />
        ))}
      </Section>
    </>
  );
}

function LiftRow({ lift, units, first, open, onPress }: { lift: LiftRecord; units: 'kg' | 'lb'; first: boolean; open: boolean; onPress: () => void }) {
  const { c } = useTheme();
  const history = lift.history.map((h) => h.e1rm);
  const gain = history.length > 1 ? history[history.length - 1]! - history[0]! : 0;
  return (
    <View style={!first ? { borderTopWidth: 0.5, borderTopColor: c.separator } : undefined}>
      <Pressable accessibilityRole="button" accessibilityState={{ expanded: open }} onPress={() => { tap('select'); onPress(); }} style={({ pressed }) => [{ flexDirection: 'row', alignItems: 'center', gap: space.md, padding: space.lg }, pressed && { backgroundColor: c.fill2 }]}>
        <IconTile icon="trophy" hue="yellow" size={32} />
        <VStack gap={0} style={{ flex: 1 }}>
          <T variant="body" weight="600" lines={1}>{lift.name}</T>
          <T variant="footnote" tone="label2">
            {formatLoad(lift.bestLoadGrams, units)} × {lift.bestReps} · {shortDate(lift.on)}
          </T>
        </VStack>
        <VStack gap={2} align="flex-end">
          <T variant="headline">{formatLoad(halfKg(lift.bestE1rmGrams), units)}</T>
          {gain > 0 ? <T variant="caption" tone="green" weight="600">▲ {formatLoad(halfKg(gain), units)}</T> : <Sparkline values={history} hue="yellow" width={48} height={16} />}
        </VStack>
      </Pressable>
      {open && lift.history.length > 1 ? (
        <View style={{ paddingHorizontal: space.lg, paddingBottom: space.lg }}>
          <LineChart
            hue="orange"
            data={lift.history.map((h) => ({ label: shortDate(h.on), value: halfKg(h.e1rm) }))}
            format={(v) => formatLoad(v, units)}
          />
        </View>
      ) : null}
    </View>
  );
}

/** An estimate is not worth more precision than a half-kilo plate. */
function halfKg(grams: number): number {
  return Math.round(grams / 500) * 500;
}

function ProgressTab({ clientId, units }: { clientId: string; units: 'kg' | 'lb' }) {
  const { db, touch, syncNow } = useApp();
  const history = useQuery((d) => biometricHistory(d, clientId, 24), [clientId]);
  const [adding, setAdding] = useState(false);
  const [weight, setWeight] = useState('');
  const [fat, setFat] = useState('');

  const rows = history.data ?? [];
  const chrono = useMemo(() => [...rows].reverse(), [rows]);
  const weights = chrono.filter((r) => r.weightGrams != null).map((r) => ({ label: shortDate(r.measuredOn), value: r.weightGrams! }));
  const fats = chrono.filter((r) => r.bodyFatBP != null).map((r) => ({ label: shortDate(r.measuredOn), value: r.bodyFatBP! }));
  const weightChange = weights.length > 1 ? weights[weights.length - 1]!.value - weights[0]!.value : null;

  const save = async () => {
    if (!db) return;
    const w = parseLoad(weight, units);
    const f = parseBodyFat(fat);
    if (w === null && f === null) return;
    await recordBiometrics(db, clientId, { weightGrams: w ?? undefined, bodyFatBP: f ?? undefined });
    tap('success');
    setWeight(''); setFat(''); setAdding(false);
    touch(); history.reload(); void syncNow();
  };

  return (
    <>
      <Section title="Body Weight" action={adding ? 'Cancel' : 'Add'} onAction={() => setAdding(!adding)} plain>
        {adding ? (
          <Card style={{ marginBottom: space.md }}>
            <HStack gap={space.md}>
              <NumberField label="Weight" unit={units} value={weight} onChangeText={setWeight} placeholder={units === 'kg' ? '82.4' : '181'} />
              <NumberField label="Body fat" unit="%" value={fat} onChangeText={setFat} placeholder="15.5" />
            </HStack>
            <Spacer size={space.md} />
            <Button label="Save Measurement" variant="filled" full disabled={parseLoad(weight, units) === null && parseBodyFat(fat) === null} onPress={() => { void save(); }} />
          </Card>
        ) : null}
        <Card>
          {weights.length > 0 ? (
            <>
              <LineChart data={weights} hue="tint" format={(v) => formatLoad(v, units)} />
              {weightChange !== null ? (
                <T variant="footnote" weight="600" tone={weightChange <= 0 ? 'green' : 'orange'} style={{ marginTop: space.sm }}>
                  {weightChange <= 0 ? '▼' : '▲'} {formatLoad(Math.abs(weightChange), units)} since {weights[0]!.label}
                </T>
              ) : null}
            </>
          ) : (
            <EmptyState icon="scale" title="No measurements yet" message="Record a weigh-in to start the trend." action={adding ? undefined : 'Add Measurement'} onAction={() => setAdding(true)} />
          )}
        </Card>
      </Section>

      {fats.length > 0 ? (
        <Section title="Body Fat" plain>
          <Card><LineChart data={fats} hue="purple" format={(v) => formatBodyFat(v)} /></Card>
        </Section>
      ) : null}

      {rows.length > 0 ? (
        <Section title="History">
          {rows.slice(0, 10).map((r, i) => (
            <ListRow key={r.id} first={i === 0} title={shortDate(r.measuredOn)}
              value={[r.weightGrams != null ? formatLoad(r.weightGrams, units) : null, r.bodyFatBP != null ? formatBodyFat(r.bodyFatBP) : null].filter(Boolean).join(' · ')}
              accessory="none" />
          ))}
        </Section>
      ) : null}
    </>
  );
}

function BillingTab({ clientId, currency, onSell, onPay }: { clientId: string; currency: string; onSell: () => void; onPay: (invoiceId: string) => void }) {
  const { c } = useTheme();
  const packs = useQuery((d) => clientPackages(d, clientId), [clientId]);
  const invoices = useQuery((d) => outstandingInvoices(d, clientId), [clientId]);
  const payments = useQuery((d) => clientPayments(d, clientId), [clientId]);

  return (
    <>
      <Section title="Packages" action="Sell" onAction={onSell}>
        {(packs.data ?? []).length === 0 ? (
          <ListRow first title="No packages yet" titleTone="label2" accessory="none" />
        ) : (packs.data ?? []).map((p, i) => (
          <View key={p.id} style={[{ padding: space.lg, gap: space.sm }, i > 0 && { borderTopWidth: 0.5, borderTopColor: c.separator }]}>
            <HStack justify="space-between">
              <T variant="body" weight="600" lines={1} style={{ flex: 1 }}>{p.name}</T>
              <Tag label={p.status === 'active' ? 'Active' : p.status === 'exhausted' ? 'Used up' : p.status} hue={p.status === 'active' ? 'green' : 'gray'} />
            </HStack>
            <ProgressBar value={p.creditsTotal > 0 ? (p.creditsTotal - p.creditsRemaining) / p.creditsTotal : 0} hue={p.creditsRemaining <= 2 ? 'orange' : 'tint'} />
            <T variant="footnote" tone="label2">
              {p.creditsRemaining} of {p.creditsTotal} left · {money(p.unitPriceMinor, p.currency || currency)} each
              {p.expiresAt ? ` · expires ${shortDate(p.expiresAt)}` : ''}
            </T>
          </View>
        ))}
      </Section>

      <Section title="Outstanding">
        {(invoices.data ?? []).length === 0 ? (
          <ListRow first icon="checkCircle" hue="green" title="All paid up" accessory="none" />
        ) : (invoices.data ?? []).map((inv, i) => (
          <ListRow
            key={inv.id}
            first={i === 0}
            icon="invoice"
            hue={dueLabel(inv.dueDate).endsWith('overdue') ? 'red' : 'orange'}
            title={inv.number ?? 'Invoice'}
            subtitle={dueLabel(inv.dueDate) || 'No due date'}
            value={money(inv.totalMinor - inv.paidMinor, inv.currency || currency)}
            valueTone="label"
            onPress={() => onPay(inv.id)}
          />
        ))}
      </Section>

      <Section title="Payments">
        {(payments.data ?? []).length === 0 ? (
          <ListRow first title="No payments yet" titleTone="label2" accessory="none" />
        ) : (payments.data ?? []).map((p, i) => (
          <ListRow key={p.id} first={i === 0} icon="money" hue="green" title={money(p.amountMinor, p.currency || currency)}
            subtitle={[shortDate(p.receivedOn), p.invoiceNumber].filter(Boolean).join(' · ')} accessory="none" />
        ))}
      </Section>
    </>
  );
}

const useStyles = makeStyles((c) => ({
  actions: { flexDirection: 'row', gap: space.sm, paddingHorizontal: space.lg, marginTop: space.xl, marginBottom: space.lg },
  quick: {
    flex: 1, alignItems: 'center', justifyContent: 'center', gap: 4, height: 62,
    borderRadius: radius.md, backgroundColor: c.surface,
  },
  duo: { flexDirection: 'row', gap: space.md, marginHorizontal: space.lg, marginTop: space.lg },
  nextUp: {
    flexDirection: 'row', alignItems: 'center', gap: space.md, padding: space.md,
    borderRadius: radius.md, backgroundColor: wash(c.tint, 0.08),
  },
}));
