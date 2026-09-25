/**
 * Everything the phone's tab bar has no room for.
 *
 * Each module with the one line that says what it is for, so a trainer
 * scanning for "where do I sell a protein bar" finds Shop by what it does.
 */

import React from 'react';
import { Pressable, Text, View } from 'react-native';
import { useRouter } from 'expo-router';

import { useT } from '@/i18n';
import { Card, Divider, Label, Spacer } from '@/ui/components';
import { Icon } from '@/ui/icon';
import { NAV_GROUPS, PHONE_TABS } from '@/ui/nav';
import { Page } from '@/ui/page';
import { radius, space } from '@/ui/theme';
import { makeStyles, useTheme } from '@/ui/theming';

export default function MoreScreen() {
  const { t } = useT();
  const router = useRouter();
  const styles = useStyles();
  const { colors } = useTheme();

  return (
    <Page title={t('more.title')}>
      {NAV_GROUPS.map((group) => {
        const modules = group.modules.filter((m) => !PHONE_TABS.includes(m.key));
        if (modules.length === 0) return null;
        return (
          <View key={group.key}>
            <Label>{t(group.label)}</Label>
            <Spacer size={space.sm} />
            <Card style={{ paddingVertical: space.xs }}>
              {modules.map((module, i) => (
                <View key={module.key}>
                  {i > 0 ? <Divider /> : null}
                  <Pressable
                    accessibilityRole="button"
                    onPress={() => router.push(module.href as never)}
                    style={({ pressed }) => [styles.row, pressed && { opacity: 0.6 }]}
                  >
                    <View style={styles.icon}><Icon name={module.icon} size={18} /></View>
                    <View style={{ flex: 1 }}>
                      <Text style={styles.name}>{t(module.label)}</Text>
                      <Text style={styles.blurb}>{t(module.blurb)}</Text>
                    </View>
                    <Icon name="next" size={18} color={colors.inkMuted} />
                  </Pressable>
                </View>
              ))}
            </Card>
            <Spacer size={space.lg} />
          </View>
        );
      })}
      <Card style={{ paddingVertical: space.xs }}>
        <Pressable
          accessibilityRole="button"
          onPress={() => router.push('/sync')}
          style={({ pressed }) => [styles.row, pressed && { opacity: 0.6 }]}
        >
          <View style={styles.icon}><Icon name="user" size={18} /></View>
          <Text style={[styles.name, { flex: 1 }]}>{t('more.account')}</Text>
          <Icon name="next" size={18} color={colors.inkMuted} />
        </Pressable>
      </Card>
    </Page>
  );
}

const useStyles = makeStyles(({ colors, type }) => ({
  row: { flexDirection: 'row', alignItems: 'center', gap: space.md, paddingVertical: space.md },
  icon: {
    width: 36, height: 36, borderRadius: radius.sm, backgroundColor: colors.surfaceRaised,
    alignItems: 'center', justifyContent: 'center',
  },
  name: { ...type.body, color: colors.ink, fontWeight: '600' },
  blurb: { ...type.caption, color: colors.inkMuted, marginTop: 2 },
}));
