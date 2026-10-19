/**
 * A training day: the exercises and what is prescribed for each.
 *
 * Prescriptions are edited in place — sets, rep range, load, RPE, rest — on
 * one compact card per exercise, because a trainer writing a programme is
 * tweaking numbers across a whole day and a screen per exercise would bury
 * them. Exercises sharing a superset letter are done back to back, and are
 * labelled A1, A2 as on any written programme.
 */

import React, { useState } from 'react';
import { TextInput, View } from 'react-native';
import { Stack, useLocalSearchParams, useRouter } from 'expo-router';

import { findDay, type Prescription, type ProgramDay } from '@/features/programs';
import { useProgram } from '@/features/use-program';
import { useApp } from '@/state/app';
import { Body, Button, Card, EmptyState, HStack, IconButton, Section, Spacer, T, Tag, VStack } from '@/ui/components';
import { ActionSheet } from '@/ui/sheet';
import { load as formatLoad, parseLoad } from '@/ui/format';
import { makeStyles, radius, space, type as typography, useTheme } from '@/ui/theme';

export default function DayScreen() {
  const { program, day: dayId } = useLocalSearchParams<{ program: string; day: string }>();
  const router = useRouter();
  const { prefs } = useApp();
  const { c } = useTheme();
  const { doc, loading, update, flush } = useProgram(program);
  const [menuFor, setMenuFor] = useState<Prescription | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const day = doc ? findDay(doc, dayId) : null;
  if (!doc || !day) return <Body><EmptyState icon="strength" title={loading ? 'Loading…' : 'Day not found'} /></Body>;

  const setDay = (change: (d: ProgramDay) => ProgramDay) =>
    update((p) => ({ ...p, blocks: p.blocks.map((b) => ({ ...b, days: b.days.map((d) => (d.id === dayId ? change(d) : d)) })) }));
  const setRx = (id: string, patch: Partial<Prescription>) =>
    setDay((d) => ({ ...d, prescriptions: d.prescriptions.map((p) => (p.id === id ? { ...p, ...patch } : p)) }));

  const move = (id: string, by: -1 | 1) => setDay((d) => {
    const list = [...d.prescriptions];
    const i = list.findIndex((p) => p.id === id);
    const j = i + by;
    if (i < 0 || j < 0 || j >= list.length) return d;
    [list[i], list[j]] = [list[j]!, list[i]!];
    return { ...d, prescriptions: list };
  });

  /** Joins an exercise to the next one's superset, or starts a new letter for the pair. */
  const supersetWithNext = (id: string) => setDay((d) => {
    const list = d.prescriptions;
    const i = list.findIndex((p) => p.id === id);
    const next = list[i + 1];
    if (!next) return d;
    const used = new Set(list.map((p) => p.superset_group).filter(Boolean));
    const letter = list[i]!.superset_group || next.superset_group
      || 'ABCDEFGH'.split('').find((l) => !used.has(l)) || 'A';
    return { ...d, prescriptions: list.map((p, k) => (k === i || k === i + 1 ? { ...p, superset_group: letter } : p)) };
  });

  const remove = () => {
    if (!menuFor) return;
    const id = menuFor.id;
    setDay((d) => ({ ...d, prescriptions: d.prescriptions.filter((p) => p.id !== id) }));
  };

  const deleteDay = () => {
    update((p) => ({ ...p, blocks: p.blocks.map((b) => ({ ...b, days: b.days.filter((d) => d.id !== dayId) })) }));
    setTimeout(() => { void flush().then(() => router.back()); }, 0);
  };

  const labels = supersetLabels(day.prescriptions);

  return (
    <>
      <Stack.Screen options={{
        title: day.name,
        headerRight: () => <IconButton icon="trash" label="Delete day" size={34} variant="gray" onPress={() => setConfirmDelete(true)} />,
      }} />
      <Body keyboard>
        <View style={{ paddingHorizontal: space.xl }}>
          <TextInput
            accessibilityLabel="Day name"
            value={day.name}
            onChangeText={(name) => setDay((d) => ({ ...d, name }))}
            style={[typography.title1, { color: c.label, padding: 0 }]}
          />
          <T variant="subhead" tone="label2" style={{ marginTop: 4 }}>{doc.name}</T>
        </View>

        <Section plain title="Exercises">
          {day.prescriptions.length === 0 ? (
            <Card><EmptyState icon="strength" hue="orange" title="No exercises yet" message="Add the first lift of the day." /></Card>
          ) : (
            <VStack gap={space.md}>
              {day.prescriptions.map((p) => (
                <RxCard key={p.id} rx={p} label={labels.get(p.id)} units={prefs.units}
                  onChange={(patch) => setRx(p.id, patch)} onMenu={() => setMenuFor(p)} />
              ))}
            </VStack>
          )}
        </Section>

        <View style={{ paddingHorizontal: space.lg, marginTop: space.lg }}>
          <Button
            label="Add Exercise" icon="plus" variant="tinted" full
            onPress={() => { void flush().then(() => router.push({ pathname: '/exercises', params: { program, day: dayId } })); }}
          />
        </View>
        <T variant="footnote" tone="label2" align="center" style={{ marginTop: space.md, paddingHorizontal: space.xl }}>
          Leave load empty to let the logger suggest one from the client&apos;s last session.
        </T>
        <Spacer />
      </Body>

      <ActionSheet
        visible={!!menuFor}
        title={menuFor?.exercise_name}
        onClose={() => setMenuFor(null)}
        actions={menuFor ? [
          { label: 'Move Up', icon: 'chevronLeft', onPress: () => move(menuFor.id, -1) },
          { label: 'Move Down', icon: 'chevronRight', onPress: () => move(menuFor.id, 1) },
          menuFor.superset_group
            ? { label: 'Remove from Superset', icon: 'layers', onPress: () => setRx(menuFor.id, { superset_group: '' }) }
            : { label: 'Superset with Next', icon: 'layers', onPress: () => supersetWithNext(menuFor.id) },
          { label: 'Remove Exercise', icon: 'trash', destructive: true, onPress: remove },
        ] : []}
      />
      <ActionSheet
        visible={confirmDelete}
        title={`Delete ${day.name}?`}
        message="Its exercises go with it. Workouts already logged against it are kept."
        onClose={() => setConfirmDelete(false)}
        actions={[{ label: 'Delete Day', destructive: true, onPress: deleteDay }]}
      />
    </>
  );
}

