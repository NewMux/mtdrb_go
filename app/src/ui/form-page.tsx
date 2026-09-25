/**
 * The frame for a screen that pushes inside a module — a settings section,
 * say — whose title is in the navigator's header rather than on the page.
 *
 * Narrower than a module page: a form stretched to a desk's width puts the
 * label a long way from its field.
 */

import React from 'react';
import { RefreshControl, ScrollView, View } from 'react-native';

import { Caption, Card, Heading, Screen, Spacer } from './components';
import { useLayout } from './layout';
import { space } from './theme';
import { useTheme } from './theming';

const FORM_WIDTH = 720;

export function FormPage({
  children, refreshing, onRefresh,
}: { children: React.ReactNode; refreshing?: boolean; onRefresh?: () => void }) {
  const { wide, gutter } = useLayout();
  const { colors } = useTheme();
  return (
    <Screen>
      <ScrollView
        contentContainerStyle={{ padding: gutter, paddingBottom: wide ? space.xxl * 2 : 176 }}
        keyboardShouldPersistTaps="handled"
        refreshControl={onRefresh ? (
          <RefreshControl refreshing={Boolean(refreshing)} onRefresh={onRefresh} tintColor={colors.inkMuted} />
        ) : undefined}
      >
        <View style={{ width: '100%', maxWidth: FORM_WIDTH, alignSelf: 'center' }}>{children}</View>
      </ScrollView>
    </Screen>
  );
}

/** A titled group of fields. */
export function Section({
  title, detail, children, trailing,
}: { title: string; detail?: string; children: React.ReactNode; trailing?: React.ReactNode }) {
  return (
    <View style={{ marginBottom: space.xl }}>
      <View style={{ flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: space.md }}>
        <View style={{ flex: 1 }}>
          <Heading>{title}</Heading>
          {detail ? <><Spacer size={space.xs} /><Caption>{detail}</Caption></> : null}
        </View>
        {trailing}
      </View>
      <Spacer size={space.sm} />
      <Card>{children}</Card>
    </View>
  );
}
