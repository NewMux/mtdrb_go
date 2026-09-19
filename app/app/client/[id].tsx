/**
 * A client's profile.
 *
 * Arranged by what a trainer actually looks up mid-conversation: how many
 * credits are left, what they owe, when they last trained, what they weighed.
 */

import React, { useCallback, useState } from 'react';
import { ScrollView, View } from 'react-native';
import { useFocusEffect, useLocalSearchParams, useRouter } from 'expo-router';

import { recordBiometrics, startWorkout } from '@/features/actions';
import {
  biometricHistory, clientDetail, openWorkout, outstandingInvoices, recentWorkouts,
} from '@/features/queries';
import { useApp, useQuery } from '@/state/app';
import {
  Body, Button, Caption, Card, Divider, Label, Metric, NumberField,
  Row, Screen, Spacer, TextButton, Title,
} from '@/ui/components';
import {
  bodyFat as formatBodyFat, dueLabel, load as formatLoad, money,
  parseBodyFat, parseWeight, shortDate,
} from '@/ui/format';
import { space } from '@/ui/theme';

export default function ClientScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const { db, account, touch, syncNow } = useApp();
  const router = useRouter();

  const [weighing, setWeighing] = useState(false);
  const [weight, setWeight] = useState('');
  const [fat, setFat] = useState('');

  const client = useQuery((database) => clientDetail(database, id), [id]);
  const invoices = useQuery((database) => outstandingInvoices(database, id), [id]);
  const workouts = useQuery((database) => recentWorkouts(database, id, 5), [id]);
  const measurements = useQuery((database) => biometricHistory(database, id, 5), [id]);

  const reloadAll = useCallback(() => {
    client.reload();
    invoices.reload();
    workouts.reload();
    measurements.reload();
  }, [client.reload, invoices.reload, workouts.reload, measurements.reload]);

  useFocusEffect(useCallback(() => { reloadAll(); }, [reloadAll]));

  const profile = client.data;

  const train = async () => {
    if (!db || !profile) return;
    const existing = await openWorkout(db, profile.id);
    const workoutId = existing?.id ?? await startWorkout(db, profile.id);
    if (!existing) touch();
    router.push({
      pathname: '/workout/[id]',
      params: { id: workoutId, client: profile.id, name: profile.fullName },
    });
  };

  const saveMeasurement = async () => {
    if (!db || !profile) return;
    const weightGrams = parseWeight(weight);
    const bodyFatBP = parseBodyFat(fat);
    if (weightGrams === null && bodyFatBP === null) return;

    await recordBiometrics(db, profile.id, {
      weightGrams: weightGrams ?? undefined,
      bodyFatBP: bodyFatBP ?? undefined,
    });
    setWeight('');
    setFat('');
    setWeighing(false);
    touch();
    measurements.reload();
    void syncNow();
  };

  if (!profile) {
    return (
      <Screen>
        <View style={{ padding: space.lg }}>
          <Body muted>{client.loading ? 'Loading…' : 'This client is not on this device.'}</Body>
        </View>
      </Screen>
    );
  }

  const currency = account?.currency ?? 'EUR';

  return (
    <Screen>
      <ScrollView
        contentContainerStyle={{ padding: space.lg, paddingBottom: space.xxl * 2 }}
        keyboardShouldPersistTaps="handled"
      >
        <Title>{profile.fullName}</Title>
        <Caption>{[profile.email, profile.phone].filter(Boolean).join(' · ') || profile.status}</Caption>

        <Spacer size={space.lg} />

        <Card tone="accent">
          <Row style={{ justifyContent: 'space-between', alignItems: 'flex-end' }}>
            <Metric
              value={String(profile.creditsRemaining)}
              unit={profile.creditsRemaining === 1 ? 'session' : 'sessions'}
              label={profile.creditsRemaining < 0 ? 'overdrawn' : 'left on their pack'}
              tone="onAccent"
            />
            {profile.nextExpiry ? <Caption tone="onAccent">expires {shortDate(profile.nextExpiry)}</Caption> : null}
          </Row>
        </Card>

        <Spacer />
        <Row>
          <Button
            label="Start a workout"
            tone={profile.creditsRemaining > 0 ? 'primary' : 'default'}
            style={{ flex: 1 }}
            onPress={() => { void train(); }}
          />
          <Button
            label="Sell a pack"
            tone={profile.creditsRemaining <= 0 ? 'primary' : 'default'}
            style={{ flex: 1 }}
            onPress={() => router.push({
              pathname: '/sell-package',
              params: { client: profile.id, name: profile.fullName },
            })}
          />
        </Row>

        <Spacer size={space.xl} />
        <Label>Owing</Label>
        <Spacer />
        {(invoices.data ?? []).length === 0 ? (
          <Caption>Nothing outstanding.</Caption>
        ) : (
          (invoices.data ?? []).map((invoice) => (
            <Card key={invoice.id} style={{ marginBottom: space.sm }}>
              <Row style={{ justifyContent: 'space-between' }}>
                <View>
                  <Body>{invoice.number ?? 'Draft'}</Body>
                  <Caption tone={isOverdue(invoice.dueDate) ? 'danger' : 'muted'}>
                    {dueLabel(invoice.dueDate) || 'no due date'}
                  </Caption>
                </View>
                <Body>{money(invoice.totalMinor - invoice.paidMinor, invoice.currency || currency)}</Body>
              </Row>
            </Card>
          ))
        )}

        <Spacer size={space.lg} />
        <Row style={{ justifyContent: 'space-between' }}>
          <Label>Measurements</Label>
          <TextButton label={weighing ? 'Cancel' : 'Record'} onPress={() => setWeighing(!weighing)} />
        </Row>
        <Spacer size={space.sm} />

        {weighing ? (
          <Card>
            <Row>
              <NumberField label="Weight (kg)" value={weight} onChangeText={setWeight} placeholder="82.4" />
              <NumberField label="Body fat (%)" value={fat} onChangeText={setFat} placeholder="15.5" />
            </Row>
            <Spacer size={space.sm} />
            <Button
              label="Save measurement"
              tone="primary"
              onPress={() => { void saveMeasurement(); }}
              disabled={parseWeight(weight) === null && parseBodyFat(fat) === null}
            />
          </Card>
        ) : null}

        {(measurements.data ?? []).length === 0 ? (
          weighing ? null : <Caption>No measurements recorded.</Caption>
        ) : (
          <>
            <Spacer size={space.sm} />
            <Card>
              {(measurements.data ?? []).map((entry, i) => (
                <View key={entry.id}>
                  {i > 0 ? <Divider /> : null}
                  <Row style={{ justifyContent: 'space-between', paddingVertical: space.md }}>
                    <Caption>{shortDate(entry.measuredOn)}</Caption>
                    <Body>{formatLoad(entry.weightGrams)}</Body>
                    <Caption>{formatBodyFat(entry.bodyFatBP)}</Caption>
                  </Row>
                </View>
              ))}
            </Card>
          </>
        )}

        <Spacer size={space.xl} />
        <Label>Recent training</Label>
        <Spacer />
        {(workouts.data ?? []).length === 0 ? (
          <Caption>No workouts logged yet.</Caption>
        ) : (
          <Card>
            {(workouts.data ?? []).map((workout, i) => (
              <View key={workout.id}>
                {i > 0 ? <Divider /> : null}
                <Row style={{ justifyContent: 'space-between', paddingVertical: space.md }}>
                  <Caption>{shortDate(workout.performedOn)}</Caption>
                  <Body>{workout.setCount} sets</Body>
                  <Caption>
                    {workout.volumeGrams > 0 ? `${Math.round(workout.volumeGrams / 1000)} kg moved` : workout.status}
                  </Caption>
                </Row>
              </View>
            ))}
          </Card>
        )}
      </ScrollView>
    </Screen>
  );
}

function isOverdue(dueDate: string | null): boolean {
  if (!dueDate) return false;
  return new Date(dueDate).getTime() < new Date().setHours(0, 0, 0, 0);
}
