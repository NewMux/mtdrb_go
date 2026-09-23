/**
 * The frame every module screen sits in.
 *
 * A title, an optional line under it, the screen's own actions on the far
 * side, and content capped in width — a table row stretched across a 2560px
 * monitor cannot be read. On a phone the sync badge sits in the header, since
 * there is no sidebar to carry it, and the content clears the tab bar.
 */

import React from 'react';
import { RefreshControl, ScrollView, View, type StyleProp, type ViewStyle } from 'react-native';

import { Caption, Row, Screen, Spacer, Title } from './components';
import { useLayout } from './layout';
import { PlanBanner } from './plan-banner';
import { SyncBadge } from './sync-badge';
import { space } from './theme';
import { useTheme } from './theming';

export function Page({
  title, subtitle, actions, children, refreshing, onRefresh, scroll = true, contentStyle, header: customHeader,
}: {
  title: string;
  /** Replaces the title block, as Today's greeting does. Actions and the sync badge still follow it. */
  header?: React.ReactNode;
  subtitle?: string;
  actions?: React.ReactNode;
  children: React.ReactNode;
  refreshing?: boolean;
  onRefresh?: () => void;
  scroll?: boolean;
  contentStyle?: StyleProp<ViewStyle>;
}) {
  const { wide, gutter, maxContentWidth } = useLayout();
  const { colors } = useTheme();

  const header = (
    <Row style={{ justifyContent: 'space-between', alignItems: 'flex-start', flexWrap: 'wrap' }}>
      {customHeader ?? (
        <View style={{ flexShrink: 1 }}>
          <Title>{title}</Title>
          {subtitle ? <Caption>{subtitle}</Caption> : null}
        </View>
      )}
      <Row style={{ gap: space.sm, flexWrap: 'wrap' }}>
        {actions}
        {wide ? null : <SyncBadge />}
      </Row>
    </Row>
  );

  const inner = (
    <View style={[{ width: '100%', maxWidth: maxContentWidth, alignSelf: 'center' }, contentStyle]}>
      {header}
      <Spacer size={space.lg} />
      <PlanBanner />
      {children}
    </View>
  );

  if (!scroll) {
    return <Screen><View style={{ flex: 1, padding: gutter, paddingTop: wide ? space.xxl : space.xl }}>{inner}</View></Screen>;
  }

  return (
    <Screen>
      <ScrollView
        contentContainerStyle={{
          padding: gutter,
          paddingTop: wide ? space.xxl : space.xl,
          // Clears the phone's tab bar and the add button above it.
          paddingBottom: wide ? space.xxl * 2 : 176,
        }}
        keyboardShouldPersistTaps="handled"
        refreshControl={onRefresh ? (
          <RefreshControl refreshing={Boolean(refreshing)} onRefresh={onRefresh} tintColor={colors.inkMuted} />
        ) : undefined}
      >
        {inner}
      </ScrollView>
    </Screen>
  );
}
