/**
 * Today.
 *
 * The first screen, and the one the product is really about: who is in front
 * of me, and what happens when this session ends.
 *
 * Tapping a client opens their outcome buttons in place rather than pushing a
 * screen. Marking attendance is done between sets, often one-handed, and a
 * navigation animation for a two-second decision is a navigation animation the
 * trainer is standing still for.
 */

import React, { useCallback, useState } from 'react';
import { Pressable, RefreshControl, ScrollView, Text, View } from 'react-native';
import { useFocusEffect, useRouter } from 'expo-router';

import { markAttendance, startWorkout } from '@/features/actions';
import { openWorkout, todaysRoster, type RosterEntry } from '@/features/queries';
import type { AttendanceStatus } from '@/api/types';
import { useT } from '@/i18n';
import { useApp, useQuery } from '@/state/app';
import {
  Avatar, Body, Button, Caption, Card, Chip, Empty, Heading, Label, Metric,
  Pill, Progress, Row, Screen, Spacer, Title,
} from '@/ui/components';
import { useConfirm } from '@/ui/overlay';
import { SyncBadge } from '@/ui/sync-badge';
import { space } from '@/ui/theme';
import { makeStyles, useTheme } from '@/ui/theming';

const OUTCOMES: AttendanceStatus[] = ['completed', 'no_show', 'late_cancel', 'early_cancel'];

