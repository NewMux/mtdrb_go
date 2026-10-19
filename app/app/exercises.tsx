/**
 * The exercise library.
 *
 * Organised by movement pattern rather than muscle, because that is how a
 * coach checks a session is balanced: a squat, a hinge, a push, a pull.
 *
 * Opened from the day editor it becomes a picker — tapping an exercise adds
 * it to that day with sensible default targets, and the editor reloads it.
 */

import React, { useState } from 'react';
import { View } from 'react-native';
import { Stack, useLocalSearchParams, useRouter } from 'expo-router';

import { MOVEMENT_PATTERNS, PATTERN_HUE, PATTERN_LABEL } from '@/features/library';
import { getProgram, newPrescription, saveProgram } from '@/features/programs';
import { useApp, useQuery } from '@/state/app';
import { Body, ChipBar, EmptyState, IconTile, ListRow, SearchField, Section, Spacer, tap } from '@/ui/components';
import { space } from '@/ui/theme';

interface Exercise { id: string; name: string; category: string; equipment: string; muscle: string }

export default function ExercisesScreen() {
  const { program, day } = useLocalSearchParams<{ program?: string; day?: string }>();
  const picking = !!program && !!day;
  const { db, touch } = useApp();
  const router = useRouter();
  const [search, setSearch] = useState('');
  const [pattern, setPattern] = useState<string>('all');

  const exercises = useQuery((d) => d.select<Exercise>(
    `SELECT id, name, coalesce(category,'other') AS category, coalesce(equipment,'') AS equipment,
            coalesce(primary_muscle,'') AS muscle
       FROM exercises
      WHERE archived_at IS NULL
        AND (? = '' OR name LIKE '%' || ? || '%' OR equipment LIKE '%' || ? || '%' OR primary_muscle LIKE '%' || ? || '%')
      ORDER BY name`,
    [search.trim(), search.trim(), search.trim(), search.trim()],
  ), [search]);

  const all = exercises.data ?? [];
  const patterns = MOVEMENT_PATTERNS.filter((p) => all.some((e) => normalise(e.category) === p));
  const shown = pattern === 'all' ? all : all.filter((e) => normalise(e.category) === pattern);
  const groups = new Map<string, Exercise[]>();
  for (const e of shown) {
    const k = normalise(e.category);
    groups.set(k, [...(groups.get(k) ?? []), e]);
  }

  const pick = async (e: Exercise) => {
    if (!db || !program || !day) return;
    tap('success');
    const doc = await getProgram(db, program);
    if (!doc) return;
    const next = {
      ...doc,
      blocks: doc.blocks.map((b) => ({
        ...b,
        days: b.days.map((d) => (d.id === day
          ? { ...d, prescriptions: [...d.prescriptions, newPrescription(e.id, e.name, d.prescriptions.length)] }
          : d)),
      })),
    };
    await saveProgram(db, next);
    touch();
    router.back();
  };

  return (
    <>
      <Stack.Screen options={{ title: picking ? 'Add Exercise' : 'Exercises' }} />
      <Body keyboard>
        <View style={{ paddingHorizontal: space.lg }}>
          <SearchField value={search} onChangeText={setSearch} placeholder="Name, equipment or muscle" />
        </View>
        <Spacer size={space.md} />
        <ChipBar
          value={pattern}
          onChange={setPattern}
          options={[{ value: 'all', label: 'All', count: all.length }, ...patterns.map((p) => ({ value: p, label: PATTERN_LABEL[p] ?? p }))]}
        />
        {shown.length === 0 ? (
          <EmptyState icon="search" title="No exercise matches" message="Try a different word." />
        ) : [...groups].map(([k, list]) => (
          <Section key={k} title={PATTERN_LABEL[k] ?? k}>
            {list.map((e, i) => (
              <ListRow
                key={e.id}
                first={i === 0}
                leading={<IconTile icon="strength" hue={PATTERN_HUE[k] ?? 'gray'} size={30} />}
                title={e.name}
                subtitle={[e.muscle, e.equipment].filter(Boolean).join(' · ')}
                accessory="none"
                value={picking ? 'Add' : undefined}
                valueTone="tint"
                onPress={picking ? () => { void pick(e); } : undefined}
              />
            ))}
          </Section>
        ))}
        <Spacer />
      </Body>
    </>
  );
}

/** The demo seed once stored patterns with spaces; the server uses underscores. */
function normalise(category: string): string {
  return category.trim().toLowerCase().replace(/\s+/g, '_') || 'other';
}
