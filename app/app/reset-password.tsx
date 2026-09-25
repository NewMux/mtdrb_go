/**
 * Choose a new password, from the link in a reset email.
 *
 * Reachable signed out — the person here cannot sign in, which is the point.
 * The link carries its token in the query string; the server decides whether
 * it is still good, and says so in words if not.
 */

import React, { useState } from 'react';
import { ScrollView } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { describeError } from '@/api/describe';
import { useT } from '@/i18n';
import { useApp } from '@/state/app';
import { Banner, Body, Button, Field, Screen, Spacer, Title } from '@/ui/components';
import { space } from '@/ui/theme';

export default function ResetPasswordScreen() {
  const { api } = useApp();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const i18n = useT();
  const { t } = i18n;
  const { token } = useLocalSearchParams<{ token?: string }>();

  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState(false);

  const mismatch = confirm !== '' && confirm !== password;

  const save = async () => {
    setBusy(true);
    setError(null);
    try {
      await api.resetPassword(String(token ?? ''), password);
      setDone(true);
    } catch (cause) {
      setError(describeError(cause, i18n));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Screen>
      <ScrollView
        contentContainerStyle={{ padding: space.xl, paddingTop: insets.top + space.xxl, flexGrow: 1, justifyContent: 'center' }}
        keyboardShouldPersistTaps="handled"
      >
        <Title>{t('resetPassword.title')}</Title>
        <Spacer size={space.xs} />
        <Body muted>{t('resetPassword.intro')}</Body>
        <Spacer size={space.xl} />

        {!token ? (
          <Banner tone="danger" message={t('resetPassword.missingToken')} />
        ) : done ? (
          <>
            <Banner tone="success" message={t('resetPassword.done')} />
            <Spacer size={space.xl} />
            <Button label={t('resetPassword.toSignIn')} tone="primary" onPress={() => router.replace('/sign-in')} />
          </>
        ) : (
          <>
            {error ? <><Banner tone="danger" message={error} /><Spacer /></> : null}
            <Field label={t('resetPassword.newPassword')} value={password} onChangeText={setPassword} secure autoCapitalize="none" />
            <Spacer />
            <Field
              label={t('resetPassword.confirm')}
              value={confirm}
              onChangeText={setConfirm}
              secure
              autoCapitalize="none"
              error={mismatch ? t('resetPassword.mismatch') : null}
            />
            <Spacer size={space.xl} />
            <Button
              label={t('resetPassword.save')}
              tone="primary"
              onPress={() => { void save(); }}
              disabled={password === '' || confirm !== password}
              busy={busy}
            />
          </>
        )}
        <Spacer size={space.sm} />
        <Button label={t('signIn.back')} tone="quiet" onPress={() => router.replace('/sign-in')} />
      </ScrollView>
    </Screen>
  );
}
