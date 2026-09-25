/**
 * Where the practice works: the studio, the gym that rents floor time, the
 * beach at dawn, a client's living room, a video call.
 *
 * Read from the device's mirror, so the list is there offline; changed on the
 * server, which checks the name is not already taken and the plan has room,
 * and then pulled back. A place is archived rather than deleted, so last
 * year's sessions still say where they happened.
 */

import React, { useState } from 'react';
import { Pressable, View } from 'react-native';

import type { Location, LocationInput } from '@/api/account';
import { describeError } from '@/api/describe';
import { listLocations, type LocalLocation, type LocationKind } from '@/features/catalog';
import { useT, type TKey } from '@/i18n';
import { useApp, useQuery } from '@/state/app';
import {
  Banner, Body, Button, Caption, Card, Empty, Field, Pill, Row, Spacer, TextButton, Toggle,
} from '@/ui/components';
import { Icon } from '@/ui/icon';
import { Sheet, useToast } from '@/ui/overlay';
import { Page } from '@/ui/page';
import { Select } from '@/ui/pickers';
import { radius, space } from '@/ui/theme';
import { useTheme } from '@/ui/theming';

const KINDS: LocationKind[] = ['studio', 'gym', 'outdoor', 'client_home', 'online'];
/** A handful of colours that read on both themes; the calendar uses them. */
export const LOCATION_COLOURS = ['#c8ff00', '#4f8cff', '#ff8a3d', '#b36bff', '#26c6a5', '#ff5c8a'];

interface Draft {
  id: string | null;
  name: string;
  kind: LocationKind;
  region: string;
  address: string;
  colour: string | null;
  isPrimary: boolean;
}

const blank: Draft = { id: null, name: '', kind: 'studio', region: '', address: '', colour: null, isPrimary: false };

export default function LocationsScreen() {
  const i18n = useT();
  const { t } = i18n;
  const { api, sync, syncNow, touch } = useApp();
  const toast = useToast();
  const { colors } = useTheme();
  const [showArchived, setShowArchived] = useState(false);
  const list = useQuery((db) => listLocations(db, showArchived), [showArchived]);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const locations = list.data ?? [];

  const write = async (work: () => Promise<unknown>, done?: string) => {
    setBusy(true);
    setError(null);
    try {
      await work();
      // The server's row is the truth; pulling it is how it reaches the list.
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
    const body: LocationInput = {
      name: draft.name.trim(),
      kind: draft.kind,
      region: draft.region.trim(),
      address: draft.address.trim(),
      colour: draft.colour ?? '',
      ...(draft.isPrimary ? { is_primary: true } : {}),
    };
    void write(
      () => (draft.id ? api.patch<Location>(`/v1/locations/${draft.id}`, body) : api.post<Location>('/v1/locations', body)),
      t('settings.saved'),
    );
  };

  const edit = (l: LocalLocation) => {
    setError(null);
    setDraft({ id: l.id, name: l.name, kind: l.kind, region: l.region, address: l.address, colour: l.colour, isPrimary: l.isPrimary });
  };

  const kindLabel = (k: LocationKind) => t(`locations.kind.${k}` as TKey);

  return (
    <Page
      title={t('nav.locations')}
      subtitle={t('locations.count', { count: locations.filter((l) => !l.archived).length })}
      actions={<Button compact icon="add" tone="primary" label={t('locations.add')} onPress={() => { setError(null); setDraft(blank); }} />}
    >
      {sync.offline ? <><Banner tone="muted" message={t('locations.offline')} /><Spacer /></> : null}

      {locations.length === 0 && !list.loading ? (
        <Empty icon="locations" title={t('locations.emptyTitle')} detail={t('locations.emptyBody')} />
      ) : (
        <View style={{ gap: space.sm }}>
          {locations.map((l) => (
            <Card key={l.id} tone={l.archived ? 'outline' : 'default'}>
              <Row style={{ gap: space.md, alignItems: 'flex-start' }}>
                <View style={{ width: 12, height: 12, borderRadius: radius.pill, marginTop: 6, backgroundColor: l.colour ?? colors.inkMuted }} />
                <View style={{ flex: 1 }}>
                  <Row style={{ gap: space.sm, flexWrap: 'wrap' }}>
                    <Body>{l.name}</Body>
                    {l.isPrimary ? <Pill label={t('locations.primary')} tone="accent" /> : null}
                    {l.archived ? <Pill label={t('locations.archived')} /> : null}
                  </Row>
                  <Caption>{[kindLabel(l.kind), l.region, l.address].filter(Boolean).join(' · ')}</Caption>
                  {!l.archived ? <Caption>{t('locations.upcoming', { count: l.upcoming })}</Caption> : null}
                </View>
                <View style={{ alignItems: 'flex-end', gap: space.xs }}>
                  {!l.archived ? <TextButton label={t('common.edit')} onPress={() => edit(l)} /> : null}
                  <TextButton
                    label={l.archived ? t('locations.restore') : t('locations.archive')}
                    onPress={() => { void write(() => api.post(`/v1/locations/${l.id}/${l.archived ? 'restore' : 'archive'}`, {})); }}
                  />
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
        title={draft?.id ? t('locations.edit') : t('locations.add')}
        footer={
          <Button
            tone="primary"
            label={t('settings.save')}
            onPress={save}
            busy={busy}
            disabled={!draft || draft.name.trim() === ''}
          />
        }
      >
        {draft ? (
          <>
            {error ? <><Banner tone="danger" message={error} /><Spacer /></> : null}
            <Field label={t('locations.name')} value={draft.name} onChangeText={(name) => setDraft({ ...draft, name })} autoCapitalize="words" />
            <Spacer />
            <Select
              label={t('locations.kindLabel')}
              value={draft.kind}
              options={KINDS.map((k) => ({ value: k, label: kindLabel(k) }))}
              onChange={(kind) => setDraft({ ...draft, kind })}
            />
            <Spacer />
            <Field label={t('locations.region')} value={draft.region} onChangeText={(region) => setDraft({ ...draft, region })} hint={t('locations.regionHint')} autoCapitalize="words" />
            <Spacer />
            <Field label={t('locations.address')} value={draft.address} onChangeText={(address) => setDraft({ ...draft, address })} multiline />
            <Spacer />
            <Caption>{t('locations.colour')}</Caption>
            <Spacer size={space.sm} />
            <Row style={{ gap: space.sm, flexWrap: 'wrap' }}>
              {LOCATION_COLOURS.map((c, index) => (
                <Pressable
                  key={c}
                  accessibilityRole="radio"
                  accessibilityState={{ selected: draft.colour === c }}
                  accessibilityLabel={t('locations.colourN', { n: index + 1 })}
                  onPress={() => setDraft({ ...draft, colour: draft.colour === c ? null : c })}
                  style={{
                    width: 40, height: 40, borderRadius: radius.pill, backgroundColor: c,
                    alignItems: 'center', justifyContent: 'center',
                  }}
                >
                  {draft.colour === c ? <Icon name="check" size={20} color="#121212" strokeWidth={3} /> : null}
                </Pressable>
              ))}
            </Row>
            <Spacer />
            {!draft.id || !locations.find((l) => l.id === draft.id)?.isPrimary ? (
              <Toggle
                label={t('locations.makePrimary')}
                hint={t('locations.primaryHint')}
                value={draft.isPrimary}
                onChange={(isPrimary) => setDraft({ ...draft, isPrimary })}
              />
            ) : (
              <Caption>{t('locations.primaryHint')}</Caption>
            )}
          </>
        ) : null}
      </Sheet>
    </Page>
  );
}
