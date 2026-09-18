/**
 * Selling a package — Journey B, start to share.
 *
 * Issue the invoice, then hand the client a link over whatever they already
 * use. CoachPulse takes no card: the link carries the trainer's own bank
 * details, the client pays into them, and the trainer marks it paid.
 *
 * This is the one screen that needs a connection, and it says so before the
 * trainer starts typing rather than after they tap Issue.
 */

import React, { useState } from 'react';
import { ScrollView, Share, View } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';

import { ApiError, NetworkError } from '@/api/client';
import type { Invoice, ShareLink } from '@/api/types';
import { sellPackage } from '@/features/billing';
import { useApp } from '@/state/app';
import {
  Banner, Body, Button, Caption, Card, Field, Heading, Metric,
  NumberField, Row, Screen, Spacer, Title,
} from '@/ui/components';
import { money, parseMoney, parseReps } from '@/ui/format';
import { space } from '@/ui/theme';

export default function SellPackageScreen() {
  const { client, name } = useLocalSearchParams<{ client: string; name?: string }>();
  const { api, account, sync, syncNow, touch } = useApp();
  const router = useRouter();

  const currency = account?.currency ?? 'EUR';

  const [description, setDescription] = useState('10-session personal training package');
  const [credits, setCredits] = useState('10');
  const [price, setPrice] = useState('50.00');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [issued, setIssued] = useState<{ invoice: Invoice; share: ShareLink | null } | null>(null);

  const creditCount = parseReps(credits);
  const unitPrice = parseMoney(price);
  const total = creditCount !== null && unitPrice !== null ? creditCount * unitPrice : null;

  const issue = async () => {
    if (creditCount === null || unitPrice === null || creditCount < 1) return;
    setBusy(true);
    setError(null);
    try {
      const result = await sellPackage(api, {
        clientId: client,
        description: description.trim(),
        credits: creditCount,
        unitPriceMinor: unitPrice,
        currency,
      });
      setIssued(result);
      // The new package and invoice are the server's rows; pulling them is how
      // they reach this device's mirror.
      await syncNow();
      touch();
    } catch (cause) {
      setError(describe(cause));
    } finally {
      setBusy(false);
    }
  };

  const shareLink = async () => {
    if (!issued?.share) return;
    await Share.share({
      message: `Here's your invoice ${issued.invoice.number ?? ''} — it has my bank details on it. ${issued.share.url}`.trim(),
      url: issued.share.url,
    });
  };

  if (issued) {
    return (
      <Screen>
        <ScrollView contentContainerStyle={{ padding: space.lg }}>
          <Title>Issued</Title>
          <Spacer size={space.xs} />
          <Body muted>
            {issued.invoice.number
              ? `Invoice ${issued.invoice.number}.`
              : 'Invoice issued.'}{' '}
            The credits are on {name ?? 'the client'}&apos;s account and the money is booked as owed,
            not yet earned.
          </Body>

          <Spacer size={space.lg} />
          <Card>
            <Metric
              value={money(issued.invoice.total_minor, issued.invoice.currency || currency)}
              label="to collect"
            />
          </Card>

          <Spacer size={space.lg} />
          {issued.share ? (
            <>
              <Button label="Send the link" tone="primary" onPress={() => { void shareLink(); }} />
              <Spacer size={space.sm} />
              <Caption>
                The link shows the invoice and your payment instructions. Anyone holding it can
                view it, so send it to the client and nobody else.
              </Caption>
            </>
          ) : (
            <Banner
              message="The invoice is issued, but the share link could not be minted. You can create one from the invoice later."
              tone="muted"
            />
          )}

          <Spacer size={space.xl} />
          <Button label="Done" onPress={() => router.back()} />
        </ScrollView>
      </Screen>
    );
  }

  return (
    <Screen>
      <ScrollView contentContainerStyle={{ padding: space.lg }} keyboardShouldPersistTaps="handled">
        <Title>Sell a package</Title>
        <Caption>{name ?? 'Client'}</Caption>

        <Spacer size={space.lg} />
        {sync.offline ? (
          <>
            <Banner
              message="No connection. Invoice numbers have to be gap-free, so they are issued by the server — this one has to wait for signal."
              tone="warning"
            />
            <Spacer />
          </>
        ) : null}
        {error ? (
          <>
            <Banner message={error} tone="danger" />
            <Spacer />
          </>
        ) : null}

        <Field label="Description" value={description} onChangeText={setDescription} />
        <Spacer />
        <Row>
          <NumberField label="Sessions" value={credits} onChangeText={setCredits} placeholder="10" />
          <NumberField label={`Price each (${currency})`} value={price} onChangeText={setPrice} placeholder="50.00" />
        </Row>

        <Spacer size={space.lg} />
        <Card>
          <Row style={{ justifyContent: 'space-between', alignItems: 'flex-end' }}>
            <View>
              <Heading>Total</Heading>
              <Caption>Credited to Deferred Revenue — earned one session at a time.</Caption>
            </View>
            <Body>{total === null ? '—' : money(total, currency)}</Body>
          </Row>
        </Card>

        <Spacer size={space.xl} />
        <Button
          label="Issue invoice"
          tone="primary"
          onPress={() => { void issue(); }}
          disabled={total === null || creditCount === null || creditCount < 1 || description.trim() === ''}
          busy={busy}
        />
      </ScrollView>
    </Screen>
  );
}

function describe(cause: unknown): string {
  if (cause instanceof NetworkError) {
    return 'No connection. An invoice number has to come from the server, so this one cannot be issued yet.';
  }
  if (cause instanceof ApiError) {
    const fields = cause.fields ? Object.values(cause.fields) : [];
    return fields[0] ?? cause.message;
  }
  return cause instanceof Error ? cause.message : 'Something went wrong.';
}
