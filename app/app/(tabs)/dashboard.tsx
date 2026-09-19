/**
 * The five numbers.
 *
 * Not a chart wall. Each line is either something to do today or money that
 * has not arrived, and the renewal list underneath is the one the business
 * actually runs on.
 *
 * The numbers come from the server because they are ledger arithmetic — a
 * payment recorded on another device moves three of them at once, and a stale
 * figure here would be quietly wrong. The renewal list is read from the device
 * as well, so the conversation it drives can happen on a gym floor with no
 * signal. When both are available the server's wins; when only one is, the app
 * says which it is showing rather than pretending.
 */

import React, { useCallback, useEffect, useState } from 'react';
import { Pressable, RefreshControl, ScrollView, View } from 'react-native';
import { useFocusEffect, useRouter } from 'expo-router';

import { NetworkError } from '@/api/client';
import type { DashboardSummary } from '@/api/types';
import { lowBalanceClients, type LowBalanceClient } from '@/features/queries';
import { useApp, useQuery } from '@/state/app';
import {
  Body, Caption, Card, Chip, Empty, Heading, Label, Metric,
  Row, Screen, Spacer, Title,
} from '@/ui/components';
import { SyncBadge } from '@/ui/sync-badge';
import { money, shortDate } from '@/ui/format';
import { colors, space } from '@/ui/theme';

export default function DashboardScreen() {
  const { api, account, sync, syncNow, revision } = useApp();
  const router = useRouter();

  const [summary, setSummary] = useState<DashboardSummary | null>(null);
  const [loading, setLoading] = useState(true);

  const threshold = summary?.low_balance_threshold ?? 2;
  const local = useQuery((db) => lowBalanceClients(db, threshold), [threshold]);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setSummary(await api.dashboard());
    } catch (cause) {
      // Being offline is the normal case this app is built for, and the
      // renewal list below still works, so the screen degrades rather than
      // fails. A null summary is what "no numbers" looks like downstream.
      if (!(cause instanceof NetworkError)) throw cause;
      setSummary(null);
    } finally {
      setLoading(false);
    }
  }, [api]);

  useEffect(() => { void load(); }, [load, revision]);
  useFocusEffect(useCallback(() => { void load(); local.reload(); }, [load, local.reload]));

  const currency = summary?.currency ?? account?.currency ?? 'EUR';

  // The server's list when it is reachable, the device's when it is not.
  const renewals: LowBalanceClient[] = summary
    ? summary.low_balance.map((c) => ({
        id: c.client_id,
        fullName: c.client_name,
        creditsRemaining: c.remaining,
        nextExpiry: c.next_expiry ?? null,
        lastSessionOn: c.last_session_on ?? null,
      }))
    : (local.data ?? []);

  return (
    <Screen>
      <ScrollView
        contentContainerStyle={{ padding: space.lg, paddingTop: space.xl, paddingBottom: 176 }}
        refreshControl={
          <RefreshControl
            refreshing={loading || sync.running}
            onRefresh={() => { void load(); void syncNow(); }}
            tintColor={colors.inkMuted}
          />
        }
      >
        <Row style={{ justifyContent: 'space-between' }}>
          <View>
            <Title>Practice</Title>
            <Caption>{new Date().toLocaleDateString(undefined, { month: 'long', year: 'numeric' })}</Caption>
          </View>
          <SyncBadge />
        </Row>

        <Spacer size={space.xl} />

        {summary ? (
          <>
            {/* Earned, not collected. Selling a pack is a liability, not income. */}
            <Card tone="accent">
              <Metric
                value={money(summary.income_this_month.minor, currency).replace(` ${currency}`, '')}
                unit={currency}
                label="earned this month"
                tone="onAccent"
              />
              <Spacer size={space.sm} />
              <Caption tone="onAccent">Revenue recognised as sessions were delivered.</Caption>
            </Card>

            <Spacer />

            <Row style={{ gap: space.md, alignItems: 'stretch' }}>
              <Card style={{ flex: 1 }}>
                <Metric value={String(summary.sessions_today)} label="today" />
                {summary.sessions_today_unmarked > 0 ? (
                  <Caption tone="warning">{summary.sessions_today_unmarked} still to mark</Caption>
                ) : null}
              </Card>
              <Card style={{ flex: 1 }}>
                <Metric value={String(summary.sessions_left_this_week)} label="left this week" />
              </Card>
            </Row>

            <Spacer />

            <Pressable accessibilityRole="button" onPress={() => router.push('/money')}>
              <Card>
                <Row style={{ justifyContent: 'space-between', alignItems: 'flex-end' }}>
                  <Metric value={String(summary.unpaid_invoices)} label="unpaid invoices" />
                  <Body muted>{money(summary.outstanding.minor, currency)}</Body>
                </Row>
              </Card>
            </Pressable>
          </>
        ) : (
          // Four empty cards would be four redaction bars. These numbers are
          // ledger arithmetic and simply do not exist without the server, so
          // the screen says that once and gets on with the part that works.
          <Card tone="raised">
            <Heading>{loading ? 'Loading the numbers…' : 'Numbers need a connection'}</Heading>
            <Spacer size={space.xs} />
            <Body muted>
              Earnings, receivables and the week&apos;s bookings are worked out from the
              ledger on the server. The renewal list below is this device&apos;s own copy
              and is always here.
            </Body>
          </Card>
        )}

        <Spacer size={space.xl} />
        <Row style={{ justifyContent: 'space-between' }}>
          <Label>Needs renewing</Label>
          <Caption>{threshold} credits or fewer</Caption>
        </Row>
        <Spacer />

        {renewals.length === 0 ? (
          <Empty
            title="Nobody to chase"
            detail="Everyone has credits left. This is where renewals appear."
          />
        ) : (
          renewals.map((client) => (
            <Pressable
              key={client.id}
              accessibilityRole="button"
              accessibilityLabel={`${client.fullName}, ${client.creditsRemaining} credits`}
              onPress={() => router.push({ pathname: '/client/[id]', params: { id: client.id } })}
              style={{ marginBottom: space.sm }}
            >
              <Card>
                <Row style={{ justifyContent: 'space-between' }}>
                  <View style={{ flex: 1 }}>
                    <Heading>{client.fullName}</Heading>
                    <Spacer size={space.xs} />
                    <Caption tone={client.creditsRemaining < 0 ? 'danger' : 'muted'}>
                      {describe(client)}
                    </Caption>
                  </View>
                  <Chip
                    label={String(client.creditsRemaining)}
                    tone={client.creditsRemaining > 0 ? 'accent' : 'danger'}
                  />
                </Row>
              </Card>
            </Pressable>
          ))
        )}
      </ScrollView>
    </Screen>
  );
}

/**
 * Why this client is on the list.
 *
 * "One session left" and "hasn't been since April" are both zero-ish balances
 * and completely different conversations, so the line says which.
 */
function describe(client: LowBalanceClient): string {
  const parts: string[] = [];
  if (client.creditsRemaining < 0) parts.push('overdrawn');
  else if (client.creditsRemaining === 0) parts.push('out of credits');
  else parts.push(`${client.creditsRemaining} session${client.creditsRemaining === 1 ? '' : 's'} left`);

  if (client.nextExpiry) parts.push(`expires ${shortDate(client.nextExpiry)}`);
  if (client.lastSessionOn) parts.push(`last trained ${shortDate(client.lastSessionOn)}`);
  return parts.join(' · ');
}
