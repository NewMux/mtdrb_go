/**
 * Setting up a new practice: five questions before the first session.
 *
 * Only the ones that shape everything after them — where the practice is
 * (its week, its VAT, its clock), whether it charges VAT, where it trains,
 * what it sells, and who its first client is. Each step is saved as it is
 * answered, so closing the app halfway loses nothing and reopening it picks
 * up where the server says things stand. Anything skippable is skippable:
 * a trainer with a client waiting should be able to get to Today in five taps.
 *
 * The owner lands here until they finish; AuthGate sends them. Everything
 * here can be changed later under Settings, Locations and Packages.
 */

import React, { useEffect, useMemo, useState } from 'react';
import { ScrollView, View } from 'react-native';
import { useRouter } from 'expo-router';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import type { Settings } from '@/api/account';
import { describeError } from '@/api/describe';
import { createClient } from '@/features/actions';
import { useT, type Locale, type TKey } from '@/i18n';
import { useApp } from '@/state/app';
import { usePreferences } from '@/state/preferences';
import {
  Banner, Body, Button, Caption, Card, Field, Label, NumberField, Progress, Row, Screen,
  SegmentedChoice, Spacer, Title, Toggle,
} from '@/ui/components';
import { parseMoney, parseReps } from '@/ui/format';
import { Select } from '@/ui/pickers';
import { space } from '@/ui/theme';

const COUNTRIES = ['AE', 'SA', 'BH', 'OM', 'QA', 'KW', 'EG', 'JO', 'GB', 'US'] as const;
/** Where a device's clock says it is, as a first guess at the country. */
const ZONE_COUNTRY: Record<string, string> = {
  'Asia/Dubai': 'AE', 'Asia/Riyadh': 'SA', 'Asia/Bahrain': 'BH', 'Asia/Muscat': 'OM',
  'Asia/Qatar': 'QA', 'Asia/Kuwait': 'KW', 'Africa/Cairo': 'EG', 'Asia/Amman': 'JO',
  'Europe/London': 'GB',
};
const STEPS = ['practice', 'vat', 'location', 'offer', 'client'] as const;
type Step = (typeof STEPS)[number];

function deviceZone(): string | undefined {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || undefined;
  } catch {
    return undefined;
  }
}

/** Basis points as the percentage people type: 500 is "5". */
function percent(bp: number): string {
  return String(bp / 100);
}

