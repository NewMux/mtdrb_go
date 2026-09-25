/**
 * The demo replay: the date shift, the recorded server, and a device seeded
 * from the real recording through the real sync engine.
 */

import { ApiError, DEMO_READ_ONLY } from '@/api/client';
import { DemoApi, replay, shiftDeep, shiftFor, type Recording } from '@/demo/replay';
import { lowBalanceClients, outstandingInvoices, todaysRoster } from '@/features/queries';
import { MemoryDatabase } from './support';

// eslint-disable-next-line @typescript-eslint/no-var-requires
const recording = require('@/demo/fixtures/recording.json') as Recording;

/** Wednesday 17 June 2026, 09:30 in Dubai. */
const recorded = { recorded_at: '2026-06-17T05:30:00Z', utc_offset_minutes: 240 };
/** A viewer's midday, in whatever zone the tests run in. */
const viewerNow = new Date(2026, 8, 23, 12, 0, 0);

describe('shiftFor', () => {
  it("makes the recorded day the viewer's today, at the studio's wall-clock time", () => {
    const shift = shiftFor(recorded, viewerNow);
    const moved = new Date(shiftDeep(recorded.recorded_at, shift));
    expect([moved.getFullYear(), moved.getMonth(), moved.getDate()]).toEqual([2026, 8, 23]);
    expect([moved.getHours(), moved.getMinutes()]).toEqual([9, 30]);
  });

  it('moves by whole days', () => {
    expect(shiftFor(recorded, viewerNow).days).toBe(98);
    expect(shiftFor(recorded, new Date(2026, 5, 17, 23, 0)).days).toBe(0);
  });
});

describe('shiftDeep', () => {
  const shift = { days: 3, ms: 3 * 86_400_000 + 3_600_000 };

  it('shifts dates by days and instants by the full shift', () => {
    expect(shiftDeep('2026-06-30', shift)).toBe('2026-07-03');
    expect(shiftDeep('2026-06-17T05:30:00+00:00', shift)).toBe('2026-06-20T06:30:00.000Z');
  });

  it('treats a midnight-UTC instant as the date it encodes', () => {
    expect(shiftDeep('2026-06-02T00:00:00Z', shift)).toBe('2026-06-05T00:00:00.000Z');
  });

  it('leaves everything else alone, at any depth', () => {
    const value = {
      number: 'INV-2026-0018',
      id: '01a0cde8-27c5-78b1-ba78-2f781e75b7b8',
      total_minor: 350000,
      nested: [{ due_date: '2026-06-22', label: '1-30 days', paid: null }],
    };
    expect(shiftDeep(value, shift)).toEqual({ ...value, nested: [{ due_date: '2026-06-25', label: '1-30 days', paid: null }] });
  });
});

describe('DemoApi', () => {
  const api = new DemoApi(recording, () => viewerNow);

  it('serves the whole recording on the first pull, then nothing', async () => {
    const first = await api.pull('', 500);
    expect(first.has_more).toBe(false);
    expect(first.changes.map((c) => c.collection)).toContain('invoices');
    const second = await api.pull(first.cursor, 500);
    expect(second.changes).toEqual([]);
  });

  it('serves recorded reads, shifted', async () => {
    const summary = await api.dashboard();
    expect(summary.currency).toBe('AED');
    expect(summary.sessions_today).toBeGreaterThan(0);
  });

  it('refuses every write', async () => {
    await expect(api.post('/v1/invoices', {})).rejects.toMatchObject({ code: DEMO_READ_ONLY });
  });

  it('says so when a read was never recorded', async () => {
    await expect(api.get('/v1/nowhere')).rejects.toBeInstanceOf(ApiError);
  });
});

describe('a device seeded from the recording', () => {
  let db: MemoryDatabase;
  beforeAll(async () => {
    db = new MemoryDatabase();
    await replay(db, new DemoApi(recording, () => viewerNow));
  });

  it("has the recorded day's roster today, one session already done", async () => {
    const roster = await todaysRoster(db, viewerNow);
    const summary = recording.reads['/v1/dashboard'] as { sessions_today: number; sessions_today_unmarked: number };
    expect(roster).toHaveLength(summary.sessions_today);
    expect(roster.filter((r) => r.status === 'scheduled')).toHaveLength(summary.sessions_today_unmarked);
    expect(new Date(roster[0]!.startsAt).getHours()).toBe(7);
  });

  it('agrees with the server about who is running low and who owes', async () => {
    const summary = recording.reads['/v1/dashboard'] as {
      low_balance: { client_name: string; remaining: number }[];
      low_balance_threshold: number;
      unpaid_invoices: number;
    };
    const local = await lowBalanceClients(db, summary.low_balance_threshold);
    expect(local.map((c) => c.fullName).sort()).toEqual(summary.low_balance.map((c) => c.client_name).sort());
    expect(await outstandingInvoices(db)).toHaveLength(summary.unpaid_invoices);
  });

  it('can be seeded again the next day without duplicating anything', async () => {
    const tomorrow = new Date(viewerNow.getTime() + 86_400_000);
    await replay(db, new DemoApi(recording, () => tomorrow));
    const clients = await db.selectOne<{ n: number }>('SELECT count(*) AS n FROM clients');
    expect(clients?.n).toBe(12);
    expect(await todaysRoster(db, tomorrow)).toHaveLength(4);
  });
});
