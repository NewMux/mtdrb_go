/**
 * The floor logger.
 *
 * Everything here is one tap or one nudge. The trainer is standing next to a
 * loaded bar with a client waiting, so the screen holds one exercise at a time
 * and the numbers are large enough to read from arm's length.
 *
 * When the workout follows a programme, it opens on the planned day: the
 * exercises in order, the prescribed sets laid out as rows waiting to be
 * ticked, and a load suggested from last time by double progression — shown
 * with its reason, never applied silently.
 *
 * "Repeat last time" writes last session's working sets in as *logged but not
 * completed* — visible, greyed, and waiting for a tap. Prefilled numbers that
 * counted as performed would put lifts in a client's history that never
 * happened, which is the one thing a training log must never do. Planned rows
 * follow the same rule: nothing is logged until it is ticked.
 */

import React, { useEffect, useMemo, useState } from 'react';
import { Pressable, ScrollView, View } from 'react-native';
import { Stack, useLocalSearchParams, useRouter } from 'expo-router';

import { cloneLastSession, completeWorkout, logSet } from '@/features/actions';
import { isPersonalRecord } from '@/features/insights';
import { workoutPlan, type Prescription } from '@/features/programs';
import { previousSets, searchExercises, workoutSets, type LoggedSet } from '@/features/queries';
import { fromUnits, plates, suggest, toUnits, increment, warmups } from '@/features/training';
import { useApp, useQuery } from '@/state/app';
import {
  Banner, Body, Button, Card, EmptyState, HStack, ListRow, SearchField, Section,
  Spacer, Stepper, T, Tag, tap, VStack,
} from '@/ui/components';
import { Icon } from '@/ui/icon';
import { RestTimer } from '@/ui/rest-timer';
import { ActionSheet } from '@/ui/sheet';
import { load as formatLoad, repRange, rest as formatRest, rpe as formatRpe } from '@/ui/format';
import { makeStyles, radius, space, useTheme, wash } from '@/ui/theme';

interface Slot {
  exerciseId: string;
  name: string;
  rx: Prescription | null;
  label: string | null;
}

const RPES = [6, 7, 8, 9, 10];

