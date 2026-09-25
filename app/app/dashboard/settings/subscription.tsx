/**
 * The plan: what it is, what it includes, and how much of it is used.
 *
 * No payment happens here yet, and the screen does not pretend otherwise.
 * Changing plan is an email to the team, answered the same day; cancelling
 * and un-cancelling are real, because the server already honours them.
 */

import React from 'react';
import { View } from 'react-native';
import * as Linking from 'expo-linking';

import type { Subscription } from '@/api/account';
import { describeError } from '@/api/describe';
import { useT, type I18n, type TKey } from '@/i18n';
import { useApp, useRemote } from '@/state/app';
import {
  Banner, Body, Button, Caption, Card, Divider, Heading, Progress, Row, Spacer, Title, UpgradePrompt,
} from '@/ui/components';
import { FormPage, Section } from '@/ui/form-page';
import { SHOW_UNBUILT } from '@/ui/nav';
import { Icon } from '@/ui/icon';
import { useConfirm, useToast } from '@/ui/overlay';
import { space } from '@/ui/theme';
import { useTheme } from '@/ui/theming';

const FEATURES = ['shop', 'analytics', 'insights', 'automations'] as const;
// Plans are sold by invoice for now; this is who to write to.
const SALES = process.env.EXPO_PUBLIC_SUPPORT_EMAIL || 'hello@coachpulse.io';

export default function SubscriptionScreen() {
  const i18n = useT();
  const { t } = i18n;
  const { api, account } = useApp();
  const confirm = useConfirm();
  const toast = useToast();
  const { colors } = useTheme();
  const remote = useRemote((client) => client.get<Subscription>('/v1/subscription'));
  const sub = remote.data;
  const owner = account?.role === 'owner';

  if (remote.offline) {
    return <FormPage><Banner tone="muted" message={t('errors.offline')} /></FormPage>;
  }
  if (!sub) return <FormPage refreshing={remote.loading} onRefresh={remote.reload}>{null}</FormPage>;

  const change = async (cancel: boolean) => {
    if (cancel) {
      const choice = await confirm({
        title: t('subscription.cancelTitle'),
        message: t('subscription.cancelBody'),
        actions: [
          { value: 'keep', label: t('subscription.resume') },
          { value: 'cancel', label: t('subscription.cancel'), tone: 'danger' },
        ],
      });
      if (choice !== 'cancel') return;
    }
    try {
      await api.post<Subscription>(cancel ? '/v1/subscription/cancel' : '/v1/subscription/resume', {});
      remote.reload();
    } catch (cause) {
      toast(describeError(cause, i18n), 'danger');
    }
  };

  const contact = () => {
    const subject = encodeURIComponent(`CoachPulse plan — ${account?.email ?? ''}`);
    void Linking.openURL(`mailto:${SALES}?subject=${subject}`);
  };

  return (
    <FormPage refreshing={remote.loading} onRefresh={remote.reload}>
      {sub.lapsed ? <><Banner tone="danger" message={t('plan.readOnlyBody')} /><Spacer /></> : null}

      <Card tone="accent">
        <Caption tone="onAccent">{t('subscription.plan')}</Caption>
        <Spacer size={space.xs} />
        <Title>{planName(sub.plan, i18n)}</Title>
        <Spacer size={space.sm} />
        <Body onAccent>{status(sub, i18n)}</Body>
      </Card>
      <Spacer size={space.xl} />

      <Section title={t('subscription.usage')}>
        <Usage label={t('subscription.activeClients')} used={sub.usage.active_clients ?? 0} max={sub.limits.active_clients} i18n={i18n} />
        <Spacer size={space.lg} />
        <Usage label={t('subscription.locations')} used={sub.usage.locations ?? 0} max={sub.limits.locations} i18n={i18n} />
      </Section>

      {/* The plan features are modules not built yet; listing them would sell
          what does not exist. */}
      {SHOW_UNBUILT ? <Section title={t('subscription.features')}>
        {FEATURES.map((feature, index) => {
          const included = sub.features.includes(feature);
          return (
            <View key={feature}>
              {index > 0 ? <><Spacer size={space.sm} /><Divider /><Spacer size={space.sm} /></> : null}
              <Row style={{ gap: space.md }}>
                <Icon name={included ? 'success' : 'locked'} size={18} color={included ? colors.success : colors.inkMuted} />
                <View style={{ flex: 1 }}>
                  <Body muted={!included}>{t(`subscription.feature.${feature}` as TKey)}</Body>
                  {!included ? <Caption>{t('subscription.notIncluded')}</Caption> : null}
                </View>
              </Row>
            </View>
          );
        })}
      </Section> : null}

      <UpgradePrompt
        title={t('subscription.upgradeTitle')}
        detail={t('subscription.upgradeBody')}
        actionLabel={t('subscription.contact')}
        onPress={contact}
      />

      {owner && sub.plan !== 'trial' && !sub.lapsed ? (
        <>
          <Spacer size={space.xl} />
          {sub.cancel_at_period_end ? (
            <Button label={t('subscription.resume')} tone="primary" onPress={() => { void change(false); }} />
          ) : (
            <Button label={t('subscription.cancel')} tone="danger" onPress={() => { void change(true); }} />
          )}
        </>
      ) : null}
    </FormPage>
  );
}

function planName(plan: Subscription['plan'], { t }: I18n): string {
  return plan === 'trial' ? t('plan.trial') : plan === 'starter' ? t('plan.starter') : t('plan.pro');
}

function status(sub: Subscription, i18n: I18n): string {
  const { t, date } = i18n;
  if (sub.plan === 'trial') {
    return sub.lapsed ? t('subscription.trialEnded') : t('subscription.trialLeft', { count: sub.trial_days_left ?? 0 });
  }
  if (sub.status === 'past_due') return t('subscription.pastDue');
  if (sub.renews_on) {
    return sub.cancel_at_period_end
      ? t('subscription.cancelling', { date: date(sub.renews_on, 'long') })
      : t('subscription.renewsOn', { date: date(sub.renews_on, 'long') });
  }
  return '';
}

function Usage({ label, used, max, i18n }: { label: string; used: number; max: number | undefined; i18n: I18n }) {
  const { t, number } = i18n;
  return (
    <View>
      <Row style={{ justifyContent: 'space-between' }}>
        <Heading>{label}</Heading>
        <Caption>
          {max === undefined
            ? t('subscription.unlimited', { used: number(used) })
            : t('subscription.usedOf', { used: number(used), max: number(max) })}
        </Caption>
      </Row>
      {max !== undefined ? <><Spacer size={space.sm} /><Progress value={max > 0 ? Math.min(1, used / max) : 1} /></> : null}
    </View>
  );
}
