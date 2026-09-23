/**
 * The desk's navigation.
 *
 * Every module, grouped by what a trainer is doing — their day, their people,
 * their money, how it is going, how it is set up — with the sync state and
 * the account at the foot, where they are glanced at rather than hunted for.
 */

import React from 'react';
import { Pressable, ScrollView, Text, View } from 'react-native';
import { usePathname, useRouter } from 'expo-router';

import { useT } from '@/i18n';
import { useApp } from '@/state/app';
import { Avatar } from './components';
import { Icon } from './icon';
import { moduleForPath, NAV_GROUPS } from './nav';
import { SyncBadge } from './sync-badge';
import { radius, space } from './theme';
import { makeStyles, useTheme } from './theming';

export const SIDEBAR_WIDTH = 248;

export function Sidebar() {
  const styles = useStyles();
  const { colors } = useTheme();
  const { t } = useT();
  const { account } = useApp();
  const router = useRouter();
  const pathname = usePathname();
  const active = moduleForPath(pathname);

  return (
    <View style={styles.sidebar} accessibilityRole="menu">
      <View style={styles.brand}>
        <View style={styles.logo}><Icon name="activity" size={18} color={colors.onAccent} strokeWidth={2.5} /></View>
        <Text style={styles.brandName}>{t('signIn.brand')}</Text>
      </View>

      <ScrollView style={{ flex: 1 }} contentContainerStyle={{ paddingBottom: space.lg }}>
        {NAV_GROUPS.map((group) => (
          <View key={group.key} style={styles.group}>
            <Text style={styles.groupLabel}>{t(group.label)}</Text>
            {group.modules.map((module) => {
              const selected = active?.key === module.key;
              return (
                <Pressable
                  key={module.key}
                  accessibilityRole="menuitem"
                  accessibilityState={{ selected }}
                  onPress={() => router.navigate(module.href as never)}
                  style={({ pressed, hovered }: { pressed: boolean; hovered?: boolean }) => [
                    styles.item,
                    selected && styles.itemSelected,
                    !selected && (pressed || hovered) && styles.itemHover,
                  ]}
                >
                  <Icon name={module.icon} size={18} color={selected ? colors.ink : colors.inkMuted} />
                  <Text style={[styles.itemLabel, selected && styles.itemLabelSelected]}>{t(module.label)}</Text>
                </Pressable>
              );
            })}
          </View>
        ))}
      </ScrollView>

      <View style={styles.foot}>
        <SyncBadge />
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={account?.display_name ?? ''}
          onPress={() => router.navigate('/dashboard/settings' as never)}
          style={({ pressed }) => [styles.account, pressed && { opacity: 0.7 }]}
        >
          <Avatar name={account?.display_name ?? ''} size={32} self />
          <View style={{ flex: 1 }}>
            <Text style={styles.accountName} numberOfLines={1}>{account?.display_name ?? ''}</Text>
            <Text style={styles.accountEmail} numberOfLines={1}>{account?.email ?? ''}</Text>
          </View>
        </Pressable>
      </View>
    </View>
  );
}

const useStyles = makeStyles(({ colors, type }) => ({
  sidebar: {
    width: SIDEBAR_WIDTH,
    backgroundColor: colors.chrome,
    borderEndWidth: 1,
    borderEndColor: colors.border,
    paddingTop: space.lg,
  },
  brand: { flexDirection: 'row', alignItems: 'center', gap: space.sm, paddingHorizontal: space.lg, marginBottom: space.lg },
  logo: { width: 30, height: 30, borderRadius: radius.sm, backgroundColor: colors.accent, alignItems: 'center', justifyContent: 'center' },
  brandName: { ...type.heading, color: colors.ink },
  group: { paddingHorizontal: space.sm, marginBottom: space.md },
  groupLabel: { ...type.label, color: colors.inkMuted, paddingHorizontal: space.md, marginBottom: space.xs, fontSize: 10 },
  item: {
    flexDirection: 'row', alignItems: 'center', gap: space.md,
    paddingHorizontal: space.md, minHeight: 38, borderRadius: radius.sm,
  },
  itemSelected: { backgroundColor: colors.accentSoft },
  itemHover: { backgroundColor: colors.surfaceRaised },
  itemLabel: { ...type.small, color: colors.inkMuted, fontWeight: '600' },
  itemLabelSelected: { color: colors.ink, fontWeight: '700' },
  foot: { borderTopWidth: 1, borderTopColor: colors.border, padding: space.md, gap: space.md },
  account: { flexDirection: 'row', alignItems: 'center', gap: space.sm },
  accountName: { ...type.small, color: colors.ink, fontWeight: '700' },
  accountEmail: { ...type.caption, color: colors.inkMuted },
}));
