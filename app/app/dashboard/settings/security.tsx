/**
 * Two-step sign-in, and the devices signed in to the account.
 *
 * Two-step is switched on in two moves on purpose: the server hands over a
 * secret, and only a code read back from the trainer's authenticator app
 * turns it on. A botched scan therefore costs a retry, never a locked
 * account. The recovery codes are shown once, at that moment, with ways to
 * keep them — the server holds only their hashes.
 */

import React, { useState } from 'react';
import { Platform, Share, View } from 'react-native';
import * as Linking from 'expo-linking';
import * as Clipboard from 'expo-clipboard';

import type { Device, MFASetup, Profile } from '@/api/account';
import { describeError } from '@/api/describe';
import { describeUserAgent } from '@/features/devices';
import { practiceSettings } from '@/features/plan';
import { useT, type I18n } from '@/i18n';
import { useApp, useQuery, useRemote } from '@/state/app';
import {
  Banner, Body, Button, Caption, Card, Divider, Field, Label, Pill, Row, Spacer, TextButton,
} from '@/ui/components';
import { FormPage, Section } from '@/ui/form-page';
import { Icon } from '@/ui/icon';
import { useConfirm, useToast } from '@/ui/overlay';
import { QRCode } from '@/ui/qr';
import { space } from '@/ui/theme';
import { useTheme } from '@/ui/theming';

