/**
 * The roster.
 *
 * Searchable because a trainer with sixty clients looks people up by whatever
 * they remember — a first name, a phone number, the email they booked with.
 */

import React, { useCallback, useState } from 'react';
import { Pressable, View } from 'react-native';
import { useFocusEffect, useRouter } from 'expo-router';

import { listClients } from '@/features/queries';
import { useT, type I18n } from '@/i18n';
import { useApp, useQuery } from '@/state/app';
import { Avatar, Body, Button, Caption, Card, Chip, Empty, Label, Row, SearchField, Spacer } from '@/ui/components';
import { useLayout } from '@/ui/layout';
import { Page } from '@/ui/page';
import { space } from '@/ui/theme';

export default function ClientsScreen() {
  const router = useRouter();
  const { sync } = useApp();
  const i18n = useT();
  const { t } = i18n;
  const { wide } = useLayout();
  const [search, setSearch] = useState('');

  const clients = useQuery((db) => listClients(db, search), [search]);
  useFocusEffect(useCallback(() => { clients.reload(); }, [clients.reload]));

  const rows = clients.data ?? [];

  return (
    <Page
      title={t('clients.title')}
      subtitle={t('clients.onRoster', { count: rows.length })}
      actions={wide ? (
        // The phone has its floating button; a desk looks for the action here.
        <Button compact tone="primary" icon="add" label={t('nav.addClient')} onPress={() => router.push('/dashboard/clients/new')} />
      ) : undefined}
    >
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
              onPress={() => router.push({ pathname: '/dashboard/clients/[id]', params: { id: client.id } })}
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
    </Page>
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
