/**
 * Putting a client on a programme.
 *
 * Reached from either end — a programme looking for clients, or a client
 * looking for a programme — so it lists whichever side is missing.
 * Assigning replaces whatever the client was following; a client follows one
 * plan at a time.
 */

import React, { useState } from 'react';
import { View } from 'react-native';
import { Stack, useLocalSearchParams, useRouter } from 'expo-router';

import { assignProgram, listPrograms } from '@/features/programs';
import { rosterOverview } from '@/features/queries';
import { useApp, useQuery } from '@/state/app';
import { Avatar, Body, Button, EmptyState, ListRow, Section, Segmented, T, tap } from '@/ui/components';
import { space } from '@/ui/theme';

function nextMonday(): Date {
  const d = new Date();
  d.setDate(d.getDate() + ((8 - d.getDay()) % 7 || 7));
  return d;
}

export default function AssignScreen() {
  const params = useLocalSearchParams<{ program?: string; client?: string }>();
  const { db, touch, syncNow } = useApp();
  const router = useRouter();
  const [programId, setProgramId] = useState(params.program ?? null);
  const [clientId, setClientId] = useState(params.client ?? null);
  const [start, setStart] = useState<'today' | 'monday'>('today');
  const [busy, setBusy] = useState(false);

  const programs = useQuery((d) => listPrograms(d), []);
  const clients = useQuery((d) => rosterOverview(d), []);

  const program = (programs.data ?? []).find((p) => p.id === programId);
  const client = (clients.data ?? []).find((c) => c.id === clientId);

  const assign = async () => {
    if (!db || !programId || !clientId) return;
    setBusy(true);
    await assignProgram(db, programId, clientId, start === 'today' ? new Date() : nextMonday());
    tap('success');
    touch();
    void syncNow();
    setBusy(false);
    router.back();
  };

  return (
    <>
      <Stack.Screen options={{ title: 'Assign Program' }} />
      <Body>
        {!params.program ? (
          <Section title="Program">
            {(programs.data ?? []).length === 0 ? (
              <EmptyState icon="programs" title="No programs yet" message="Create one or copy a template from the Programs tab." />
            ) : (programs.data ?? []).map((p, i) => (
              <ListRow key={p.id} first={i === 0} icon="programs" hue="tint" title={p.name}
                subtitle={`${p.weeks} wk · ${p.days} days`} accessory={p.id === programId ? 'check' : 'none'}
                onPress={() => setProgramId(p.id)} />
            ))}
          </Section>
        ) : null}

        {!params.client ? (
          <Section title="Client" footer="A client follows one program at a time — assigning replaces their current one.">
            {(clients.data ?? []).map((c, i) => (
              <ListRow key={c.id} first={i === 0} leading={<Avatar name={c.fullName} size={32} />} title={c.fullName}
                subtitle={c.programName ? `On ${c.programName}` : 'No program'}
                accessory={c.id === clientId ? 'check' : 'none'} onPress={() => setClientId(c.id)} />
            ))}
          </Section>
        ) : null}

        <Section title="Starting" plain>
          <Segmented options={[{ value: 'today', label: 'Today' }, { value: 'monday', label: 'Next Monday' }] as const} value={start} onChange={setStart} />
        </Section>

        <View style={{ paddingHorizontal: space.lg, marginTop: space.xl, gap: space.sm }}>
          {program && client ? (
            <T variant="subhead" tone="label2" align="center">{client.fullName} → {program.name}</T>
          ) : null}
          <Button label="Assign" variant="filled" full busy={busy} disabled={!programId || !clientId} onPress={() => { void assign(); }} />
        </View>
      </Body>
    </>
  );
}
