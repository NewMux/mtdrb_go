/**
 * The trainer's name, email and password.
 *
 * Online-only, and says so through the same "needs a connection" answer as
 * every other server screen: an email change needs the server to check the
 * address is free, and a password change is the server's to make.
 */

import React, { useEffect, useState } from 'react';

import type { Profile } from '@/api/account';
import { describeError } from '@/api/describe';
import { useT } from '@/i18n';
import { useApp, useRemote } from '@/state/app';
import { Banner, Button, Caption, Field, Spacer } from '@/ui/components';
import { FormPage, Section } from '@/ui/form-page';
import { useToast } from '@/ui/overlay';
import { space } from '@/ui/theme';

export default function ProfileScreen() {
  const i18n = useT();
  const { t, date } = i18n;
  const { api, updateAccount } = useApp();
  const toast = useToast();
  const remote = useRemote((client) => client.get<Profile>('/v1/session/profile'));
  const profile = remote.data;

  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [emailPassword, setEmailPassword] = useState('');
  const [saving, setSaving] = useState(false);
  const [profileError, setProfileError] = useState<string | null>(null);

  const [current, setCurrent] = useState('');
  const [next, setNext] = useState('');
  const [again, setAgain] = useState('');
  const [changing, setChanging] = useState(false);
  const [passwordError, setPasswordError] = useState<string | null>(null);

  useEffect(() => {
    if (!profile) return;
    setName(profile.display_name);
    setEmail(profile.email);
  }, [profile]);

  const emailChanged = profile !== null && email.trim().toLowerCase() !== profile.email;
  const nameChanged = profile !== null && name.trim() !== profile.display_name;

  const saveProfile = async () => {
    setSaving(true);
    setProfileError(null);
    try {
      const updated = await api.patch<Profile>('/v1/session/profile', {
        ...(nameChanged ? { display_name: name.trim() } : {}),
        ...(emailChanged ? { email: email.trim(), current_password: emailPassword } : {}),
      });
      await updateAccount({ display_name: updated.display_name, email: updated.email });
      setEmailPassword('');
      remote.reload();
      toast(t('settings.saved'), 'success');
    } catch (cause) {
      setProfileError(describeError(cause, i18n));
    } finally {
      setSaving(false);
    }
  };

  const changePassword = async () => {
    setChanging(true);
    setPasswordError(null);
    try {
      await api.post('/v1/session/password', { current_password: current, new_password: next });
      setCurrent('');
      setNext('');
      setAgain('');
      remote.reload();
      toast(t('profile.passwordChanged'), 'success');
    } catch (cause) {
      setPasswordError(describeError(cause, i18n));
    } finally {
      setChanging(false);
    }
  };

  if (remote.offline) {
    return <FormPage><Banner tone="muted" message={t('errors.offline')} /></FormPage>;
  }

  return (
    <FormPage refreshing={remote.loading} onRefresh={remote.reload}>
      <Section title={t('settings.profile')}>
        {profileError ? <><Banner tone="danger" message={profileError} /><Spacer /></> : null}
        <Field label={t('profile.name')} value={name} onChangeText={setName} autoCapitalize="words" />
        <Spacer />
        <Field label={t('profile.email')} value={email} onChangeText={setEmail} keyboardType="email-address" autoCapitalize="none" />
        {emailChanged ? (
          <>
            <Spacer />
            <Field
              label={t('profile.currentPassword')}
              value={emailPassword}
              onChangeText={setEmailPassword}
              secure
              autoCapitalize="none"
              hint={t('profile.emailNeedsPassword')}
            />
          </>
        ) : null}
        <Spacer size={space.lg} />
        <Button
          label={t('settings.save')}
          tone="primary"
          onPress={() => { void saveProfile(); }}
          disabled={!profile || (!nameChanged && !emailChanged) || name.trim() === '' || (emailChanged && emailPassword === '')}
          busy={saving}
        />
      </Section>

      <Section title={t('profile.passwordTitle')} detail={t('profile.passwordBody')}>
        {passwordError ? <><Banner tone="danger" message={passwordError} /><Spacer /></> : null}
        <Field label={t('profile.currentPassword')} value={current} onChangeText={setCurrent} secure autoCapitalize="none" />
        <Spacer />
        <Field label={t('profile.newPassword')} value={next} onChangeText={setNext} secure autoCapitalize="none" />
        <Spacer />
        <Field
          label={t('profile.confirmPassword')}
          value={again}
          onChangeText={setAgain}
          secure
          autoCapitalize="none"
          error={again !== '' && again !== next ? t('profile.mismatch') : null}
        />
        <Spacer size={space.lg} />
        <Button
          label={t('profile.changePassword')}
          onPress={() => { void changePassword(); }}
          disabled={current === '' || next === '' || next !== again}
          busy={changing}
        />
        {profile?.password_changed_at ? (
          <>
            <Spacer size={space.sm} />
            <Caption>{t('profile.lastChanged', { date: date(profile.password_changed_at, 'medium') })}</Caption>
          </>
        ) : null}
      </Section>
    </FormPage>
  );
}