export default function SecurityScreen() {
  const i18n = useT();
  const { t } = i18n;
  const { api, account, deleteAccount } = useApp();
  const owner = account?.role === 'owner';
  const [deleting, setDeleting] = useState(false);
  const [deletePassword, setDeletePassword] = useState('');
  const toast = useToast();
  const confirm = useConfirm();
  const { colors } = useTheme();

  const profile = useRemote((client) => client.get<Profile>('/v1/session/profile'));
  const devices = useRemote((client) => client.get<{ devices: Device[] }>('/v1/session/devices'));
  const timeout = useQuery(practiceSettings).data?.session_timeout_days ?? null;

  const [setup, setSetup] = useState<MFASetup | null>(null);
  const [code, setCode] = useState('');
  const [recovery, setRecovery] = useState<string[] | null>(null);
  const [disabling, setDisabling] = useState(false);
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const run = async (work: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    try {
      await work();
    } catch (cause) {
      setError(describeError(cause, i18n));
    } finally {
      setBusy(false);
    }
  };

  const begin = () => run(async () => {
    setSetup(await api.post<MFASetup>('/v1/session/mfa/setup', {}));
    setCode('');
  });

  const enable = () => run(async () => {
    const result = await api.post<{ recovery_codes: string[] }>('/v1/session/mfa/enable', { code: code.trim() });
    setSetup(null);
    setRecovery(result.recovery_codes);
    profile.reload();
  });

  const disable = () => run(async () => {
    await api.post('/v1/session/mfa/disable', { password });
    setDisabling(false);
    setPassword('');
    profile.reload();
  });

  const signOutDevice = async (device: Device) => {
    const choice = await confirm({
      title: t('security.signOutDeviceTitle'),
      message: t('security.signOutDeviceBody'),
      actions: [
        { value: 'cancel', label: t('common.cancel') },
        { value: 'go', label: t('security.signOutDevice'), tone: 'danger' },
      ],
    });
    if (choice !== 'go') return;
    await run(async () => {
      await api.delete(`/v1/session/devices/${device.id}`);
      devices.reload();
    });
  };

  const signOutOthers = async () => {
    const choice = await confirm({
      title: t('security.signOutOthersTitle'),
      message: t('security.signOutOthersBody'),
      actions: [
        { value: 'cancel', label: t('common.cancel') },
        { value: 'go', label: t('security.signOutOthers'), tone: 'danger' },
      ],
    });
    if (choice !== 'go') return;
    await run(async () => {
      await api.post('/v1/session/devices/sign-out-others', {});
      devices.reload();
    });
  };

  // Two steps, like turning two-step off: the password, then a last
  // question that says plainly what goes.
  const removeAccount = async () => {
    const choice = await confirm({
      title: t('security.deleteConfirmTitle'),
      message: owner ? t('security.deleteConfirmOwner', { days: PURGE_DAYS }) : t('security.deleteConfirmMember'),
      actions: [
        { value: 'cancel', label: t('common.cancel') },
        { value: 'go', label: t('security.deleteConfirm'), tone: 'danger' },
      ],
    });
    if (choice !== 'go') return;
    await run(async () => {
      await deleteAccount(deletePassword);
      toast(t('security.deleted'), 'success');
    });
  };

  const copy = async (text: string) => {
    await Clipboard.setStringAsync(text);
    toast(t('security.copied'), 'success');
  };

  if (profile.offline || devices.offline) {
    return <FormPage><Banner tone="muted" message={t('errors.offline')} /></FormPage>;
  }

  const enabled = profile.data?.mfa_enabled ?? false;
  const list = devices.data?.devices ?? [];
  const others = list.filter((d) => !d.current);

  return (
    <FormPage refreshing={profile.loading || devices.loading} onRefresh={() => { profile.reload(); devices.reload(); }}>
      {error ? <><Banner tone="danger" message={error} /><Spacer /></> : null}

      <Section
        title={t('security.twoStepTitle')}
        detail={t('security.twoStepBody')}
        trailing={profile.data ? <Pill label={enabled ? t('security.on') : t('security.off')} tone={enabled ? 'success' : 'muted'} /> : null}
      >
        {recovery ? (
          <>
            <Label>{t('security.recoveryTitle')}</Label>
            <Spacer size={space.xs} />
            <Body muted>{t('security.recoveryBody')}</Body>
            <Spacer />
            <Card tone="raised">
              <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: space.md }}>
                {recovery.map((c) => (
                  // Codes are Latin whatever the language, and read left to right.
                  <Body key={c} style={{ fontVariant: ['tabular-nums'], writingDirection: 'ltr', minWidth: 120 }}>{c}</Body>
                ))}
              </View>
            </Card>
            <Spacer />
            <Row style={{ flexWrap: 'wrap', gap: space.sm }}>
              <Button compact icon="copy" label={t('security.copyCodes')} onPress={() => { void copy(recovery.join('\n')); }} />
              {Platform.OS !== 'web' ? (
                <Button compact icon="share" label={t('security.shareCodes')} onPress={() => { void Share.share({ message: recovery.join('\n') }); }} />
              ) : null}
              <Button compact tone="primary" label={t('security.savedCodes')} onPress={() => setRecovery(null)} />
            </Row>
          </>
        ) : setup ? (
          <>
            <Label>{t('security.scanTitle')}</Label>
            <Spacer size={space.xs} />
            <Caption>{t('security.scanBody')}</Caption>
            <Spacer />
            <View style={{ alignItems: 'center' }}>
              <QRCode value={setup.otpauth_uri} label={t('security.qrLabel')} size={184} />
            </View>
            <Spacer />
            <Caption>{t('security.secretLabel')}</Caption>
            <Spacer size={space.xs} />
            <Row style={{ justifyContent: 'space-between', flexWrap: 'wrap' }}>
              <Body style={{ writingDirection: 'ltr', letterSpacing: 1 }}>{groupSecret(setup.secret)}</Body>
              <TextButton label={t('security.copySecret')} onPress={() => { void copy(setup.secret); }} />
            </Row>
            {Platform.OS !== 'web' ? (
              <>
                <Spacer size={space.sm} />
                <TextButton label={t('security.openInApp')} onPress={() => { void Linking.openURL(setup.otpauth_uri); }} />
              </>
            ) : null}
            <Spacer />
            <Field label={t('security.enterCode')} value={code} onChangeText={setCode} keyboardType="number-pad" autoCapitalize="none" />
            <Spacer />
            <Row style={{ gap: space.sm }}>
              <Button tone="primary" label={t('security.confirm')} onPress={() => { void enable(); }} disabled={code.trim().length < 6} busy={busy} />
              <Button tone="quiet" label={t('common.cancel')} onPress={() => setSetup(null)} />
            </Row>
          </>
        ) : enabled ? (
          disabling ? (
            <>
              <Body muted>{t('security.turnOffBody')}</Body>
              <Spacer />
              <Field label={t('security.password')} value={password} onChangeText={setPassword} secure autoCapitalize="none" />
              <Spacer />
              <Row style={{ gap: space.sm }}>
                <Button tone="danger" label={t('security.turnOff')} onPress={() => { void disable(); }} disabled={password === ''} busy={busy} />
                <Button tone="quiet" label={t('common.cancel')} onPress={() => { setDisabling(false); setPassword(''); }} />
              </Row>
            </>
          ) : (
            <Button tone="quiet" label={t('security.turnOff')} onPress={() => setDisabling(true)} />
          )
        ) : (
          <Button tone="primary" icon="key" label={t('security.setUp')} onPress={() => { void begin(); }} busy={busy} disabled={!profile.data} />
        )}
      </Section>

      <Section title={t('security.devicesTitle')} detail={t('security.devicesBody')}>
        {list.map((device, index) => (
          <View key={device.id}>
            {index > 0 ? <><Spacer size={space.md} /><Divider /><Spacer size={space.md} /></> : null}
            <Row style={{ gap: space.md }}>
              <Icon name={deviceIcon(device)} size={22} color={colors.inkMuted} />
              <View style={{ flex: 1 }}>
                <Body>{deviceName(device, i18n)}</Body>
                <Caption>{t('security.lastSeen', { time: i18n.date(device.last_seen_at, 'medium') })}</Caption>
              </View>
              {device.current ? (
                <Pill label={t('security.thisDevice')} tone="accent" />
              ) : (
                <TextButton label={t('security.signOutDevice')} onPress={() => { void signOutDevice(device); }} />
              )}
            </Row>
          </View>
        ))}
        <Spacer />
        {others.length > 0 ? (
          <Button tone="danger" label={t('security.signOutOthers')} onPress={() => { void signOutOthers(); }} />
        ) : (
          <Caption>{t('security.noOthers')}</Caption>
        )}
        {timeout ? <><Spacer size={space.sm} /><Caption>{t('security.timeout', { count: timeout })}</Caption></> : null}
      </Section>

      <Section
        title={t('security.deleteTitle')}
        detail={owner ? t('security.deleteOwnerBody', { days: PURGE_DAYS }) : t('security.deleteMemberBody')}
      >
        {deleting ? (
          <>
            <Field label={t('security.password')} value={deletePassword} onChangeText={setDeletePassword} secure autoCapitalize="none" />
            <Spacer />
            <Row style={{ gap: space.sm }}>
              <Button tone="danger" label={t('security.deleteButton')} onPress={() => { void removeAccount(); }} disabled={deletePassword === ''} busy={busy} />
              <Button tone="quiet" label={t('common.cancel')} onPress={() => { setDeleting(false); setDeletePassword(''); }} />
            </Row>
          </>
        ) : (
          <Button tone="danger" label={t('security.deleteButton')} onPress={() => setDeleting(true)} />
        )}
      </Section>
    </FormPage>
  );
}

/** The server's grace period before a deleted practice is purged. */
const PURGE_DAYS = 30;

/** "JBSW Y3DP EHPK 3PXP": easier to type into a phone from a screen. */
function groupSecret(secret: string): string {
  return secret.replace(/(.{4})(?=.)/g, '$1 ');
}

function deviceIcon(device: Device) {
  return describeUserAgent(device.user_agent)?.kind === 'computer' ? 'laptop' : 'phoneDevice';
}

function deviceName(device: Device, { t }: I18n): string {
  const name = describeUserAgent(device.user_agent);
  if (!name) return t('security.deviceUnknown');
  if (!name.browser) return name.system ? `${t('security.deviceApp')} · ${name.system}` : t('security.deviceApp');
  return name.system ? t('security.browserOn', { browser: name.browser, system: name.system }) : name.browser;
}
