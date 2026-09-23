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
import { useT, type I18n } from '@/i18n';
import { space } from '@/ui/theme';
import { useTheme } from '@/ui/theming';

export default function DashboardScreen() {
  const { api, account, sync, syncNow, revision } = useApp();
  const router = useRouter();
  const i18n = useT();
  const { t, amount, money, date } = i18n;
  const { colors } = useTheme();

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
            <Title>{t('practice.title')}</Title>
            <Caption>{date(new Date(), 'month')}</Caption>
          </View>
          <SyncBadge />
        </Row>

        <Spacer size={space.xl} />

        {summary ? (
          <>
            {/* Earned, not collected. Selling a pack is a liability, not income. */}
            <Card tone="accent">
              <Metric
                value={amount(summary.income_this_month.minor, currency)}
                unit={currency}
                label={t('practice.earnedThisMonth')}
                tone="onAccent"
              />
              <Spacer size={space.sm} />
              <Caption tone="onAccent">{t('practice.earnedExplainer')}</Caption>
            </Card>

            <Spacer />

            <Row style={{ gap: space.md, alignItems: 'stretch' }}>
              <Card style={{ flex: 1 }}>
                <Metric value={i18n.number(summary.sessions_today)} label={t('practice.today')} />
                {summary.sessions_today_unmarked > 0 ? (
                  <Caption tone="warning">{t('practice.stillToMark', { count: summary.sessions_today_unmarked })}</Caption>
                ) : null}
              </Card>
              <Card style={{ flex: 1 }}>
                <Metric value={i18n.number(summary.sessions_left_this_week)} label={t('practice.leftThisWeek')} />
              </Card>
            </Row>

            <Spacer />

            <Pressable accessibilityRole="button" onPress={() => router.push('/money')}>
              <Card>
                <Row style={{ justifyContent: 'space-between', alignItems: 'flex-end' }}>
                  <Metric value={i18n.number(summary.unpaid_invoices)} label={t('practice.unpaidInvoices')} />
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
            <Heading>{loading ? t('practice.loadingNumbers') : t('practice.needConnection')}</Heading>
            <Spacer size={space.xs} />
            <Body muted>{t('practice.needConnectionBody')}</Body>
          </Card>
        )}

        <Spacer size={space.xl} />
        <Row style={{ justifyContent: 'space-between' }}>
          <Label>{t('practice.needsRenewing')}</Label>
          <Caption>{t('practice.threshold', { count: threshold })}</Caption>
        </Row>
        <Spacer />

        {renewals.length === 0 ? (
          <Empty
            icon="clients"
            title={t('practice.nobodyToChase')}
            detail={t('practice.nobodyToChaseBody')}
          />
        ) : (
          renewals.map((client) => (
            <Pressable
              key={client.id}
              accessibilityRole="button"
              accessibilityLabel={t('practice.creditsA11y', { name: client.fullName, count: client.creditsRemaining })}
              onPress={() => router.push({ pathname: '/client/[id]', params: { id: client.id } })}
              style={{ marginBottom: space.sm }}
            >
              <Card>
                <Row style={{ justifyContent: 'space-between' }}>
                  <View style={{ flex: 1 }}>
                    <Heading>{client.fullName}</Heading>
                    <Spacer size={space.xs} />
                    <Caption tone={client.creditsRemaining < 0 ? 'danger' : 'muted'}>
                      {describe(client, i18n)}
                    </Caption>
                  </View>
                  <Chip
                    label={i18n.number(client.creditsRemaining)}
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
function describe(client: LowBalanceClient, { t, date }: I18n): string {
  const parts: string[] = [];
  if (client.creditsRemaining < 0) parts.push(t('practice.overdrawn'));
  else if (client.creditsRemaining === 0) parts.push(t('practice.outOfCredits'));
  else parts.push(t('practice.sessionsLeft', { count: client.creditsRemaining }));

  if (client.nextExpiry) parts.push(t('practice.expires', { date: date(client.nextExpiry, 'short') }));
  if (client.lastSessionOn) parts.push(t('practice.lastTrained', { date: date(client.lastSessionOn, 'short') }));
  return parts.join(' · ');
}
