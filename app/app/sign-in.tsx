/**
 * Sign in, or start an account.
 *
 * The only screen in the app that needs the network to do anything, and the
 * only one that says so.
 *
 * Two detours hang off it. A right password on an account with two-step
 * sign-in comes back as a challenge, and the screen asks for the code in
 * place rather than sending the trainer somewhere else. And "forgot your
 * password" sends a reset link, answering the same way whether or not the
 * address has an account.
 */

import React, { useState } from 'react';
import { KeyboardAvoidingView, Platform, ScrollView, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { ApiError } from '@/api/client';
import { describeError } from '@/api/describe';
import { useT, type I18n } from '@/i18n';
import { useApp } from '@/state/app';
import {
  Banner, Body, Button, Caption, Field, Screen, Spacer, TextButton, Title,
} from '@/ui/components';
import { LegalLinks } from '@/ui/legal-links';
import { space } from '@/ui/theme';

type Mode = 'sign-in' | 'sign-up' | 'mfa' | 'forgot' | 'forgot-sent';

export default function SignInScreen() {
  const { api, signIn, signUp, completeMfa } = useApp();
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
  const [mfaToken, setMfaToken] = useState<string | null>(null);
  const [code, setCode] = useState('');

  const go = (next: Mode) => { setMode(next); setError(null); };

  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      if (mode === 'sign-in') {
        try {
          await signIn(email.trim(), password);
        } catch (cause) {
          const challenge = cause instanceof ApiError && cause.code === 'mfa_required'
            ? cause.meta?.mfa_token : undefined;
          if (typeof challenge !== 'string') throw cause;
          setMfaToken(challenge);
          setCode('');
          go('mfa');
        }
      } else if (mode === 'mfa') {
        await completeMfa(mfaToken ?? '', code.trim());
      } else if (mode === 'forgot') {
        await api.forgotPassword(email.trim());
        go('forgot-sent');
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

  const canSubmit =
    mode === 'mfa' ? code.trim().length >= 6 :
    mode === 'forgot' ? email.trim() !== '' :
    email.trim() !== '' && password !== '' && (mode === 'sign-in' || displayName.trim() !== '');

  const intro =
    mode === 'sign-in' ? t('signIn.signInIntro') :
    mode === 'sign-up' ? t('signIn.signUpIntro') :
    mode === 'mfa' ? t('signIn.mfaBody') : t('signIn.forgotBody');

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
          <Title>
            {mode === 'mfa' ? t('signIn.mfaTitle') : mode === 'forgot' || mode === 'forgot-sent' ? t('signIn.forgotTitle') : t('signIn.brand')}
          </Title>
          <Spacer size={space.xs} />
          {mode === 'forgot-sent' ? null : <Body muted>{intro}</Body>}
          <Spacer size={space.xl} />

          {error ? (
            <>
              <Banner message={error} tone="danger" />
              <Spacer />
            </>
          ) : null}

          {mode === 'forgot-sent' ? (
            <>
              <Banner tone="success" message={t('signIn.forgotSent')} />
              <Spacer size={space.xl} />
              <Button label={t('signIn.back')} onPress={() => go('sign-in')} />
            </>
          ) : mode === 'mfa' ? (
            <>
              <Field
                label={t('signIn.code')}
                value={code}
                onChangeText={setCode}
                keyboardType="number-pad"
                autoCapitalize="none"
                hint={t('signIn.mfaHint')}
              />
              <Spacer size={space.xl} />
              <Button
                label={t('signIn.verify')}
                tone="primary"
                onPress={() => { void submit(); }}
                disabled={!canSubmit}
                busy={busy}
              />
              <Spacer size={space.sm} />
              <Button label={t('signIn.back')} tone="quiet" onPress={() => { setMfaToken(null); go('sign-in'); }} />
            </>
          ) : mode === 'forgot' ? (
            <>
              <Field
                label={t('signIn.email')}
                value={email}
                onChangeText={setEmail}
                keyboardType="email-address"
                autoCapitalize="none"
                placeholder={t('signIn.emailPlaceholder')}
              />
              <Spacer size={space.xl} />
              <Button
                label={t('signIn.sendLink')}
                tone="primary"
                onPress={() => { void submit(); }}
                disabled={!canSubmit}
                busy={busy}
              />
              <Spacer size={space.sm} />
              <Button label={t('signIn.back')} tone="quiet" onPress={() => go('sign-in')} />
            </>
          ) : (
            <>
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
              {mode === 'sign-in' ? (
                <View style={{ alignItems: 'flex-start', marginTop: space.sm }}>
                  <TextButton label={t('signIn.forgot')} onPress={() => go('forgot')} />
                </View>
              ) : null}

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

              {mode === 'sign-up' ? (
                <><Spacer /><Caption>{t('legal.agree')}</Caption></>
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
                onPress={() => go(mode === 'sign-in' ? 'sign-up' : 'sign-in')}
              />
              <Spacer size={space.lg} />
              <LegalLinks />
            </>
          )}

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
function describe(cause: unknown, i18n: I18n): string {
  return describeError(cause, i18n, 'signIn.noConnection');
}

function deviceTimezone(): string | undefined {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || undefined;
  } catch {
    return undefined;
  }
}
