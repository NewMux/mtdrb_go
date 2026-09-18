import { MemoryDatabase, stubFetch, memoryTokens } from './support';
import { ApiClient } from '@/api/client';
import { pull, push, synchronise } from '@/sync/engine';
import * as outbox from '@/sync/outbox';
import { getMeta } from '@/db/types';
import { newId } from '@/lib/id';
import type { PullResult, PushResult } from '@/api/types';

function client(handler: Parameters<typeof stubFetch>[0]) {
  const { fetch, calls } = stubFetch(handler);
  const api = new ApiClient({
    baseUrl: 'https://api.test',
    tokens: memoryTokens(),
    fetchImpl: fetch,
  });
  return { api, calls };
}

describe('sync engine — pull', () => {
  let db: MemoryDatabase;
  beforeEach(() => { db = new MemoryDatabase(); });
  afterEach(() => { db.close(); });

  it('writes pulled rows into the local mirror and stores the cursor', async () => {
    const page: PullResult = {
      cursor: 'cursor-1',
      has_more: false,
      server_time: '2026-05-01T09:00:00Z',
      changes: [
        {
          collection: 'clients',
          rows: [
            { id: 'c1', full_name: 'Dana Rivers', status: 'active', notes: '' },
            { id: 'c2', full_name: 'Morgan Hale', status: 'paused', notes: '' },
          ],
        },
      ],
    };
    const { api } = client(() => ({ status: 200, body: page }));

    const pulled = await pull(db, api);
    expect(pulled).toBe(2);

    const rows = await db.select<{ full_name: string }>('SELECT full_name FROM clients ORDER BY full_name');
    expect(rows.map((r) => r.full_name)).toEqual(['Dana Rivers', 'Morgan Hale']);
    expect(await getMeta(db, 'sync.cursor')).toBe('cursor-1');
  });

  it('sends the stored cursor on the next pull', async () => {
    let seen: string[] = [];
    const { api } = client((url) => {
      seen.push(url);
      return {
        status: 200,
        body: { cursor: 'cursor-2', has_more: false, server_time: '', changes: [] } as PullResult,
      };
    });

    await pull(db, api);
    await pull(db, api);

    expect(seen[0]).not.toContain('cursor=');
    expect(seen[1]).toContain('cursor=cursor-2');
  });

  it('follows has_more rather than waiting for the next interval', async () => {
    let page = 0;
    const { api } = client(() => {
      page++;
      return {
        status: 200,
        body: {
          cursor: `cursor-${page}`,
          has_more: page < 3,
          server_time: '',
          changes: [{ collection: 'clients', rows: [{ id: `c${page}`, full_name: `Client ${page}` }] }],
        } as PullResult,
      };
    });

    const pulled = await pull(db, api);
    expect(pulled).toBe(3);
    expect(page).toBe(3);
  });

  it('updates an existing row rather than duplicating it', async () => {
    const rows = [{ id: 'c1', full_name: 'Dana Rivers', status: 'active' }];
    let call = 0;
    const { api } = client(() => {
      call++;
      return {
        status: 200,
        body: {
          cursor: `cursor-${call}`,
          has_more: false,
          server_time: '',
          changes: [{
            collection: 'clients',
            rows: call === 1 ? rows : [{ id: 'c1', full_name: 'Dana R. Rivers', status: 'active' }],
          }],
        } as PullResult,
      };
    });

    await pull(db, api);
    await pull(db, api);

    const all = await db.select<{ full_name: string }>('SELECT full_name FROM clients');
    expect(all).toHaveLength(1);
    expect(all[0]?.full_name).toBe('Dana R. Rivers');
  });

  it('ignores a collection it does not know about', async () => {
    // An older app talking to a newer server must keep working rather than
    // refusing to sync.
    const { api } = client(() => ({
      status: 200,
      body: {
        cursor: 'c', has_more: false, server_time: '',
        changes: [
          { collection: 'something_new', rows: [{ id: 'x' }] },
          { collection: 'clients', rows: [{ id: 'c1', full_name: 'Dana' }] },
        ],
      } as PullResult,
    }));

    await expect(pull(db, api)).resolves.toBe(1);
    expect(await db.select('SELECT id FROM clients')).toHaveLength(1);
  });

  it('drops a column the local schema does not have', async () => {
    const { api } = client(() => ({
      status: 200,
      body: {
        cursor: 'c', has_more: false, server_time: '',
        changes: [{
          collection: 'clients',
          rows: [{ id: 'c1', full_name: 'Dana', a_field_from_the_future: 'x' }],
        }],
      } as PullResult,
    }));

    await expect(pull(db, api)).resolves.toBe(1);
    const row = await db.selectOne<{ full_name: string }>('SELECT full_name FROM clients');
    expect(row?.full_name).toBe('Dana');
  });

  it('serialises objects and booleans SQLite cannot store directly', async () => {
    const { api } = client(() => ({
      status: 200,
      body: {
        cursor: 'c', has_more: false, server_time: '',
        changes: [{
          collection: 'biometric_entries',
          rows: [{
            id: 'b1', client_id: 'c1', measured_on: '2026-05-01',
            weight_grams: 72400, circumferences: { waist: 810 },
          }],
        }],
      } as PullResult,
    }));

    await pull(db, api);
    const row = await db.selectOne<{ circumferences: string; weight_grams: number }>(
      'SELECT circumferences, weight_grams FROM biometric_entries',
    );
    expect(JSON.parse(row!.circumferences)).toEqual({ waist: 810 });
    expect(row!.weight_grams).toBe(72400);
  });
});

