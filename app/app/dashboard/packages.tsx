/**
 * The price list: the packs and coaching the practice sells.
 *
 * An offer is a template. Selling one copies its terms onto the invoice, so
 * changing a price here never reprices anything already sold — which is why
 * an offer is archived rather than deleted, and why the sell screen starts
 * from this list instead of from a blank form every time.
 */

import React, { useState } from 'react';
import { View } from 'react-native';

import type { PackageOffer, PackageOfferInput } from '@/api/account';
import { describeError } from '@/api/describe';
import { listOffers, type Cycle, type LocalOffer, type OfferKind } from '@/features/catalog';
import { useT, type TKey } from '@/i18n';
import { useApp, useQuery } from '@/state/app';
import {
  Banner, Body, Button, Caption, Card, Empty, Field, NumberField, Pill, Row, Spacer, TextButton, Toggle,
} from '@/ui/components';
import { amountText, parseMoney, parseReps } from '@/ui/format';
import { Sheet, useToast } from '@/ui/overlay';
import { Page } from '@/ui/page';
import { Select } from '@/ui/pickers';
import { space } from '@/ui/theme';

const KINDS: OfferKind[] = ['session_pack', 'semi_private', 'monthly_coaching', 'online_coaching'];
const CYCLES: Cycle[] = ['one_off', 'weekly', 'monthly', 'annual'];

interface Draft {
  id: string | null;
  name: string;
  description: string;
  kind: OfferKind;
  credits: string;
  price: string;
  validity: string;
  cycle: Cycle;
  includesVat: boolean;
}

