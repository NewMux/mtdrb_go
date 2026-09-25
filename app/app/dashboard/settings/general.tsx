/**
 * Language and appearance — for this device.
 *
 * These are the trainer's, not the practice's: one person can read the app
 * in Arabic on their phone and in English on the studio's laptop. What
 * invoices and emails are written in is a practice setting, under Business.
 */

import React from 'react';

import { useT } from '@/i18n';
import { usePreferences } from '@/state/preferences';
import { Banner, Caption, Label, SegmentedChoice, Spacer } from '@/ui/components';
import { FormPage, Section } from '@/ui/form-page';
import { space } from '@/ui/theme';

export default function GeneralSettingsScreen() {
  const { t } = useT();
  const preferences = usePreferences();

  return (
    <FormPage>
      <Caption>{t('settings.deviceOnly')}</Caption>
      <Spacer />
      <Section title={t('settings.general')}>
        <Label>{t('syncScreen.language')}</Label>
        <Spacer size={space.sm} />
        <SegmentedChoice
          options={[{ value: 'en', label: t('prefs.english') }, { value: 'ar', label: t('prefs.arabic') }] as const}
          value={preferences.locale}
          onChange={preferences.setLocale}
        />
        {preferences.restartPending ? (
          <>
            <Spacer size={space.sm} />
            <Banner
              tone="muted"
              message={t('prefs.restartBody')}
              action={{ label: t('prefs.restart'), onPress: preferences.restart }}
            />
          </>
        ) : null}
        <Spacer />
        <Label>{t('syncScreen.digits')}</Label>
        <Spacer size={space.sm} />
        <SegmentedChoice
          options={[{ value: 'latn', label: t('prefs.latin') }, { value: 'arab', label: t('prefs.arabicIndic') }] as const}
          value={preferences.prefs.digits}
          onChange={preferences.setDigits}
        />
        <Spacer />
        <Label>{t('syncScreen.theme')}</Label>
        <Spacer size={space.sm} />
        <SegmentedChoice
          options={[
            { value: 'system', label: t('prefs.system') },
            { value: 'light', label: t('prefs.light') },
            { value: 'dark', label: t('prefs.dark') },
          ] as const}
          value={preferences.prefs.theme}
          onChange={preferences.setTheme}
        />
      </Section>
    </FormPage>
  );
}
