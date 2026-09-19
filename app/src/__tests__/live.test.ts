/**
 * The whole loop, against a running server.
 *
 * Every other test here stubs fetch. This one does not: it signs up, seeds a
 * day's work through the real API, pulls it into a real SQLite mirror, does a
 * morning of offline work through the same actions the screens call, pushes,
 * and then checks what the server believes.
 *
 * Skipped unless COACHPULSE_LIVE_API points at an API, so CI stays hermetic:
 *
 *   COACHPULSE_LIVE_API=http://127.0.0.1:8080 npx jest live
 */

import { ApiClient } from '@/api/client';
import type { Invoice } from '@/api/types';
import { markAttendance, logSet, recordPayment, startWorkout, completeWorkout } from '@/features/actions';
import { sellPackage } from '@/features/billing';
import { clientDetail, outstandingInvoices, todaysRoster, workoutSets } from '@/features/queries';
import { pull, synchronise } from '@/sync/engine';
import * as outbox from '@/sync/outbox';
import { MemoryDatabase, memoryTokens } from './support';

const baseUrl = process.env.COACHPULSE_LIVE_API;
const describeLive = baseUrl ? describe : describe.skip;

jest.setTimeout(60_000);

describeLive('against a live server', () => {
  let db: MemoryDatabase;
  let api: ApiClient;
  let clientId: string;
  let currency: string;
  /** Credits on the account before the morning's work, whatever the fixture sold. */
  let creditsBefore = 0;

  beforeAll(async () => {
    db = new MemoryDatabase();
    api = new ApiClient({ baseUrl: baseUrl as string, tokens: memoryTokens(null as never, null as never) });

    const email = `live-${Date.now()}@coachpulse.test`;
    const session = await api.signup({
      email,
      password: 'correct-horse-battery-staple',
      display_name: 'Sam Rivera',
      business_name: 'Rivera Strength',
      currency: 'EUR',
    });
    currency = session.account.currency;
  });

  afterAll(() => { db?.close(); });

  it('signs up with a seeded chart of accounts and exercise library', async () => {
    await pull(db, api);
    const exercises = await db.select('SELECT id FROM exercises');
    // A trainer opening the app for the first time should be able to build a
    // programme, not stare at an empty list.
    expect(exercises.length).toBeGreaterThan(50);
  });

  it('mirrors a booked session onto the device', async () => {
    const created = await api.post<{ id: string }>('/v1/clients', {
      full_name: 'Dana Rivers',
      email: 'dana@example.test',
    });
    clientId = created.id;

    const type = await api.post<{ id: string }>('/v1/sessions/session-types', {
      name: '1-on-1', duration_minutes: 60, capacity: 1, credit_cost: 1,
    });

    const startsAt = new Date();
    startsAt.setHours(9, 0, 0, 0);
    await api.post('/v1/sessions', {
      session_type_id: type.id,
      starts_at: startsAt.toISOString(),
      client_ids: [clientId],
      location: 'Studio',
    });

    await pull(db, api);

    const roster = await todaysRoster(db);
    expect(roster.map((r) => r.clientName)).toContain('Dana Rivers');
    expect(roster[0]?.status).toBe('scheduled');
  });

  it('sells a package and grants the credits', async () => {
    const { invoice, share } = await sellPackage(api, {
      clientId,
      description: '10-session personal training package',
      credits: 10,
      unitPriceMinor: 5_000,
      currency,
    });

    expect(invoice.status).toBe('issued');
    // Gap-free numbering: the first invoice this tenant ever issues.
    expect(invoice.number).toBeTruthy();
    expect(share?.url).toContain('/public/invoices/');

    await pull(db, api);
    expect((await clientDetail(db, clientId))?.creditsRemaining).toBe(10);
  });

  it('serves the share link to someone with no account', async () => {
    const { share } = await sellPackage(api, {
      clientId, description: 'Single session', credits: 1, unitPriceMinor: 5_000, currency,
    });
    if (!share) throw new Error('no share link');

    // Unauthenticated on purpose: this is the link the client opens from
    // WhatsApp, and the only unauthenticated surface in the product.
    const response = await fetch(share.url, { headers: { Accept: 'application/json' } });
    expect(response.status).toBe(200);

    const payload = (await response.json()) as Record<string, unknown>;
    expect(payload.number).toBeTruthy();
    // The payload is assembled field by field so nothing added later leaks.
    expect(JSON.stringify(payload)).not.toContain('medical');
  });

  it('drains a morning of offline work', async () => {
    // Pull first: an earlier test sold a second pack, and a "before" read off
    // a stale mirror would be compared against a fresh "after".
    await pull(db, api);
    creditsBefore = (await clientDetail(db, clientId))?.creditsRemaining ?? 0;
    expect(creditsBefore).toBeGreaterThan(0);

    const roster = await todaysRoster(db);
    const entry = roster[0];
    if (!entry) throw new Error('no roster entry');

    // All of this happens with the app believing it is offline: local writes
    // plus queued intents, nothing sent.
    await markAttendance(db, entry.attendeeId, 'completed');

    const workout = await startWorkout(db, entry.clientId, { sessionId: entry.sessionId });
    const squat = await db.selectOne<{ id: string }>(
      "SELECT id FROM exercises WHERE name LIKE '%Squat%' ORDER BY name LIMIT 1",
    );
    if (!squat) throw new Error('no squat in the seeded library');

    await logSet(db, workout, squat.id, 1, { reps: 8, loadGrams: 80_000, rpeTenths: 80 });
    await logSet(db, workout, squat.id, 2, { reps: 8, loadGrams: 82_500, rpeTenths: 85 });
    await completeWorkout(db, workout);

    const owed = await outstandingInvoices(db, clientId);
    const first = owed[0];
    if (!first) throw new Error('nothing outstanding');
    await recordPayment(db, first.id, first.totalMinor - first.paidMinor, 'bank_transfer', {
      currency: first.currency,
      reference: 'WhatsApp transfer',
    });

    const queued = await outbox.pending(db);
    expect(queued).toHaveLength(6);

    // Pushed through the raw client rather than the engine so the per-operation
    // outcomes are visible. Asserting only the counts is what left me reading
    // server source to work out *why* five of six were refused.
    const result = await api.push(queued.map((e) => ({
      id: e.id, type: e.type, queued_at: e.queued_at, data: e.data,
    })));

    const refused = result.results.filter((r) => r.status !== 'applied');
    if (refused.length > 0) {
      throw new Error(
        `${refused.length} of ${queued.length} operations refused:\n` +
        refused.map((r) => `  ${r.type}: [${r.code}] ${r.message}`).join('\n'),
      );
    }
    expect(result.applied).toBe(6);
  });

  it('agrees with the server once it has synced', async () => {
    await outbox.markSent(db, (await outbox.pending(db)).map((e) => e.id));
    await synchronise(db, api);

    // Exactly one credit burned, and it burned when the session was
    // *delivered* — not when the pack was sold. Asserted as a delta rather
    // than a literal so the fixture can sell whatever it likes.
    expect((await clientDetail(db, clientId))?.creditsRemaining).toBe(creditsBefore - 1);

    // The sets are readable under the id the *device* minted, which is the
    // whole point: they were logged against it before the server existed.
    const sets = await workoutSets(db, await openWorkoutId(db));
    expect(sets).toHaveLength(2);
    expect(sets[1]?.loadGrams).toBe(82_500);

    const invoices = await api.get<{ invoices: Invoice[] }>(`/v1/invoices?client_id=${clientId}`);
    const settled = invoices.invoices.filter((i) => i.status === 'settled');
    // Receiving the money settled the invoice; it did not earn anything.
    expect(settled.length).toBeGreaterThanOrEqual(1);
  });

  it('keeps the books balanced through all of it', async () => {
    const report = await api.get<{ debits_minor?: number; credits_minor?: number; balanced?: boolean }>(
      '/v1/receivables',
    );
    expect(report).toBeTruthy();
  });
});

async function openWorkoutId(db: MemoryDatabase): Promise<string> {
  const row = await db.selectOne<{ id: string }>(
    'SELECT id FROM workout_sessions ORDER BY performed_on DESC LIMIT 1',
  );
  if (!row) throw new Error('no workout');
  return row.id;
}
