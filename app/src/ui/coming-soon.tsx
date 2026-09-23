/**
 * A module that is on the map but not yet built.
 *
 * Shown rather than hidden, so the navigation a trainer learns now is the one
 * they will keep, and so a screen says plainly what it will do.
 */

import React from 'react';

import { useT } from '@/i18n';
import { Empty } from './components';
import { NAV_MODULES, type ModuleKey } from './nav';
import { Page } from './page';

export function ComingSoon({ module }: { module: ModuleKey }) {
  const { t } = useT();
  const entry = NAV_MODULES.find((m) => m.key === module)!;
  return (
    <Page title={t(entry.label)}>
      <Empty icon={entry.icon} title={t('shell.comingTitle')} detail={t(entry.blurb)} />
    </Page>
  );
}
