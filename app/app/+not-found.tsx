import React from 'react';
import { Link } from 'expo-router';
import { View } from 'react-native';

import { useT } from '@/i18n';
import { Body, Screen, Spacer, Title } from '@/ui/components';
import { space } from '@/ui/theme';
import { useTheme } from '@/ui/theming';

export default function NotFound() {
  const { t } = useT();
  const { colors } = useTheme();
  return (
    <Screen>
      <View style={{ padding: space.xl }}>
        <Title>{t('notFound.title')}</Title>
        <Spacer size={space.sm} />
        <Body muted>{t('notFound.body')}</Body>
        <Spacer />
        <Link href="/" style={{ color: colors.accentInk, fontSize: 16 }}>{t('notFound.back')}</Link>
      </View>
    </Screen>
  );
}