describe('sync engine — push', () => {
  let db: MemoryDatabase;
  beforeEach(() => { db = new MemoryDatabase(); });
  afterEach(() => { db.close(); });

  it('clears operations the server applied', async () => {
    const a = newId();
    const b = newId();
    await outbox.enqueue(db, a, 'client.create', { full_name: 'Dana' });
    await outbox.enqueue(db, b, 'client.create', { full_name: 'Morgan' });

    const { api } = client((_url, init) => {
      const sent = JSON.parse(String(init?.body)) as { operations: { id: string }[] };
      return {
        status: 200,
        body: {
          applied: sent.operations.length, conflicts: 0, rejected: 0, cursor: 'c',
          results: sent.operations.map((o) => ({ id: o.id, type: 'client.create', status: 'applied' })),
        } as PushResult,
      };
    });

    const result = await push(db, api);
    expect(result.pushed).toBe(2);
    expect(await outbox.pending(db)).toHaveLength(0);
  });

  it('parks a conflict with its reason instead of retrying it forever', async () => {
    const id = newId();
    await outbox.enqueue(db, id, 'attendance.mark', { attendee_id: 'a1', status: 'completed' });

    const { api } = client(() => ({
      status: 200,
      body: {
        applied: 0, conflicts: 1, rejected: 0, cursor: 'c',
        results: [{
          id, type: 'attendance.mark', status: 'conflict',
          code: 'insufficient_credits',
          message: 'this client has 0 credits remaining but 1 are needed',
        }],
      } as PushResult,
    }));

    const result = await push(db, api);
    expect(result.conflicts).toBe(1);
    expect(await outbox.pending(db)).toHaveLength(0);

    const attention = await outbox.needsAttention(db);
    expect(attention[0]?.error_code).toBe('insufficient_credits');
    // The trainer needs to read this, so it must survive the round trip.
    expect(attention[0]?.error_message).toContain('0 credits remaining');
  });

  it('keeps everything pending when the device is offline', async () => {
    const id = newId();
    await outbox.enqueue(db, id, 'workout.log_set', {});

    const { fetch } = stubFetch(() => { throw new Error('no signal'); });
    const api = new ApiClient({ baseUrl: 'https://api.test', tokens: memoryTokens(), fetchImpl: fetch });

    await expect(push(db, api)).rejects.toThrow();
    const pending = await outbox.pending(db);
    expect(pending).toHaveLength(1);
    expect(pending[0]?.attempts).toBe(1);
  });

  it('sends operations in queued order', async () => {
    const first = newId();
    const second = newId();
    await outbox.enqueue(db, second, 'attendance.mark', { n: 2 }, new Date('2026-05-01T11:00:00Z'));
    await outbox.enqueue(db, first, 'attendance.mark', { n: 1 }, new Date('2026-05-01T10:00:00Z'));

    let sentOrder: number[] = [];
    const { api } = client((_url, init) => {
      const sent = JSON.parse(String(init?.body)) as { operations: { id: string; data: { n: number } }[] };
      sentOrder = sent.operations.map((o) => o.data.n);
      return {
        status: 200,
        body: {
          applied: sent.operations.length, conflicts: 0, rejected: 0, cursor: 'c',
          results: sent.operations.map((o) => ({ id: o.id, type: 'attendance.mark', status: 'applied' })),
        } as PushResult,
      };
    });

    await push(db, api);
    expect(sentOrder).toEqual([1, 2]);
  });
});

describe('synchronise', () => {
  let db: MemoryDatabase;
  beforeEach(() => { db = new MemoryDatabase(); });
  afterEach(() => { db.close(); });

  it('pulls before pushing, so fresh writes are not overwritten by stale rows', async () => {
    const order: string[] = [];
    const { api } = client((url) => {
      if (url.includes('/sync/pull')) {
        order.push('pull');
        return { status: 200, body: { cursor: 'c', has_more: false, server_time: '', changes: [] } as PullResult };
      }
      order.push('push');
      return { status: 200, body: { applied: 0, conflicts: 0, rejected: 0, cursor: 'c', results: [] } as PushResult };
    });

    await outbox.enqueue(db, newId(), 'client.create', {});
    await synchronise(db, api);

    expect(order).toEqual(['pull', 'push']);
  });

  it('reports being offline as a fact, not an error', async () => {
    // The app is built for basements. Being offline is the normal case and
    // must not surface as a failure.
    const { fetch } = stubFetch(() => { throw new Error('no signal'); });
    const api = new ApiClient({ baseUrl: 'https://api.test', tokens: memoryTokens(), fetchImpl: fetch });

    const report = await synchronise(db, api);
    expect(report.offline).toBe(true);
    expect(report.error).toBeUndefined();
  });
});
