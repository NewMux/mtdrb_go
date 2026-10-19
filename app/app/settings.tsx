/**
 * Settings.
 *
 * Preferences are kept on this device — a trainer may want dark mode on the
 * gym tablet and light on their phone. The account and its money live on the
 * server.
 */

import React, { useState } from 'react';
import { View } from 'react-native';
import { useRouter } from 'expo-router';
import Constants from 'expo-constants';

import { DEMO, useApp } from '@/state/app';
import { Avatar, Body, HStack, ListRow, Section, Segmented, T, VStack } from '@/ui/components';
import { ActionSheet } from '@/ui/sheet';
import { describe } from '@/ui/sync-badge';
import { space } from '@/ui/theme';

export default function SettingsScreen() {
  const { account, prefs, setPrefs, sync, signOut } = useApp();
  const router = useRouter();
  const status = describe(sync);

  const [confirming, setConfirming] = useState(false);
  const signOutMessage = sync.pending > 0
    ? `${sync.pending} change${sync.pending === 1 ? ' is' : 's are'} still waiting to send. They stay on this device and send when you sign back in.`
    : 'You can sign back in at any time.';

  return (
    <Body>
      <Section>
        <View style={{ padding: space.lg }}>
          <HStack gap={space.lg}>
            <Avatar name={account?.display_name ?? 'Coach'} size={60} />
            <VStack gap={2} style={{ flex: 1 }}>
              <T variant="title3" weight="700">{account?.display_name ?? 'Coach'}</T>
              <T variant="subhead" tone="label2">{account?.email ?? ''}</T>
              <T variant="footnote" tone="label2">Ledger currency · {account?.currency ?? 'EUR'}</T>
            </VStack>
          </HStack>
        </View>
      </Section>

      <Section title="Appearance">
        <View style={{ padding: space.lg, gap: space.md }}>
          <Segmented
            options={[
              { value: 'system', label: 'Automatic' },
              { value: 'light', label: 'Light' },
              { value: 'dark', label: 'Dark' },
            ] as const}
            value={prefs.theme}
            onChange={(theme) => setPrefs({ theme })}
          />
        </View>
      </Section>

      <Section title="Training" footer="The rest timer starts by itself when a working set is ticked off. You can always add or skip time from the timer.">
        <View style={{ padding: space.lg, gap: space.lg }}>
          <VStack gap={space.sm}>
            <T variant="subhead" tone="label2">Weight units</T>
            <Segmented
              options={[{ value: 'kg', label: 'Kilograms' }, { value: 'lb', label: 'Pounds' }] as const}
              value={prefs.units}
              onChange={(units) => setPrefs({ units })}
            />
          </VStack>
          <VStack gap={space.sm}>
            <T variant="subhead" tone="label2">Default rest</T>
            <Segmented
              options={[
                { value: '60', label: '1:00' }, { value: '90', label: '1:30' },
                { value: '120', label: '2:00' }, { value: '180', label: '3:00' },
              ] as const}
              value={String(prefs.restSeconds) as '60' | '90' | '120' | '180'}
              onChange={(v) => setPrefs({ restSeconds: Number(v) })}
            />
          </VStack>
        </View>
      </Section>

      <Section title="Data">
        <ListRow
          first
          icon="sync"
          hue={status.hue === 'gray' ? 'gray' : status.hue}
          title="Sync"
          value={DEMO ? 'Demo' : status.label}
          onPress={() => router.push('/sync')}
        />
        <ListRow icon="lock" hue="gray" title="Stored on this device" value="Works offline" accessory="none" />
      </Section>

      <Section footer={DEMO ? 'This is a demo build with no server. Everything you do stays in this browser.' : undefined}>
        <ListRow first title="Sign Out" destructive accessory="none" onPress={() => setConfirming(true)} />
      </Section>

      <T variant="footnote" tone="label3" align="center" style={{ marginTop: space.xl }}>
        CoachPulse {Constants.expoConfig?.version ?? ''}
      </T>

      <ActionSheet
        visible={confirming}
        title="Sign out?"
        message={signOutMessage}
        onClose={() => setConfirming(false)}
        actions={[{ label: 'Sign Out', destructive: true, onPress: () => { void signOut(); } }]}
      />
    </Body>
  );
}