export default function WorkoutScreen() {
  const params = useLocalSearchParams<{ id: string; client?: string; name?: string }>();
  const workoutId = params.id;
  const { db, touch, syncNow, prefs } = useApp();
  const router = useRouter();
  const { c } = useTheme();
  const s = useStyles();
  const units = prefs.units;

  const meta = useQuery((d) => d.selectOne<{ clientId: string; name: string; status: string; startedAt: string | null }>(
    `SELECT w.client_id AS clientId, c.full_name AS name, w.status, w.created_at AS startedAt
       FROM workout_sessions w JOIN clients c ON c.id = w.client_id WHERE w.id = ?`, [workoutId]), [workoutId]);
  const clientId = params.client ?? meta.data?.clientId ?? '';
  const clientName = params.name ?? meta.data?.name ?? 'Workout';

  const plan = useQuery((d) => workoutPlan(d, workoutId), [workoutId]);
  const sets = useQuery((d) => workoutSets(d, workoutId), [workoutId]);
  const [extra, setExtra] = useState<{ id: string; name: string }[]>([]);
  const [current, setCurrent] = useState<string | null>(null);
  const [picking, setPicking] = useState(false);
  const [search, setSearch] = useState('');
  const results = useQuery((d) => (picking ? searchExercises(d, search, 30) : Promise.resolve([])), [picking, search]);

  const [loadValue, setLoadValue] = useState(0);
  const [reps, setReps] = useState(8);
  const [rpe, setRpe] = useState<number | null>(null);
  const [rest, setRest] = useState<{ endsAt: number; total: number } | null>(null);
  const [pr, setPr] = useState<string | null>(null);
  const [showPlates, setShowPlates] = useState(false);
  const [confirmFinish, setConfirmFinish] = useState(false);
  const [startedAt] = useState(() => Date.now());

  const all = sets.data ?? [];

  /** The exercises in play: the plan's, in order, then any added on the floor. */
  const slots: Slot[] = useMemo(() => {
    const out: Slot[] = [];
    const seen = new Set<string>();
    const counts = new Map<string, number>();
    for (const rx of plan.data?.day.prescriptions ?? []) {
      if (seen.has(rx.exercise_id)) continue;
      seen.add(rx.exercise_id);
      let label: string | null = null;
      if (rx.superset_group) {
        const n = (counts.get(rx.superset_group) ?? 0) + 1;
        counts.set(rx.superset_group, n);
        label = `${rx.superset_group}${n}`;
      }
      out.push({ exerciseId: rx.exercise_id, name: rx.exercise_name ?? 'Exercise', rx, label });
    }
    for (const set of all) {
      if (!seen.has(set.exerciseId)) { seen.add(set.exerciseId); out.push({ exerciseId: set.exerciseId, name: set.exerciseName, rx: null, label: null }); }
    }
    for (const e of extra) {
      if (!seen.has(e.id)) { seen.add(e.id); out.push({ exerciseId: e.id, name: e.name, rx: null, label: null }); }
    }
    return out;
  }, [plan.data, all, extra]);

  const active = slots.find((x) => x.exerciseId === current) ?? slots.find((x) => {
    const done = all.filter((st) => st.exerciseId === x.exerciseId && st.completed === 1 && st.isWarmup !== 1).length;
    return done < (x.rx?.target_sets ?? 1);
  }) ?? slots[0] ?? null;

  const last = useQuery((d) => (active && clientId
    ? previousSets(d, clientId, active.exerciseId, workoutId)
    : Promise.resolve([] as LoggedSet[])), [active?.exerciseId, clientId, workoutId]);
  const category = useQuery((d) => (active
    ? d.selectOne<{ category: string | null }>(`SELECT category FROM exercises WHERE id = ?`, [active.exerciseId])
    : Promise.resolve(null)), [active?.exerciseId]);

  const tip = useMemo(() => active ? suggest({
    previous: last.data ?? [],
    repsMin: active.rx?.target_reps_min, repsMax: active.rx?.target_reps_max,
    targetLoadGrams: active.rx?.target_load_grams, category: category.data?.category, units,
  }) : null, [active, last.data, category.data, units]);

  const mine = all.filter((st) => st.exerciseId === active?.exerciseId).sort((a, b) => a.setIndex - b.setIndex);
  const working = mine.filter((st) => st.isWarmup !== 1);
  const nextIndex = mine.reduce((m, st) => Math.max(m, st.setIndex), 0) + 1;
  const planned = Math.max(0, (active?.rx?.target_sets ?? 0) - working.length);

  // When the exercise changes, prefill from the suggestion, else last time, else the prescription.
  useEffect(() => {
    if (!active) return;
    const lastWorking = (last.data ?? []).filter((x) => x.completed === 1);
    const grams = tip?.loadGrams ?? lastWorking.at(-1)?.loadGrams ?? active.rx?.target_load_grams ?? 0;
    setLoadValue(toUnits(grams ?? 0, units));
    setReps(tip?.reps ?? active.rx?.target_reps_min ?? lastWorking.at(-1)?.reps ?? 8);
    setRpe(active.rx?.target_rpe_tenths ? active.rx.target_rpe_tenths / 10 : null);
    setShowPlates(false);
  }, [active?.exerciseId, tip?.loadGrams]); // eslint-disable-line react-hooks/exhaustive-deps

  const after = () => { touch(); sets.reload(); void syncNow(); };

  const startRest = () => {
    const seconds = active?.rx?.rest_seconds ?? prefs.restSeconds;
    if (seconds > 0) setRest({ endsAt: Date.now() + seconds * 1000, total: seconds });
  };

  const checkPr = async (loadGrams: number, r: number) => {
    if (!db || !active || !clientId) return;
    const result = await isPersonalRecord(db, clientId, active.exerciseId, workoutId, loadGrams, r);
    if (result.e1rm || result.load) {
      tap('success');
      setPr(`${active.name}: ${formatLoad(loadGrams, units)} × ${r}${result.load ? ' — heaviest ever' : ' — best estimated max'}`);
      setTimeout(() => setPr(null), 6000);
    }
  };

  const logNew = async (index = nextIndex, warmup = false, values?: { loadGrams: number; reps: number }) => {
    if (!db || !active) return;
    const grams = values?.loadGrams ?? fromUnits(loadValue, units);
    const r = values?.reps ?? reps;
    tap('success');
    await logSet(db, workoutId, active.exerciseId, index, {
      reps: r, loadGrams: grams, rpeTenths: warmup || rpe == null ? null : Math.round(rpe * 10),
      isWarmup: warmup, completed: true, programExerciseId: active.rx?.id ?? null,
    });
    after();
    if (!warmup) { startRest(); void checkPr(grams, r); }
  };

  /** Ticking a set toggles whether it was performed; a cloned set is confirmed by the same tap. */
  const toggle = async (set: LoggedSet) => {
    if (!db) return;
    const completing = set.completed !== 1;
    if (completing) tap('success');
    await logSet(db, workoutId, set.exerciseId, set.setIndex, {
      reps: set.reps, loadGrams: set.loadGrams, rpeTenths: set.rpeTenths,
      isWarmup: set.isWarmup === 1, completed: completing, programExerciseId: active?.rx?.id ?? null,
    });
    after();
    if (completing && set.isWarmup !== 1) { startRest(); if (set.loadGrams && set.reps) void checkPr(set.loadGrams, set.reps); }
  };

  const clone = async () => {
    if (!db || !active || !clientId) return;
    await cloneLastSession(db, workoutId, clientId, active.exerciseId);
    after();
  };

  const addWarmups = async () => {
    if (!db || !active) return;
    const ramp = warmups(fromUnits(loadValue, units), units);
    // Warm-ups go in ahead of the working sets as logged-but-not-completed rows.
    let index = nextIndex;
    for (const w of ramp) {
      await logSet(db, workoutId, active.exerciseId, index++, {
        reps: w.reps, loadGrams: w.loadGrams, isWarmup: true, completed: false, programExerciseId: active.rx?.id ?? null,
      });
    }
    after();
  };

  const finish = async () => {
    if (!db) return;
    await completeWorkout(db, workoutId);
    tap('success');
    touch();
    void syncNow();
    router.replace({
      pathname: '/workout/summary',
      params: { id: workoutId, name: clientName, minutes: String(Math.max(1, Math.round((Date.now() - startedAt) / 60_000))) },
    });
  };

  const pick = (id: string, name: string) => {
    setExtra((e) => [...e, { id, name }]);
    setCurrent(id);
    setPicking(false);
    setSearch('');
  };

  const doneSets = all.filter((x) => x.completed === 1 && x.isWarmup !== 1);
  const volume = doneSets.reduce((n, x) => n + (x.reps ?? 0) * (x.loadGrams ?? 0), 0);
  const step = toUnits(increment(category.data?.category, units), units) / (units === 'kg' ? 2 : 1);
  const plateLoad = plates(fromUnits(loadValue, units), units);

  return (
    <>
      <Stack.Screen options={{
        title: clientName,
        headerRight: () => (
          <Pressable accessibilityRole="button" onPress={() => setConfirmFinish(true)} hitSlop={8}>
            <T variant="headline" tone="tint">Finish</T>
          </Pressable>
        ),
      }} />
      <Body keyboard contentStyle={{ paddingBottom: rest ? 140 : space.xxl }}>
        {/* The session at a glance */}
        <View style={{ paddingHorizontal: space.lg }}>
          <HStack justify="space-between">
            <VStack gap={0}>
              <T variant="title2">{plan.data ? plan.data.day.name : 'Workout'}</T>
              <T variant="subhead" tone="label2">
                {plan.data ? `${plan.data.program.name} · Week ${plan.data.week}` : 'Free session'}
              </T>
            </VStack>
            <HStack gap={space.lg}>
              <VStack gap={0} align="center"><T variant="headline">{doneSets.length}</T><T variant="caption2" tone="label2">SETS</T></VStack>
              <VStack gap={0} align="center"><T variant="headline">{Math.round(toUnits(volume, units)).toLocaleString()}</T><T variant="caption2" tone="label2">{units.toUpperCase()} MOVED</T></VStack>
            </HStack>
          </HStack>
        </View>

        {pr ? <Banner style={{ marginHorizontal: space.lg, marginTop: space.md }} tone="yellow" icon="trophy" title="New personal record" message={pr} /> : null}

        {/* Exercise strip */}
        <View style={{ marginTop: space.lg }}>
          <Strip>
            {slots.map((x) => {
              const on = x.exerciseId === active?.exerciseId;
              const done = all.filter((st) => st.exerciseId === x.exerciseId && st.completed === 1 && st.isWarmup !== 1).length;
              const target = x.rx?.target_sets ?? 0;
              const complete = target > 0 && done >= target;
              return (
                <Pressable key={x.exerciseId} accessibilityRole="tab" accessibilityState={{ selected: on }}
                  onPress={() => { tap('select'); setCurrent(x.exerciseId); setPicking(false); }}
                  style={[s.chip, on && s.chipOn]}>
                  {complete ? <Icon name="checkCircle" size={14} color={on ? '#FFF' : c.green} /> : null}
                  {x.label ? <T variant="caption" weight="700" style={on ? s.onText : { color: c.purple }}>{x.label}</T> : null}
                  <T variant="subhead" weight="600" style={on ? s.onText : undefined} lines={1}>{x.name}</T>
                  {target > 0 ? <T variant="caption" style={on ? s.onTextDim : undefined} tone={on ? undefined : 'label2'}>{done}/{target}</T> : null}
                </Pressable>
              );
            })}
            <Pressable accessibilityRole="button" accessibilityLabel="Add exercise" onPress={() => setPicking(!picking)} style={[s.chip, picking && s.chipOn]}>
              <Icon name="plus" size={14} color={picking ? '#FFF' : c.tint} />
              <T variant="subhead" weight="600" style={picking ? s.onText : { color: c.tint }}>Exercise</T>
            </Pressable>
          </Strip>
        </View>

        {picking ? (
          <Section plain>
            <Card padded={false}>
              <View style={{ padding: space.md }}>
                <SearchField value={search} onChangeText={setSearch} placeholder="Bench, hinge, dumbbell…" />
              </View>
              {(results.data ?? []).map((e) => (
                <ListRow key={e.id} title={e.name} subtitle={[e.category.replace(/_/g, ' '), e.equipment].filter(Boolean).join(' · ')}
                  onPress={() => pick(e.id, e.name)} accessory="none" />
              ))}
            </Card>
          </Section>
        ) : null}

        {!active ? (
          <Section plain>
            <Card><EmptyState icon="strength" hue="orange" title="Pick a lift" message="Add an exercise to start logging sets." action="Add Exercise" onAction={() => setPicking(true)} /></Card>
          </Section>
        ) : (
          <Section plain>
            <Card>
              <HStack justify="space-between" align="flex-start">
                <VStack gap={2} style={{ flex: 1 }}>
                  <T variant="title3" weight="700">{active.name}</T>
                  {active.rx ? (
                    <T variant="subhead" tone="label2">
                      {[
                        `${active.rx.target_sets} × ${repRange(active.rx.target_reps_min, active.rx.target_reps_max)}`,
                        active.rx.target_rpe_tenths ? `RPE ${formatRpe(active.rx.target_rpe_tenths)}` : null,
                        active.rx.rest_seconds ? `rest ${formatRest(active.rx.rest_seconds)}` : null,
                      ].filter(Boolean).join(' · ')}
                    </T>
                  ) : null}
                  {active.rx?.notes ? <T variant="footnote" tone="indigo">“{active.rx.notes}”</T> : null}
                </VStack>
                {(last.data ?? []).length > 0 ? <Button label="Repeat" icon="repeat" size="small" variant="tinted" onPress={() => { void clone(); }} /> : null}
              </HStack>

              {(last.data ?? []).length > 0 ? (
                <T variant="footnote" tone="label2" style={{ marginTop: space.sm }}>
                  Last time: {(last.data ?? []).filter((x) => x.completed === 1).map((x) => `${formatLoad(x.loadGrams, units)} × ${x.reps ?? '—'}`).join(', ')}
                </T>
              ) : null}

              {tip && tip.direction !== 'first' ? (
                <Pressable accessibilityRole="button" onPress={() => { tap('select'); setLoadValue(toUnits(tip.loadGrams, units)); if (tip.reps) setReps(tip.reps); }}
                  style={[s.tip, { backgroundColor: wash(tip.direction === 'up' ? c.green : c.tint, 0.12) }]}>
                  <Icon name={tip.direction === 'up' ? 'up' : 'target'} size={16} color={tip.direction === 'up' ? c.green : c.tint} />
                  <VStack gap={0} style={{ flex: 1 }}>
                    <T variant="subhead" weight="700">Suggested {formatLoad(tip.loadGrams, units)}{tip.reps ? ` × ${tip.reps}` : ''}</T>
                    <T variant="footnote" tone="label2">{tip.reason}</T>
                  </VStack>
                </Pressable>
              ) : null}

              {/* Set table */}
              <View style={{ marginTop: space.md }}>
                <HStack style={s.tableHead}>
                  <T variant="caption2" tone="label2" weight="700" style={{ width: 36 }}>SET</T>
                  <T variant="caption2" tone="label2" weight="700" style={{ flex: 1 }}>{units.toUpperCase()}</T>
                  <T variant="caption2" tone="label2" weight="700" style={{ flex: 1 }}>REPS</T>
                  <T variant="caption2" tone="label2" weight="700" style={{ width: 44 }}>RPE</T>
                  <View style={{ width: 40 }} />
                </HStack>
                {mine.map((set) => {
                  const done = set.completed === 1;
                  return (
                    <View key={set.id} style={[s.setRow, done && { backgroundColor: wash(c.green, 0.1) }]}>
                      <View style={{ width: 36 }}>
                        {set.isWarmup === 1 ? <Tag label="W" hue="orange" /> : <T variant="headline" tone="label2">{working.indexOf(set) + 1}</T>}
                      </View>
                      <T variant="headline" style={{ flex: 1 }} tone={done ? 'label' : 'label3'}>{formatLoad(set.loadGrams, units).replace(/\s.*$/, '')}</T>
                      <T variant="headline" style={{ flex: 1 }} tone={done ? 'label' : 'label3'}>{set.reps ?? '—'}</T>
                      <T variant="subhead" style={{ width: 44 }} tone="label2">{set.rpeTenths ? formatRpe(set.rpeTenths) : '—'}</T>
                      <Pressable accessibilityRole="checkbox" accessibilityState={{ checked: done }} accessibilityLabel={`Set ${set.setIndex} done`}
                        onPress={() => { void toggle(set); }} hitSlop={6}
                        style={[s.tick, { backgroundColor: done ? c.green : c.fill2 }]}>
                        <Icon name="check" size={16} color={done ? '#FFF' : c.label3} />
                      </Pressable>
                    </View>
                  );
                })}
                {Array.from({ length: planned }).map((_, i) => (
                  <View key={`plan-${i}`} style={s.setRow}>
                    <T variant="headline" tone="label3" style={{ width: 36 }}>{working.length + i + 1}</T>
                    <T variant="headline" tone="label3" style={{ flex: 1 }}>{loadValue ? String(loadValue) : '—'}</T>
                    <T variant="headline" tone="label3" style={{ flex: 1 }}>{reps}</T>
                    <T variant="subhead" tone="label3" style={{ width: 44 }}>{rpe ?? '—'}</T>
                    <Pressable accessibilityRole="checkbox" accessibilityState={{ checked: false }} accessibilityLabel={`Log planned set ${working.length + i + 1}`}
                      onPress={() => { void logNew(nextIndex + i); }} hitSlop={6}
                      style={[s.tick, { backgroundColor: c.fill2 }]}>
                      <Icon name="check" size={16} color={c.label3} />
                    </Pressable>
                  </View>
                ))}
              </View>

              <Spacer size={space.lg} />
              <HStack gap={space.md}>
                <Stepper label="Load" unit={units} value={loadValue} step={step} onChange={setLoadValue}
                  format={(v) => (Number.isInteger(v) ? String(v) : v.toFixed(units === 'kg' ? 2 : 1).replace(/0$/, ''))} />
                <Stepper label="Reps" value={reps} step={1} min={0} onChange={setReps} />
              </HStack>
              <Spacer size={space.md} />
              <HStack gap={6}>
                <T variant="footnote" tone="label2" style={{ width: 34 }}>RPE</T>
                {RPES.map((r) => (
                  <Pressable key={r} accessibilityRole="radio" accessibilityState={{ selected: rpe === r }}
                    onPress={() => { tap('select'); setRpe(rpe === r ? null : r); }}
                    style={[s.rpe, rpe === r && { backgroundColor: c.tint }]}>
                    <T variant="subhead" weight="600" style={rpe === r ? s.onText : undefined}>{r}</T>
                  </Pressable>
                ))}
              </HStack>
              <Spacer size={space.lg} />
              <Button label={`Log Set ${working.length + 1}`} icon="check" variant="filled" full onPress={() => { void logNew(); }} />

              <HStack gap={space.sm} style={{ marginTop: space.md }}>
                <Button label="Plates" icon="plates" size="small" variant="gray" onPress={() => setShowPlates(!showPlates)} />
                {warmups(fromUnits(loadValue, units), units).length > 0 && working.length === 0 ? (
                  <Button label="Warm-ups" icon="flame" size="small" variant="gray" onPress={() => { void addWarmups(); }} />
                ) : null}
                <Button label="Rest" icon="timer" size="small" variant="gray" onPress={startRest} />
              </HStack>

              {showPlates ? (
                <View style={s.plates}>
                  <T variant="footnote" tone="label2">Each side of a {formatLoad(plateLoad.barGrams, units)} bar</T>
                  <HStack gap={6} wrap style={{ marginTop: space.sm }}>
                    {plateLoad.perSide.length === 0 ? <T variant="subhead">Just the bar</T> : plateLoad.perSide.map((p, i) => (
                      <View key={i} style={[s.plate, { backgroundColor: plateColour(p, units, c) }]}>
                        <T variant="footnote" weight="700" style={{ color: '#FFF' }}>{p}</T>
                      </View>
                    ))}
                  </HStack>
                  {plateLoad.remainderGrams > 0 ? (
                    <T variant="footnote" tone="orange" style={{ marginTop: 6 }}>{formatLoad(plateLoad.remainderGrams, units)} can&apos;t be made with standard plates</T>
                  ) : null}
                </View>
              ) : null}
            </Card>

            {(() => {
              const i = slots.findIndex((x) => x.exerciseId === active.exerciseId);
              const nextSlot = slots[i + 1];
              return nextSlot ? (
                <Pressable accessibilityRole="button" onPress={() => { tap(); setCurrent(nextSlot.exerciseId); }} style={({ pressed }) => [s.next, pressed && { opacity: 0.6 }]}>
                  <T variant="subhead" tone="label2">Next</T>
                  <T variant="subhead" weight="600" style={{ flex: 1 }} lines={1}>{nextSlot.name}</T>
                  <Icon name="chevronRight" size={14} color={c.label3} weight="bold" />
                </Pressable>
              ) : (
                <View style={{ marginTop: space.lg }}>
                  <Button label="Finish Workout" icon="checkCircle" variant="tinted" hue="green" full onPress={() => setConfirmFinish(true)} />
                </View>
              );
            })()}
          </Section>
        )}
      </Body>

      {rest ? (
        <RestTimer
          endsAt={rest.endsAt}
          total={rest.total}
          onAdjust={(sec) => setRest((r) => (r ? { endsAt: Math.max(Date.now(), r.endsAt + sec * 1000), total: Math.max(1, r.total + sec) } : r))}
          onSkip={() => setRest(null)}
        />
      ) : null}

      <ActionSheet
        visible={confirmFinish}
        title="Finish this workout?"
        message={`${doneSets.length} working set${doneSets.length === 1 ? '' : 's'} logged. Sets not ticked are left out.`}
        onClose={() => setConfirmFinish(false)}
        actions={[
          { label: 'Finish Workout', icon: 'checkCircle', hue: 'green', onPress: () => { void finish(); } },
          { label: 'Leave Open for Later', icon: 'clock', onPress: () => router.back() },
        ]}
      />
    </>
  );
}

