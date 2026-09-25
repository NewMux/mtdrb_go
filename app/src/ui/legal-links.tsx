/**
 * The privacy policy and terms, served by the API at stable URLs (the same
 * ones the store listings give), opened in the browser.
 */

import React from 'react';
import { View } from 'react-native';
import * as Linking from 'expo-linking';

import { useT } from '@/i18n';
import { baseUrl } from '@/state/app';
import { TextButton } from './components';
import { space } from './theme';

export function LegalLinks() {
  const { t } = useT();
  const open = (page: 'privacy' | 'terms') => { void Linking.openURL(`${baseUrl()}/legal/${page}`); };
  return (
    <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: space.lg, justifyContent: 'center' }}>
      <TextButton label={t('legal.privacy')} onPress={() => open('privacy')} />
      <TextButton label={t('legal.terms')} onPress={() => open('terms')} />
    </View>
  );
}
