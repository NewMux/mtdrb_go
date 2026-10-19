/**
 * The business.
 *
 * Earned, owed, collected — kept apart because they are different things.
 * "Earned this month" is revenue the ledger recognised as sessions were
 * delivered, and only the server can say it honestly, so it shows when there
 * is signal. Money collected, invoices outstanding and the prepaid balance are
 * on the device, so they are always here.
 *
 * Recording a payment does **not** flip an invoice to settled on this device.
 * Whether it settles depends on what else has been paid, possibly from another
 * device, and on the server's refusal to accept an overpayment — so the row
 * says "recorded" until the server agrees.
 */

import React, { useCallback, useEffect, useState } from 'react';
import { View } from 'react-native';
import { useFocusEffect, useRouter } from 'expo-router';

import { NetworkError } from '@/api/client';
import type { DashboardSummary } from '@/api/types';
import { collectedByMonth, practiceStats, recentPayments } from '@/features/insights';
import { lowBalanceClients, outstandingInvoices } from '@/features/queries';
import { DEMO, useApp, useQuery } from '@/state/app';
import { BarChart } from '@/ui/charts';
import {
  Avatar, Banner, Card, EmptyState, HStack, ListRow, Page, Section, Spacer, Stat, StatGrid, T, Tag, VStack,
} from '@/ui/components';
import { SyncBadge } from '@/ui/sync-badge';
import { dueLabel, money, shortDate } from '@/ui/format';
import { space } from '@/ui/theme';

const INSTRUMENT_LABEL: Record<string, string> = {
  bank_transfer: 'Bank transfer', cash: 'Cash', digital_wallet: 'Wallet', cheque: 'Cheque',
};

