/**
 * A programme: its shape, and who is on it.
 *
 * Programme → blocks of weeks → training days that repeat each week → the
 * exercises on each day. A four-week block repeats its days rather than
 * storing twenty-eight copies, so editing "Upper A" once changes it for every
 * week of the block.
 *
 * Also shows a gallery template read-only, with the one button that matters.
 */

import React, { useCallback, useState } from 'react';
import { TextInput, View } from 'react-native';
import { Stack, useFocusEffect, useLocalSearchParams, useRouter } from 'expo-router';

import { TEMPLATES, type ProgramTemplate } from '@/features/library';
import {
  archiveProgram, copyTemplate, duplicateProgram, newDay, programClients, totalWeeks,
  type ProgramBlock,
} from '@/features/programs';
import { useProgram } from '@/features/use-program';
import { useApp, useQuery } from '@/state/app';
import {
  Avatar, Banner, Body, Button, Card, EmptyState, HStack, IconButton, IconTile, ListRow, Section,
  Spacer, Stepper, T, Tag, tap, VStack,
} from '@/ui/components';
import { ActionSheet } from '@/ui/sheet';
import { repRange, rest as formatRest } from '@/ui/format';
import { makeStyles, space, type as typography, useTheme } from '@/ui/theme';
import { newId } from '@/lib/id';

export default function ProgramScreen() {
  const { id, template, fresh } = useLocalSearchParams<{ id: string; template?: string; fresh?: string }>();
  if (id === 'template' && template) {
    const t = TEMPLATES.find((x) => x.key === template);
    return t ? <TemplatePreview template={t} /> : <Body><EmptyState icon="info" title="Template not found" /></Body>;
  }
  return <Builder id={id} fresh={fresh === '1'} />;
}

/* ───────────────────────────── builder ───────────────────────────── */

