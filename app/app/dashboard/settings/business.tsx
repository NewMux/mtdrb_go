/**
 * How the practice runs: where it is, its week, its policies, its targets.
 *
 * One form, saved as one change, because these settings lean on each other:
 * picking Saudi Arabia moves the week to Sunday, and the working hours are
 * drawn in the order that week runs. Only what changed is sent, so two people
 * on two devices do not overwrite each other's unrelated edits.
 *
 * Only the owner changes these; anyone else sees them read-only, and the
 * server would refuse them anyway.
 */

import React, { useEffect, useMemo, useState } from 'react';
import { TextInput, View } from 'react-native';

import type { Settings, SettingsPatch } from '@/api/account';
import { describeError } from '@/api/describe';
import { cleanHours, weekOrder, type Interval } from '@/features/working-hours';
import { useT, type TKey } from '@/i18n';
import { useApp, useRemote } from '@/state/app';
import {
  Banner, Button, Caption, Divider, Field, IconButton, Label, NumberField, Row, SegmentedChoice, Spacer, TextButton, Toggle,
} from '@/ui/components';
import { amountText, parseMoney } from '@/ui/format';
import { FormPage, Section } from '@/ui/form-page';
import { useLayout } from '@/ui/layout';
import { useToast } from '@/ui/overlay';
import { Select } from '@/ui/pickers';
import { radius, space } from '@/ui/theme';
import { makeStyles } from '@/ui/theming';

const COUNTRIES = ['AE', 'SA', 'BH', 'OM', 'QA', 'KW', 'EG', 'JO', 'GB', 'US'] as const;
const TIMEZONES = [
  'Asia/Dubai', 'Asia/Riyadh', 'Asia/Bahrain', 'Asia/Muscat', 'Asia/Qatar', 'Asia/Kuwait',
  'Africa/Cairo', 'Asia/Amman', 'Europe/London', 'America/New_York', 'UTC',
];
/** Countries whose week starts on Sunday; the server's DefaultWeekStart. */
const SUNDAY_WEEK = new Set<string>(['SA', 'BH', 'KW', 'OM', 'QA']);
/** A sensible default working day for a day switched on. */
const DEFAULT_HOURS: Interval = ['06:00', '12:00'];

interface Draft {
  businessName: string;
  country: string | null;
  timezone: string;
  currency: string;
  documentLanguage: 'en' | 'ar' | 'bilingual';
  language: 'en' | 'ar';
  weekStart: number;
  hours: Record<string, Interval[]>;
  buffer: string;
  overdraft: boolean;
  noShow: boolean;
  lowBalance: string;
  timeout: string;
  revenue: string;
  sessions: string;
  clients: string;
  vatRegistered: boolean;
  trn: string;
  vatRate: string;
  pricesIncludeVat: boolean;
}

function draftOf(s: Settings): Draft {
  const hours: Record<string, Interval[]> = {};
  for (const [day, intervals] of Object.entries(s.working_hours ?? {})) {
    hours[day] = intervals.map((iv) => [iv[0] ?? '', iv[1] ?? ''] as Interval);
  }
  const target = (n: number | undefined) => (n === undefined ? '' : String(n));
  return {
    businessName: s.business_name,
    country: s.country,
    timezone: s.timezone,
    currency: s.currency,
    documentLanguage: s.document_language,
    language: s.language,
    weekStart: s.week_start,
    hours,
    buffer: String(s.buffer_minutes),
    overdraft: s.allow_overdraft,
    noShow: s.no_show_is_billable,
    lowBalance: String(s.low_balance_threshold),
    timeout: String(s.session_timeout_days),
    revenue: s.targets.monthly_revenue_minor === undefined ? '' : amountText(s.targets.monthly_revenue_minor, s.currency),
    sessions: target(s.targets.weekly_sessions),
    clients: target(s.targets.active_clients),
    vatRegistered: s.vat_registered,
    trn: s.trn ?? '',
    vatRate: String(s.vat_rate_bp / 100),
    pricesIncludeVat: s.prices_include_vat,
  };
}

