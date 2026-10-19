/**
 * Adding a client.
 *
 * Only the name is required. PAR-Q, waiver, a package and a programme are
 * all things a trainer does sitting down later; making any of them required
 * here would mean the person standing in front of them cannot be added.
 * A goal and a starting weight are offered because they are what the first
 * conversation is about.
 */

import React, { useState } from 'react';
import { View } from 'react-native';
import { useRouter } from 'expo-router';

import { createClient, recordBiometrics } from '@/features/actions';
import { useApp } from '@/state/app';
import { Body, Button, FieldRow, Section, Spacer } from '@/ui/components';
import { parseLoad } from '@/ui/format';
import { space } from '@/ui/theme';

export default function NewClientScreen() {
  const { db, touch, syncNow, prefs } = useApp();
  const router = useRouter();

  const [fullName, setFullName] = useState('');
  const [email, setEmail] = useState('');
  const [phone, setPhone] = useState('');
  const [goal, setGoal] = useState('');
  const [weight, setWeight] = useState('');
  const [busy, setBusy] = useState(false);

  const save = async () => {
    if (!db || fullName.trim() === '') return;
    setBusy(true);
    const id = await createClient(db, fullName.trim(), {
      email: email.trim() || undefined,
      phone: phone.trim() || undefined,
      notes: goal.trim() ? `Goal: ${goal.trim()}` : '',
    });
    const grams = parseLoad(weight, prefs.units);
    if (grams !== null) await recordBiometrics(db, id, { weightGrams: grams });
    touch();
    void syncNow();
    setBusy(false);
    // Replaced rather than pushed, so backing out of the profile does not
    // return to a form that has already been submitted.
    router.replace({ pathname: '/client/[id]', params: { id } });
  };

  return (
    <Body keyboard>
      <Section footer="They're saved the moment you tap Add — with or without signal.">
        <FieldRow first label="Name" value={fullName} onChangeText={setFullName} autoCapitalize="words" placeholder="Required" />
        <FieldRow label="Phone" value={phone} onChangeText={setPhone} keyboardType="phone-pad" placeholder="For WhatsApp & SMS" />
        <FieldRow label="Email" value={email} onChangeText={setEmail} keyboardType="email-address" autoCapitalize="none" placeholder="For invoice links" />
      </Section>
      <Section title="Starting Point" footer="Optional. The goal goes in their coach notes; the weight starts their progress chart.">
        <FieldRow first label="Goal" value={goal} onChangeText={setGoal} placeholder="e.g. Lose 6 kg, first pull-up" />
        <FieldRow label="Weight" value={weight} onChangeText={setWeight} keyboardType="decimal-pad" placeholder="0" suffix={prefs.units} />
      </Section>
      <View style={{ paddingHorizontal: space.lg, marginTop: space.xl }}>
        <Button label="Add Client" variant="filled" full onPress={() => { void save(); }} disabled={fullName.trim() === ''} busy={busy} />
      </View>
      <Spacer />
    </Body>
  );
}