function Builder({ id, fresh }: { id: string; fresh: boolean }) {
  const router = useRouter();
  const { db, touch } = useApp();
  const { c } = useTheme();
  const s = useStyles();
  const { doc, loading, update, flush } = useProgram(id);
  const clients = useQuery((d) => programClients(d, id), [id]);
  const [menu, setMenu] = useState(false);
  const [confirmArchive, setConfirmArchive] = useState(false);

  useFocusEffect(useCallback(() => { clients.reload(); }, [clients.reload]));

  if (!doc) return <Body><EmptyState icon="programs" title={loading ? 'Loading…' : 'Program not found'} /></Body>;

  const setBlock = (blockId: string, change: (b: ProgramBlock) => ProgramBlock) =>
    update((d) => ({ ...d, blocks: d.blocks.map((b) => (b.id === blockId ? change(b) : b)) }));

  const addDay = (blockId: string) => {
    const block = doc.blocks.find((b) => b.id === blockId);
    if (!block) return;
    const day = newDay(`Day ${String.fromCharCode(65 + block.days.length)}`, block.days.length);
    setBlock(blockId, (b) => ({ ...b, days: [...b.days, day] }));
    tap();
    // Saved before opening, so the day editor finds it.
    setTimeout(() => { void flush().then(() => router.push({ pathname: '/program/day', params: { program: id, day: day.id } })); }, 0);
  };

  const addBlock = () => {
    update((d) => ({
      ...d,
      blocks: [...d.blocks, { id: newId(), name: `Block ${d.blocks.length + 1}`, weeks: 4, sort_order: d.blocks.length, notes: '', days: [] }],
    }));
  };

  const removeBlock = (blockId: string) => update((d) => ({ ...d, blocks: d.blocks.filter((b) => b.id !== blockId) }));

  const duplicate = async () => {
    if (!db) return;
    await flush();
    const copy = await duplicateProgram(db, id);
    touch();
    if (copy) router.replace({ pathname: '/program/[id]', params: { id: copy } });
  };

  const archive = async () => {
    if (!db) return;
    await flush();
    await archiveProgram(db, id);
    touch();
    router.back();
  };

  const weeks = totalWeeks(doc);
  const dayCount = doc.blocks.reduce((n, b) => n + b.days.length, 0);

  return (
    <>
      <Stack.Screen options={{
        title: '',
        headerRight: () => <IconButton icon="more" label="More" size={34} variant="gray" onPress={() => setMenu(true)} />,
      }} />
      <Body keyboard>
        <View style={{ paddingHorizontal: space.xl }}>
          <TextInput
            accessibilityLabel="Program name"
            value={doc.name}
            onChangeText={(name) => update((d) => ({ ...d, name }))}
            placeholder="Program name"
            placeholderTextColor={c.label3}
            autoFocus={fresh}
            selectTextOnFocus={fresh}
            style={[typography.title1, { color: c.label, padding: 0 }]}
          />
          <TextInput
            accessibilityLabel="Description"
            value={doc.description}
            onChangeText={(description) => update((d) => ({ ...d, description }))}
            placeholder="What is it for? Who is it for?"
            placeholderTextColor={c.label3}
            multiline
            style={[typography.subhead, { color: c.label2, padding: 0, marginTop: 6 }]}
          />
          <HStack gap={6} style={{ marginTop: space.md }}>
            <Tag label={`${weeks} week${weeks === 1 ? '' : 's'}`} hue="tint" icon="hourglass" />
            <Tag label={`${dayCount} day${dayCount === 1 ? '' : 's'} / week`} hue="indigo" icon="calendar" />
            {(clients.data ?? []).length > 0 ? <Tag label={`${(clients.data ?? []).length} on it`} hue="green" icon="clients" /> : null}
          </HStack>
        </View>

        {doc.blocks.map((block, bi) => (
          <Section
            key={block.id}
            title={doc.blocks.length > 1 ? `${bi + 1}. ${block.name}` : 'Training Days'}
            action={doc.blocks.length > 1 ? 'Remove' : undefined}
            onAction={() => removeBlock(block.id)}
            plain
          >
            <Card padded={false}>
              <View style={s.blockHead}>
                <TextInput
                  accessibilityLabel="Block name"
                  value={block.name}
                  onChangeText={(name) => setBlock(block.id, (b) => ({ ...b, name }))}
                  style={[typography.headline, { color: c.label, flex: 1, padding: 0 }]}
                />
              </View>
              <View style={{ paddingHorizontal: space.lg, paddingBottom: space.md }}>
                <Stepper
                  label="Weeks" value={block.weeks} min={1} step={1}
                  onChange={(weeks) => setBlock(block.id, (b) => ({ ...b, weeks: Math.min(52, weeks) }))}
                />
              </View>
              {block.days.map((day) => (
                <ListRow
                  key={day.id}
                  icon="strength"
                  hue="orange"
                  title={day.name}
                  subtitle={day.prescriptions.length
                    ? day.prescriptions.map((p) => p.exercise_name).join(', ')
                    : 'No exercises yet'}
                  value={day.prescriptions.length ? String(day.prescriptions.length) : undefined}
                  onPress={() => { void flush().then(() => router.push({ pathname: '/program/day', params: { program: id, day: day.id } })); }}
                />
              ))}
              <ListRow icon="plus" hue="tint" title="Add Training Day" titleTone="tint" accessory="none" onPress={() => addDay(block.id)} />
            </Card>
            <T variant="footnote" tone="label2" style={{ marginTop: space.sm, paddingHorizontal: space.lg }}>
              {block.days.length
                ? `These ${block.days.length} day${block.days.length === 1 ? '' : 's'} repeat each week for ${block.weeks} week${block.weeks === 1 ? '' : 's'}.`
                : 'Add the days a client trains each week.'}
            </T>
          </Section>
        ))}

        <View style={{ paddingHorizontal: space.lg, marginTop: space.lg }}>
          <Button label="Add Another Block" icon="layers" variant="gray" full onPress={addBlock} />
        </View>

        <Section title="Clients" action="Assign" onAction={() => { void flush().then(() => router.push({ pathname: '/program/assign', params: { program: id } })); }}>
          {(clients.data ?? []).length === 0 ? (
            <ListRow first icon="personAdd" hue="green" title="Assign to a Client" titleTone="tint" accessory="none"
              onPress={() => { void flush().then(() => router.push({ pathname: '/program/assign', params: { program: id } })); }} />
          ) : (clients.data ?? []).map((cl, i) => (
            <ListRow key={cl.id} first={i === 0} leading={<Avatar name={cl.fullName} size={32} />} title={cl.fullName}
              subtitle={`Since ${new Date(cl.startsOn).toLocaleDateString(undefined, { day: 'numeric', month: 'short' })}`}
              onPress={() => router.push({ pathname: '/client/[id]', params: { id: cl.id } })} />
          ))}
        </Section>
        <Spacer />
      </Body>

      <ActionSheet
        visible={menu}
        title={doc.name}
        onClose={() => setMenu(false)}
        actions={[
          { label: 'Duplicate', icon: 'copy', onPress: () => { void duplicate(); } },
          { label: 'Archive Program', icon: 'trash', destructive: true, onPress: () => setTimeout(() => setConfirmArchive(true), 250) },
        ]}
      />
      <ActionSheet
        visible={confirmArchive}
        title="Archive this program?"
        message={(clients.data ?? []).length ? 'Clients already on it keep following it.' : 'It disappears from your list.'}
        onClose={() => setConfirmArchive(false)}
        actions={[{ label: 'Archive', destructive: true, onPress: () => { void archive(); } }]}
      />
    </>
  );
}

