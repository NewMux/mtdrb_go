import { MemoryDatabase } from './support';
import { expiryFor, listLocations, listOffers } from '@/features/catalog';

describe('the local catalogue', () => {
  let db: MemoryDatabase;
  beforeEach(async () => {
    db = new MemoryDatabase();
    await db.execute(`INSERT INTO locations (id, name, kind, is_primary, archived_at) VALUES
      ('l1', 'Kite Beach', 'outdoor', 0, NULL),
      ('l2', 'Studio', 'studio', 1, NULL),
      ('l3', 'Old Gym', 'gym', 0, '2026-01-01T00:00:00Z')`);
    await db.execute(`INSERT INTO session_types (id, name, duration_minutes, capacity, credit_cost) VALUES ('t', '1-on-1', 60, 1, 1)`);
    await db.execute(`INSERT INTO sessions (id, session_type_id, starts_at, ends_at, status, location, location_id) VALUES
      ('s1', 't', '2099-01-01T06:00:00.000Z', '2099-01-01T07:00:00.000Z', 'scheduled', 'Studio', 'l2'),
      ('s2', 't', '2000-01-01T06:00:00.000Z', '2000-01-01T07:00:00.000Z', 'scheduled', 'Studio', 'l2')`);
    await db.execute(`INSERT INTO package_offers (id, name, kind, credits, price_minor, currency, validity_days, sort_order, archived_at) VALUES
      ('o1', '20 sessions', 'session_pack', 20, 640000, 'AED', 120, 0, NULL),
      ('o2', '10 sessions', 'session_pack', 10, 350000, 'AED', 90, 0, NULL),
      ('o3', 'Old pack', 'session_pack', 5, 100000, 'AED', NULL, 0, '2026-01-01T00:00:00Z')`);
  });
  afterEach(() => db.close());

  it('lists places primary first, with the sessions still ahead of each', async () => {
    const list = await listLocations(db);
    expect(list.map((l) => l.name)).toEqual(['Studio', 'Kite Beach']);
    expect(list[0]).toMatchObject({ isPrimary: true, upcoming: 1, archived: false });
    expect(await listLocations(db, true)).toHaveLength(3);
  });

  it('lists the price list cheapest first, archived offers hidden', async () => {
    expect((await listOffers(db)).map((o) => o.name)).toEqual(['10 sessions', '20 sessions']);
    expect((await listOffers(db, true)).at(-1)?.archived).toBe(true);
  });

  it('dates an offer’s expiry from the day it is sold', () => {
    expect(expiryFor({ validityDays: 90 }, new Date(2026, 5, 17))).toBe('2026-09-15');
    expect(expiryFor({ validityDays: null })).toBeNull();
  });
});
