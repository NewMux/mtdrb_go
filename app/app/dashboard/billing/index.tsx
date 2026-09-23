/**
 * Money — Journey B's second half.
 *
 * The chase list, oldest first, and the two taps that close one: how much
 * arrived, and how. CoachPulse processes no cards; this records that money
 * turned up somewhere else.
 *
 * Marking a payment does **not** flip the invoice to settled on this device.
 * Whether it settles depends on what else has been paid, possibly from another
 * device, and on the server's refusal to accept an overpayment. Showing
 * "settled" and then discovering it was refused is exactly the error that
 * costs a trainer money, so the row says "recorded" until the server agrees.
 */

import React, { useCallback, useMemo, useState } from 'react';
import { Pressable, View } from 'react-native';
import { useFocusEffect } from 'expo-router';

import { recordPayment } from '@/features/actions';
import { outstandingInvoices, type InvoiceSummary } from '@/features/queries';
import type { PaymentInstrument } from '@/api/types';
import { useT } from '@/i18n';
import { useApp, useQuery } from '@/state/app';
import { Body, Button, Caption, Card, Empty, Field, Label, Metric, NumberField, Row, SegmentedChoice, Spacer } from '@/ui/components';
import { Page } from '@/ui/page';
import { amountText, parseMoney } from '@/ui/format';
import { daysUntil } from '@/i18n';
import { space } from '@/ui/theme';

const INSTRUMENTS: readonly PaymentInstrument[] = ['bank_transfer', 'cash', 'digital_wallet', 'cheque'];

export default function MoneyScreen() {
  const { db, account, touch, syncNow, sync } = useApp();
  const i18n = useT();
  const { t, money, amount: formatAmount } = i18n;
  const instruments = INSTRUMENTS.map((value) => ({ value, label: t(`instruments.${value}`) }));

  const [open, setOpen] = useState<string | null>(null);
  const [amount, setAmount] = useState('');
  const [instrument, setInstrument] = useState<PaymentInstrument>('bank_transfer');
  const [reference, setReference] = useState('');
  const [recorded, setRecorded] = useState<Set<string>>(new Set());

  const invoices = useQuery((database) => outstandingInvoices(database), []);
  useFocusEffect(useCallback(() => { invoices.reload(); }, [invoices.reload]));

  const rows = invoices.data ?? [];
  const fallbackCurrency = account?.currency ?? 'EUR';

  /** Totals per currency — a trainer with a foreign client still gets one honest number each. */
  const totals = useMemo(() => {
    const byCurrency = new Map<string, number>();
    for (const invoice of rows) {
      const currency = invoice.currency || fallbackCurrency;
      byCurrency.set(currency, (byCurrency.get(currency) ?? 0) + invoice.totalMinor - invoice.paidMinor);
    }
    return [...byCurrency];
  }, [rows, fallbackCurrency]);

  const expand = (invoice: InvoiceSummary) => {
    if (open === invoice.id) {
      setOpen(null);
      return;
    }
    setOpen(invoice.id);
    // Prefilled with the balance, because that is what a client almost always
    // pays — and typing it again is a chance to mistype it.
    const balance = invoice.totalMinor - invoice.paidMinor;
    setAmount(amountText(balance, invoice.currency || fallbackCurrency));
    setReference('');
  };

  const record = async (invoice: InvoiceSummary) => {
    if (!db) return;
    const minor = parseMoney(amount, invoice.currency || fallbackCurrency);
    if (minor === null || minor <= 0) return;

    await recordPayment(db, invoice.id, minor, instrument, {
      currency: invoice.currency || fallbackCurrency,
      reference: reference.trim(),
    });
    setRecorded(new Set([...recorded, invoice.id]));
    setOpen(null);
    touch();
    void syncNow();
  };

  return (
    <Page
      title={t('money.title')}
      subtitle={t('money.outstanding', { count: rows.length })}
      refreshing={sync.running}
      onRefresh={() => { void syncNow(); }}
    >
        <Spacer size={space.lg} />
        <Card tone="accent">
          {totals.length === 0 ? (
            <Metric value={formatAmount(0, fallbackCurrency)} unit={fallbackCurrency} label={t('money.owedToYou')} tone="onAccent" />
          ) : (
            totals.map(([currency, minor]) => (
              <View key={currency} style={{ marginBottom: space.sm }}>
                <Metric
                  value={formatAmount(minor, currency)}
                  unit={currency}
                  label={t('money.owedToYou')}
                  tone="onAccent"
                />
              </View>
            ))
          )}
        </Card>

        <Spacer size={space.xl} />
        <Label>{t('money.awaiting')}</Label>
        <Spacer />

        {rows.length === 0 ? (
          <Empty
            icon="money"
            title={t('money.nothingOutstanding')}
            detail={invoices.loading ? t('common.loading') : t('money.allSettled')}
          />
        ) : (
          rows.map((invoice) => {
            const balance = invoice.totalMinor - invoice.paidMinor;
            const currency = invoice.currency || fallbackCurrency;
            const label = i18n.due(invoice.dueDate);
            const late = invoice.dueDate !== null && daysUntil(invoice.dueDate) < 0;

            return (
              <Pressable
                key={invoice.id}
                accessibilityRole="button"
                accessibilityLabel={t('money.outstandingA11y', { name: invoice.clientName, amount: money(balance, currency) })}
                onPress={() => expand(invoice)}
                style={{ marginBottom: space.md }}
              >
              <Card>
                <Row style={{ justifyContent: 'space-between', alignItems: 'flex-start' }}>
                  <View style={{ flex: 1 }}>
                    <Body>{invoice.clientName}</Body>
                    <Caption tone={late ? 'danger' : 'muted'}>
                      {[invoice.number, label].filter(Boolean).join(' · ') || t('money.issued')}
                    </Caption>
                    {recorded.has(invoice.id) ? (
                      <Caption tone="success">{t('money.recorded')}</Caption>
                    ) : null}
                  </View>
                  <View style={{ alignItems: 'flex-end' }}>
                    <Body>{money(balance, currency)}</Body>
                    {invoice.paidMinor > 0 ? (
                      <Caption>{t('money.paid', { amount: money(invoice.paidMinor, currency) })}</Caption>
                    ) : null}
                  </View>
                </Row>

                {open === invoice.id ? (
                  <>
                    <Spacer />
                    <Row>
                      <NumberField label={t('money.amount', { currency })} value={amount} onChangeText={setAmount} />
                    </Row>
                    <Spacer size={space.sm} />
                    <SegmentedChoice options={instruments} value={instrument} onChange={setInstrument} />
                    <Spacer size={space.sm} />
                    <Field
                      label={t('money.reference')}
                      value={reference}
                      onChangeText={setReference}
                      placeholder={t('money.referencePlaceholder')}
                    />
                    <Spacer size={space.sm} />
                    <Caption>{t('money.notIncome')}</Caption>
                    <Spacer size={space.sm} />
                    <Button
                      label={t('money.recordPayment')}
                      tone="primary"
                      onPress={() => { void record(invoice); }}
                      disabled={(parseMoney(amount, currency) ?? 0) <= 0}
                    />
                  </>
                ) : null}
              </Card>
              </Pressable>
            );
          })
        )}
    </Page>
  );
}
