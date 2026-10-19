/**
 * The roster.
 *
 * Searchable because a trainer with sixty clients looks people up by whatever
 * they remember — a first name, a phone number, the email they booked with.
 * Filterable because the questions a trainer asks of the whole roster are few
 * and fixed: who is running low, who has gone quiet, who has no plan.
 */

import React, { useCallback, useMemo, useState } from 'react';
import { View } from 'react-native';
import { useFocusEffect, useLocalSearchParams, useRouter } from 'expo-router';

import { rosterOverview, type RosterClient } from '@/features/queries';
import { useApp, useQuery } from '@/state/app';
import {
  Avatar, Card, ChipBar, EmptyState, IconButton, ListRow, Page, SearchField, Section, Spacer, T, Tag,
} from '@/ui/components';
import { SyncBadge } from '@/ui/sync-badge';
import { shortDate } from '@/ui/format';
import { space } from '@/ui/theme';

type Filter = 'all' | 'low' | 'quiet' | 'noplan';

const QUIET_DAYS = 14;

export default function ClientsScreen() {
  const router = useRouter();
  const params = useLocalSearchParams<{ filter?: Filter }>();
  const { sync } = useApp();
  const [search, setSearch] = useState('');
  const [filter, setFilter] = useState<Filter>(params.filter ?? 'all');

  const clients = useQuery((db) => rosterOverview(db, search), [search]);
  useFocusEffect(useCallback(() => { clients.reload(); }, [clients.reload]));

  const all = clients.data ?? [];
  const counts = useMemo(() => ({
    low: all.filter(isLow).length,
    quiet: all.filter(isQuiet).length,
    noplan: all.filter((c) => !c.programName).length,
  }), [all]);

  const rows = all.filter((c) =>
    filter === 'low' ? isLow(c) : filter === 'quiet' ? isQuiet(c) : filter === 'noplan' ? !c.programName : true);

  return (
    <Page
      title="Clients"
      eyebrow={`${all.length} on the roster`}
      keyboard
      trailing={
        <>
          <SyncBadge />
          <IconButton icon="personAdd" label="Add client" onPress={() => router.push('/client/new')} />
        </>
      }
    >
      <View style={{ paddingHorizontal: space.lg }}>
        <SearchField value={search} onChangeText={setSearch} placeholder="Name, email or phone" />
      </View>
      <Spacer size={space.md} />
      <ChipBar
        value={filter}
        onChange={setFilter}
        options={[
          { value: 'all', label: 'All', count: all.length },
          { value: 'low', label: 'Running low', count: counts.low },
          { value: 'quiet', label: 'Gone quiet', count: counts.quiet },
          { value: 'noplan', label: 'No program', count: counts.noplan },
        ] as const}
      />

      {rows.length === 0 ? (
        <Section plain>
          <Card>
            <EmptyState
              icon={search ? 'search' : 'clients'}
              title={search ? 'Nobody matches' : filter === 'all' ? 'No clients yet' : 'Nobody here'}
              message={search ? 'Try a different name, email or phone.'
                : filter === 'all' ? 'Add the first person you train. It works with or without signal.'
                : 'Good news — nobody fits this filter right now.'}
              action={filter === 'all' && !search ? 'Add Client' : undefined}
              onAction={() => router.push('/client/new')}
            />
          </Card>
        </Section>
      ) : (
        <Section>
          {rows.map((client, i) => (
            <ListRow
              key={client.id}
              first={i === 0}
              leading={<Avatar name={client.fullName} size={40} />}
              title={client.fullName}
              subtitle={describe(client)}
              trailing={<CreditTag credits={client.creditsRemaining} />}
              onPress={() => router.push({ pathname: '/client/[id]', params: { id: client.id } })}
            />
          ))}
        </Section>
      )}

      {sync.offline ? (
        <T variant="footnote" tone="label2" align="center" style={{ marginTop: space.lg, paddingHorizontal: space.xxl }}>
          Showing the copy on this device. It refreshes when there is signal.
        </T>
      ) : null}
    </Page>
  );
}

function isLow(c: RosterClient) { return c.creditsRemaining <= 2; }

function isQuiet(c: RosterClient) {
  if (c.nextSession) return false;
  if (!c.lastTrained) return false;
  return (Date.now() - new Date(c.lastTrained).getTime()) / 86_400_000 >= QUIET_DAYS;
}

function describe(c: RosterClient): string {
  const parts: string[] = [];
  if (c.programName) parts.push(c.programName);
  if (c.nextSession) parts.push(`Next ${shortDate(c.nextSession)}`);
  else if (c.lastTrained) parts.push(`Last trained ${shortDate(c.lastTrained)}`);
  return parts.join(' · ') || c.email || c.phone || 'New client';
}

/** A negative balance is a real state — an overdrawn client — and is shown as one. */
function CreditTag({ credits }: { credits: number }) {
  if (credits < 0) return <Tag label={`${credits}`} hue="red" />;
  if (credits === 0) return <Tag label="0" hue="red" />;
  if (credits <= 2) return <Tag label={String(credits)} hue="orange" />;
  return <Tag label={String(credits)} hue="gray" />;
}
