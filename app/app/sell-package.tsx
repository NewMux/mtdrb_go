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
import { Share, View } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';

import { ApiError, NetworkError } from '@/api/client';
import type { Invoice, ShareLink } from '@/api/types';
import { sellPackage } from '@/features/billing';
import { useApp } from '@/state/app';
import {
  Avatar, Banner, Body, Button, Card, FieldRow, HStack, IconBubble, Section, Segmented, T, VStack,
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
      <Body>
        <VStack align="center" gap={space.sm} style={{ paddingHorizontal: space.xl, paddingTop: space.lg }}>
          <IconBubble icon="checkCircle" hue="green" size={72} />
          <T variant="title2" align="center">Invoice Issued</T>
          <T variant="subhead" tone="label2" align="center">
            {issued.invoice.number ? `${issued.invoice.number} · ` : ''}The sessions are on {name ?? 'the client'}&apos;s account. The money is booked as owed — it&apos;s earned as each session is delivered.
          </T>
        </VStack>

        <View style={{ paddingHorizontal: space.lg, marginTop: space.xl }}>
          <Card>
            <T variant="footnote" tone="label2" weight="600">TO COLLECT</T>
            <T variant="metric">{money(issued.invoice.total_minor, issued.invoice.currency || currency)}</T>
          </Card>
        </View>

        <View style={{ paddingHorizontal: space.lg, marginTop: space.xl, gap: space.sm }}>
          {issued.share ? (
            <>
              <Button label="Send the Link" icon="share" variant="filled" full onPress={() => { void shareLink(); }} />
              <T variant="footnote" tone="label2" align="center">
                The link shows the invoice and your payment details. Anyone holding it can view it, so send it to the client only.
              </T>
            </>
          ) : (
            <Banner tone="gray" message="The invoice is issued, but the share link could not be created. You can make one from the invoice later." />
          )}
          <Button label="Done" variant="gray" full onPress={() => router.back()} />
        </View>
      </Body>
    );
  }

  return (
    <Body keyboard>
      <View style={{ paddingHorizontal: space.lg, gap: space.md }}>
        <HStack gap={space.md}>
          <Avatar name={name ?? 'Client'} size={44} />
          <VStack gap={0}>
            <T variant="headline">{name ?? 'Client'}</T>
            <T variant="footnote" tone="label2">New package</T>
          </VStack>
        </HStack>
        {sync.offline ? (
          <Banner tone="orange" icon="offline" message="No connection. Invoice numbers must be gap-free, so the server issues them — this one has to wait for signal." />
        ) : null}
        {error ? <Banner tone="red" message={error} /> : null}
      </View>

      <Section title="Pack Size" plain>
        <Segmented
          options={PRESETS.map((p) => ({ value: String(p), label: `${p} sessions` }))}
          value={PRESETS.includes(Number(credits)) ? credits : '__custom'}
          onChange={(v) => { setCredits(v); setDescription(`${v}-session personal training package`); }}
        />
      </Section>

      <Section>
        <FieldRow first label="Description" value={description} onChangeText={setDescription} />
        <FieldRow label="Sessions" value={credits} onChangeText={setCredits} keyboardType="number-pad" />
        <FieldRow label="Price each" value={price} onChangeText={setPrice} keyboardType="decimal-pad" suffix={currency} />
      </Section>

      <View style={{ paddingHorizontal: space.lg, marginTop: space.xl }}>
        <Card>
          <HStack justify="space-between" align="flex-end">
            <VStack gap={2} style={{ flex: 1 }}>
              <T variant="headline">Total</T>
              <T variant="footnote" tone="label2">Held as prepaid until each session is delivered.</T>
            </VStack>
            <T variant="metricSmall">{total === null ? '—' : money(total, currency)}</T>
          </HStack>
        </Card>
        <View style={{ height: space.lg }} />
        <Button
          label="Issue Invoice"
          icon="invoice"
          variant="filled"
          full
          onPress={() => { void issue(); }}
          disabled={total === null || creditCount === null || creditCount < 1 || description.trim() === ''}
          busy={busy}
        />
      </View>
    </Body>
  );
}

const PRESETS = [5, 10, 20];

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
