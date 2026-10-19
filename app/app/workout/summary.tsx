/**
 * The end of a workout: what was done, and anything worth celebrating.
 *
 * A client who sees their numbers go up keeps coming back, so the summary
 * can be sent to them as a message — a plain recap, no app or login needed.
 */

import React from 'react';
import { Share, View } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { e1rm } from '@/features/insights';
import { workoutSets } from '@/features/queries';
import { useApp, useQuery } from '@/state/app';
import { Body, Button, Card, HStack, IconBubble, ListRow, Section, T, VStack } from '@/ui/components';
import { load as formatLoad } from '@/ui/format';
import { space } from '@/ui/theme';

export default function SummaryScreen() {
  const { id, name, minutes } = useLocalSearchParams<{ id: string; name?: string; minutes?: string }>();
  const { prefs } = useApp();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const units = prefs.units;

  const sets = useQuery((d) => workoutSets(d, id), [id]);
  const records = useQuery(async (d) => {
    // Lifts where today's best estimated max beats every earlier session.
    const today = (await workoutSets(d, id)).filter((x) => x.completed === 1 && x.isWarmup !== 1 && x.reps && x.loadGrams);
    const w = await d.selectOne<{ client_id: string }>(`SELECT client_id FROM workout_sessions WHERE id = ?`, [id]);
    if (!w) return [] as string[];
    const out: string[] = [];
    const byLift = new Map<string, { name: string; best: number }>();
    for (const x of today) {
      const est = e1rm(x.loadGrams!, x.reps!);
      if (est > (byLift.get(x.exerciseId)?.best ?? 0)) byLift.set(x.exerciseId, { name: x.exerciseName, best: est });
    }
    for (const [exerciseId, v] of byLift) {
      const prev = await d.selectOne<{ best: number | null; n: number }>(
        `SELECT max(sl.load_grams * (1 + sl.reps / 30.0)) AS best, count(*) AS n
           FROM set_logs sl JOIN workout_sessions ws ON ws.id = sl.workout_session_id
          WHERE ws.client_id = ? AND sl.exercise_id = ? AND ws.id != ? AND sl.completed = 1 AND sl.is_warmup = 0
            AND sl.reps BETWEEN 1 AND 12`,
        [w.client_id, exerciseId, id],
      );
      if (prev && prev.n > 0 && v.best > (prev.best ?? 0) + 1) out.push(v.name);
    }
    return out;
  }, [id]);

  const done = (sets.data ?? []).filter((x) => x.completed === 1 && x.isWarmup !== 1);
  const volume = done.reduce((n, x) => n + (x.reps ?? 0) * (x.loadGrams ?? 0), 0);
  const lifts = new Map<string, { name: string; sets: number; top: { load: number; reps: number } | null }>();
  for (const x of done) {
    const cur = lifts.get(x.exerciseId) ?? { name: x.exerciseName, sets: 0, top: null };
    cur.sets += 1;
    if (x.loadGrams && (!cur.top || x.loadGrams > cur.top.load)) cur.top = { load: x.loadGrams, reps: x.reps ?? 0 };
    lifts.set(x.exerciseId, cur);
  }
  const prs = records.data ?? [];
  const firstName = (name ?? '').split(' ')[0] || 'there';

  const share = async () => {
    const lines = [...lifts.values()].map((l) => `• ${l.name}: ${l.sets} sets${l.top ? `, top ${formatLoad(l.top.load, units)} × ${l.top.reps}` : ''}`);
    const text = [
      `Great session today, ${firstName}! 💪`,
      '',
      ...lines,
      '',
      `${done.length} sets · ${Math.round(volume / (units === 'kg' ? 1000 : 453.59237)).toLocaleString()} ${units} moved`,
      prs.length ? `New personal best${prs.length > 1 ? 's' : ''}: ${prs.join(', ')} 🏆` : '',
    ].filter((l, i, arr) => l !== '' || arr[i - 1] !== '').join('\n');
    await Share.share({ message: text });
  };

  return (
    <Body contentStyle={{ paddingTop: insets.top + space.xl }}>
      <VStack align="center" gap={space.sm} style={{ paddingHorizontal: space.xl }}>
        <IconBubble icon={prs.length ? 'trophy' : 'checkCircle'} hue={prs.length ? 'yellow' : 'green'} size={80} />
        <T variant="largeTitle" align="center">Workout Done</T>
        <T variant="body" tone="label2" align="center">{name ?? 'Client'}{minutes ? ` · ${minutes} min` : ''}</T>
      </VStack>

      <View style={{ flexDirection: 'row', gap: space.md, marginHorizontal: space.lg, marginTop: space.xl }}>
        {[
          { k: String(done.length), v: 'sets' },
          { k: Math.round(volume / (units === 'kg' ? 1000 : 453.59237)).toLocaleString(), v: `${units} moved` },
          { k: String(prs.length), v: prs.length === 1 ? 'record' : 'records' },
        ].map((m) => (
          <Card key={m.v} style={{ flex: 1, alignItems: 'center' }}>
            <T variant="metricSmall">{m.k}</T>
            <T variant="footnote" tone="label2">{m.v}</T>
          </Card>
        ))}
      </View>

      {prs.length ? (
        <Section title="Personal Records">
          {prs.map((p, i) => <ListRow key={p} first={i === 0} icon="trophy" hue="yellow" title={p} subtitle="Best estimated max ever" accessory="none" />)}
        </Section>
      ) : null}

      <Section title="Exercises">
        {[...lifts.values()].map((l, i) => (
          <ListRow key={l.name} first={i === 0} icon="strength" hue="orange" title={l.name}
            subtitle={`${l.sets} set${l.sets === 1 ? '' : 's'}`}
            value={l.top ? `${formatLoad(l.top.load, units)} × ${l.top.reps}` : undefined} accessory="none" />
        ))}
        {lifts.size === 0 ? <ListRow first title="No working sets were ticked" titleTone="label2" accessory="none" /> : null}
      </Section>

      <HStack gap={space.sm} style={{ paddingHorizontal: space.lg, marginTop: space.xl }}>
        <Button label="Send to Client" icon="share" variant="tinted" style={{ flex: 1 }} full onPress={() => { void share(); }} />
        <Button label="Done" variant="filled" style={{ flex: 1 }} full onPress={() => router.back()} />
      </HStack>
    </Body>
  );
}
