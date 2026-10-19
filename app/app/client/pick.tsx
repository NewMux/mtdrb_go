/**
 * Choose a client, then carry on to whatever needed one.
 */

import React, { useState } from 'react';
import { View } from 'react-native';
import { Stack, useLocalSearchParams, useRouter } from 'expo-router';

import { listClients } from '@/features/queries';
import { openOrStartWorkout } from '@/features/workflow';
import { useApp, useQuery } from '@/state/app';
import { Avatar, Body, EmptyState, ListRow, SearchField, Section, Tag } from '@/ui/components';
import { space } from '@/ui/theme';

export default function PickClientScreen() {
  const { next } = useLocalSearchParams<{ next: 'workout' | 'sell' }>();
  const { db, touch } = useApp();
  const router = useRouter();
  const [search, setSearch] = useState('');
  const clients = useQuery((database) => listClients(database, search), [search]);

  const choose = async (id: string, name: string) => {
    if (!db) return;
    if (next === 'workout') {
      const { id: workoutId, created } = await openOrStartWorkout(db, id);
      if (created) touch();
      router.replace({ pathname: '/workout/[id]', params: { id: workoutId, client: id, name } });
    } else {
      router.replace({ pathname: '/sell-package', params: { client: id, name } });
    }
  };

  return (
    <>
      <Stack.Screen options={{ title: next === 'workout' ? 'Start Workout' : 'Sell a Package' }} />
      <Body keyboard>
        <View style={{ paddingHorizontal: space.lg }}>
          <SearchField value={search} onChangeText={setSearch} placeholder="Find a client" />
        </View>
        {(clients.data ?? []).length === 0 ? (
          <EmptyState icon="clients" title="No clients found" />
        ) : (
          <Section>
            {(clients.data ?? []).map((c, i) => (
              <ListRow
                key={c.id}
                first={i === 0}
                leading={<Avatar name={c.fullName} size={36} />}
                title={c.fullName}
                trailing={<Tag label={`${c.creditsRemaining} left`} hue={c.creditsRemaining <= 0 ? 'red' : 'gray'} />}
                onPress={() => { void choose(c.id, c.fullName); }}
              />
            ))}
          </Section>
        )}
      </Body>
    </>
  );
}