/** "A1", "A2" for exercises sharing a superset letter, in order. */
function supersetLabels(list: Prescription[]): Map<string, string> {
  const seen = new Map<string, number>();
  const out = new Map<string, string>();
  for (const p of list) {
    if (!p.superset_group) continue;
    const n = (seen.get(p.superset_group) ?? 0) + 1;
    seen.set(p.superset_group, n);
    out.set(p.id, `${p.superset_group}${n}`);
  }
  return out;
}

function RxCard({
  rx, label, units, onChange, onMenu,
}: {
  rx: Prescription;
  label?: string;
  units: 'kg' | 'lb';
  onChange: (patch: Partial<Prescription>) => void;
  onMenu: () => void;
}) {
  const s = useStyles();
  return (
    <Card>
      <HStack gap={space.sm}>
        {label ? <Tag label={label} hue="purple" solid /> : null}
        <T variant="headline" lines={1} style={{ flex: 1 }}>{rx.exercise_name ?? 'Exercise'}</T>
        <IconButton icon="more" label="Exercise options" variant="gray" size={30} onPress={onMenu} />
      </HStack>
      <View style={s.grid}>
        <Mini label="Sets" value={String(rx.target_sets)} onChange={(t) => {
          const n = parseInt(t, 10); if (Number.isFinite(n) && n > 0) onChange({ target_sets: Math.min(50, n) });
        }} />
        <Mini label="Reps" value={rx.target_reps_min == null ? '' : rx.target_reps_max != null && rx.target_reps_max !== rx.target_reps_min ? `${rx.target_reps_min}-${rx.target_reps_max}` : String(rx.target_reps_min)}
          keyboard="numbers-and-punctuation" placeholder="8-10"
          onChange={(t) => {
            const m = t.match(/^\s*(\d+)\s*(?:[-–]\s*(\d+))?\s*$/);
            if (!t.trim()) onChange({ target_reps_min: null, target_reps_max: null });
            else if (m) {
              const lo = Number(m[1]); const hi = m[2] ? Number(m[2]) : lo;
              onChange({ target_reps_min: Math.min(lo, hi), target_reps_max: Math.max(lo, hi) });
            }
          }} />
        <Mini label={units} value={rx.target_load_grams == null ? '' : formatLoad(rx.target_load_grams, units).replace(/\s.*$/, '')} placeholder="auto"
          onChange={(t) => onChange({ target_load_grams: parseLoad(t, units) })} />
        <Mini label="RPE" value={rx.target_rpe_tenths == null ? '' : String(rx.target_rpe_tenths / 10)} placeholder="—"
          onChange={(t) => {
            const v = Number(t.replace(',', '.'));
            onChange({ target_rpe_tenths: t.trim() && Number.isFinite(v) && v >= 0 && v <= 10 ? Math.round(v * 10) : null });
          }} />
        <Mini label="Rest s" value={rx.rest_seconds == null ? '' : String(rx.rest_seconds)} placeholder="90"
          onChange={(t) => { const n = parseInt(t, 10); onChange({ rest_seconds: Number.isFinite(n) ? n : null }); }} />
      </View>
      <NoteField value={rx.notes} onChange={(notes) => onChange({ notes })} />
    </Card>
  );
}