export default function BusinessSettingsScreen() {
  const i18n = useT();
  const { t } = i18n;
  const { api, account } = useApp();
  const toast = useToast();
  const remote = useRemote((client) => client.get<Settings>('/v1/settings'));
  const owner = account?.role === 'owner';
  const { wide } = useLayout();

  const [draft, setDraft] = useState<Draft | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => { if (remote.data) setDraft(draftOf(remote.data)); }, [remote.data]);
  const set = <K extends keyof Draft>(key: K, value: Draft[K]) => setDraft((d) => (d ? { ...d, [key]: value } : d));

  const countryOptions = useMemo(
    () => COUNTRIES.map((c) => ({ value: c, label: t(`countries.${c}` as TKey) })),
    [t],
  );
  const timezoneOptions = useMemo(() => {
    const zones = remote.data && !TIMEZONES.includes(remote.data.timezone) ? [remote.data.timezone, ...TIMEZONES] : TIMEZONES;
    return zones.map((z) => ({ value: z, label: z.replace('_', ' ') }));
  }, [remote.data]);
  const weekdayOptions = useMemo(
    () => ['0', '1', '6'].map((d) => ({ value: d, label: t(`weekdays.${d}` as TKey) })),
    [t],
  );

  if (remote.offline) {
    return <FormPage><Banner tone="muted" message={t('errors.offline')} /></FormPage>;
  }
  if (!draft || !remote.data) return <FormPage onRefresh={remote.reload} refreshing={remote.loading}>{null}</FormPage>;

  const saved = remote.data;
  const original = draftOf(saved);

  const save = async () => {
    setError(null);
    const hours = cleanHours(draft.hours);
    if (!hours.ok) {
      setError(`${t(`weekdays.${hours.day}` as TKey)}: ${t('business.hoursFormat')}`);
      return;
    }
    const revenue = draft.revenue.trim() === '' ? undefined : parseMoney(draft.revenue, draft.currency);
    if (revenue === null) {
      setError(t('business.monthlyRevenue'));
      return;
    }
    const whole = (text: string) => (text.trim() === '' ? undefined : Math.max(0, Math.round(Number(text))));

    const patch: SettingsPatch = {};
    if (draft.businessName.trim() !== saved.business_name) patch.business_name = draft.businessName.trim();
    if (draft.country && draft.country !== saved.country) patch.country = draft.country;
    if (draft.timezone !== saved.timezone) patch.timezone = draft.timezone;
    if (draft.currency.trim().toUpperCase() !== saved.currency) patch.currency = draft.currency.trim().toUpperCase();
    if (draft.documentLanguage !== saved.document_language) patch.document_language = draft.documentLanguage;
    if (draft.language !== saved.language) patch.language = draft.language;
    if (draft.weekStart !== original.weekStart) patch.week_start = draft.weekStart;
    if (JSON.stringify(hours.hours) !== JSON.stringify(saved.working_hours)) patch.working_hours = hours.hours;
    if (draft.buffer !== original.buffer) patch.buffer_minutes = whole(draft.buffer) ?? 0;
    if (draft.overdraft !== saved.allow_overdraft) patch.allow_overdraft = draft.overdraft;
    if (draft.noShow !== saved.no_show_is_billable) patch.no_show_is_billable = draft.noShow;
    if (draft.lowBalance !== original.lowBalance) patch.low_balance_threshold = whole(draft.lowBalance) ?? 0;
    if (draft.timeout !== original.timeout) patch.session_timeout_days = whole(draft.timeout) ?? 30;
    const targets = { monthly_revenue_minor: revenue, weekly_sessions: whole(draft.sessions), active_clients: whole(draft.clients) };
    if (JSON.stringify(targets) !== JSON.stringify({
      monthly_revenue_minor: saved.targets.monthly_revenue_minor,
      weekly_sessions: saved.targets.weekly_sessions,
      active_clients: saved.targets.active_clients,
    })) patch.targets = targets;

    const rateBp = Math.round(Number(draft.vatRate.replace(',', '.')) * 100);
    if (!Number.isFinite(rateBp)) {
      setError(t('onboarding.rateInvalid'));
      return;
    }
    if (draft.vatRegistered !== saved.vat_registered) patch.vat_registered = draft.vatRegistered;
    if (draft.trn.trim() !== (saved.trn ?? '')) patch.trn = draft.trn.trim();
    if (rateBp !== saved.vat_rate_bp) patch.vat_rate_bp = rateBp;
    if (draft.pricesIncludeVat !== saved.prices_include_vat) patch.prices_include_vat = draft.pricesIncludeVat;

    if (Object.keys(patch).length === 0) return;
    setSaving(true);
    try {
      await api.patch<Settings>('/v1/settings', patch);
      remote.reload();
      toast(t('settings.saved'), 'success');
    } catch (cause) {
      setError(describeError(cause, i18n));
    } finally {
      setSaving(false);
    }
  };

  const editInterval = (day: string, index: number, side: 0 | 1, value: string) => {
    const next = { ...draft.hours };
    const intervals = [...(next[day] ?? [])];
    const current = intervals[index] ?? DEFAULT_HOURS;
    intervals[index] = side === 0 ? [value, current[1]] : [current[0], value];
    next[day] = intervals;
    set('hours', next);
  };
  const addInterval = (day: string) => {
    const intervals = draft.hours[day] ?? [];
    const last = intervals[intervals.length - 1];
    const next: Interval = last ? ['16:00', '20:00'] : DEFAULT_HOURS;
    set('hours', { ...draft.hours, [day]: [...intervals, next] });
  };
  const removeInterval = (day: string, index: number) => {
    set('hours', { ...draft.hours, [day]: (draft.hours[day] ?? []).filter((_, i) => i !== index) });
  };

  return (
    <FormPage refreshing={remote.loading} onRefresh={remote.reload}>
      {!owner ? <><Banner tone="muted" message={t('settings.ownerOnly')} /><Spacer /></> : null}
      {error ? <><Banner tone="danger" message={error} /><Spacer /></> : null}

      <View pointerEvents={owner ? 'auto' : 'none'} style={owner ? undefined : { opacity: 0.6 }}>
        <Section title={t('business.practice')}>
          <Field label={t('business.businessName')} value={draft.businessName} onChangeText={(v) => set('businessName', v)} hint={t('business.businessNameHint')} />
          <Spacer />
          <Select
            label={t('business.country')}
            value={draft.country}
            options={countryOptions}
            // The server moves the week with the country; show it moving.
            onChange={(v) => setDraft((d) => (d ? { ...d, country: v, weekStart: SUNDAY_WEEK.has(v) ? 0 : 1 } : d))}
          />
          <Spacer size={space.xs} />
          <Caption>{t('business.countryHint')}</Caption>
          <Spacer />
          <Select label={t('business.timezone')} value={draft.timezone} options={timezoneOptions} onChange={(v) => set('timezone', v)} />
          <Spacer />
          {saved.currency_locked ? (
            <>
              <Label>{t('business.currency')}</Label>
              <Spacer size={space.xs} />
              <Caption>{`${saved.currency} · ${t('business.currencyLocked')}`}</Caption>
            </>
          ) : (
            <Field label={t('business.currency')} value={draft.currency} onChangeText={(v) => set('currency', v)} autoCapitalize="none" />
          )}
          <Spacer />
          <Label>{t('business.documentLanguage')}</Label>
          <Spacer size={space.sm} />
          <SegmentedChoice
            options={[
              { value: 'en', label: t('business.documentEn') },
              { value: 'ar', label: t('business.documentAr') },
              { value: 'bilingual', label: t('business.documentBilingual') },
            ] as const}
            value={draft.documentLanguage}
            onChange={(v) => set('documentLanguage', v)}
          />
          <Spacer />
          <Label>{t('business.emailLanguage')}</Label>
          <Spacer size={space.sm} />
          <SegmentedChoice
            options={[{ value: 'en', label: t('prefs.english') }, { value: 'ar', label: t('prefs.arabic') }] as const}
            value={draft.language}
            onChange={(v) => set('language', v)}
          />
        </Section>

        <Section title={t('onboarding.vat.title')} detail={t('onboarding.vat.body')}>
          <Toggle label={t('onboarding.vat.registered')} value={draft.vatRegistered} onChange={(v) => set('vatRegistered', v)} />
          {draft.vatRegistered ? (
            <>
              <Spacer />
              <Field label={t('onboarding.vat.trn')} value={draft.trn} onChangeText={(v) => set('trn', v)} keyboardType="number-pad" autoCapitalize="none" hint={t('onboarding.vat.trnHint')} />
            </>
          ) : null}
          <Spacer />
          <Row><NumberField label={t('onboarding.vat.rate')} value={draft.vatRate} onChangeText={(v) => set('vatRate', v)} /></Row>
          <Spacer />
          <Toggle label={t('packages.includesVat')} hint={t('packages.includesVatHint')} value={draft.pricesIncludeVat} onChange={(v) => set('pricesIncludeVat', v)} />
        </Section>

        <Section title={t('business.week')}>
          <Label>{t('business.weekStart')}</Label>
          <Spacer size={space.sm} />
          <SegmentedChoice
            options={weekdayOptions}
            value={String(draft.weekStart)}
            onChange={(v) => set('weekStart', Number(v))}
          />
          <Spacer size={space.lg} />
          <Label>{t('business.hours')}</Label>
          <Spacer size={space.xs} />
          <Caption>{t('business.hoursBody')}</Caption>
          <Spacer />
          {weekOrder(draft.weekStart).map((day, index) => {
            const intervals = draft.hours[day] ?? [];
            return (
              <View key={day}>
                {index > 0 ? <><Spacer size={space.sm} /><Divider /><Spacer size={space.sm} /></> : null}
                {/* Side by side on a desk; on a phone the day goes above its hours. */}
                <View style={wide ? { flexDirection: 'row', alignItems: 'flex-start', gap: space.md } : { gap: space.sm }}>
                  <View style={wide ? { width: 104, paddingTop: intervals.length ? space.md : 0 } : undefined}>
                    <Label>{t(`weekdays.${day}` as TKey)}</Label>
                  </View>
                  <View style={{ flex: 1, gap: space.sm }}>
                    {intervals.length === 0 ? <Caption>{t('business.dayOff')}</Caption> : null}
                    {intervals.map((iv, i) => (
                      <Row key={i} style={{ gap: space.sm }}>
                        <TimeInput label={t('business.from')} value={iv[0]} onChange={(v) => editInterval(day, i, 0, v)} />
                        <Caption>–</Caption>
                        <TimeInput label={t('business.to')} value={iv[1]} onChange={(v) => editInterval(day, i, 1, v)} />
                        <IconButton icon="close" label={t('business.removeHours')} onPress={() => removeInterval(day, i)} />
                      </Row>
                    ))}
                    {intervals.length < 3 ? <TextButton label={t('business.addHours')} onPress={() => addInterval(day)} /> : null}
                  </View>
                </View>
              </View>
            );
          })}
          <Spacer size={space.sm} />
          <Caption>{t('business.hoursFormat')}</Caption>
        </Section>

        <Section title={t('business.policies')}>
          <Row style={{ gap: space.md }}>
            <NumberField label={t('business.buffer')} value={draft.buffer} onChangeText={(v) => set('buffer', v)} />
            <NumberField label={t('business.lowBalance')} value={draft.lowBalance} onChangeText={(v) => set('lowBalance', v)} />
          </Row>
          <Spacer size={space.xs} />
          <Caption>{`${t('business.bufferHint')} ${t('business.lowBalanceHint')}`}</Caption>
          <Spacer size={space.lg} />
          <Toggle label={t('business.overdraft')} hint={t('business.overdraftHint')} value={draft.overdraft} onChange={(v) => set('overdraft', v)} />
          <Spacer />
          <Toggle label={t('business.noShow')} hint={t('business.noShowHint')} value={draft.noShow} onChange={(v) => set('noShow', v)} />
          <Spacer size={space.lg} />
          <NumberField label={t('business.timeout')} value={draft.timeout} onChangeText={(v) => set('timeout', v)} />
          <Spacer size={space.xs} />
          <Caption>{t('business.timeoutHint')}</Caption>
        </Section>

        <Section title={t('business.targets')} detail={t('business.targetsBody')}>
          <NumberField label={`${t('business.monthlyRevenue')} (${saved.currency})`} value={draft.revenue} onChangeText={(v) => set('revenue', v)} />
          <Spacer />
          <Row style={{ gap: space.md }}>
            <NumberField label={t('business.weeklySessions')} value={draft.sessions} onChangeText={(v) => set('sessions', v)} />
            <NumberField label={t('business.activeClients')} value={draft.clients} onChangeText={(v) => set('clients', v)} />
          </Row>
        </Section>
      </View>

      {owner ? (
        <Button label={t('settings.save')} tone="primary" onPress={() => { void save(); }} busy={saving} />
      ) : null}
    </FormPage>
  );
}

/**
 * A time of day, sized for "06:00" rather than for a price. The label is for
 * screen readers; the row's layout says which end is which.
 */
function TimeInput({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  const styles = useStyles();
  return (
    <TextInput
      accessibilityLabel={label}
      value={value}
      onChangeText={onChange}
      keyboardType="numbers-and-punctuation"
      maxLength={5}
      selectTextOnFocus
      style={styles.time}
    />
  );
}

const useStyles = makeStyles(({ colors, type }) => ({
  time: {
    ...type.body,
    color: colors.ink,
    backgroundColor: colors.surfaceRaised,
    borderRadius: radius.md,
    paddingVertical: space.sm,
    paddingHorizontal: space.md,
    width: 84,
    textAlign: 'center',
    fontVariant: ['tabular-nums'],
    // Times read left to right in Arabic too.
    writingDirection: 'ltr',
  },
}));
