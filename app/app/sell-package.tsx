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

import { describeError } from '@/api/describe';
import type { Invoice, ShareLink } from '@/api/types';
import { sellPackage } from '@/features/billing';
import { expiryFor, listOffers, type LocalOffer } from '@/features/catalog';
import { useT } from '@/i18n';
import { useApp, useQuery } from '@/state/app';
import {
  Banner, Body, Button, Caption, Card, Field, Heading, Label, Metric,
  NumberField, Row, Screen, SegmentedChoice, Spacer, Title,
} from '@/ui/components';
import { amountText, parseMoney, parseReps } from '@/ui/format';
import { space } from '@/ui/theme';

export default function SellPackageScreen() {
  const { client, name } = useLocalSearchParams<{ client: string; name?: string }>();
  const { api, account, sync, syncNow, touch } = useApp();
  const router = useRouter();
  const i18n = useT();
  const { t, money } = i18n;

  const currency = account?.currency ?? 'EUR';

  const [description, setDescription] = useState(() => t('sellPackage.defaultDescription'));
  const [credits, setCredits] = useState('10');
  const [price, setPrice] = useState('50.00');
  const [priceMode, setPriceMode] = useState<'each' | 'pack'>('each');
  const [packPrice, setPackPrice] = useState('');
  const [offerId, setOfferId] = useState<string | null>(null);
  const [expiresOn, setExpiresOn] = useState<string | null>(null);
  // Packs from the price list: sold by the session, so they have a count.
  const offers = (useQuery((db) => listOffers(db)).data ?? []).filter((o) => o.credits !== null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [issued, setIssued] = useState<{ invoice: Invoice; share: ShareLink | null } | null>(null);

  const creditCount = parseReps(credits);
  const unitPrice = parseMoney(price, currency);
  const packTotal = parseMoney(packPrice, currency);
  const total = priceMode === 'pack'
    ? packTotal
    : creditCount !== null && unitPrice !== null ? creditCount * unitPrice : null;

  const pick = (offer: LocalOffer) => {
    setOfferId(offer.id);
    setDescription(offer.name);
    setCredits(String(offer.credits ?? ''));
    setPriceMode('pack');
    setPackPrice(amountText(offer.priceMinor, currency));
    setExpiresOn(expiryFor(offer));
  };

  const issue = async () => {
    if (creditCount === null || creditCount < 1 || total === null) return;
    setBusy(true);
    setError(null);
    try {
      const result = await sellPackage(api, {
        clientId: client,
        description: description.trim(),
        credits: creditCount,
        unitPriceMinor: unitPrice ?? 0,
        ...(priceMode === 'pack' && packTotal !== null ? { packPriceMinor: packTotal } : {}),
        expiresOn,
        currency,
      });
      setIssued(result);
      // The new package and invoice are the server's rows; pulling them is how
      // they reach this device's mirror.
      await syncNow();
      touch();
    } catch (cause) {
      setError(describeError(cause, i18n, 'sellPackage.offlineError'));
    } finally {
      setBusy(false);
    }
  };

  const shareLink = async () => {
    if (!issued?.share) return;
    await Share.share({
      message: t('sellPackage.shareMessage', { number: issued.invoice.number ?? '', url: issued.share.url }),
      url: issued.share.url,
    });
  };

  if (issued) {
    return (
      <Screen>
        <ScrollView contentContainerStyle={{ padding: space.lg }}>
          <Title>{t('sellPackage.issuedTitle')}</Title>
          <Spacer size={space.xs} />
          <Body muted>
            {issued.invoice.number
              ? t('sellPackage.invoiceNumbered', { number: issued.invoice.number })
              : t('sellPackage.invoiceIssued')}{' '}
            {t('sellPackage.bookedAsOwed', { name: name ?? t('sellPackage.theClient') })}
          </Body>

          <Spacer size={space.lg} />
          <Card>
            <Metric
              value={money(issued.invoice.total_minor, issued.invoice.currency || currency)}
              label={t('sellPackage.toCollect')}
            />
          </Card>

          <Spacer size={space.lg} />
          {issued.share ? (
            <>
              <Button label={t('sellPackage.sendLink')} icon="share" tone="primary" onPress={() => { void shareLink(); }} />
              <Spacer size={space.sm} />
              <Caption>{t('sellPackage.linkWarning')}</Caption>
            </>
          ) : (
            <Banner
              message={t('sellPackage.noLink')}
              tone="muted"
            />
          )}

          <Spacer size={space.xl} />
          <Button label={t('common.done')} onPress={() => router.back()} />
        </ScrollView>
      </Screen>
    );
  }

  return (
    <Screen>
      <ScrollView contentContainerStyle={{ padding: space.lg }} keyboardShouldPersistTaps="handled">
        <Title>{t('sellPackage.title')}</Title>
        <Caption>{name ?? t('sellPackage.clientFallback')}</Caption>

        <Spacer size={space.lg} />
        {sync.offline ? (
          <>
            <Banner
              message={t('sellPackage.offline')}
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

        {offers.length > 0 ? (
          <>
            <Label>{t('sellPackage.fromPriceList')}</Label>
            <Spacer size={space.sm} />
            <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: space.sm }}>
              {offers.map((offer) => (
                <Button
                  key={offer.id}
                  compact
                  tone={offer.id === offerId ? 'primary' : 'default'}
                  label={`${offer.name} · ${money(offer.priceMinor, currency)}`}
                  onPress={() => pick(offer)}
                />
              ))}
            </View>
            <Spacer size={space.lg} />
          </>
        ) : null}

        <Field label={t('sellPackage.description')} value={description} onChangeText={(v) => { setDescription(v); setOfferId(null); }} />
        <Spacer />
        <SegmentedChoice
          options={[
            { value: 'each', label: t('sellPackage.perSession') },
            { value: 'pack', label: t('sellPackage.wholePack') },
          ] as const}
          value={priceMode}
          onChange={setPriceMode}
        />
        <Spacer />
        <Row>
          <NumberField label={t('sellPackage.sessions')} value={credits} onChangeText={setCredits} placeholder="10" />
          {priceMode === 'each' ? (
            <NumberField label={t('sellPackage.priceEach', { currency })} value={price} onChangeText={setPrice} placeholder="50.00" />
          ) : (
            <NumberField label={t('sellPackage.packPrice', { currency })} value={packPrice} onChangeText={setPackPrice} placeholder="3500.00" />
          )}
        </Row>
        {expiresOn ? (
          <>
            <Spacer size={space.sm} />
            <Caption>{t('sellPackage.expiresOn', { date: i18n.date(expiresOn, 'long') })}</Caption>
          </>
        ) : null}

        <Spacer size={space.lg} />
        <Card>
          <Row style={{ justifyContent: 'space-between', alignItems: 'flex-end', gap: space.md }}>
            <View style={{ flex: 1 }}>
              <Heading>{t('sellPackage.total')}</Heading>
              <Caption>{t('sellPackage.totalExplainer')}</Caption>
            </View>
            <Body>{total === null ? '—' : money(total, currency)}</Body>
          </Row>
        </Card>

        <Spacer size={space.xl} />
        <Button
          label={t('sellPackage.issue')}
          tone="primary"
          onPress={() => { void issue(); }}
          disabled={total === null || creditCount === null || creditCount < 1 || description.trim() === ''}
          busy={busy}
        />
      </ScrollView>
    </Screen>
  );
}
