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
import { useApp } from '@/state/app';
import {
  Banner, Body, Button, Caption, Card, Empty, Heading, Row, Screen, Spacer, Title,
} from '@/ui/components';
import { space } from '@/ui/theme';

export default function SyncScreen() {
  const { db, sync, syncNow, signOut, touch, account } = useApp();
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
        <Title>Sync</Title>
        <Caption>
          {sync.lastSyncAt
            ? `Last synced ${new Date(sync.lastSyncAt).toLocaleTimeString()}`
            : 'Not synced on this device yet'}
        </Caption>

        <Spacer size={space.lg} />

        <Card>
          <Row style={{ justifyContent: 'space-between' }}>
            <View>
              <Body>{sync.pending} waiting to send</Body>
              <Caption>
                {sync.offline
                  ? 'No connection. They are safe on this device.'
                  : 'They send themselves in the background.'}
              </Caption>
            </View>
          </Row>
          <Spacer size={space.sm} />
          <Button
            label="Sync now"
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
        <Heading>Refused</Heading>
        <Spacer size={space.sm} />

        {failed.length === 0 ? (
          <Empty title="Nothing refused" detail="Everything the server has seen, it accepted." />
        ) : (
          failed.map((entry) => (
            <Card key={entry.id} style={{ marginBottom: space.sm }}>
              <Body>{describeOperation(entry)}</Body>
              <Spacer size={space.xs} />
              <Caption tone="danger">
                {entry.error_message ?? 'The server refused this.'}
              </Caption>
              <Spacer size={space.sm} />
              <Row>
                <Button label="Try again" style={{ flex: 1 }} onPress={() => { void retry(entry); }} />
                <Button label="Discard" tone="danger" onPress={() => { void discard(entry); }} />
              </Row>
            </Card>
          ))
        )}

        <Spacer size={space.xxl} />
        <Caption>{account?.email ?? ''}</Caption>
        <Spacer size={space.sm} />
        <Button label="Sign out" tone="quiet" onPress={() => { void signOut(); }} />
        <Spacer size={space.xs} />
        <Caption>
          Signing out leaves anything still waiting on this device. It sends when you sign back in.
        </Caption>
      </ScrollView>
    </Screen>
  );
}

/** What the trainer did, in their words rather than the operation's. */
function describeOperation(entry: OutboxEntry): string {
  switch (entry.type) {
    case 'attendance.mark':
      return entry.error_code === ErrorCode.InsufficientCredits
        ? 'Marking a session completed — the client had no credits left'
        : 'Marking a session';
    case 'workout.start': return 'Starting a workout';
    case 'workout.log_set': return 'Logging a set';
    case 'workout.complete': return 'Finishing a workout';
    case 'payment.record':
      return entry.error_code === ErrorCode.Overpayment
        ? 'Recording a payment — it was more than the invoice owed'
        : 'Recording a payment';
    case 'client.create': return 'Adding a client';
    case 'client.update': return 'Updating a client';
    case 'biometrics.record': return 'Recording a measurement';
    default: return 'A change';
  }
}
