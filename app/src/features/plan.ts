/**
 * The practice's plan, as the device knows it.
 *
 * The server decides what a plan allows and refuses what it does not; this is
 * only so the app can *say* so before the trainer finds out from a refusal —
 * a banner when a trial is about to end, and an explanation when the account
 * has gone read-only. It reads the mirrored settings row, so it works in a
 * basement too. The rule is the server's (internal/subscription), repeated
 * here for display and nothing else.
 */

import type { Database } from '@/db/types';

export interface PracticeSettings {
  id: string;
  business_name: string | null;
  currency: string | null;
  timezone: string | null;
  country: string | null;
  language: string | null;
  week_start: number | null;
  session_timeout_days: number | null;
  plan: 'trial' | 'starter' | 'pro' | null;
  plan_status: 'active' | 'past_due' | 'cancelled' | null;
  trial_ends_at: string | null;
  plan_renews_on: string | null;
  cancel_at_period_end: number | null;
}

/** The practice's mirrored settings row, or null before the first sync. */
export async function practiceSettings(db: Database): Promise<PracticeSettings | null> {
  return db.selectOne<PracticeSettings>(
    `SELECT id, business_name, currency, timezone, country, language, week_start,
            session_timeout_days, plan,
            plan_status, trial_ends_at, plan_renews_on, cancel_at_period_end
       FROM settings LIMIT 1`,
  );
}

export interface PlanState {
  plan: 'trial' | 'starter' | 'pro';
  /** Read-only until renewed. */
  lapsed: boolean;
  /** Whole days left in a trial, rounded up; null when not on trial. */
  trialDaysLeft: number | null;
  cancelling: boolean;
}

const DAY_MS = 86_400_000;

/** Whether a trial is ending soon enough to mention on every screen. */
export const TRIAL_WARNING_DAYS = 3;

export function planState(row: PracticeSettings | null, now: Date = new Date()): PlanState | null {
  if (!row?.plan) return null;
  const at = now.getTime();
  const cancelling = row.cancel_at_period_end === 1;
  let lapsed = false;
  let trialDaysLeft: number | null = null;

  if (row.plan === 'trial') {
    const ends = row.trial_ends_at ? Date.parse(row.trial_ends_at) : NaN;
    lapsed = Number.isNaN(ends) || at >= ends;
    trialDaysLeft = Number.isNaN(ends) ? 0 : Math.max(0, Math.ceil((ends - at) / DAY_MS));
  } else if (row.plan_status === 'cancelled') {
    lapsed = true;
  } else if (cancelling && row.plan_renews_on) {
    // The paid period runs to the end of its renewal day.
    lapsed = at >= Date.parse(`${row.plan_renews_on}T00:00:00Z`) + DAY_MS;
  }
  return { plan: row.plan, lapsed, trialDaysLeft, cancelling };
}
