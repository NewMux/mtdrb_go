/**
 * Steps that span more than one module.
 */

import type { Database } from '@/db/types';
import { startWorkout } from './actions';
import { nextSession } from './programs';
import { openWorkout } from './queries';

/**
 * Opens the workout a trainer means when they tap "Start workout".
 *
 * Resumes one already in progress — a trainer who backs out to check the
 * roster mid-session should return to the sets already logged — and
 * otherwise starts the client's next programmed day, if they are on a
 * programme, or a blank workout if not.
 */
export async function openOrStartWorkout(
  db: Database, clientId: string, opts: { sessionId?: string } = {},
): Promise<{ id: string; created: boolean }> {
  const existing = await openWorkout(db, clientId);
  if (existing) return { id: existing.id, created: false };

  const plan = await nextSession(db, clientId);
  const id = await startWorkout(db, clientId, {
    sessionId: opts.sessionId,
    assignmentId: plan?.assignmentId,
    dayId: plan?.day.id,
    weekNumber: plan?.week ?? 1,
  });
  return { id, created: true };
}