/* ───────────────────────────── template ───────────────────────────── */

function TemplatePreview({ template: t }: { template: ProgramTemplate }) {
  const router = useRouter();
  const { db, touch } = useApp();
  const [busy, setBusy] = useState(false);
  const [skipped, setSkipped] = useState<string[]>([]);

  const use = async () => {
    if (!db) return;
    setBusy(true);
    const result = await copyTemplate(db, t);
    touch();
    setBusy(false);
    if (result.skipped.length) setSkipped(result.skipped);
    router.replace({ pathname: '/program/[id]', params: { id: result.id } });
  };

  const exercises = t.days.reduce((n, d) => n + d.exercises.length, 0);

  return (
    <>
      <Stack.Screen options={{ title: '' }} />
      <Body>
        <VStack align="center" gap={space.sm} style={{ paddingHorizontal: space.xl }}>
          <IconTile icon={t.icon} hue={t.hue} size={72} />
          <Spacer size={space.xs} />
          <T variant="title1" align="center">{t.name}</T>
          <HStack gap={6}>
            <Tag label={t.goal} hue={t.hue} />
            <Tag label={t.level} hue="gray" />
          </HStack>
          <T variant="body" tone="label2" align="center" style={{ marginTop: space.xs }}>{t.summary}</T>
        </VStack>

        <View style={{ flexDirection: 'row', gap: space.md, marginHorizontal: space.lg, marginTop: space.xl }}>
          {[
            { k: `${t.daysPerWeek}×`, v: 'per week' },
            { k: String(t.weeks), v: 'weeks' },
            { k: String(exercises), v: 'exercises' },
          ].map((m) => (
            <Card key={m.v} style={{ flex: 1, alignItems: 'center' }}>
              <T variant="metricSmall">{m.k}</T>
              <T variant="footnote" tone="label2">{m.v}</T>
            </Card>
          ))}
        </View>

        <View style={{ paddingHorizontal: space.lg, marginTop: space.xl }}>
          <Button label="Use This Template" icon="copy" variant="filled" full busy={busy} onPress={() => { void use(); }} />
          <T variant="footnote" tone="label2" align="center" style={{ marginTop: space.sm }}>
            Copies it into My Programs, where you can change anything.
          </T>
        </View>
        {skipped.length ? <Banner style={{ margin: space.lg }} tone="orange" message={`Not in your library, so left out: ${skipped.join(', ')}.`} /> : null}

        {t.days.map((day) => (
          <Section key={day.name} title={day.name}>
            {day.exercises.map((e, i) => (
              <ListRow
                key={`${e.name}-${i}`}
                first={i === 0}
                leading={e.superset ? <Tag label={`${e.superset}${day.exercises.filter((x, j) => x.superset === e.superset && j <= i).length}`} hue="purple" /> : undefined}
                title={e.name}
                subtitle={[
                  `${e.sets} × ${repRange(e.reps[0], e.reps[1])}`,
                  e.rpe ? `RPE ${e.rpe}` : null,
                  e.rest ? `rest ${formatRest(e.rest)}` : null,
                  e.notes,
                ].filter(Boolean).join(' · ')}
                accessory="none"
              />
            ))}
          </Section>
        ))}
        <Spacer />
      </Body>
    </>
  );
}

const useStyles = makeStyles(() => ({
  blockHead: { paddingHorizontal: space.lg, paddingTop: space.lg, paddingBottom: space.md },
}));
