import React from 'react';
import { View } from 'react-native';
import { useRouter } from 'expo-router';

import { EmptyState } from '@/ui/components';
import { useTheme } from '@/ui/theme';

export default function NotFound() {
  const router = useRouter();
  const { c } = useTheme();
  return (
    <View style={{ flex: 1, backgroundColor: c.bg, justifyContent: 'center' }}>
      <EmptyState
        icon="info"
        title="Nothing here"
        message="That screen does not exist."
        action="Back to Today"
        onAction={() => router.replace('/')}
      />
    </View>
  );
}
