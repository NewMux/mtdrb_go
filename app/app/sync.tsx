/**
 * What is waiting, and what went wrong.
 *
 * The screen exists because an outbox that silently parks a refused operation
 * is worse than no outbox at all: the trainer marked a session completed,
 * believes the credit burned, and nothing ever tells them otherwise. Refusals
 * are shown here in plain words, with the choice of sending again or letting
 * it go.
 */

import React, { useCallback, useEffect, useState } from 'react';
import { ScrollView, View } from 'react-native';

import * as outbox from '@/sync/outbox';
import type { OutboxEntry } from '@/sync/outbox';
import { ErrorCode } from '@/api/types';
import { useT, type I18n } from '@/i18n';
import { DEMO, useApp } from '@/state/app';
import { usePreferences } from '@/state/preferences';
import {
  Banner, Body, Button, Caption, Card, Empty, Heading, Label, Row, Screen, SegmentedChoice, Spacer, Title,
} from '@/ui/components';
import { space } from '@/ui/theme';

export default function SyncScreen() {
  const { db, sync, syncNow, signOut, touch, account } = useApp();
  const i18n = useT();
  const { t, time } = i18n;
  const preferences = usePreferences();
  const [failed, setFailed] = useState<OutboxEntry[]>([]);

  const load = useCallback(async () => {
    if (!db) return;
    setFailed(await outbox.needsAttention(db));
  }, [db]);

  useEffect(() => { void load(); }, [load, sync.pending, sync.failed]);

  const retry = async (entry: OutboxEntry) => {
    if (!db) return;
    // Re-queueing rather than flipping the status, so it goes back to the end
    // of the line with a fresh queued_at and cannot jump ahead of work done
    // since.
    await outbox.enqueue(db, entry.id, entry.type, entry.data, new Date());
    await load();
    touch();
    void syncNow();
  };

  const discard = async (entry: OutboxEntry) => {
    if (!db) return;
    await outbox.discard(db, entry.id);
    await load();
    touch();
  };

  return (
    <Screen>
      <ScrollView contentContainerStyle={{ padding: space.lg, paddingBottom: space.xxl }}>
        <Title>{t('syncScreen.title')}</Title>
        <Caption>
          {sync.lastSyncAt ? t('syncScreen.lastSynced', { time: time(sync.lastSyncAt) }) : t('syncScreen.notYet')}
        </Caption>

        <Spacer size={space.lg} />

        {DEMO ? (
          <>
            <Banner
              message={t('syncScreen.demo')}
              tone="warning"
            />
            <Spacer />
          </>
        ) : null}

        <Card>
          <Row style={{ justifyContent: 'space-between' }}>
            <View>
              <Body>{t('syncScreen.waitingToSend', { count: sync.pending })}</Body>
              <Caption>{sync.offline ? t('syncScreen.safeOffline') : t('syncScreen.sendThemselves')}</Caption>
            </View>
          </Row>
          <Spacer size={space.sm} />
          <Button
            label={t('syncScreen.syncNow')}
            icon="sync"
            tone="primary"
            busy={sync.running}
            onPress={() => { void syncNow(); }}
          />
        </Card>

        {sync.error ? (
          <>
            <Spacer />
            <Banner message={sync.error} tone="danger" />
          </>
        ) : null}

        <Spacer size={space.lg} />
        <Heading>{t('syncScreen.refused')}</Heading>
        <Spacer size={space.sm} />

        {failed.length === 0 ? (
          <Empty icon="success" title={t('syncScreen.nothingRefused')} detail={t('syncScreen.nothingRefusedBody')} />
        ) : (
          failed.map((entry) => (
            <Card key={entry.id} style={{ marginBottom: space.sm }}>
              <Body>{describeOperation(entry, i18n)}</Body>
              <Spacer size={space.xs} />
              <Caption tone="danger">
                {entry.error_message ?? t('syncScreen.serverRefused')}
              </Caption>
              <Spacer size={space.sm} />
              <Row>
                <Button label={t('common.retry')} style={{ flex: 1 }} onPress={() => { void retry(entry); }} />
                <Button label={t('common.discard')} tone="danger" onPress={() => { void discard(entry); }} />
              </Row>
            </Card>
          ))
        )}

        <Spacer size={space.xl} />
        <Heading>{t('syncScreen.preferences')}</Heading>
        <Spacer size={space.sm} />
        <Card>
          <Label>{t('syncScreen.language')}</Label>
          <Spacer size={space.sm} />
          <SegmentedChoice
            options={[{ value: 'en', label: t('prefs.english') }, { value: 'ar', label: t('prefs.arabic') }] as const}
            value={preferences.locale}
            onChange={preferences.setLocale}
          />
          {preferences.restartPending ? (
            <>
              <Spacer size={space.sm} />
              <Banner
                tone="muted"
                message={t('prefs.restartBody')}
                action={{ label: t('prefs.restart'), onPress: preferences.restart }}
              />
            </>
          ) : null}
          <Spacer />
          <Label>{t('syncScreen.digits')}</Label>
          <Spacer size={space.sm} />
          <SegmentedChoice
            options={[{ value: 'latn', label: t('prefs.latin') }, { value: 'arab', label: t('prefs.arabicIndic') }] as const}
            value={preferences.prefs.digits}
            onChange={preferences.setDigits}
          />
          <Spacer />
          <Label>{t('syncScreen.theme')}</Label>
          <Spacer size={space.sm} />
          <SegmentedChoice
            options={[
              { value: 'system', label: t('prefs.system') },
              { value: 'light', label: t('prefs.light') },
              { value: 'dark', label: t('prefs.dark') },
            ] as const}
            value={preferences.prefs.theme}
            onChange={preferences.setTheme}
          />
        </Card>

        <Spacer size={space.xxl} />
        <Caption>{account?.email ?? ''}</Caption>
        <Spacer size={space.sm} />
        <Button label={t('syncScreen.signOut')} icon="signOut" tone="quiet" onPress={() => { void signOut(); }} />
        <Spacer size={space.xs} />
        <Caption>{t('syncScreen.signOutNote')}</Caption>
      </ScrollView>
    </Screen>
  );
}

/** What the trainer did, in their words rather than the operation's. */
function describeOperation(entry: OutboxEntry, { t }: I18n): string {
  switch (entry.type) {
    case 'attendance.mark':
      return entry.error_code === ErrorCode.InsufficientCredits
        ? t('syncScreen.ops.markNoCredits')
        : t('syncScreen.ops.mark');
    case 'workout.start': return t('syncScreen.ops.startWorkout');
    case 'workout.log_set': return t('syncScreen.ops.logSet');
    case 'workout.complete': return t('syncScreen.ops.completeWorkout');
    case 'payment.record':
      return entry.error_code === ErrorCode.Overpayment
        ? t('syncScreen.ops.paymentOver')
        : t('syncScreen.ops.payment');
    case 'client.create': return t('syncScreen.ops.createClient');
    case 'client.update': return t('syncScreen.ops.updateClient');
    case 'biometrics.record': return t('syncScreen.ops.biometrics');
    default: return t('syncScreen.ops.unknown');
  }
}
