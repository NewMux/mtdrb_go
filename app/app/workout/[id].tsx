/**
 * The floor logger.
 *
 * Everything here is one tap or one number. The trainer is standing next to a
 * loaded bar with a client waiting, so the screen holds one exercise at a time
 * and the numbers are large enough to read from arm's length.
 *
 * The important behaviour is cloning. "Repeat last session" writes last week's
 * working sets in as *logged but not completed* — visible, greyed, and waiting
 * for a tap. Prefilled numbers that counted as performed would put lifts in a
 * client's history that never happened, which is the one thing a training log
 * must never do.
 */

import React, { useMemo, useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';

import { cloneLastSession, completeWorkout, logSet } from '@/features/actions';
import {
  previousSets, searchExercises, workoutSets, type LoggedSet,
} from '@/features/queries';
import { useApp, useQuery } from '@/state/app';
import {
  Body, Button, Caption, Card, Empty, Field, Heading, NumberField,
  Row, Screen, Spacer, TextButton, Title,
} from '@/ui/components';
import { load as formatLoad, parseLoad, parseReps, parseRpe, rpe as formatRpe } from '@/ui/format';
import { colors, radius, space, type as typography } from '@/ui/theme';

export default function WorkoutScreen() {
  const params = useLocalSearchParams<{ id: string; client?: string; name?: string }>();
  const workoutId = params.id;
  const clientId = params.client ?? '';
  const clientName = params.name ?? 'Workout';

  const { db, touch, syncNow } = useApp();
  const router = useRouter();

  const [picked, setPicked] = useState<{ id: string; name: string } | null>(null);
  const [picking, setPicking] = useState(false);
  const [search, setSearch] = useState('');
  const [reps, setReps] = useState('');
  const [kg, setKg] = useState('');
  const [rpeInput, setRpeInput] = useState('');

  const sets = useQuery((database) => workoutSets(database, workoutId), [workoutId]);
  const results = useQuery(
    (database) => (picking ? searchExercises(database, search, 30) : Promise.resolve([])),
    [picking, search],
  );

  const all = sets.data ?? [];

  /**
   * The exercises in play: those already logged against, plus one just picked.
   *
   * The picked one has to be here explicitly. Deriving the list from the sets
   * alone meant an exercise chosen but not yet logged had no chip and no name,
   * so the card above the keypad read "Exercise" until the first set landed.
   */
  const inWorkout = useMemo(() => {
    const seen = new Map<string, string>();
    for (const set of all) if (!seen.has(set.exerciseId)) seen.set(set.exerciseId, set.exerciseName);
    if (picked && !seen.has(picked.id)) seen.set(picked.id, picked.name);
    return [...seen].map(([id, name]) => ({ id, name }));
  }, [all, picked]);

  const current = picked?.id ?? inWorkout[0]?.id ?? null;
  const last = useQuery(
    (database) => (current && clientId
      ? previousSets(database, clientId, current, workoutId)
      : Promise.resolve([] as LoggedSet[])),
    [current, clientId, workoutId],
  );

  const currentSets = all.filter((s) => s.exerciseId === current);
  const currentName = inWorkout.find((e) => e.id === current)?.name ?? '';
  const nextIndex = currentSets.reduce((max, s) => Math.max(max, s.setIndex), 0) + 1;

  const after = () => { touch(); sets.reload(); last.reload(); void syncNow(); };

  const addSet = async () => {
    if (!db || !current) return;
    await logSet(db, workoutId, current, nextIndex, {
      reps: parseReps(reps),
      loadGrams: parseLoad(kg),
      rpeTenths: parseRpe(rpeInput),
      completed: true,
    });
    // The load and RPE stay: the next set is usually the same weight, and
    // retyping 82.5 four times is four chances to mistype it.
    setReps('');
    after();
  };

  /**
   * Toggles whether a set was actually performed.
   *
   * Tapping a cloned set confirms it; tapping a confirmed one undoes a mis-tap.
   * Both go through `logSet`, so the server sees the same intent either way
   * rather than a second, contradictory row.
   */
  const confirm = async (set: LoggedSet) => {
    if (!db || !current) return;
    await logSet(db, workoutId, set.exerciseId, set.setIndex, {
      reps: set.reps,
      loadGrams: set.loadGrams,
      rpeTenths: set.rpeTenths,
      isWarmup: set.isWarmup === 1,
      completed: set.completed !== 1,
    });
    after();
  };

  const clone = async () => {
    if (!db || !current || !clientId) return;
    await cloneLastSession(db, workoutId, clientId, current);
    after();
  };

  const finish = async () => {
    if (!db) return;
    await completeWorkout(db, workoutId);
    touch();
    void syncNow();
    router.back();
  };

  const pick = (id: string, name: string) => {
    setPicked({ id, name });
    setPicking(false);
    setSearch('');
    setReps('');
    setKg('');
    setRpeInput('');
  };

  return (
    <Screen>
      <ScrollView
        contentContainerStyle={{ padding: space.lg, paddingBottom: space.xxl }}
        keyboardShouldPersistTaps="handled"
      >
        <Title>{clientName}</Title>
        <Caption>{all.filter((s) => s.completed === 1).length} sets logged</Caption>
        <Spacer size={space.lg} />

        {/* Exercises already in this workout, plus a way to add one. */}
        <ScrollView horizontal showsHorizontalScrollIndicator={false}>
          <Row style={{ paddingRight: space.md }}>
            {inWorkout.map((exercise) => (
              <Pressable
                key={exercise.id}
                accessibilityRole="tab"
                accessibilityState={{ selected: exercise.id === current }}
                onPress={() => { setPicked(exercise); setPicking(false); }}
                style={[styles.chip, exercise.id === current && styles.chipActive]}
              >
                <Text style={[styles.chipLabel, exercise.id === current && styles.chipLabelActive]}>
                  {exercise.name}
                </Text>
              </Pressable>
            ))}
            <Pressable
              accessibilityRole="button"
              accessibilityLabel="Add exercise"
              onPress={() => setPicking(!picking)}
              style={[styles.chip, picking && styles.chipActive]}
            >
              <Text style={[styles.chipLabel, picking && styles.chipLabelActive]}>+ Exercise</Text>
            </Pressable>
          </Row>
        </ScrollView>

        <Spacer />

        {picking ? (
          <Card>
            <Field
              label="Find an exercise"
              value={search}
              onChangeText={setSearch}
              placeholder="Bench press, hinge, dumbbell…"
              autoCapitalize="none"
            />
            <Spacer size={space.sm} />
            {(results.data ?? []).map((exercise) => (
              <Pressable
                key={exercise.id}
                accessibilityRole="button"
                onPress={() => pick(exercise.id, exercise.name)}
                style={({ pressed }) => [styles.result, pressed && { opacity: 0.6 }]}
              >
                <Body>{exercise.name}</Body>
                <Caption>{[exercise.category, exercise.equipment].filter(Boolean).join(' · ')}</Caption>
              </Pressable>
            ))}
            {(results.data ?? []).length === 0 ? <Caption>No exercise matches that.</Caption> : null}
          </Card>
        ) : null}

        {!current ? (
          <Empty
            title="Pick a lift"
            detail="Choose an exercise to start logging sets."
          />
        ) : (
          <>
            <Card>
              <Row style={{ justifyContent: 'space-between' }}>
                <Heading>{currentName || 'Exercise'}</Heading>
                {(last.data ?? []).length > 0 ? (
                  <TextButton label="Repeat last time" onPress={() => { void clone(); }} />
                ) : null}
              </Row>

              {(last.data ?? []).length > 0 ? (
                <>
                  <Spacer size={space.sm} />
                  <Caption>
                    Last time: {(last.data ?? [])
                      .map((s) => `${s.reps ?? '—'} × ${formatLoad(s.loadGrams)}`)
                      .join(', ')}
                  </Caption>
                </>
              ) : null}

              <Spacer size={space.lg} />

              {currentSets.length === 0 ? (
                <Caption>No sets yet.</Caption>
              ) : (
                currentSets.map((set) => (
                  <Pressable
                    key={set.id}
                    accessibilityRole="checkbox"
                    accessibilityState={{ checked: set.completed === 1 }}
                    accessibilityLabel={`Set ${set.setIndex}`}
                    onPress={() => { void confirm(set); }}
                    style={({ pressed }) => [
                      styles.setRow,
                      set.completed !== 1 && styles.setRowPending,
                      pressed && { opacity: 0.6 },
                    ]}
                  >
                    <Text style={styles.setIndex}>{set.setIndex}</Text>
                    <Text style={styles.setValue}>{set.reps ?? '—'} reps</Text>
                    <Text style={styles.setValue}>{formatLoad(set.loadGrams)}</Text>
                    <Text style={styles.setRpe}>RPE {formatRpe(set.rpeTenths)}</Text>
                    <Text style={[styles.setMark, set.completed === 1 && styles.setMarkDone]}>
                      {set.completed === 1 ? '✓' : 'tap'}
                    </Text>
                  </Pressable>
                ))
              )}

              <Spacer size={space.lg} />

              <Row>
                <NumberField label="Reps" value={reps} onChangeText={setReps} placeholder="8" />
                <NumberField label="kg" value={kg} onChangeText={setKg} placeholder="80" />
                <NumberField label="RPE" value={rpeInput} onChangeText={setRpeInput} placeholder="8" />
              </Row>
              <Spacer size={space.sm} />
              <Button
                label={`Log set ${nextIndex}`}
                tone="primary"
                onPress={() => { void addSet(); }}
                disabled={parseReps(reps) === null && parseLoad(kg) === null}
              />
            </Card>

            <Spacer size={space.xl} />
            <Button label="Finish workout" onPress={() => { void finish(); }} />
          </>
        )}
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  chip: {
    paddingHorizontal: space.lg,
    paddingVertical: space.md,
    borderRadius: radius.pill,
    borderWidth: 1,
    borderColor: colors.border,
    backgroundColor: colors.surface,
  },
  chipActive: { backgroundColor: colors.accent, borderColor: colors.accent },
  chipLabel: { ...typography.caption, color: colors.inkMuted },
  chipLabelActive: { color: colors.bg },

  result: {
    paddingVertical: space.md,
    borderBottomWidth: 1,
    borderBottomColor: colors.border,
  },

  setRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: space.sm,
    paddingVertical: space.md,
    paddingHorizontal: space.sm,
    borderBottomWidth: 1,
    borderBottomColor: colors.border,
  },
  /** A cloned set: present, legible, and plainly not yet performed. */
  setRowPending: { opacity: 0.55 },
  setIndex: { ...typography.caption, color: colors.inkMuted, width: 16 },
  setValue: { ...typography.heading, color: colors.ink },
  setRpe: { ...typography.caption, color: colors.inkMuted },
  setMark: { ...typography.caption, color: colors.inkMuted, width: 28, textAlign: 'right' },
  setMarkDone: { color: colors.success, fontSize: 18 },
});
