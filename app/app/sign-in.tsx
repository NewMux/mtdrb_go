/**
 * Sign in, or start an account.
 *
 * The only screen in the app that needs the network to do anything, and the
 * only one that says so.
 */

import React, { useState } from 'react';
import { KeyboardAvoidingView, Platform, ScrollView, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { ApiError, NetworkError } from '@/api/client';
import { ErrorCode } from '@/api/types';
import { useT, type I18n } from '@/i18n';
import { useApp } from '@/state/app';
import {
  Banner, Body, Button, Caption, Field, Screen, Spacer, Title,
} from '@/ui/components';
import { space } from '@/ui/theme';

type Mode = 'sign-in' | 'sign-up';

export default function SignInScreen() {
  const { signIn, signUp } = useApp();
  const insets = useSafeAreaInsets();
  const i18n = useT();
  const { t } = i18n;

  const [mode, setMode] = useState<Mode>('sign-in');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [businessName, setBusinessName] = useState('');
  const [currency, setCurrency] = useState('AED');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      if (mode === 'sign-in') {
        await signIn(email.trim(), password);
      } else {
        await signUp({
          email: email.trim(),
          password,
          display_name: displayName.trim(),
          business_name: businessName.trim() || displayName.trim(),
          currency: currency.trim().toUpperCase() || 'AED',
          // Without it every tenant was UTC, and "today" ran four hours late
          // in Dubai. The device knows where it is.
          timezone: deviceTimezone(),
        });
      }
    } catch (cause) {
      setError(describe(cause, i18n));
    } finally {
      setBusy(false);
    }
  };

  const canSubmit = email.trim() !== '' && password !== ''
    && (mode === 'sign-in' || displayName.trim() !== '');

  return (
    <Screen>
      <KeyboardAvoidingView
        style={{ flex: 1 }}
        behavior={Platform.OS === 'ios' ? 'padding' : undefined}
      >
        <ScrollView
          contentContainerStyle={{
            padding: space.xl,
            paddingTop: insets.top + space.xxl,
            flexGrow: 1,
            justifyContent: 'center',
          }}
          keyboardShouldPersistTaps="handled"
        >
          <Title>{t('signIn.brand')}</Title>
          <Spacer size={space.xs} />
          <Body muted>{mode === 'sign-in' ? t('signIn.signInIntro') : t('signIn.signUpIntro')}</Body>
          <Spacer size={space.xl} />

          {error ? (
            <>
              <Banner message={error} tone="danger" />
              <Spacer />
            </>
          ) : null}

          <Field
            label={t('signIn.email')}
            value={email}
            onChangeText={setEmail}
            keyboardType="email-address"
            autoCapitalize="none"
            placeholder={t('signIn.emailPlaceholder')}
          />
          <Spacer />
          <Field
            label={t('signIn.password')}
            value={password}
            onChangeText={setPassword}
            secure
            autoCapitalize="none"
          />

          {mode === 'sign-up' ? (
            <>
              <Spacer />
              <Field
                label={t('signIn.yourName')}
                value={displayName}
                onChangeText={setDisplayName}
                autoCapitalize="words"
              />
              <Spacer />
              <Field
                label={t('signIn.businessName')}
                value={businessName}
                onChangeText={setBusinessName}
                autoCapitalize="words"
                hint={t('signIn.businessHint')}
              />
              <Spacer />
              <Field
                label={t('signIn.currency')}
                value={currency}
                onChangeText={setCurrency}
                autoCapitalize="none"
                hint={t('signIn.currencyHint')}
              />
            </>
          ) : null}

          <Spacer size={space.xl} />
          <Button
            label={mode === 'sign-in' ? t('signIn.signIn') : t('signIn.createAccount')}
            tone="primary"
            onPress={() => { void submit(); }}
            disabled={!canSubmit}
            busy={busy}
          />
          <Spacer size={space.sm} />
          <Button
            label={mode === 'sign-in' ? t('signIn.startNew') : t('signIn.haveAccount')}
            tone="quiet"
            onPress={() => { setMode(mode === 'sign-in' ? 'sign-up' : 'sign-in'); setError(null); }}
          />

          <Spacer size={space.xl} />
          <View style={{ alignItems: 'center' }}>
            <Caption>{t('signIn.needsConnection')}</Caption>
          </View>
        </ScrollView>
      </KeyboardAvoidingView>
    </Screen>
  );
}

/** Turns the server's machine code into something a person can act on. */
function describe(cause: unknown, { t }: I18n): string {
  if (cause instanceof NetworkError) return t('signIn.noConnection');
  if (cause instanceof ApiError) {
    switch (cause.code) {
      case ErrorCode.InvalidCredentials:
        return t('signIn.badCredentials');
      case ErrorCode.Validation: {
        const fields = cause.fields ? Object.values(cause.fields) : [];
        return fields[0] ?? cause.message;
      }
      default:
        return cause.message;
    }
  }
  return cause instanceof Error ? cause.message : t('common.somethingWrong');
}

function deviceTimezone(): string | undefined {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || undefined;
  } catch {
    return undefined;
  }
}
