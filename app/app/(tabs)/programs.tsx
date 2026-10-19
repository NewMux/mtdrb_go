/**
 * Programs: the trainer's own, and the gallery to start from.
 *
 * Writing a programme from a blank page is the slow part of coaching, so the
 * gallery sits one segment away. Using a template copies it into "My
 * Programs" as an ordinary programme, ready to edit and assign.
 */

import React, { useCallback, useState } from 'react';
import { Pressable, View } from 'react-native';
import { useFocusEffect, useRouter } from 'expo-router';

import { TEMPLATES, type ProgramTemplate } from '@/features/library';
import { emptyProgram, listPrograms, saveProgram } from '@/features/programs';
import { useApp, useQuery } from '@/state/app';
import {
  Card, EmptyState, HStack, IconButton, IconTile, ListRow, Page, Section, Segmented, Spacer, T, Tag, tap, VStack,
} from '@/ui/components';
import { Icon } from '@/ui/icon';
import { SyncBadge } from '@/ui/sync-badge';
import { makeStyles, radius, space, useTheme, wash } from '@/ui/theme';

export default function ProgramsScreen() {
  const router = useRouter();
  const { db, touch } = useApp();
  const [tab, setTab] = useState<'mine' | 'templates'>('mine');

  const programs = useQuery((database) => listPrograms(database), []);
  useFocusEffect(useCallback(() => { programs.reload(); }, [programs.reload]));

  const create = async () => {
    if (!db) return;
    const doc = emptyProgram();
    await saveProgram(db, doc);
    touch();
    router.push({ pathname: '/program/[id]', params: { id: doc.id, fresh: '1' } });
  };

  const mine = programs.data ?? [];

  return (
    <Page
      title="Programs"
      eyebrow={`${mine.length} program${mine.length === 1 ? '' : 's'} · ${TEMPLATES.length} templates`}
      trailing={
        <>
          <SyncBadge />
          <IconButton icon="plus" label="New program" onPress={() => { void create(); }} />
        </>
      }
    >
      <View style={{ paddingHorizontal: space.lg }}>
        <Segmented
          options={[{ value: 'mine', label: 'My Programs' }, { value: 'templates', label: 'Templates' }] as const}
          value={tab}
          onChange={setTab}
        />
      </View>

      {tab === 'mine' ? (
        mine.length === 0 ? (
          <Section plain>
            <Card>
              <EmptyState
                icon="programs"
                title="No programs yet"
                message="Start from a proven template, or build your own from the exercise library."
                action="Browse Templates"
                onAction={() => setTab('templates')}
              />
            </Card>
          </Section>
        ) : (
          <Section>
            {mine.map((p, i) => (
              <ListRow
                key={p.id}
                first={i === 0}
                icon="programs"
                hue="tint"
                title={p.name}
                subtitle={[
                  `${p.weeks} wk`,
                  `${p.days} day${p.days === 1 ? '' : 's'}`,
                  `${p.exercises} exercises`,
                ].join(' · ')}
                trailing={p.clients > 0 ? <Tag label={`${p.clients} client${p.clients === 1 ? '' : 's'}`} hue="green" /> : undefined}
                onPress={() => router.push({ pathname: '/program/[id]', params: { id: p.id } })}
              />
            ))}
          </Section>
        )
      ) : (
        <Section plain footer="Templates are starting points. Using one copies it into My Programs, where you can change anything.">
          <VStack gap={space.md}>
            {TEMPLATES.map((t) => (
              <TemplateCard
                key={t.key}
                template={t}
                onPress={() => router.push({ pathname: '/program/[id]', params: { id: 'template', template: t.key } })}
              />
            ))}
          </VStack>
        </Section>
      )}

      <Section title="Library">
        <ListRow
          first
          icon="list"
          hue="indigo"
          title="Exercise Library"
          subtitle="Browse by movement pattern"
          onPress={() => router.push('/exercises')}
        />
      </Section>
      <Spacer />
    </Page>
  );
}

function TemplateCard({ template: t, onPress }: { template: ProgramTemplate; onPress: () => void }) {
  const { c } = useTheme();
  const s = useStyles();
  const color = (c as unknown as Record<string, string>)[t.hue]!;
  const exercises = t.days.reduce((n, d) => n + d.exercises.length, 0);
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${t.name}, ${t.level}, ${t.goal}`}
      onPress={() => { tap(); onPress(); }}
      style={({ pressed }) => [s.card, pressed && { opacity: 0.8, transform: [{ scale: 0.985 }] }]}
    >
      <View style={[s.band, { backgroundColor: wash(color, c.scheme === 'dark' ? 0.22 : 0.12) }]}>
        <IconTile icon={t.icon} hue={t.hue} size={44} />
        <VStack gap={2} style={{ flex: 1 }}>
          <T variant="headline">{t.name}</T>
          <HStack gap={6} wrap>
            <Tag label={t.goal} hue={t.hue} />
            <Tag label={t.level} hue="gray" />
          </HStack>
        </VStack>
        <Icon name="chevronRight" size={14} color={c.label3} weight="bold" />
      </View>
      <View style={s.body}>
        <T variant="subhead" tone="label2" lines={2}>{t.summary}</T>
        <HStack gap={space.lg} style={{ marginTop: space.md }}>
          <Meta icon="calendar" label={`${t.daysPerWeek}× per week`} />
          <Meta icon="hourglass" label={`${t.weeks} weeks`} />
          <Meta icon="list" label={`${exercises} exercises`} />
        </HStack>
      </View>
    </Pressable>
  );
}

function Meta({ icon, label }: { icon: 'calendar' | 'hourglass' | 'list'; label: string }) {
  const { c } = useTheme();
  return (
    <HStack gap={4}>
      <Icon name={icon} size={13} color={c.label2} />
      <T variant="footnote" tone="label2">{label}</T>
    </HStack>
  );
}

const useStyles = makeStyles((c) => ({
  card: { backgroundColor: c.surface, borderRadius: radius.lg, overflow: 'hidden' },
  band: { flexDirection: 'row', alignItems: 'center', gap: space.md, padding: space.lg },
  body: { paddingHorizontal: space.lg, paddingBottom: space.lg, paddingTop: space.md },
}));
