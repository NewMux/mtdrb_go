/**
 * Says so when the plan is about to stop, or has.
 *
 * On every module screen, because a trainer who finds out their account is
 * read-only from a refused booking has already lost the moment. It reads the
 * mirrored settings row, so it is right offline too.
 */

import React from 'react';
import { useRouter } from 'expo-router';

import { planState, practiceSettings, TRIAL_WARNING_DAYS } from '@/features/plan';
import { useT } from '@/i18n';
import { useQuery } from '@/state/app';
import { Banner, Spacer } from './components';
import { space } from './theme';

export function PlanBanner() {
  const router = useRouter();
  const { t } = useT();
  const state = planState(useQuery(practiceSettings).data);
  if (!state) return null;

  const action = { label: t('plan.seePlan'), onPress: () => router.push('/dashboard/settings/subscription') };
  if (state.lapsed) {
    return <><Banner tone="danger" message={t('plan.readOnlyTitle')} action={action} /><Spacer size={space.lg} /></>;
  }
  if (state.trialDaysLeft !== null && state.trialDaysLeft <= TRIAL_WARNING_DAYS) {
    return (
      <>
        <Banner tone="warning" message={t('plan.trialEnding', { count: state.trialDaysLeft })} action={action} />
        <Spacer size={space.lg} />
      </>
    );
  }
  return null;
}
