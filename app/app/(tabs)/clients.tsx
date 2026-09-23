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
import { useT, type I18n } from '@/i18n';
import { useApp, useQuery } from '@/state/app';
import {
  Avatar, Body, Caption, Card, Chip, Empty, Label, Row, Screen, SearchField, Spacer, Title,
} from '@/ui/components';
import { SyncBadge } from '@/ui/sync-badge';
import { space } from '@/ui/theme';

export default function ClientsScreen() {
  const router = useRouter();
  const { sync } = useApp();
  const i18n = useT();
  const { t } = i18n;
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
            <Title>{t('clients.title')}</Title>
            <Caption>{t('clients.onRoster', { count: rows.length })}</Caption>
          </View>
          <SyncBadge />
        </Row>

        <Spacer />
        <SearchField
          label={t('common.search')}
          value={search}
          onChangeText={setSearch}
          placeholder={t('clients.searchPlaceholder')}
        />
        <Spacer size={space.xl} />
        <Label>{search ? t('clients.matches') : t('clients.everyone')}</Label>
        <Spacer />

        {rows.length === 0 ? (
          <Empty
            icon={search ? 'search' : 'clients'}
            title={search ? t('clients.nobodyMatches') : t('clients.none')}
            detail={search ? t('clients.tryDifferent') : t('clients.noneBody')}
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
                  <Avatar name={client.fullName} size={36} />
                  <View style={{ flex: 1 }}>
                    <Body>{client.fullName}</Body>
                    <Caption>{client.email ?? client.phone ?? client.status}</Caption>
                  </View>
                  <Row style={{ gap: space.sm }}>
                    <Caption tone={creditTone(client.creditsRemaining)}>
                      {creditLabel(client.creditsRemaining, i18n)}
                    </Caption>
                    <Chip
                      label={i18n.number(client.creditsRemaining)}
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
            <Caption>{t('clients.deviceCopy')}</Caption>
          </>
        ) : null}
      </ScrollView>
    </Screen>
  );
}

/** A negative balance is a real state — an overdrawn client — and is shown as one. */
function creditLabel(credits: number, { t }: I18n): string {
  if (credits < 0) return t('clients.owed', { count: Math.abs(credits) });
  if (credits === 0) return t('clients.noCredits');
  return t('common.credits', { count: credits });
}

function creditTone(credits: number): 'muted' | 'warning' | 'danger' {
  if (credits < 0) return 'danger';
  if (credits === 0) return 'warning';
  return 'muted';
}
