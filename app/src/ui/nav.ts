/**
 * Where everything is.
 *
 * One list drives the desk's sidebar, the phone's tab bar and the phone's
 * More screen, so a module added here appears in all three and cannot be
 * reachable on one and missing on another.
 *
 * The phone gets five tabs, chosen for the gym floor: who is in front of me
 * (Today), when is the next one (Calendar), who are they (Clients), have they
 * paid (Billing), and everything else. The desk sees all of it at once.
 */

import type { TKey } from '@/i18n';
import type { IconName } from './icon';

export type ModuleKey =
  | 'today' | 'dashboard' | 'clients' | 'calendar' | 'programs'
  | 'billing' | 'shop' | 'packages' | 'analytics' | 'insights'
  | 'tasks' | 'locations' | 'settings';

export interface NavModule {
  key: ModuleKey;
  /** The Tabs screen name, relative to app/dashboard. */
  screen: string;
  href: string;
  icon: IconName;
  label: TKey;
  /** One line on the More screen: what the module is for. */
  blurb: TKey;
  /** On the phone's tab bar, rather than behind More. */
  phoneTab?: boolean;
  /**
   * Not built yet. Its screen only says what it will do, so it stays out of
   * the sidebar, the tab bar and More: a store reviewer, like a trainer,
   * reads a menu of placeholders as an unfinished app.
   */
  unbuilt?: boolean;
}

/**
 * Show unbuilt modules anyway, for development and demos of what is coming:
 * EXPO_PUBLIC_SHOW_UNBUILT=1, inlined at build time.
 */
export const SHOW_UNBUILT = process.env.EXPO_PUBLIC_SHOW_UNBUILT === '1';

type NavGroup = { key: string; label: TKey; modules: NavModule[] };

/** Every module, built or not. */
export const ALL_NAV_GROUPS: NavGroup[] = [
  {
    key: 'day',
    label: 'navGroup.day',
    modules: [
      { key: 'today', screen: 'today', href: '/dashboard/today', icon: 'today', label: 'nav.today', blurb: 'blurb.today', phoneTab: true },
      { key: 'dashboard', screen: 'index', href: '/dashboard', icon: 'dashboard', label: 'nav.dashboard', blurb: 'blurb.dashboard' },
    ],
  },
  {
    key: 'people',
    label: 'navGroup.people',
    modules: [
      { key: 'clients', screen: 'clients', href: '/dashboard/clients', icon: 'clients', label: 'nav.clients', blurb: 'blurb.clients', phoneTab: true },
      { key: 'calendar', screen: 'calendar', href: '/dashboard/calendar', icon: 'calendar', label: 'nav.calendar', blurb: 'blurb.calendar', phoneTab: true, unbuilt: true },
      { key: 'programs', screen: 'programs', href: '/dashboard/programs', icon: 'programs', label: 'nav.programs', blurb: 'blurb.programs', unbuilt: true },
    ],
  },
  {
    key: 'money',
    label: 'navGroup.money',
    modules: [
      { key: 'billing', screen: 'billing', href: '/dashboard/billing', icon: 'billing', label: 'nav.billing', blurb: 'blurb.billing', phoneTab: true },
      { key: 'shop', screen: 'shop', href: '/dashboard/shop', icon: 'shop', label: 'nav.shop', blurb: 'blurb.shop', unbuilt: true },
      { key: 'packages', screen: 'packages', href: '/dashboard/packages', icon: 'packages', label: 'nav.packages', blurb: 'blurb.packages' },
    ],
  },
  {
    key: 'insight',
    label: 'navGroup.insight',
    modules: [
      { key: 'analytics', screen: 'analytics', href: '/dashboard/analytics', icon: 'analytics', label: 'nav.analytics', blurb: 'blurb.analytics', unbuilt: true },
      { key: 'insights', screen: 'insights', href: '/dashboard/insights', icon: 'insights', label: 'nav.insights', blurb: 'blurb.insights', unbuilt: true },
      { key: 'tasks', screen: 'tasks', href: '/dashboard/tasks', icon: 'tasks', label: 'nav.tasks', blurb: 'blurb.tasks', unbuilt: true },
    ],
  },
  {
    key: 'setup',
    label: 'navGroup.setup',
    modules: [
      { key: 'locations', screen: 'locations', href: '/dashboard/locations', icon: 'locations', label: 'nav.locations', blurb: 'blurb.locations' },
      { key: 'settings', screen: 'settings', href: '/dashboard/settings', icon: 'settings', label: 'nav.settings', blurb: 'blurb.settings' },
    ],
  },
];

export const ALL_NAV_MODULES: NavModule[] = ALL_NAV_GROUPS.flatMap((g) => g.modules);

const shown = (m: NavModule) => SHOW_UNBUILT || !m.unbuilt;

/** What the sidebar and More list: built modules, in groups that have any. */
export const NAV_GROUPS: NavGroup[] = ALL_NAV_GROUPS
  .map((g) => ({ ...g, modules: g.modules.filter(shown) }))
  .filter((g) => g.modules.length > 0);

export const NAV_MODULES: NavModule[] = NAV_GROUPS.flatMap((g) => g.modules);

/** Modules whose screens exist but are kept out of every menu. */
export const HIDDEN_MODULES: NavModule[] = ALL_NAV_MODULES.filter((m) => !shown(m));

/** The phone's tab bar, in order, with More last. */
export const PHONE_TABS: ModuleKey[] = (['today', 'calendar', 'clients', 'billing'] as ModuleKey[])
  .filter((key) => NAV_MODULES.some((m) => m.key === key));

/** Which module a path belongs to: the longest href that prefixes it. */
export function moduleForPath(pathname: string): NavModule | undefined {
  const path = pathname.replace(/\/+$/, '') || '/';
  let best: NavModule | undefined;
  for (const m of NAV_MODULES) {
    if (path === m.href || path.startsWith(`${m.href}/`)) {
      if (!best || m.href.length > best.href.length) best = m;
    }
  }
  return best;
}