export default function PackagesScreen() {
  const i18n = useT();
  const { t, money } = i18n;
  const { api, account, sync, syncNow, touch } = useApp();
  const toast = useToast();
  const currency = account?.currency ?? 'AED';
  const [showArchived, setShowArchived] = useState(false);
  const list = useQuery((db) => listOffers(db, showArchived), [showArchived]);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const offers = list.data ?? [];
  const blank: Draft = {
    id: null, name: '', description: '', kind: 'session_pack', credits: '10', price: '',
    validity: '90', cycle: 'one_off', includesVat: true,
  };

  const write = async (work: () => Promise<unknown>, done?: string) => {
    setBusy(true);
    setError(null);
    try {
      await work();
      await syncNow();
      touch();
      setDraft(null);
      if (done) toast(done, 'success');
    } catch (cause) {
      const message = describeError(cause, i18n);
      if (draft) setError(message);
      else toast(message, 'danger');
    } finally {
      setBusy(false);
    }
  };

  const save = () => {
    if (!draft) return;
    const price = parseMoney(draft.price, currency);
    const credits = draft.credits.trim() === '' ? null : parseReps(draft.credits);
    const validity = draft.validity.trim() === '' ? null : parseReps(draft.validity);
    if (price === null) {
      setError(t('packages.priceInvalid'));
      return;
    }
    const body: PackageOfferInput = {
      name: draft.name.trim(),
      description: draft.description.trim(),
      kind: draft.kind,
      price_minor: price,
      price_includes_vat: draft.includesVat,
      cycle: draft.cycle,
      ...(credits ? { credits } : { clear_credits: true }),
      ...(validity ? { validity_days: validity } : { clear_validity: true }),
    };
    if (!draft.id) {
      delete body.clear_credits;
      delete body.clear_validity;
    }
    void write(
      () => (draft.id
        ? api.patch<PackageOffer>(`/v1/package-offers/${draft.id}`, body)
        : api.post<PackageOffer>('/v1/package-offers', body)),
      t('settings.saved'),
    );
  };

  const edit = (o: LocalOffer) => {
    setError(null);
    setDraft({
      id: o.id, name: o.name, description: o.description, kind: o.kind,
      credits: o.credits === null ? '' : String(o.credits), price: amountText(o.priceMinor, currency),
      validity: o.validityDays === null ? '' : String(o.validityDays), cycle: o.cycle, includesVat: o.priceIncludesVat,
    });
  };

  const terms = (o: LocalOffer) => [
    t(`packages.kind.${o.kind}` as TKey),
    o.credits !== null ? t('packages.sessions', { count: o.credits }) : null,
    o.validityDays !== null ? t('packages.validFor', { count: o.validityDays }) : t('packages.noExpiry'),
    o.cycle !== 'one_off' ? t(`packages.cycle.${o.cycle}` as TKey) : null,
  ].filter(Boolean).join(' · ');

  return (
    <Page
      title={t('nav.packages')}
      subtitle={t('packages.subtitle')}
      actions={<Button compact icon="add" tone="primary" label={t('packages.add')} onPress={() => { setError(null); setDraft(blank); }} />}
    >
      {sync.offline ? <><Banner tone="muted" message={t('locations.offline')} /><Spacer /></> : null}

      {offers.length === 0 && !list.loading ? (
        <Empty icon="packages" title={t('packages.emptyTitle')} detail={t('packages.emptyBody')} />
      ) : (
        <View style={{ gap: space.sm }}>
          {offers.map((o) => (
            <Card key={o.id} tone={o.archived ? 'outline' : 'default'}>
              <Row style={{ gap: space.md, alignItems: 'flex-start' }}>
                <View style={{ flex: 1 }}>
                  <Row style={{ gap: space.sm, flexWrap: 'wrap' }}>
                    <Body>{o.name}</Body>
                    {o.archived ? <Pill label={t('locations.archived')} /> : null}
                  </Row>
                  <Caption>{terms(o)}</Caption>
                  {o.description ? <Caption>{o.description}</Caption> : null}
                </View>
                <View style={{ alignItems: 'flex-end', gap: space.xs }}>
                  <Body>{money(o.priceMinor, o.currency || currency)}</Body>
                  <Caption>{o.priceIncludesVat ? t('packages.inclVat') : t('packages.exclVat')}</Caption>
                  <Row style={{ gap: space.md }}>
                    {!o.archived ? <TextButton label={t('common.edit')} onPress={() => edit(o)} /> : null}
                    <TextButton
                      label={o.archived ? t('locations.restore') : t('locations.archive')}
                      onPress={() => { void write(() => api.post(`/v1/package-offers/${o.id}/${o.archived ? 'restore' : 'archive'}`, {})); }}
                    />
                  </Row>
                </View>
              </Row>
            </Card>
          ))}
        </View>
      )}

      <Spacer />
      <Toggle label={t('locations.showArchived')} value={showArchived} onChange={setShowArchived} />

      <Sheet
        visible={draft !== null}
        onClose={() => setDraft(null)}
        title={draft?.id ? t('packages.edit') : t('packages.add')}
        footer={
          <Button
            tone="primary"
            label={t('settings.save')}
            onPress={save}
            busy={busy}
            disabled={!draft || draft.name.trim() === '' || draft.price.trim() === ''
              || (draft.kind === 'session_pack' && draft.credits.trim() === '')}
          />
        }
      >
        {draft ? (
          <>
            {error ? <><Banner tone="danger" message={error} /><Spacer /></> : null}
            <Field label={t('packages.name')} value={draft.name} onChangeText={(name) => setDraft({ ...draft, name })} />
            <Spacer />
            <Select
              label={t('packages.kindLabel')}
              value={draft.kind}
              options={KINDS.map((k) => ({ value: k, label: t(`packages.kind.${k}` as TKey) }))}
              onChange={(kind) => setDraft({ ...draft, kind })}
            />
            <Spacer />
            <Row style={{ gap: space.md }}>
              <NumberField label={t('packages.credits')} value={draft.credits} onChangeText={(credits) => setDraft({ ...draft, credits })} />
              <NumberField label={t('packages.price', { currency })} value={draft.price} onChangeText={(price) => setDraft({ ...draft, price })} />
            </Row>
            <Spacer size={space.xs} />
            <Caption>{t('packages.creditsHint')}</Caption>
            <Spacer />
            <Row style={{ gap: space.md }}>
              <NumberField label={t('packages.validity')} value={draft.validity} onChangeText={(validity) => setDraft({ ...draft, validity })} />
              <View style={{ flex: 1 }}>
                <Select
                  label={t('packages.cycleLabel')}
                  value={draft.cycle}
                  options={CYCLES.map((c) => ({ value: c, label: t(`packages.cycle.${c}` as TKey) }))}
                  onChange={(cycle) => setDraft({ ...draft, cycle })}
                />
              </View>
            </Row>
            <Spacer size={space.xs} />
            <Caption>{t('packages.validityHint')}</Caption>
            <Spacer />
            <Toggle label={t('packages.includesVat')} hint={t('packages.includesVatHint')} value={draft.includesVat} onChange={(includesVat) => setDraft({ ...draft, includesVat })} />
            <Spacer />
            <Field label={t('packages.description')} value={draft.description} onChangeText={(description) => setDraft({ ...draft, description })} multiline />
          </>
        ) : null}
      </Sheet>
    </Page>
  );
}
