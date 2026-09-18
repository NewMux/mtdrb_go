import { MemoryDatabase } from './support';
import * as outbox from '@/sync/outbox';
import { newId } from '@/lib/id';

describe('outbox', () => {
  let db: MemoryDatabase;
  beforeEach(() => { db = new MemoryDatabase(); });
  afterEach(() => { db.close(); });

  it('drains in queued order, not insertion order', async () => {
    // The server applies a batch in queued order, so the client must agree
    // about what the trainer did first.
    const later = new Date('2026-05-01T10:00:00Z');
    const earlier = new Date('2026-05-01T09:00:00Z');

    await outbox.enqueue(db, newId(), 'attendance.mark', { n: 'second' }, later);
    await outbox.enqueue(db, newId(), 'attendance.mark', { n: 'first' }, earlier);

    const pending = await outbox.pending(db);
    expect(pending.map((e) => e.data.n)).toEqual(['first', 'second']);
  });

  it('removes operations the server accepted', async () => {
    const keep = newId();
    const drop = newId();
    await outbox.enqueue(db, drop, 'client.create', {});
    await outbox.enqueue(db, keep, 'client.create', {});

    await outbox.markSent(db, [drop]);

    const pending = await outbox.pending(db);
    expect(pending).toHaveLength(1);
    expect(pending[0]?.id).toBe(keep);
  });

  it('parks a refused operation so the queue cannot jam behind it', async () => {
    // Left pending, an operation the server will always refuse would block
    // every later one forever and the trainer would never find out.
    const refused = newId();
    await outbox.enqueue(db, refused, 'attendance.mark', {});
    await outbox.markFailed(db, refused, 'conflict', 'insufficient_credits', 'no credits left');

    expect(await outbox.pending(db)).toHaveLength(0);

    const attention = await outbox.needsAttention(db);
    expect(attention).toHaveLength(1);
    expect(attention[0]?.error_code).toBe('insufficient_credits');
    expect(attention[0]?.error_message).toBe('no credits left');
  });

  it('keeps a network failure pending', async () => {
    // Being offline says nothing about whether the operation is valid.
    const id = newId();
    await outbox.enqueue(db, id, 'workout.log_set', {});
    await outbox.recordAttempt(db, [id]);

    const pending = await outbox.pending(db);
    expect(pending).toHaveLength(1);
    expect(pending[0]?.attempts).toBe(1);
  });

  it('re-queueing the same id replaces rather than duplicates', async () => {
    const id = newId();
    await outbox.enqueue(db, id, 'workout.log_set', { load_grams: 100000 });
    await outbox.enqueue(db, id, 'workout.log_set', { load_grams: 105000 });

    const pending = await outbox.pending(db);
    expect(pending).toHaveLength(1);
    expect(pending[0]?.data.load_grams).toBe(105000);
  });

  it('counts pending and failed separately for the sync indicator', async () => {
    const failed = newId();
    await outbox.enqueue(db, newId(), 'client.create', {});
    await outbox.enqueue(db, failed, 'attendance.mark', {});
    await outbox.markFailed(db, failed, 'rejected', 'validation_failed', 'bad');

    expect(await outbox.counts(db)).toEqual({ pending: 1, failed: 1 });
  });
});
