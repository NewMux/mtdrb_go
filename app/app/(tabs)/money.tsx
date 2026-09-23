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
import { Pressable, RefreshControl, ScrollView, View } from 'react-native';
import { useFocusEffect } from 'expo-router';

import { recordPayment } from '@/features/actions';
import { outstandingInvoices, type InvoiceSummary } from '@/features/queries';
import type { PaymentInstrument } from '@/api/types';
import { useApp, useQuery } from '@/state/app';
import {
  Body, Button, Caption, Card, Empty, Field, Label, Metric, NumberField,
  Row, Screen, SegmentedChoice, Spacer, Title,
} from '@/ui/components';
import { SyncBadge } from '@/ui/sync-badge';
import { amountText, dueLabel, money, parseMoney } from '@/ui/format';
import { colors, space } from '@/ui/theme';

const INSTRUMENTS: readonly { value: PaymentInstrument; label: string }[] = [
  { value: 'bank_transfer', label: 'Bank' },
  { value: 'cash', label: 'Cash' },
  { value: 'digital_wallet', label: 'Wallet' },
  { value: 'cheque', label: 'Cheque' },
];

export default function MoneyScreen() {
  const { db, account, touch, syncNow, sync } = useApp();

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
    <Screen>
      <ScrollView
        contentContainerStyle={{ padding: space.lg, paddingTop: space.xl, paddingBottom: 176 }}
        keyboardShouldPersistTaps="handled"
        refreshControl={
          <RefreshControl
            refreshing={sync.running}
            onRefresh={() => { void syncNow(); }}
            tintColor={colors.inkMuted}
          />
        }
      >
        <Row style={{ justifyContent: 'space-between' }}>
          <View>
            <Title>Money</Title>
            <Caption>{rows.length} invoice{rows.length === 1 ? '' : 's'} outstanding</Caption>
          </View>
          <SyncBadge />
        </Row>

        <Spacer size={space.lg} />
        <Card tone="accent">
          {totals.length === 0 ? (
            <Metric value="0.00" unit={fallbackCurrency} label="owed to you" tone="onAccent" />
          ) : (
            totals.map(([currency, minor]) => (
              <View key={currency} style={{ marginBottom: space.sm }}>
                <Metric
                  value={money(minor, currency).replace(` ${currency}`, '')}
                  unit={currency}
                  label="owed to you"
                  tone="onAccent"
                />
              </View>
            ))
          )}
        </Card>

        <Spacer size={space.xl} />
        <Label>Awaiting payment</Label>
        <Spacer />

        {rows.length === 0 ? (
          <Empty
            title="Nothing outstanding"
            detail={invoices.loading ? 'Loading…' : 'Every issued invoice has been settled.'}
          />
        ) : (
          rows.map((invoice) => {
            const balance = invoice.totalMinor - invoice.paidMinor;
            const currency = invoice.currency || fallbackCurrency;
            const label = dueLabel(invoice.dueDate);
            const late = label.endsWith('overdue');

            return (
              <Pressable
                key={invoice.id}
                accessibilityRole="button"
                accessibilityLabel={`${invoice.clientName}, ${money(balance, currency)} outstanding`}
                onPress={() => expand(invoice)}
                style={{ marginBottom: space.md }}
              >
              <Card>
                <Row style={{ justifyContent: 'space-between', alignItems: 'flex-start' }}>
                  <View style={{ flex: 1 }}>
                    <Body>{invoice.clientName}</Body>
                    <Caption tone={late ? 'danger' : 'muted'}>
                      {[invoice.number, label].filter(Boolean).join(' · ') || 'issued'}
                    </Caption>
                    {recorded.has(invoice.id) ? (
                      <Caption tone="success">Payment recorded — waiting on the server</Caption>
                    ) : null}
                  </View>
                  <View style={{ alignItems: 'flex-end' }}>
                    <Body>{money(balance, currency)}</Body>
                    {invoice.paidMinor > 0 ? (
                      <Caption>{money(invoice.paidMinor, currency)} paid</Caption>
                    ) : null}
                  </View>
                </Row>

                {open === invoice.id ? (
                  <>
                    <Spacer />
                    <Row>
                      <NumberField label={`Amount (${currency})`} value={amount} onChangeText={setAmount} />
                    </Row>
                    <Spacer size={space.sm} />
                    <SegmentedChoice options={INSTRUMENTS} value={instrument} onChange={setInstrument} />
                    <Spacer size={space.sm} />
                    <Field
                      label="Reference"
                      value={reference}
                      onChangeText={setReference}
                      placeholder="Bank reference, envelope, cheque number"
                    />
                    <Spacer size={space.sm} />
                    <Caption>
                      Received money moves between your accounts. It is not income —
                      that was earned when the sessions were delivered.
                    </Caption>
                    <Spacer size={space.sm} />
                    <Button
                      label="Record payment"
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
      </ScrollView>
    </Screen>
  );
}
