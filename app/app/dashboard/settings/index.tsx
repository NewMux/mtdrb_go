/**
 * Settings: where each part of the account lives.
 *
 * A list rather than one long form. The trainer who opens this is after one
 * thing — the password, the working hours, what the plan includes — and a
 * list gets them to it in one tap on a phone or a glance on a desk.
 */

import React from 'react';
import { View } from 'react-native';
import { useRouter } from 'expo-router';

import { planState, practiceSettings } from '@/features/plan';
import { useT } from '@/i18n';
import { useApp, useQuery } from '@/state/app';
import { Button, Caption, NavRow, Pill, Spacer } from '@/ui/components';
import { useConfirm } from '@/ui/overlay';
import { Page } from '@/ui/page';
import { space } from '@/ui/theme';

export default function SettingsScreen() {
  const router = useRouter();
  const { t } = useT();
  const { account, signOut } = useApp();
  const confirm = useConfirm();
  const plan = planState(useQuery(practiceSettings).data);

  const planLabel = plan
    ? plan.plan === 'trial' ? t('plan.trial') : plan.plan === 'starter' ? t('plan.starter') : t('plan.pro')
    : null;

  const sections = [
    { href: '/dashboard/settings/general', icon: 'language', title: t('settings.general'), detail: t('settings.generalDetail') },
    { href: '/dashboard/settings/profile', icon: 'user', title: t('settings.profile'), detail: t('settings.profileDetail') },
    { href: '/dashboard/settings/security', icon: 'security', title: t('settings.security'), detail: t('settings.securityDetail') },
    { href: '/dashboard/settings/business', icon: 'business', title: t('settings.business'), detail: t('settings.businessDetail') },
  ] as const;

  const leave = async () => {
    const choice = await confirm({
      title: t('settings.signOut'),
      message: t('settings.signOutNote'),
      actions: [
        { value: 'cancel', label: t('common.cancel') },
        { value: 'sign-out', label: t('settings.signOut'), tone: 'danger' },
      ],
    });
    if (choice === 'sign-out') await signOut();
  };

  return (
    <Page title={t('settings.title')} subtitle={t('settings.subtitle')}>
      <View style={{ gap: space.sm, maxWidth: 720 }}>
        {sections.map((section) => (
          <NavRow
            key={section.href}
            icon={section.icon}
            title={section.title}
            detail={section.detail}
            onPress={() => router.push(section.href)}
          />
        ))}
        <NavRow
          icon="pro"
          title={t('settings.subscription')}
          detail={t('settings.subscriptionDetail')}
          trailing={planLabel ? <Pill label={plan?.lapsed ? t('sync.readOnly') : planLabel} tone={plan?.lapsed ? 'danger' : 'accent'} /> : null}
          onPress={() => router.push('/dashboard/settings/subscription')}
        />
      </View>

      <Spacer size={space.xxl} />
      <View style={{ maxWidth: 720 }}>
        <Caption>{account?.email ?? ''}</Caption>
        <Spacer size={space.sm} />
        <Button label={t('settings.signOut')} icon="signOut" tone="quiet" onPress={() => { void leave(); }} />
      </View>
    </Page>
  );
}
