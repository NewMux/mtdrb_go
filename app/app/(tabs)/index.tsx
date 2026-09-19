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
import { Alert, Pressable, RefreshControl, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useFocusEffect, useRouter } from 'expo-router';

import { markAttendance, startWorkout } from '@/features/actions';
import { openWorkout, todaysRoster, type RosterEntry } from '@/features/queries';
import type { AttendanceStatus } from '@/api/types';
import { useApp, useQuery } from '@/state/app';
import {
  Body, Button, Caption, Card, Chip, Empty, Heading, Label, Metric,
  Pill, Row, Screen, Spacer, Title,
} from '@/ui/components';
import { SyncBadge } from '@/ui/sync-badge';
import { clockTime } from '@/ui/format';
import { colors, radius, space, type as typography } from '@/ui/theme';

const OUTCOMES: { status: AttendanceStatus; label: string }[] = [
  { status: 'completed', label: 'Completed' },
  { status: 'no_show', label: 'No-show' },
  { status: 'late_cancel', label: 'Late cancel' },
  { status: 'early_cancel', label: 'Early cancel' },
];

export default function TodayScreen() {
  const { db, account, touch, syncNow, sync } = useApp();
  const router = useRouter();
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
  const confirmCompleted = (entry: RosterEntry) => {
    if (entry.creditsRemaining > 0) {
      void mark(entry, 'completed');
      return;
    }
    Alert.alert(
      'No credits left',
      `${entry.clientName} has no credits remaining. Renew the package, or record this session against an overdraft.`,
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: 'Renew package',
          onPress: () => router.push({ pathname: '/client/[id]', params: { id: entry.clientId } }),
        },
        { text: 'Complete anyway', onPress: () => { void mark(entry, 'completed', true); } },
      ],
    );
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
  const firstName = (account?.display_name ?? '').split(' ')[0] || 'there';

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
            <View style={styles.avatar}>
              <Text style={styles.avatarGlyph}>{firstName.slice(0, 1).toUpperCase()}</Text>
            </View>
            <View>
              <Title>Hello {firstName}</Title>
              <Caption>
                {new Date().toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' })}
              </Caption>
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
              value={String(marked)}
              unit={`/ ${entries.length}`}
              label="sessions marked"
              tone="onAccent"
            />
            <Heading onAccent>
              {entries.length === 0 ? 'Clear day' : marked === entries.length ? 'All done' : `${entries.length - marked} to go`}
            </Heading>
          </Row>
          <Spacer />
          <View style={styles.progressOnAccent}>
            <View
              style={[
                styles.progressOnAccentFill,
                { width: `${entries.length === 0 ? 0 : (marked / entries.length) * 100}%` },
              ]}
            />
          </View>
        </Card>

        <Spacer size={space.xl} />
        <Label>Roster</Label>
        <Spacer />

        {entries.length === 0 ? (
          <Empty
            title="Nothing booked"
            detail={roster.loading ? 'Loading…' : 'Sessions appear here as they sync.'}
          />
        ) : (
          entries.map((entry) => (
            <View key={entry.attendeeId} style={{ marginBottom: space.md }}>
              <Pressable
                accessibilityRole="button"
                accessibilityLabel={`${entry.clientName} at ${clockTime(entry.startsAt)}`}
                onPress={() => setExpanded(expanded === entry.attendeeId ? null : entry.attendeeId)}
              >
                <Card>
                  <Row style={{ justifyContent: 'space-between', alignItems: 'flex-start' }}>
                    <View style={{ flex: 1 }}>
                      <Row style={{ gap: space.md }}>
                        <Text style={styles.time}>{clockTime(entry.startsAt)}</Text>
                        <Text style={styles.name} numberOfLines={1}>{entry.clientName}</Text>
                      </Row>
                      <Spacer size={space.sm} />
                      <Row style={{ gap: space.sm }}>
                        <StatusPill status={entry.status} />
                        <Caption>
                          {[entry.sessionTypeName, entry.location].filter(Boolean).join(' · ') || 'Session'}
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
                            key={outcome.status}
                            label={outcome.label}
                            tone={outcome.status === 'completed' ? 'primary' : 'default'}
                            style={styles.gridItem}
                            onPress={() => {
                              if (outcome.status === 'completed') confirmCompleted(entry);
                              else void mark(entry, outcome.status);
                            }}
                          />
                        ))}
                      </View>
                      <Spacer size={space.sm} />
                      <Row>
                        <Button
                          label="Log workout"
                          style={{ flex: 1 }}
                          onPress={() => { void toWorkout(entry); }}
                        />
                        <Button
                          label="Profile"
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
            <Body muted>
              {sync.pending} change{sync.pending === 1 ? '' : 's'} waiting to reach the server. They are
              safe on this device and will send themselves.
            </Body>
          </>
        ) : null}
      </ScrollView>
    </Screen>
  );
}

function StatusPill({ status }: { status: AttendanceStatus }) {
  switch (status) {
    case 'completed': return <Pill label="Completed" tone="success" />;
    case 'no_show': return <Pill label="No-show" tone="danger" />;
    case 'late_cancel': return <Pill label="Late cancel" tone="warning" />;
    case 'early_cancel': return <Pill label="Early cancel" tone="muted" />;
    default: return <Pill label="Scheduled" tone="muted" />;
  }
}

const styles = StyleSheet.create({
  // Bottom padding clears the tab bar and the circle straddling it.
  scroll: { padding: space.lg, paddingTop: space.xl, paddingBottom: 176 },

  avatar: {
    width: 46,
    height: 46,
    borderRadius: radius.pill,
    backgroundColor: colors.accent,
    alignItems: 'center',
    justifyContent: 'center',
  },
  avatarGlyph: { ...typography.title, color: colors.onAccent },

  progressOnAccent: {
    height: 8,
    borderRadius: radius.pill,
    backgroundColor: 'rgba(18,18,18,0.18)',
    overflow: 'hidden',
  },
  progressOnAccentFill: { height: '100%', borderRadius: radius.pill, backgroundColor: colors.onAccent },

  time: { ...typography.heading, color: colors.inkMuted, minWidth: 48 },
  name: { ...typography.title, color: colors.ink, flexShrink: 1 },
  grid: { flexDirection: 'row', flexWrap: 'wrap', gap: space.sm },
  gridItem: { flexGrow: 1, flexBasis: '45%' },
});