export default function TodayScreen() {
  const { db, account, touch, syncNow, sync } = useApp();
  const router = useRouter();
  const confirm = useConfirm();
  const { t, time, date, number } = useT();
  const { colors } = useTheme();
  const styles = useStyles();
  const [expanded, setExpanded] = useState<string | null>(null);

  const roster = useQuery((database) => todaysRoster(database), []);

  // Coming back from a workout should show the attendance mark that was made
  // there, not the roster as it looked when the screen was last mounted.
  useFocusEffect(useCallback(() => { roster.reload(); }, [roster.reload]));

  const mark = async (entry: RosterEntry, status: AttendanceStatus, allowOverdraft = false) => {
    if (!db) return;
    await markAttendance(db, entry.attendeeId, status, { allowOverdraft });
    setExpanded(null);
    touch();
    void syncNow();
  };

  /**
   * Journey A's hinge.
   *
   * The local balance is a mirror and may be stale, so this is a warning and
   * not a refusal — the server holds the real answer and will refuse if it has
   * to. What the trainer gets is the choice the PRD asks for: sell a renewal,
   * or let this one run into overdraft, decided before the client leaves.
   */
  const confirmCompleted = async (entry: RosterEntry) => {
    if (entry.creditsRemaining > 0) {
      void mark(entry, 'completed');
      return;
    }
    // A sheet rather than Alert.alert: on the web Alert is a silent no-op,
    // so tapping Completed for an out-of-credit client used to do nothing.
    const choice = await confirm({
      title: t('today.noCreditsTitle'),
      message: t('today.noCreditsBody', { name: entry.clientName }),
      actions: [
        { value: 'renew', label: t('today.renew'), tone: 'primary' },
        { value: 'overdraft', label: t('today.completeAnyway') },
      ],
    });
    if (choice === 'renew') router.push({ pathname: '/client/[id]', params: { id: entry.clientId } });
    else if (choice === 'overdraft') void mark(entry, 'completed', true);
  };

  const toWorkout = async (entry: RosterEntry) => {
    if (!db) return;
    // Resume rather than open a second workout: a trainer who backs out to
    // check the roster mid-session should return to the sets already logged.
    const existing = await openWorkout(db, entry.clientId);
    const id = existing?.id ?? await startWorkout(db, entry.clientId, { sessionId: entry.sessionId });
    if (!existing) touch();
    router.push({
      pathname: '/workout/[id]',
      params: { id, client: entry.clientId, name: entry.clientName },
    });
  };

  const entries = roster.data ?? [];
  const marked = entries.filter((e) => e.status !== 'scheduled').length;
  const firstName = (account?.display_name ?? '').split(' ')[0] || t('today.greetingFallback');

  return (
    <Screen>
      <ScrollView
        contentContainerStyle={styles.scroll}
        refreshControl={
          <RefreshControl
            refreshing={sync.running}
            onRefresh={() => { void syncNow(); }}
            tintColor={colors.inkMuted}
          />
        }
      >
        <Row style={{ justifyContent: 'space-between' }}>
          <Row>
            <Avatar name={account?.display_name ?? firstName} size={46} self />
            <View>
              <Title>{t('today.greeting', { name: firstName })}</Title>
              <Caption>{date(new Date(), 'long')}</Caption>
            </View>
          </Row>
          <SyncBadge />
        </Row>

        <Spacer size={space.xl} />

        {/* The day at a glance. Lime because getting through the roster is the
            job this screen exists for. */}
        <Card tone="accent">
          <Row style={{ justifyContent: 'space-between', alignItems: 'flex-start' }}>
            <Metric
              value={number(marked)}
              unit={t('today.ofTotal', { count: entries.length })}
              label={t('today.sessionsMarked')}
              tone="onAccent"
            />
            <Heading onAccent>
              {entries.length === 0
                ? t('today.clearDay')
                : marked === entries.length ? t('today.allDone') : t('today.toGo', { count: entries.length - marked })}
            </Heading>
          </Row>
          <Spacer />
          <Progress value={entries.length === 0 ? 0 : marked / entries.length} onAccent />
        </Card>

        <Spacer size={space.xl} />
        <Label>{t('today.roster')}</Label>
        <Spacer />

        {entries.length === 0 ? (
          <Empty
            icon="calendar"
            title={t('today.nothingBooked')}
            detail={roster.loading ? t('common.loading') : t('today.appearsAsSynced')}
          />
        ) : (
          entries.map((entry) => (
            <View key={entry.attendeeId} style={{ marginBottom: space.md }}>
              <Pressable
                accessibilityRole="button"
                accessibilityLabel={t('today.clientAt', { name: entry.clientName, time: time(entry.startsAt) })}
                onPress={() => setExpanded(expanded === entry.attendeeId ? null : entry.attendeeId)}
              >
                <Card>
                  <Row style={{ justifyContent: 'space-between', alignItems: 'flex-start' }}>
                    <View style={{ flex: 1 }}>
                      <Row style={{ gap: space.md }}>
                        <Text style={styles.time}>{time(entry.startsAt)}</Text>
                        <Text style={styles.name} numberOfLines={1}>{entry.clientName}</Text>
                      </Row>
                      <Spacer size={space.sm} />
                      <Row style={{ gap: space.sm }}>
                        <StatusPill status={entry.status} />
                        <Caption>
                          {[entry.sessionTypeName, entry.location].filter(Boolean).join(' · ') || t('common.session')}
                        </Caption>
                      </Row>
                    </View>
                    <Chip
                      label={entry.creditsRemaining > 0 ? `${entry.creditsRemaining}` : '0'}
                      tone={entry.creditsRemaining > 0 ? 'accent' : 'danger'}
                    />
                  </Row>

                  {expanded === entry.attendeeId ? (
                    <>
                      <Spacer size={space.lg} />
                      <View style={styles.grid}>
                        {OUTCOMES.map((outcome) => (
                          <Button
                            key={outcome}
                            label={t(`attendance.${outcome}`)}
                            tone={outcome === 'completed' ? 'primary' : 'default'}
                            style={styles.gridItem}
                            onPress={() => {
                              if (outcome === 'completed') void confirmCompleted(entry);
                              else void mark(entry, outcome);
                            }}
                          />
                        ))}
                      </View>
                      <Spacer size={space.sm} />
                      <Row>
                        <Button
                          label={t('today.logWorkout')}
                          style={{ flex: 1 }}
                          onPress={() => { void toWorkout(entry); }}
                        />
                        <Button
                          label={t('today.profile')}
                          tone="quiet"
                          onPress={() => router.push({ pathname: '/client/[id]', params: { id: entry.clientId } })}
                        />
                      </Row>
                    </>
                  ) : null}
                </Card>
              </Pressable>
            </View>
          ))
        )}

        {sync.pending > 0 ? (
          <>
            <Spacer />
            <Body muted>{t('today.pendingChanges', { count: sync.pending })}</Body>
          </>
        ) : null}
      </ScrollView>
    </Screen>
  );
}

function StatusPill({ status }: { status: AttendanceStatus }) {
  const { t } = useT();
  const tone = status === 'completed' ? 'success'
    : status === 'no_show' ? 'danger'
    : status === 'late_cancel' ? 'warning' : 'muted';
  return <Pill label={t(`attendance.${status}`)} tone={tone} />;
}

const useStyles = makeStyles(({ colors, type }) => ({
  // Bottom padding clears the tab bar and the circle above it.
  scroll: { padding: space.lg, paddingTop: space.xl, paddingBottom: 176 },
  time: { ...type.heading, color: colors.inkMuted, minWidth: 48 },
  name: { ...type.title, color: colors.ink, flexShrink: 1 },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: space.sm },
  gridItem: { flexGrow: 1, flexBasis: '45%' },
}));