/** A horizontal scroller, without the scroll indicator, padded to the page. */
function Strip({ children }: { children: React.ReactNode }) {
  return (
    <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={{ gap: space.sm, paddingHorizontal: space.lg }}>
      {children}
    </ScrollView>
  );
}

/** Competition plate colours, so the readout matches the rack. */
function plateColour(p: number, units: 'kg' | 'lb', c: { red: string; tint: string; yellow: string; green: string; gray: string }): string {
  if (units === 'kg') {
    if (p >= 25) return c.red;
    if (p >= 20) return c.tint;
    if (p >= 15) return '#D4A100';
    if (p >= 10) return c.green;
    return c.gray;
  }
  if (p >= 45) return c.tint;
  if (p >= 35) return '#D4A100';
  if (p >= 25) return c.green;
  return c.gray;
}

const useStyles = makeStyles((c) => ({
  chip: {
    flexDirection: 'row', alignItems: 'center', gap: 6, height: 38, paddingHorizontal: 14,
    borderRadius: radius.pill, backgroundColor: c.surface, maxWidth: 260,
  },
  chipOn: { backgroundColor: c.tint },
  onText: { color: '#FFFFFF' },
  onTextDim: { color: 'rgba(255,255,255,0.8)' },
  tip: { flexDirection: 'row', alignItems: 'center', gap: space.md, padding: space.md, borderRadius: radius.md, marginTop: space.md },
  tableHead: { paddingHorizontal: space.sm, paddingBottom: 6 },
  setRow: {
    flexDirection: 'row', alignItems: 'center', paddingHorizontal: space.sm, height: 48, borderRadius: radius.sm,
  },
  tick: { width: 40, height: 34, borderRadius: radius.sm, alignItems: 'center', justifyContent: 'center' },
  rpe: { flex: 1, height: 36, borderRadius: radius.pill, backgroundColor: c.fill2, alignItems: 'center', justifyContent: 'center' },
  plates: { marginTop: space.md, padding: space.md, borderRadius: radius.md, backgroundColor: c.surface2 },
  plate: { minWidth: 40, height: 40, borderRadius: 20, alignItems: 'center', justifyContent: 'center', paddingHorizontal: 6 },
  next: {
    flexDirection: 'row', alignItems: 'center', gap: space.sm, marginTop: space.md,
    padding: space.lg, borderRadius: radius.lg, backgroundColor: c.surface,
  },
}));
