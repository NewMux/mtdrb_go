/**
 * Adding a client.
 *
 * Name only. Everything else — PAR-Q, waiver, measurements, a package — is
 * something a trainer does sitting down later; making any of it required here
 * would mean the person standing in front of them cannot be added.
 */

import React, { useState } from 'react';
import { ScrollView } from 'react-native';
import { useRouter } from 'expo-router';

import { createClient } from '@/features/actions';
import { useT } from '@/i18n';
import { useApp } from '@/state/app';
import { Body, Button, Field, Screen, Spacer, Title } from '@/ui/components';
import { space } from '@/ui/theme';

export default function NewClientScreen() {
  const { db, touch, syncNow } = useApp();
  const router = useRouter();
  const { t } = useT();

  const [fullName, setFullName] = useState('');
  const [email, setEmail] = useState('');
  const [phone, setPhone] = useState('');
  const [busy, setBusy] = useState(false);

  const save = async () => {
    if (!db || fullName.trim() === '') return;
    setBusy(true);
    const id = await createClient(db, fullName.trim(), {
      email: email.trim() || undefined,
      phone: phone.trim() || undefined,
    });
    touch();
    void syncNow();
    setBusy(false);
    // Replaced rather than pushed, so backing out of the profile does not
    // return to a form that has already been submitted.
    router.replace({ pathname: '/dashboard/clients/[id]', params: { id } });
  };

  return (
    <Screen>
      <ScrollView contentContainerStyle={{ padding: space.lg }} keyboardShouldPersistTaps="handled">
        <Title>{t('newClient.title')}</Title>
        <Spacer size={space.xs} />
        <Body muted>{t('newClient.intro')}</Body>
        <Spacer size={space.lg} />

        <Field label={t('newClient.fullName')} value={fullName} onChangeText={setFullName} autoCapitalize="words" />
        <Spacer />
        <Field
          label={t('newClient.email')}
          value={email}
          onChangeText={setEmail}
          keyboardType="email-address"
          autoCapitalize="none"
          hint={t('newClient.emailHint')}
        />
        <Spacer />
        <Field label={t('newClient.phone')} value={phone} onChangeText={setPhone} keyboardType="phone-pad" />

        <Spacer size={space.xl} />
        <Button
          label={t('newClient.save')}
          tone="primary"
          onPress={() => { void save(); }}
          disabled={fullName.trim() === ''}
          busy={busy}
        />
      </ScrollView>
    </Screen>
  );
}
