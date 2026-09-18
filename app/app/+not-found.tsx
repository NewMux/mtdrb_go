import React from 'react';
import { Link } from 'expo-router';
import { View } from 'react-native';

import { Body, Screen, Spacer, Title } from '@/ui/components';
import { colors, space } from '@/ui/theme';

export default function NotFound() {
  return (
    <Screen>
      <View style={{ padding: space.xl }}>
        <Title>Nothing here</Title>
        <Spacer size={space.sm} />
        <Body muted>That screen does not exist.</Body>
        <Spacer />
        <Link href="/" style={{ color: colors.accent, fontSize: 16 }}>Back to today</Link>
      </View>
    </Screen>
  );
}
