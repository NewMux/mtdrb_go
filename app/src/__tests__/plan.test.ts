import { planState, type PracticeSettings } from '@/features/plan';

const base: PracticeSettings = {
  id: 't', business_name: 'Rivera Strength', currency: 'AED', timezone: 'Asia/Dubai', country: 'AE',
  language: 'en', week_start: 1, session_timeout_days: 30, plan: 'trial', plan_status: 'active',
  trial_ends_at: '2026-07-01T12:00:00Z', plan_renews_on: null, cancel_at_period_end: 0, onboarded_at: null,
};

describe('planState', () => {
  it('counts a trial down in whole days, rounded up', () => {
    expect(planState(base, new Date('2026-06-30T13:00:00Z'))).toMatchObject({ lapsed: false, trialDaysLeft: 1 });
    expect(planState(base, new Date('2026-06-17T12:00:00Z'))).toMatchObject({ trialDaysLeft: 14 });
  });

  it('lapses a trial the moment it ends, as the server does', () => {
    expect(planState(base, new Date('2026-07-01T12:00:00Z'))).toMatchObject({ lapsed: true, trialDaysLeft: 0 });
  });

  it('keeps a paid plan open to the end of its renewal day when cancelling', () => {
    const cancelling = { ...base, plan: 'pro' as const, plan_renews_on: '2026-07-01', cancel_at_period_end: 1 };
    expect(planState(cancelling, new Date('2026-07-01T23:00:00Z'))?.lapsed).toBe(false);
    expect(planState(cancelling, new Date('2026-07-02T00:00:00Z'))?.lapsed).toBe(true);
  });

  it('treats a cancelled plan as read-only and past due as a grace period', () => {
    expect(planState({ ...base, plan: 'pro', plan_status: 'cancelled' })?.lapsed).toBe(true);
    expect(planState({ ...base, plan: 'pro', plan_status: 'past_due' })?.lapsed).toBe(false);
  });

  it('knows nothing before the first sync', () => {
    expect(planState(null)).toBeNull();
  });
});