export default function OnboardingScreen() {
  const i18n = useT();
  const { t } = i18n;
  const { api, db, account, syncNow, touch } = useApp();
  const preferences = usePreferences();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const currency = account?.currency ?? 'AED';

  const [step, setStep] = useState<Step>('practice');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [settings, setSettings] = useState<Settings | null>(null);

  // Practice
  const [business, setBusiness] = useState('');
  const [country, setCountry] = useState<string | null>(ZONE_COUNTRY[deviceZone() ?? ''] ?? null);
  // VAT
  const [registered, setRegistered] = useState<'yes' | 'no'>('no');
  const [trn, setTrn] = useState('');
  const [rate, setRate] = useState('5');
  const [inclusive, setInclusive] = useState(true);
  // First place, first offer, first client
  const [place, setPlace] = useState('');
  const [offerName, setOfferName] = useState('');
  const [offerSessions, setOfferSessions] = useState('10');
  const [offerPrice, setOfferPrice] = useState('');
  const [offerDays, setOfferDays] = useState('90');
  const [clientName, setClientName] = useState('');
  const [clientPhone, setClientPhone] = useState('');

  useEffect(() => {
    let cancelled = false;
    api.get<Settings>('/v1/settings')
      .then((s) => {
        if (cancelled) return;
        setSettings(s);
        setBusiness(s.business_name);
        if (s.country) setCountry(s.country);
        setRegistered(s.vat_registered ? 'yes' : 'no');
        setTrn(s.trn ?? '');
        setRate(percent(s.vat_rate_bp));
        setInclusive(s.prices_include_vat);
      })
      .catch((cause: unknown) => { if (!cancelled) setError(describeError(cause, i18n)); });
    return () => { cancelled = true; };
    // Once, on arrival.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [api]);

  useEffect(() => { setOfferName(t('onboarding.offerDefault')); setPlace(t('onboarding.placeDefault')); }, [t]);

  const countryOptions = useMemo(
    () => COUNTRIES.map((c) => ({ value: c, label: t(`countries.${c}` as TKey) })),
    [t],
  );
  const index = STEPS.indexOf(step);
  const next = () => setStep(STEPS[Math.min(index + 1, STEPS.length - 1)]!);

  const run = async (work: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    try {
      await work();
    } catch (cause) {
      setError(describeError(cause, i18n));
    } finally {
      setBusy(false);
    }
  };

  const savePractice = () => run(async () => {
    const saved = await api.patch<Settings>('/v1/settings', {
      business_name: business.trim(),
      ...(country ? { country } : {}),
      language: preferences.locale,
      ...(deviceZone() ? { timezone: deviceZone() } : {}),
    });
    setSettings(saved);
    // The country set the VAT rate; show it on the next step.
    setRate(percent(saved.vat_rate_bp));
    next();
  });

  const saveVat = () => run(async () => {
    const bp = Math.round(Number(rate.replace(',', '.')) * 100);
    if (!Number.isFinite(bp)) throw new Error(t('onboarding.rateInvalid'));
    const saved = await api.patch<Settings>('/v1/settings', registered === 'yes'
      ? { vat_registered: true, trn, vat_rate_bp: bp, prices_include_vat: inclusive }
      : { vat_registered: false, prices_include_vat: inclusive });
    setSettings(saved);
    next();
  });

  const savePlace = () => run(async () => {
    await api.post('/v1/locations', { name: place.trim(), kind: 'studio' });
    next();
  });

  const saveOffer = () => run(async () => {
    const price = parseMoney(offerPrice, currency);
    const sessions = parseReps(offerSessions);
    const days = offerDays.trim() === '' ? null : parseReps(offerDays);
    if (price === null || !sessions) throw new Error(t('packages.priceInvalid'));
    await api.post('/v1/package-offers', {
      name: offerName.trim(), kind: 'session_pack', credits: sessions, price_minor: price,
      price_includes_vat: inclusive, ...(days ? { validity_days: days } : {}),
    });
    next();
  });

  const finish = () => run(async () => {
    // Offline-first like any other client: queued now, sent with the rest.
    if (db && clientName.trim() !== '') {
      await createClient(db, clientName.trim(), { phone: clientPhone.trim() || undefined });
    }
    await api.post('/v1/settings/onboarded', {});
    await syncNow();
    touch();
    router.replace('/');
  });

  const skip = () => { setError(null); next(); };

  return (
    <Screen>
      <ScrollView
        contentContainerStyle={{ padding: space.xl, paddingTop: insets.top + space.xl, flexGrow: 1 }}
        keyboardShouldPersistTaps="handled"
      >
        <View style={{ width: '100%', maxWidth: 560, alignSelf: 'center' }}>
          <Caption>{t('onboarding.stepOf', { n: index + 1, total: STEPS.length })}</Caption>
          <Spacer size={space.sm} />
          <Progress value={(index + 1) / STEPS.length} />
          <Spacer size={space.xl} />
          <Title>{t(`onboarding.${step}.title` as TKey)}</Title>
          <Spacer size={space.xs} />
          <Body muted>{t(`onboarding.${step}.body` as TKey)}</Body>
          <Spacer size={space.xl} />

          {error ? <><Banner tone="danger" message={error} /><Spacer /></> : null}

          {step === 'practice' ? (
            <>
              <Field label={t('business.businessName')} value={business} onChangeText={setBusiness} autoCapitalize="words" />
              <Spacer />
              <Select label={t('business.country')} value={country} options={countryOptions} onChange={setCountry} />
              <Spacer />
              <Label>{t('syncScreen.language')}</Label>
              <Spacer size={space.sm} />
              <SegmentedChoice
                options={[{ value: 'en', label: t('prefs.english') }, { value: 'ar', label: t('prefs.arabic') }] as const}
                value={preferences.locale}
                onChange={(l: Locale) => preferences.setLocale(l)}
              />
              {preferences.restartPending ? (
                <><Spacer size={space.sm} /><Banner tone="muted" message={t('prefs.restartBody')} action={{ label: t('prefs.restart'), onPress: preferences.restart }} /></>
              ) : null}
              <Spacer size={space.xl} />
              <Button tone="primary" label={t('onboarding.continue')} onPress={() => { void savePractice(); }} busy={busy} disabled={business.trim() === '' || !settings} />
            </>
          ) : null}

          {step === 'vat' ? (
            <>
              <SegmentedChoice
                options={[{ value: 'no', label: t('onboarding.vat.notRegistered') }, { value: 'yes', label: t('onboarding.vat.registered') }] as const}
                value={registered}
                onChange={setRegistered}
              />
              {registered === 'yes' ? (
                <>
                  <Spacer />
                  <Field label={t('onboarding.vat.trn')} value={trn} onChangeText={setTrn} keyboardType="number-pad" autoCapitalize="none" hint={t('onboarding.vat.trnHint')} />
                  <Spacer />
                  <Row><NumberField label={t('onboarding.vat.rate')} value={rate} onChangeText={setRate} /></Row>
                </>
              ) : null}
              <Spacer />
              <Toggle label={t('packages.includesVat')} hint={t('packages.includesVatHint')} value={inclusive} onChange={setInclusive} />
              <Spacer size={space.xl} />
              <Button tone="primary" label={t('onboarding.continue')} onPress={() => { void saveVat(); }} busy={busy}
                disabled={registered === 'yes' && trn.replace(/\D/g, '').length !== 15} />
            </>
          ) : null}

          {step === 'location' ? (
            <>
              <Field label={t('locations.name')} value={place} onChangeText={setPlace} autoCapitalize="words" />
              <Spacer size={space.xs} />
              <Caption>{t('onboarding.location.later')}</Caption>
              <Spacer size={space.xl} />
              <Button tone="primary" label={t('onboarding.continue')} onPress={() => { void savePlace(); }} busy={busy} disabled={place.trim() === ''} />
              <Spacer size={space.sm} />
              <Button tone="quiet" label={t('onboarding.skip')} onPress={skip} />
            </>
          ) : null}

          {step === 'offer' ? (
            <>
              <Field label={t('packages.name')} value={offerName} onChangeText={setOfferName} />
              <Spacer />
              <Row style={{ gap: space.md }}>
                <NumberField label={t('packages.credits')} value={offerSessions} onChangeText={setOfferSessions} />
                <NumberField label={t('packages.price', { currency })} value={offerPrice} onChangeText={setOfferPrice} />
              </Row>
              <Spacer />
              <Row><NumberField label={t('packages.validity')} value={offerDays} onChangeText={setOfferDays} /></Row>
              <Spacer size={space.xl} />
              <Button tone="primary" label={t('onboarding.continue')} onPress={() => { void saveOffer(); }} busy={busy}
                disabled={offerName.trim() === '' || offerPrice.trim() === ''} />
              <Spacer size={space.sm} />
              <Button tone="quiet" label={t('onboarding.skip')} onPress={skip} />
            </>
          ) : null}

          {step === 'client' ? (
            <>
              <Field label={t('onboarding.client.name')} value={clientName} onChangeText={setClientName} autoCapitalize="words" />
              <Spacer />
              <Field label={t('onboarding.client.phone')} value={clientPhone} onChangeText={setClientPhone} keyboardType="phone-pad" autoCapitalize="none" />
              <Spacer size={space.xs} />
              <Caption>{t('onboarding.client.import')}</Caption>
              <Spacer size={space.xl} />
              <Card tone="raised">
                <Body>{t('onboarding.readyTitle')}</Body>
                <Spacer size={space.xs} />
                <Caption>{t('onboarding.readyBody')}</Caption>
              </Card>
              <Spacer />
              <Button tone="primary" label={clientName.trim() ? t('onboarding.finishWithClient') : t('onboarding.finish')}
                onPress={() => { void finish(); }} busy={busy} />
            </>
          ) : null}

          {index > 0 ? (
            <>
              <Spacer size={space.sm} />
              <Button tone="quiet" label={t('onboarding.back')} onPress={() => { setError(null); setStep(STEPS[index - 1]!); }} />
            </>
          ) : null}
        </View>
      </ScrollView>
    </Screen>
  );
}
