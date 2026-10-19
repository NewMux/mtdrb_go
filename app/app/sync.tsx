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
import { View } from 'react-native';

import * as outbox from '@/sync/outbox';
import type { OutboxEntry } from '@/sync/outbox';
import { ErrorCode } from '@/api/types';
import { DEMO, useApp } from '@/state/app';
import {
  Banner, Body, Button, Card, EmptyState, HStack, IconBubble, Section, T, VStack,
} from '@/ui/components';
import { describe as describeSync } from '@/ui/sync-badge';
import { space } from '@/ui/theme';

export default function SyncScreen() {
  const { db, sync, syncNow, touch } = useApp();
  const [failed, setFailed] = useState<OutboxEntry[]>([]);
  const status = describeSync(sync);

  const load = useCallback(async () => {
    if (!db) return;
    setFailed(await outbox.needsAttention(db));
  }, [db]);

  useEffect(() => { void load(); }, [load, sync.pending, sync.failed]);

  const retry = async (entry: OutboxEntry) => {
    if (!db) return;
    // Re-queued rather than flipped back to pending, so it goes to the end of
    // the line with a fresh queued_at and cannot jump ahead of work done since.
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
    <Body>
      <View style={{ paddingHorizontal: space.lg }}>
        <Card>
          <HStack gap={space.md}>
            <IconBubble icon={status.icon} hue={DEMO ? 'orange' : status.hue} size={48} />
            <VStack gap={2} style={{ flex: 1 }}>
              <T variant="headline">{DEMO ? 'Demo mode' : status.label}</T>
              <T variant="subhead" tone="label2">
                {sync.lastSyncAt ? `Last synced ${new Date(sync.lastSyncAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}` : 'Not synced on this device yet'}
              </T>
            </VStack>
          </HStack>
          <View style={{ height: space.lg }} />
          <HStack justify="space-between">
            <VStack gap={0}>
              <T variant="metricSmall">{sync.pending}</T>
              <T variant="footnote" tone="label2">waiting to send</T>
            </VStack>
            <Button label="Sync Now" icon="sync" variant="filled" size="medium" busy={sync.running} onPress={() => { void syncNow(); }} />
          </HStack>
        </Card>
      </View>

      {DEMO ? (
        <Banner
          style={{ marginHorizontal: space.lg, marginTop: space.lg }}
          tone="orange"
          title="No server behind this build"
          message="Everything you do is saved on this device and queued — but nothing can be sent, so credits are not burned, revenue is not recognised and invoices are not settled. That happens in the ledger, behind the API."
        />
      ) : null}

      {sync.error ? (
        <Banner style={{ marginHorizontal: space.lg, marginTop: space.lg }} tone="red" message={sync.error} />
      ) : null}

      <Section title="Refused" plain footer={failed.length ? 'The server said no to these. Nothing else is held up behind them.' : undefined}>
        {failed.length === 0 ? (
          <Card><EmptyState icon="checkCircle" hue="green" title="Nothing refused" message="Everything the server has seen, it accepted." /></Card>
        ) : (
          <VStack gap={space.md}>
            {failed.map((entry) => (
              <Card key={entry.id}>
                <T variant="headline">{describeOperation(entry)}</T>
                <T variant="subhead" tone="red" style={{ marginTop: 4 }}>{entry.error_message ?? 'The server refused this.'}</T>
                <HStack gap={space.sm} style={{ marginTop: space.md }}>
                  <Button label="Try Again" variant="tinted" size="medium" style={{ flex: 1 }} full onPress={() => { void retry(entry); }} />
                  <Button label="Discard" variant="destructive" size="medium" onPress={() => { void discard(entry); }} />
                </HStack>
              </Card>
            ))}
          </VStack>
        )}
      </Section>
    </Body>
  );
}

/** What the trainer did, in their words rather than the operation's. */
function describeOperation(entry: OutboxEntry): string {
  switch (entry.type) {
    case 'attendance.mark':
      return entry.error_code === ErrorCode.InsufficientCredits
        ? 'Marking a session attended — the client had no sessions left'
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
    case 'session.book': return 'Booking a session — the slot may clash with another';
    case 'session.reschedule': return 'Moving a session';
    case 'session.cancel': return 'Cancelling a session';
    case 'program.save': return `Saving the program "${String(entry.data.name ?? '')}"`;
    case 'program.assign': return 'Assigning a program';
    default: return 'A change';
  }
}
