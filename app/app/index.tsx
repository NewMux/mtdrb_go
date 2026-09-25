/**
 * The front door. A phone opens on Today — who is in front of me — and a desk
 * on the Dashboard, the view of the whole practice a laptop has room for.
 */

import React from 'react';
import { Redirect } from 'expo-router';

import { useLayout } from '@/ui/layout';

export default function Index() {
  const { wide } = useLayout();
  return <Redirect href={wide ? '/dashboard' : '/dashboard/today'} />;
}
