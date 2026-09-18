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
import { useApp } from '@/state/app';
import {
  Banner, Body, Button, Caption, Field, Screen, Spacer, Title,
} from '@/ui/components';
import { space } from '@/ui/theme';

type Mode = 'sign-in' | 'sign-up';

export default function SignInScreen() {
  const { signIn, signUp } = useApp();
  const insets = useSafeAreaInsets();

  const [mode, setMode] = useState<Mode>('sign-in');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [businessName, setBusinessName] = useState('');
  const [currency, setCurrency] = useState('EUR');
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
          currency: currency.trim().toUpperCase() || 'EUR',
        });
      }
    } catch (cause) {
      setError(describe(cause));
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
          <Title>CoachPulse</Title>
          <Spacer size={space.xs} />
          <Body muted>
            {mode === 'sign-in'
              ? 'Sign in to your practice.'
              : 'Set up your practice. Your books start balanced.'}
          </Body>
          <Spacer size={space.xl} />

          {error ? (
            <>
              <Banner message={error} tone="danger" />
              <Spacer />
            </>
          ) : null}

          <Field
            label="Email"
            value={email}
            onChangeText={setEmail}
            keyboardType="email-address"
            autoCapitalize="none"
            placeholder="you@example.com"
          />
          <Spacer />
          <Field
            label="Password"
            value={password}
            onChangeText={setPassword}
            secure
            autoCapitalize="none"
          />

          {mode === 'sign-up' ? (
            <>
              <Spacer />
              <Field
                label="Your name"
                value={displayName}
                onChangeText={setDisplayName}
                autoCapitalize="words"
              />
              <Spacer />
              <Field
                label="Business name"
                value={businessName}
                onChangeText={setBusinessName}
                autoCapitalize="words"
                hint="Appears on the invoices your clients see."
              />
              <Spacer />
              <Field
                label="Currency"
                value={currency}
                onChangeText={setCurrency}
                autoCapitalize="none"
                hint="Three letters, e.g. EUR. Your ledger is kept in this currency."
              />
            </>
          ) : null}

          <Spacer size={space.xl} />
          <Button
            label={mode === 'sign-in' ? 'Sign in' : 'Create account'}
            tone="primary"
            onPress={() => { void submit(); }}
            disabled={!canSubmit}
            busy={busy}
          />
          <Spacer size={space.sm} />
          <Button
            label={mode === 'sign-in' ? 'Start a new practice' : 'I already have an account'}
            tone="quiet"
            onPress={() => { setMode(mode === 'sign-in' ? 'sign-up' : 'sign-in'); setError(null); }}
          />

          <Spacer size={space.xl} />
          <View style={{ alignItems: 'center' }}>
            <Caption>Signing in needs a connection. Everything after it does not.</Caption>
          </View>
        </ScrollView>
      </KeyboardAvoidingView>
    </Screen>
  );
}

/** Turns the server's machine code into something a person can act on. */
function describe(cause: unknown): string {
  if (cause instanceof NetworkError) {
    return 'No connection. Signing in is the one thing that needs one.';
  }
  if (cause instanceof ApiError) {
    switch (cause.code) {
      case ErrorCode.InvalidCredentials:
        return 'That email and password do not match an account.';
      case ErrorCode.Validation: {
        const fields = cause.fields ? Object.values(cause.fields) : [];
        return fields[0] ?? cause.message;
      }
      default:
        return cause.message;
    }
  }
  return cause instanceof Error ? cause.message : 'Something went wrong.';
}
