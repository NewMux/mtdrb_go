/**
 * Recording money that arrived — Journey B's second half.
 *
 * Prefilled with the balance, because that is what a client almost always
 * pays, and typing it again is a chance to mistype it. CoachPulse processes
 * no cards; this records that money turned up somewhere else.
 *
 * The invoice is not marked settled here: whether it settles is the server's
 * call (see the Business tab). The sheet says so rather than implying it.
 */

import React, { useEffect, useState } from 'react';
import { View } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';

import { recordPayment } from '@/features/actions';
import { invoiceById } from '@/features/queries';
import type { PaymentInstrument } from '@/api/types';
import { useApp, useQuery } from '@/state/app';
import { Body, Button, Card, FieldRow, HStack, Section, Segmented, T, tap, VStack } from '@/ui/components';
import { dueLabel, money, parseMoney } from '@/ui/format';
import { space } from '@/ui/theme';

const INSTRUMENTS = [
  { value: 'bank_transfer', label: 'Bank' },
  { value: 'cash', label: 'Cash' },
  { value: 'digital_wallet', label: 'Wallet' },
  { value: 'cheque', label: 'Cheque' },
] as const;

export default function PaymentScreen() {
  const { invoice: invoiceId } = useLocalSearchParams<{ invoice: string }>();
  const { db, account, touch, syncNow } = useApp();
  const router = useRouter();
  const invoice = useQuery((d) => invoiceById(d, invoiceId), [invoiceId]);

  const [amount, setAmount] = useState('');
  const [instrument, setInstrument] = useState<PaymentInstrument>('bank_transfer');
  const [reference, setReference] = useState('');
  const [busy, setBusy] = useState(false);

  const inv = invoice.data;
  const currency = inv?.currency || account?.currency || 'EUR';
  const balance = inv ? inv.totalMinor - inv.paidMinor : 0;

  useEffect(() => { if (inv) setAmount((balance / 100).toFixed(2)); }, [inv?.id]); // eslint-disable-line react-hooks/exhaustive-deps

  const minor = parseMoney(amount);

  const save = async () => {
    if (!db || !inv || minor === null || minor <= 0) return;
    setBusy(true);
    await recordPayment(db, inv.id, minor, instrument, { currency, reference: reference.trim() });
    tap('success');
    touch();
    void syncNow();
    setBusy(false);
    router.back();
  };

  if (!inv) return <Body><T variant="body" tone="label2" align="center">{invoice.loading ? 'Loading…' : 'Invoice not found.'}</T></Body>;

  return (
    <Body keyboard>
      <View style={{ paddingHorizontal: space.lg }}>
        <Card>
          <HStack justify="space-between" align="flex-start">
            <VStack gap={2}>
              <T variant="headline">{inv.clientName}</T>
              <T variant="subhead" tone="label2">{[inv.number, dueLabel(inv.dueDate)].filter(Boolean).join(' · ')}</T>
            </VStack>
            <VStack gap={0} align="flex-end">
              <T variant="metricSmall">{money(balance, currency)}</T>
              <T variant="footnote" tone="label2">outstanding</T>
            </VStack>
          </HStack>
        </Card>
      </View>

      <Section title="Received">
        <FieldRow first label="Amount" value={amount} onChangeText={setAmount} keyboardType="decimal-pad" suffix={currency} />
        <FieldRow label="Reference" value={reference} onChangeText={setReference} placeholder="Bank ref, envelope…" />
      </Section>

      <Section title="How" plain footer="Received money moves between your accounts. It isn't income — that was earned when the sessions were delivered.">
        <Segmented options={INSTRUMENTS} value={instrument} onChange={setInstrument} />
      </Section>

      <View style={{ paddingHorizontal: space.lg, marginTop: space.xl }}>
        <Button
          label={minor && minor > 0 ? `Record ${money(minor, currency)}` : 'Record Payment'}
          variant="filled" full busy={busy}
          disabled={minor === null || minor <= 0}
          onPress={() => { void save(); }}
        />
        {minor !== null && minor > balance ? (
          <T variant="footnote" tone="orange" align="center" style={{ marginTop: space.sm }}>
            That is more than is owed — the server will refuse an overpayment.
          </T>
        ) : null}
      </View>
    </Body>
  );
}
