/**
 * The roster.
 *
 * Searchable because a trainer with sixty clients looks people up by whatever
 * they remember — a first name, a phone number, the email they booked with.
 */

import React, { useCallback, useState } from 'react';
import { Pressable, ScrollView, View } from 'react-native';
import { useFocusEffect, useRouter } from 'expo-router';

import { listClients } from '@/features/queries';
import { useApp, useQuery } from '@/state/app';
import {
  Body, Caption, Card, Chip, Empty, Field, Label, Row, Screen, Spacer, Title,
} from '@/ui/components';
import { SyncBadge } from '@/ui/sync-badge';
import { space } from '@/ui/theme';

export default function ClientsScreen() {
  const router = useRouter();
  const { sync } = useApp();
  const [search, setSearch] = useState('');

  const clients = useQuery((db) => listClients(db, search), [search]);
  useFocusEffect(useCallback(() => { clients.reload(); }, [clients.reload]));

  const rows = clients.data ?? [];

  return (
    <Screen>
      <ScrollView
        contentContainerStyle={{ padding: space.lg, paddingTop: space.xl, paddingBottom: 176 }}
        keyboardShouldPersistTaps="handled"
      >
        <Row style={{ justifyContent: 'space-between' }}>
          <View>
            <Title>Clients</Title>
            <Caption>{rows.length} on the roster</Caption>
          </View>
          <SyncBadge />
        </Row>

        <Spacer />
        <Field
          label="Search"
          value={search}
          onChangeText={setSearch}
          placeholder="Name, email or phone"
          autoCapitalize="none"
        />
        <Spacer size={space.xl} />
        <Label>{search ? 'Matches' : 'Everyone'}</Label>
        <Spacer />

        {rows.length === 0 ? (
          <Empty
            title={search ? 'Nobody matches' : 'No clients yet'}
            detail={search ? 'Try a different search.' : 'Tap + below to add your first client.'}
          />
        ) : (
          rows.map((client) => (
            <Pressable
              key={client.id}
              accessibilityRole="button"
              accessibilityLabel={client.fullName}
              onPress={() => router.push({ pathname: '/client/[id]', params: { id: client.id } })}
              style={{ marginBottom: space.sm }}
            >
              <Card>
                <Row style={{ justifyContent: 'space-between' }}>
                  <View style={{ flex: 1 }}>
                    <Body>{client.fullName}</Body>
                    <Caption>{client.email ?? client.phone ?? client.status}</Caption>
                  </View>
                  <Row style={{ gap: space.sm }}>
                    <Caption tone={creditTone(client.creditsRemaining)}>
                      {creditLabel(client.creditsRemaining)}
                    </Caption>
                    <Chip
                      label={String(client.creditsRemaining)}
                      tone={client.creditsRemaining > 0 ? 'accent' : 'danger'}
                    />
                  </Row>
                </Row>
              </Card>
            </Pressable>
          ))
        )}

        {sync.offline ? (
          <>
            <Spacer />
            <Caption>Showing the copy on this device. It refreshes when there is signal.</Caption>
          </>
        ) : null}
      </ScrollView>
    </Screen>
  );
}

/** A negative balance is a real state — an overdrawn client — and is shown as one. */
function creditLabel(credits: number): string {
  if (credits < 0) return `${credits} owed`;
  if (credits === 0) return 'no credits';
  return `${credits} credit${credits === 1 ? '' : 's'}`;
}

function creditTone(credits: number): 'muted' | 'warning' | 'danger' {
  if (credits < 0) return 'danger';
  if (credits === 0) return 'warning';
  return 'muted';
}