/**
 * A small numeric cell.
 *
 * Holds its own text while focused, so typing "7." or "8-" is not reformatted
 * out from under the thumb; the parsed value goes up on every keystroke.
 */
function Mini({
  label, value, onChange, placeholder, keyboard = 'decimal-pad',
}: {
  label: string; value: string; onChange: (t: string) => void; placeholder?: string;
  keyboard?: 'decimal-pad' | 'numbers-and-punctuation';
}) {
  const { c } = useTheme();
  const s = useStyles();
  const [text, setText] = useState<string | null>(null);
  return (
    <View style={s.mini}>
      <T variant="caption2" tone="label2" weight="600" align="center">{label.toUpperCase()}</T>
      <TextInput
        accessibilityLabel={label}
        value={text ?? value}
        onFocus={() => setText(value)}
        onBlur={() => setText(null)}
        onChangeText={(t) => { setText(t); onChange(t); }}
        placeholder={placeholder}
        placeholderTextColor={c.label3}
        keyboardType={keyboard}
        selectTextOnFocus
        style={[typography.headline, s.miniInput, { color: c.label }]}
      />
    </View>
  );
}

function NoteField({ value, onChange }: { value: string; onChange: (t: string) => void }) {
  const { c } = useTheme();
  return (
    <TextInput
      accessibilityLabel="Coaching note"
      value={value}
      onChangeText={onChange}
      placeholder="Coaching cue or note"
      placeholderTextColor={c.label3}
      style={[typography.subhead, { color: c.label2, marginTop: space.md, padding: 0 }]}
    />
  );
}

const useStyles = makeStyles((c) => ({
  grid: { flexDirection: 'row', gap: 6, marginTop: space.md },
  mini: { flex: 1, backgroundColor: c.surface2, borderRadius: radius.sm, paddingTop: 6, paddingBottom: 2 },
  miniInput: { textAlign: 'center', height: 34, padding: 0, fontVariant: ['tabular-nums'] },
}));