export default function BusinessScreen() {
  const { api, account, revision, sync, syncNow } = useApp();
  const router = useRouter();
  const currency = account?.currency ?? 'EUR';

  const [summary, setSummary] = useState<DashboardSummary | null>(null);
  const [loading, setLoading] = useState(!DEMO);

  const invoices = useQuery((db) => outstandingInvoices(db), []);
  const months = useQuery((db) => collectedByMonth(db, 6), []);
  const stats = useQuery((db) => practiceStats(db), []);
  const payments = useQuery((db) => recentPayments(db, 6), []);
  const renewals = useQuery((db) => lowBalanceClients(db, summary?.low_balance_threshold ?? 2), [summary?.low_balance_threshold]);

  const load = useCallback(async () => {
    if (DEMO) return;
    setLoading(true);
    try {
      setSummary(await api.dashboard());
    } catch (cause) {
      // Offline is the normal case this app is built for; the device's own
      // figures still show, and the ledger's wait for signal.
      if (!(cause instanceof NetworkError)) throw cause;
      setSummary(null);
    } finally {
      setLoading(false);
    }
  }, [api]);

  useEffect(() => { void load(); }, [load, revision]);
  useFocusEffect(useCallback(() => {
    invoices.reload(); months.reload(); stats.reload(); payments.reload(); renewals.reload();
  }, [invoices.reload, months.reload, stats.reload, payments.reload, renewals.reload]));

  const rows = invoices.data ?? [];
  const owed = rows.reduce((n, i) => n + i.totalMinor - i.paidMinor, 0);
  const overdue = rows.filter((i) => dueLabel(i.dueDate).endsWith('overdue'));
  const series = (months.data ?? []).map((m) => ({ label: m.label, value: m.minor }));
  const thisMonth = series.at(-1)?.value ?? 0;
  const lastMonth = series.at(-2)?.value ?? 0;
  const delta = lastMonth > 0 ? Math.round(((thisMonth - lastMonth) / lastMonth) * 100) : null;
  const st = stats.data;

  return (
    <Page
      title="Business"
      eyebrow={new Date().toLocaleDateString(undefined, { month: 'long', year: 'numeric' })}
      refreshing={loading || sync.running}
      onRefresh={() => { void load(); void syncNow(); }}
      trailing={<SyncBadge />}
    >
      {summary ? (
        <Card style={{ marginHorizontal: space.lg }}>
          <T variant="footnote" weight="600" tone="green">EARNED THIS MONTH</T>
          <HStack gap={6} align="baseline" style={{ marginTop: 4 }}>
            <T variant="metric">{money(summary.income_this_month.minor, summary.currency).replace(` ${summary.currency}`, '')}</T>
            <T variant="subhead" tone="label2">{summary.currency}</T>
          </HStack>
          <T variant="footnote" tone="label2">Recognised as sessions were delivered — prepaid packs count when they are trained, not when they are sold.</T>
        </Card>
      ) : null}

      <Section title="Collected" plain>
        <Card>
          <BarChart data={series} hue="green" format={(v) => money(v, currency)} />
          {delta !== null ? (
            <T variant="footnote" tone={delta >= 0 ? 'green' : 'red'} weight="600" style={{ marginTop: space.sm }}>
              {delta >= 0 ? '▲' : '▼'} {Math.abs(delta)}% on last month
            </T>
          ) : null}
        </Card>
      </Section>

      <Section title="Overview" plain>
        <StatGrid>
          <Stat icon="invoice" hue="orange" label="Owed to you" value={money(owed, currency).replace(` ${currency}`, '')} unit={currency}
            footnote={overdue.length ? `${overdue.length} overdue` : `${rows.length} open`} />
          <Stat icon="wallet" hue="indigo" label="Prepaid sessions" value={String(st?.prepaidCredits ?? 0)} footnote="still to deliver" />
          <Stat icon="clients" hue="tint" label="Active clients" value={String(st?.activeClients ?? 0)}
            footnote={st?.newThisMonth ? `+${st.newThisMonth} this month` : undefined} />
          <Stat icon="checkCircle" hue="green" label="Sessions" value={String(st?.sessionsThisMonth ?? 0)} footnote="delivered this month" />
          <Stat icon="closeCircle" hue="red" label="No-show rate"
            value={st?.noShowRate == null ? '—' : String(Math.round(st.noShowRate * 100))}
            unit={st?.noShowRate == null ? undefined : '%'} footnote="this month" />
          <Stat icon="cart" hue="purple" label="Average pack"
            value={st?.averagePackMinor == null ? '—' : money(st.averagePackMinor, currency).replace(` ${currency}`, '')}
            unit={st?.averagePackMinor == null ? undefined : currency} />
        </StatGrid>
      </Section>

      {!summary && !DEMO && !loading ? (
        <Banner
          style={{ marginHorizontal: space.lg, marginTop: space.lg }}
          tone="gray"
          icon="offline"
          message="Earned revenue comes from the ledger on the server and shows when you are back online. Everything else here is this device's own copy."
        />
      ) : null}

      <Section title="Awaiting Payment" footer={rows.length ? 'Tap an invoice to record what arrived. CoachPulse takes no cards — this records money paid to you directly.' : undefined}>
        {rows.length === 0 ? (
          <EmptyState icon="checkCircle" hue="green" title="Nothing outstanding" message="Every issued invoice has been paid." />
        ) : rows.map((inv, i) => {
          const balance = inv.totalMinor - inv.paidMinor;
          const label = dueLabel(inv.dueDate);
          const late = label.endsWith('overdue');
          return (
            <ListRow
              key={inv.id}
              first={i === 0}
              leading={<Avatar name={inv.clientName} size={36} />}
              title={inv.clientName}
              subtitle={[inv.number, label].filter(Boolean).join(' · ')}
              trailing={
                <VStack gap={2} align="flex-end">
                  <T variant="body" weight="600">{money(balance, inv.currency || currency)}</T>
                  {late ? <Tag label="Overdue" hue="red" /> : inv.paidMinor > 0 ? <Tag label="Part paid" hue="orange" /> : null}
                </VStack>
              }
              onPress={() => router.push({ pathname: '/payment', params: { invoice: inv.id } })}
            />
          );
        })}
      </Section>

      {(renewals.data ?? []).length > 0 ? (
        <Section title="Renewals Due" footer="Clients at or under their last two sessions. Renewal is the business — this is where it starts.">
          {(renewals.data ?? []).map((c, i) => (
            <ListRow
              key={c.id}
              first={i === 0}
              leading={<Avatar name={c.fullName} size={36} />}
              title={c.fullName}
              subtitle={[
                c.creditsRemaining < 0 ? 'Overdrawn' : c.creditsRemaining === 0 ? 'Out of sessions' : `${c.creditsRemaining} left`,
                c.lastSessionOn ? `last in ${shortDate(c.lastSessionOn)}` : null,
              ].filter(Boolean).join(' · ')}
              value="Renew"
              valueTone="tint"
              onPress={() => router.push({ pathname: '/sell-package', params: { client: c.id, name: c.fullName } })}
            />
          ))}
        </Section>
      ) : null}

      <Section title="Recent Payments">
        {(payments.data ?? []).length === 0 ? (
          <View style={{ padding: space.lg }}><T variant="subhead" tone="label2">No payments recorded yet.</T></View>
        ) : (payments.data ?? []).map((p, i) => (
          <ListRow
            key={p.id}
            first={i === 0}
            icon={p.instrument === 'cash' ? 'money' : p.instrument === 'bank_transfer' ? 'bank' : 'wallet'}
            hue="green"
            title={p.clientName || 'Payment'}
            subtitle={[INSTRUMENT_LABEL[p.instrument] ?? p.instrument, shortDate(p.receivedOn), p.reference].filter(Boolean).join(' · ')}
            value={money(p.amountMinor, p.currency || currency)}
            valueTone="label"
          />
        ))}
      </Section>
      <Spacer />
    </Page>
  );
}
